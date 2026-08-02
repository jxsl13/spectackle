// Package workspace implements root detection and the .spectackle folder
// contract: every file the server writes lives inside a .spectackle/ folder
// (root or nested context dirs); the rest of the workspace is never touched.
//
// Root detection order: walk up from the start dir for .spectackle/config.yaml
// (the root marker — nested context dirs also have .spectackle/ folders, so the
// folder alone is ambiguous), then for .git (directory or file — a linked git
// worktree's .git is a file, and it terminates the walk exactly like a real
// checkout's .git directory does), then fall back to the -root flag.
package workspace

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jxsl13/spectackle/internal/ignore"
	"github.com/jxsl13/spectackle/internal/migrate"
	"github.com/jxsl13/spectackle/internal/wt"
)

// Dot is the folder name every server write is confined to.
const Dot = ".spectackle"

// IsRecordsPath reports whether a repo-relative path lies inside ANY context
// dir's records folder — the root's `.spectackle/` or a nested one such as
// `internal/mcpserver/.spectackle/`. It exists because three call sites had
// each hand-written this test and two of them anchored it at the repo root:
//
//	f == Dot || strings.HasPrefix(f, Dot+"/")
//
// which silently excludes every non-root context. Since a records write is the
// server's own unavoidable side effect, a gate that fails to recognize it
// blames the caller for it — and the scope gate did exactly that, refusing an
// item's archive because the server had just re-scoped that item into a nested
// context and written its own block there. No transition could clear it, and
// the more precisely an item scoped itself to one subtree the likelier the
// deadlock became (B-01KYSDBZTEF1A).
//
// The test is per SEGMENT, not a substring: a file named ".spectacklefoo" is
// ordinary work, which the older HasPrefix spelling would have swallowed.
//
// It is also per NAME, not per directory (B-01KYSK7HQFFPM). The first version
// of this predicate exempted anything under a `.spectackle` segment, which
// made the folder a smuggling surface at every depth: a .go file or a shell
// script dropped into one passed the scope gate and was then committed under
// the server's own records subject. Four such paths were measured passing at
// exit 0. The Go toolchain ignores dot-directories, so nothing BUILT from
// there — but that is a language-specific accident the predicate never
// stated, and a script or asset under a records dir is perfectly reachable in
// any other language. So the exemption now lists the names the server
// actually writes and refuses everything else.
//
// The anchor is the FIRST `.spectackle` segment, and that choice is load
// bearing: a path can contain two (the retained migration backup holds whole
// copies of nested bundles), and the two readings disagree. Last-match would
// exempt `.spectackle/anything/.spectackle/work.md`, re-opening the smuggling
// hole one directory deeper; first-match keeps the outermost records folder
// in charge of everything below it.
//
// Below that anchor three shapes are records:
//
//   - the folder itself;
//   - a gitignored-or-server-owned SUBTREE — `cache/`, `wt/`, or a retained
//     `migrate-backup-*` — exempt wholesale, since their contents are the
//     server's own scratch and are not enumerable;
//   - a direct CHILD file whose basename the server writes (the bundle files,
//     the scaffolded git dotfiles, or one of the two temp spellings).
//
// Anything else under a records folder is ordinary work and must face the
// scope gate like any other file.
func IsRecordsPath(rel string) bool {
	segs := strings.Split(filepath.ToSlash(rel), "/")
	i := -1
	for j, seg := range segs {
		if seg == Dot {
			i = j
			break
		}
	}
	if i < 0 {
		return false
	}
	if i == len(segs)-1 {
		return true // the records folder itself
	}
	child := segs[i+1]
	// Whole subtrees, and ONLY under the ROOT records folder (i == 0).
	//
	// All three are root-only by construction: CacheDir and the worktree path
	// are filepath.Join(r.Dir, Dot, …) with no context segment, and migrate
	// writes its backup to filepath.Join(dir, Dot, backup). A nested records
	// folder never legitimately contains any of them.
	//
	// Root-anchoring is the fix for a measured hole, not tidiness. Anchored
	// only on the NAME, `internal/x/.spectackle/wt/evil.sh` and
	// `internal/x/.spectackle/migrate-backup-v9/evil.go` were both measured
	// reaching wt.DirtyFiles and passing the gate at exit 0 — the attacker
	// cost is naming a directory `wt` inside any nested records folder. At the
	// root the scaffolded .gitignore hides such a directory, but EnsureScaffold
	// writes no .gitignore for a nested context (see ctx != "" below), so
	// nested folders had nothing masking it.
	//
	// The exemption is still required at the root, and the ignore line alone
	// is not enough there: a workspace scaffolded by an older build has a
	// .gitignore without it, exactly the way pre-wt/ workspaces did, and its
	// RETAINED migration backup would then reach the scope gate as undeclared
	// work that NO transition could clear — the gate is a pre-Move guard while
	// every records commit that could absorb the backup runs downstream of it.
	// That deadlock is what this predicate exists to prevent, and it was
	// measured against a real post-migration tree.
	if i == 0 && (child == "cache" || child == "wt" || migrateBackupRe.MatchString(child)) {
		return true
	}
	if i != len(segs)-2 {
		return false // a deeper file under some other subdirectory
	}
	return isRecordsFileName(child)
}

// migrateBackupPrefix duplicates internal/migrate's unexported backupPrefix
// (migrate.go:84). It is spelled out rather than imported because the backup
// is RETAINED after a successful migration and is not removed by anything, so
// this package has to recognize it from the outside; a change there without a
// change here is caught by TestIsRecordsPathCoversRetainedMigrationBackup.
const migrateBackupPrefix = "migrate-backup-"

// migrateBackupRe matches the retained backup DIRECTORY, and it is anchored
// on the version suffix rather than the bare prefix on purpose. migrate names
// it backupPrefix+From (migrate.go:199) where From is a "v<n>" stamp, so
// `migrate-backup-v0` is the only shape any release produces and `v[0-9]+`
// covers every future one.
//
// A bare HasPrefix test was measured to exempt `migrate-backup-` itself — an
// attacker-chosen SUBTREE whose every file the gate would then wave through.
// The scaffolded .gitignore hides such a directory today, but not in the
// pre-line workspaces this exemption exists for, which is precisely where it
// would have mattered.
var migrateBackupRe = regexp.MustCompile(`^` + regexp.QuoteMeta(migrateBackupPrefix) + `v[0-9]+$`)

// recordsTempRe matches the two temp-file spellings the server can leave
// inside a records folder: journal.Rewrite's `os.CreateTemp(dot, "journal-*")`
// (journal.go:219) and migrate's `os.CreateTemp(dir, "."+base+".tmp*")`
// (migrate.go:744). Both are renamed away on success, but a crash between
// create and rename strands one, and a stranded temp file must not deadlock
// the scope gate the way the migration backup did.
//
// Both branches are as NARROW as their producers, because this is an exemption
// from the scope gate and every character of slack in it is a file an author
// names freely. os.CreateTemp always substitutes decimal digits for its `*`,
// so both counts are `+` rather than `*`; and migrate's temp basename is
// always a bundle file, so the middle is that same closed set rather than
// `[^/]+`. The wider form was measured waving `.spectackle/.evil.tmp`,
// `.spectackle/.payload.sh.tmp` and `internal/x/.spectackle/.attack.sh.tmp9`
// straight through the gate and into the records commit.
var recordsTempRe = regexp.MustCompile(
	`^(?:journal-[0-9]+|\.(?:` + recordsBundleAlt + `)\.tmp[0-9]+)$`)

// recordsBundleAlt is the bundle basenames as a regexp alternation, built from
// the same list isRecordsFileName switches on so the two cannot drift.
var recordsBundleAlt = func() string {
	quoted := make([]string, 0, len(recordsBundleNames))
	for _, n := range recordsBundleNames {
		quoted = append(quoted, regexp.QuoteMeta(n))
	}
	return strings.Join(quoted, "|")
}()

// recordsBundleNames are the files the server writes directly inside a records
// folder. Exhaustive by construction: the bundle files (item.go, spec/author.go,
// journal.go, drift.go), the two dotfiles EnsureScaffold maintains, and
// config.yaml. `git ls-files` over this repository's own tracked records
// returns exactly this set.
var recordsBundleNames = []string{
	"spec.md", "work.md", "journal.ndjson", "bench.ndjson", "anchors.tsv",
	"config.yaml", ".gitignore", ".gitattributes",
}

// isRecordsFileName reports whether a basename directly inside a records
// folder is one the server writes: a bundle file, or one of the two temp
// spellings a crash can strand (see recordsTempRe).
func isRecordsFileName(name string) bool {
	for _, n := range recordsBundleNames {
		if name == n {
			return true
		}
	}
	return recordsTempRe.MatchString(name)
}

// SchemaStamp is injected into every server-written file's frontmatter.
// It marks the file format of a pre-1.0 codebase: the format may break at any
// time and the stamp changes with it.
//
// An unknown stamp is still a tool error ("regenerate"), and caches still
// simply rebuild — but "there is no migration" no longer holds unconditionally:
// the immediately-preceding stamp is migrated in place on open (see
// internal/migrate, and the hook in load below). That exception exists because
// ADR-0013 changed the shape of every record ID, which reaches every workspace
// already in users' hands; without a path forward the only honest advice would
// have been to regenerate and lose the history.
//
// Its value is the one internal/migrate produces (migrate.To), and that is
// asserted by a test rather than left to whoever edits this line next.
const SchemaStamp = "v1"

// Config is .spectackle/config.yaml (root only).
type Config struct {
	Schema        string      `yaml:"schema"`
	Langs         []string    `yaml:"langs"`
	Ignore        []string    `yaml:"ignore"`       // glob patterns, repo-relative slash paths (see SkipDir)
	IgnoreRegex   []string    `yaml:"ignore_regex"` // RE2 patterns, matched against repo-relative slash paths
	BudgetDefault int         `yaml:"budget_default"`
	Compact       CompactCfg  `yaml:"compact"`
	Benchmarks    BenchCfg    `yaml:"benchmarks"`
	Verify        []string    `yaml:"verify"` // shell commands gating work-submit (e.g. "make test")
	Swarm         SwarmCfg    `yaml:"swarm"`
	WorktreesDir  string      `yaml:"worktrees_dir"` // override for .spectackle/wt (abs or root-relative)
	Feedback      FeedbackCfg `yaml:"feedback"`
	// CoverageGate: "package" makes check COUNT uncovered packages as
	// findings (CI red until backfilled) — explicit opt-in only. Top-level
	// rather than a FeedbackCfg sibling: it gates check's repository
	// hygiene, not the review loop's rounds (T-01KYD87ZN).
	CoverageGate string `yaml:"coverage_gate"`
	Git          GitCfg `yaml:"git"`
}

// SwarmCfg tunes multi-agent coordination.
type SwarmCfg struct {
	LeaseTTL int `yaml:"lease_ttl"` // seconds a scope lease lives without refresh
	AgentTTL int `yaml:"agent_ttl"` // seconds without heartbeat before an agent counts as gone
	// PanelMax CAPS a per-item multi-agent review panel (grill op=verdict
	// panel=n, legal only on live risk signals). Config can cap a panel,
	// never raise one that was not item-justified (P-01KYES kill list).
	PanelMax int `yaml:"panel_max"`
}

// FeedbackCfg tunes the SDD orchestration v2 feedback loop (see
// internal/lifecycle: Move's done->active reopen counter and Escalate).
type FeedbackCfg struct {
	MaxRounds int    `yaml:"max_rounds"` // reopen attempts before an item escalates to blocked
	Grill     string `yaml:"grill"`      // "require" hard-gates approval on a passing review verdict; else warn
	Validate  string `yaml:"validate"`   // "require" hard-gates archive on a passing validation verdict; else warn (T-01KYD94M3)
	// Risk-gated require (T-01KYFXDCH): when Validate is NOT "require",
	// the archive gate still demands a verdict for items whose LANDED diff
	// (never declared targets — gameable, T-0135 landed 15 files against 4
	// declared) trips either input below. An explicit require is never
	// downgraded by these.
	RiskFiles      int      `yaml:"risk_files"`      // landed-file count that trips require; 0 = default 8
	DangerousPaths []string `yaml:"dangerous_paths"` // repo-relative globs ("dir/**" prefix or path.Match); default EMPTY — this repo's vocabulary is wrong for yours
}

// GitCfg tunes the git integration between task worktrees and the shared
// remote (the primitives it configures live in internal/wt). Git stays
// enabled by default, but the MODE defaults to OFFLINE (GIT-DEFAULT-001,
// user decision 2026-07-27): an absent mode key means commit-only lifecycle
// edges on the current branch — no branches, no PRs, no pushes. Online
// operation is the explicit opt-in `git: mode: online`. This flips the
// original opt-out contract (zero value used to mean enabled-and-online);
// repositories that relied on the implicit online default must add the key.
// See IsEnabled for the one field (Enabled) where a plain bool can't express
// omitted-vs-false — Mode and Remote get their defaults the same zero-block
// way every other Cfg struct in this file does.
type GitCfg struct {
	Enabled *bool  `yaml:"enabled"` // nil (key omitted) means true; see IsEnabled — a plain bool can't tell "omitted" from "explicitly false"
	Mode    string `yaml:"mode"`    // "offline" (default, GIT-DEFAULT-001: commit-only edges on the current branch) or "online" (explicit opt-in: branches, PRs, pushes to Remote); an unknown value is rejected at load, see load()
	Remote  string `yaml:"remote"`  // remote name pushed to in online mode; default "origin"
	Base    string `yaml:"base"`    // branch task branches are pushed against; default is the repository's OWN default branch, read at load (see wt.DefaultBranch) — never hardcoded "main"
	// Commits selects the edge-commit engine (T-01KYD94MG): "edges"
	// (default) commits every .spectackle-writing tool call with a
	// structured decision message derived from its journal events; "off"
	// produces zero commits and unchanged tool output (the validate
	// attribution fix that excludes spectackle( records subjects is
	// knob-independent and intended). A validator
	// argued the journal already carries eid/ag and the feature is
	// redundant; the requirement is explicit that the decision trail must
	// be readable in git log by humans, so the default stays edges and the
	// dissent is recorded on the task.
	Commits string `yaml:"commits"`

	// AwaitChecks is how many SECONDS the archive closure waits for the
	// head's CI verdict before refusing whole. It was a hardcoded 5 minutes,
	// which sat inside this repository's own CI duration distribution
	// (measured 3m43s-5m38s across ten consecutive runs) — so archives
	// refused on builds that were merely unfinished, and because a refusal
	// compensates by writing a journal event, the retry pushed a new commit
	// that started CI over. The wait could never converge
	// (B-01KYQJDJJVFC2).
	//
	// A knob rather than a bigger constant: the right value is a property of
	// the repository's CI, not of this program, and the code's own comment
	// already framed the budget as bounding damage rather than as a
	// correctness boundary. 0 or omitted means the default; see AwaitBudget.
	AwaitChecks int `yaml:"await_checks"`
}

// AwaitBudget is how long the archive closure waits for CI, as a duration.
// The default is deliberately ABOVE the slowest CI run observed in this
// repository rather than near its median: a wait that expires on a green
// build costs a retry, and a retry restarts CI, so the failure is not
// symmetric with waiting slightly too long.
func (g GitCfg) AwaitBudget() time.Duration {
	if g.AwaitChecks > 0 {
		return time.Duration(g.AwaitChecks) * time.Second
	}
	return 12 * time.Minute
}

// EdgeCommits reports whether the edge-commit engine is armed: empty (key
// omitted, incl. every pre-feature workspace) means edges — the default —
// and only an explicit "off" disarms.
func (g GitCfg) EdgeCommits() bool { return g.Commits != "off" }

// IsEnabled reports whether git integration is active. nil means the key was
// never in config.yaml at all — which includes every workspace scaffolded
// before this field existed — and that must resolve to true, not false,
// or an opt-out feature would ship silently opted out everywhere already
// running. An explicit `enabled: false` is always honored.
func (g GitCfg) IsEnabled() bool {
	return g.Enabled == nil || *g.Enabled
}

// GitMergeMethod is how a task branch is folded back into Config.Git.Base:
// always a merge commit. Fixed, not user-configurable — squash and rebase
// rewrite the branch's commits, and this codebase already treats a task
// branch's commit history as the record of what CommitCode actually did
// (TouchedFiles, the unpushed-commits check) rather than something a merge is
// free to rewrite.
const GitMergeMethod = "merge"

// CompactCfg holds the compact-due thresholds surfaced by `check`.
// BenchCfg tunes the benchmark record store (P-01KYJMVX2Q).
type BenchCfg struct {
	// History caps retained versions per unique benchmark key. Default 1:
	// the latest is what the codebase cares about (user requirement);
	// raise it to keep a benchmark history.
	History int `yaml:"history"`
}

type CompactCfg struct {
	JournalMax int `yaml:"journal_max"` // journal events since last compact
	DoneMax    int `yaml:"done_max"`    // done-but-unarchived items
}

func defaultConfig() Config {
	return Config{
		Schema:        SchemaStamp,
		Langs:         []string{"go"},
		Ignore:        []string{".git/**", "bin/**"},
		BudgetDefault: 2000,
		Compact:       CompactCfg{JournalMax: 300, DoneMax: 8},
		Benchmarks:    BenchCfg{History: 1},
		Swarm:         SwarmCfg{LeaseTTL: 600, AgentTTL: 900, PanelMax: 3},
		Feedback:      FeedbackCfg{MaxRounds: 3, RiskFiles: 8},
		Git:           GitCfg{Mode: "offline", Remote: "origin"}, // Base is left empty here: it needs dir to read the repo, see load()
	}
}

// Locker serializes a read-modify-write against one named, cross-process
// scope. coord.DB satisfies it (WithLock, generalized from what used to be a
// single hardcoded 'integrate' lock); this interface exists so item/spec/
// drift can call through Root.Lock without importing internal/coord — those
// packages persist bundle files, not swarm coordination, and coord.db is
// swarm coordination's package to own.
type Locker interface {
	// WithLock runs fn while holding name, releasing on every exit path
	// (including panic). Callers pass the ENTIRE read-modify-write as fn,
	// never just the final write: locking only the write leaves two readers
	// racing to read the same stale state, which is the defect this exists
	// to close (B-01KYD57FN3ERHBM5EQ3534YJXP).
	WithLock(name string, fn func() error) error
}

// Root is a detected workspace.
type Root struct {
	// Sink, when set, observes every journal event this Root appends —
	// the edge-commit engine's exact capture mechanism (T-01KYD94MG): the
	// server installs a per-call buffer here in its gate, so the commit
	// derives from precisely the events the call wrote, never a glob of
	// everything dirty.
	Sink func(journalPath string, raw []byte)

	Dir   string // absolute path
	Agent string // agent identity writing through this workspace ("" outside swarm contexts)
	Cfg   Config

	// Lock is nil outside a swarm-aware caller (tests, migrate, a one-shot
	// CLI invocation with no coord.db open) — those already have at most one
	// writer touching any given bundle file, so item/spec/drift run unlocked
	// exactly as they always have. mcpserver.New sets it to the same *coord.DB
	// it opens for lease/counter/event coordination (see coord.go), so a
	// server process wires this up once at construction and every write
	// through the resulting Root is automatically serialized against
	// siblings — no call site elsewhere has to remember to ask for it.
	Lock Locker
}

// Detect finds the workspace root starting at start (usually the cwd).
func Detect(start, flagRoot string) (Root, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return Root{}, err
	}
	if d, ok := walkUpToGitBoundary(abs, func(dir string) bool {
		return fileExists(filepath.Join(dir, Dot, "config.yaml"))
	}); ok {
		return load(d)
	}
	// IsNestedGitBoundary stats .git regardless of file-vs-directory: a linked
	// git worktree has a .git FILE (a "gitdir: ..." pointer), not a directory.
	// Walking past it used to land Detect on the enclosing main checkout
	// instead — harmless for main-repo resolution below (git rev-parse finds
	// the real common dir independently), but fatal for an explicit -root
	// naming the worktree itself: the active root would silently move to a
	// different directory than the one named, and every bundle write would
	// land in the shared main checkout instead (issue 27).
	if d, ok := walkUp(abs, IsNestedGitBoundary); ok {
		return load(d)
	}
	if flagRoot != "" {
		fr, err := filepath.Abs(flagRoot)
		if err != nil {
			return Root{}, err
		}
		return load(fr)
	}
	return Root{}, fmt.Errorf("workspace: no %s/config.yaml or .git found above %s (pass -root)", Dot, abs)
}

// walkUpToGitBoundary is walkUp that refuses to ascend OUT of a git
// repository or worktree: the current directory is always tested, but once it
// turns out to be a git boundary the walk stops there rather than continuing
// into the enclosing checkout.
//
// This is the other half of GitHub issue 27, and the half a .git-file
// predicate alone does not reach. Root detection tries the
// .spectackle/config.yaml marker BEFORE the .git marker, so a worktree nested
// inside a repository that already carries a bundle used to resolve straight
// past itself to the parent's config.yaml — writing the enclosing checkout's
// bundle even when an absolute -root named the worktree, which is exactly what
// the reporter observed. A nested worktree, submodule or clone is foreign
// territory; the codebase already treats it that way in every content walk
// (see IsNestedGitBoundary and its use in SkipDir and the indexer), and
// detection has to agree with them or the two disagree about what the
// workspace is.
//
// The common case is untouched: from repo/sub/dir the walk tests dir, sub and
// then repo — repo is the boundary, but it is TESTED before the walk stops, so
// a bundle at the repository root is still found from any depth inside it.
func walkUpToGitBoundary(start string, ok func(string) bool) (string, bool) {
	d := start
	for {
		if ok(d) {
			return d, true
		}
		if IsNestedGitBoundary(d) {
			return "", false
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

func walkUp(start string, ok func(string) bool) (string, bool) {
	d := start
	for {
		if ok(d) {
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

func load(dir string) (Root, error) {
	// The schema migration hooks in here, and not in Detect, because this is
	// the one function every workspace open funnels through: Detect for the
	// server, LoadRoot for spec.Load's own root. Hooking Detect alone would
	// leave spec.Load able to observe an unmigrated cascade, and hooking
	// anything higher is impossible — the stamp check below is what refuses
	// the workspace, so the migration has to run before a Root exists at all.
	//
	// It runs to completion before this function returns a Root, so no reader
	// can see a half-migrated workspace. Needed() keeps the already-current
	// path down to a Stat and up to three small reads (see its comment): this
	// is a hot path.
	if need, err := migrate.Needed(dir); err != nil {
		return Root{}, err
	} else if need {
		if _, err := migrate.Run(dir); err != nil {
			return Root{}, fmt.Errorf("workspace: schema migration: %w", err)
		}
	}
	r := Root{Dir: dir, Cfg: defaultConfig()}
	raw, err := os.ReadFile(filepath.Join(dir, Dot, "config.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return Root{}, err
	}
	// A missing config.yaml is not an error — defaultConfig() above already
	// stands as the whole Config in that case — but the zero-block defaulting
	// and the Git.Base repo read below must still run either way: Base in
	// particular can never be baked into defaultConfig() itself (it needs
	// dir, to read the actual repo), so skipping straight to a return here
	// the way this used to would leave Base empty even in a workspace whose
	// repo has a perfectly readable default branch.
	if err == nil {
		if err := yaml.Unmarshal(raw, &r.Cfg); err != nil {
			return Root{}, fmt.Errorf("workspace: config.yaml: %w", err)
		}
		if r.Cfg.Schema != "" && r.Cfg.Schema != SchemaStamp {
			if r.Cfg.Schema == "v0" {
				return Root{}, fmt.Errorf("workspace: %s is schema %q but this build expects %q — delete that one file and re-invoke (the server regenerates it; already-migrated data is unaffected)", filepath.Join(dir, Dot, "config.yaml"), r.Cfg.Schema, SchemaStamp)
			}
			return Root{}, fmt.Errorf("workspace: %s is schema %q but this build expects %q — the workspace was written by a NEWER spectackle; upgrade the binary (deleting the file would discard its real settings)", filepath.Join(dir, Dot, "config.yaml"), r.Cfg.Schema, SchemaStamp)
		}
		for _, pat := range r.Cfg.IgnoreRegex {
			if _, err := regexp.Compile(pat); err != nil {
				return Root{}, fmt.Errorf("workspace: config.yaml: ignore_regex %q: %w", pat, err)
			}
		}
		// An unknown mode is a config error HERE, at load, matching
		// ignore_regex above — not a failure deferred to whatever internal/wt
		// call happens to run first, which would make the same typo produce a
		// different error depending on which command the user ran.
		if m := r.Cfg.Git.Mode; m != "" && m != "online" && m != "offline" {
			return Root{}, fmt.Errorf("workspace: config.yaml: git.mode %q: must be \"online\" or \"offline\"", m)
		}
		// Same principle for the edge-commit knob: `commits: disabled` or
		// `commits: false` silently ARMED the engine (every non-"off"
		// value did) — a typo'd opt-out must fail loudly at load, never
		// invert into unwanted per-call commits (cross-verification of
		// T-01KYD94MG).
		if c := r.Cfg.Git.Commits; c != "" && c != "edges" && c != "off" {
			return Root{}, fmt.Errorf("workspace: config.yaml: git.commits %q: must be \"edges\" or \"off\"", c)
		}
	}
	if r.Cfg.BudgetDefault == 0 {
		r.Cfg.BudgetDefault = 2000
	}
	if r.Cfg.Compact.JournalMax == 0 {
		r.Cfg.Compact.JournalMax = 300
	}
	if r.Cfg.Compact.DoneMax == 0 {
		r.Cfg.Compact.DoneMax = 8
	}
	if r.Cfg.Swarm.LeaseTTL == 0 {
		r.Cfg.Swarm.LeaseTTL = 600
	}
	if r.Cfg.Swarm.AgentTTL == 0 {
		r.Cfg.Swarm.AgentTTL = 900
	}
	if r.Cfg.Benchmarks.History == 0 {
		r.Cfg.Benchmarks.History = 1
	}
	if r.Cfg.Feedback.MaxRounds == 0 {
		r.Cfg.Feedback.MaxRounds = 3
	}
	if r.Cfg.Git.Mode == "" {
		r.Cfg.Git.Mode = "offline" // GIT-DEFAULT-001: online is the explicit opt-in
	}
	if r.Cfg.Git.Remote == "" {
		r.Cfg.Git.Remote = "origin"
	}
	if r.Cfg.Git.Base == "" {
		// Best-effort and silent: a workspace with no git repository at all
		// (or one with an unborn HEAD, or simply no remote configured) has no
		// default branch to read, and that must degrade quietly rather than
		// failing every config load over a field nothing downstream needs
		// yet — internal/wt's primitives are the ones that actually require
		// a repo, and they fail there, at the point of use, not here.
		if b := defaultBranchCached(dir); b != "" {
			r.Cfg.Git.Base = b
		}
	}
	return r, nil
}

// LoadRoot reads dir/.spectackle/config.yaml (if present) and returns a Root
// scoped to dir, without walking up to find the workspace root. Callers that
// already know the root — e.g. a package that only receives a bare root
// string, like spec.Load — use this to get a fully configured Root (in
// particular one whose SkipDir honors the workspace's Config.Ignore /
// IgnoreRegex) without duplicating Detect's walk-up logic.
func LoadRoot(dir string) (Root, error) {
	return load(dir)
}

// SpectackleDir maps a repo-relative context dir ("" = root) to the absolute
// path of its .spectackle folder.
func (r Root) SpectackleDir(ctx string) string {
	return filepath.Join(r.Dir, filepath.FromSlash(ctx), Dot)
}

// SpecPath / WorkPath / JournalPath locate the three bundle files of a
// context dir, repo-relative ("" = root).
func (r Root) SpecPath(ctx string) string { return filepath.Join(r.SpectackleDir(ctx), "spec.md") }
func (r Root) WorkPath(ctx string) string { return filepath.Join(r.SpectackleDir(ctx), "work.md") }

// BenchPath is the context's benchmark record store (P-01KYJMVX2Q):
// keyed last-writer-wins ndjson, union-merged like the journal.
func (r Root) BenchPath(ctx string) string {
	return filepath.Join(r.SpectackleDir(ctx), "bench.ndjson")
}
func (r Root) JournalPath(ctx string) string {
	return filepath.Join(r.SpectackleDir(ctx), "journal.ndjson")
}

// AnchorsPath is root-only (workspace-wide bindings).
func (r Root) AnchorsPath() string { return filepath.Join(r.Dir, Dot, "anchors.tsv") }

// CacheDir is root-only and excluded from git by the server-written .gitignore.
func (r Root) CacheDir() string { return filepath.Join(r.Dir, Dot, "cache") }

// CoordPath is the shared multi-agent coordination DB (main repo only).
func (r Root) CoordPath() string { return filepath.Join(r.CacheDir(), "coord.db") }

// WtDir is where agent worktrees live. NOT under cache/ — cache is
// disposable, in-flight work is not.
func (r Root) WtDir() string {
	if d := r.Cfg.WorktreesDir; d != "" {
		if filepath.IsAbs(d) {
			return d
		}
		return filepath.Join(r.Dir, d)
	}
	return filepath.Join(r.Dir, Dot, "wt")
}

// defaultSkipNames are directory basenames every workspace walk skips
// unconditionally: VCS metadata, dependency/build output, and spectackle's
// own state folder. This is the generic, harness-independent replacement for
// what used to be a hardcoded '.claude' skip — agent worktrees under
// .claude/worktrees/<name> are ordinary git linked worktrees, so they are
// now caught by IsNestedGitBoundary instead of by name.
var defaultSkipNames = map[string]bool{
	".git": true, "node_modules": true, "testdata": true,
	"bin": true, "vendor": true, Dot: true,
}

// DefaultSkipName reports whether name is one of the built-in directory
// basenames every workspace walk skips unconditionally, regardless of
// config.yaml. Exposed so packages that cannot hold a full Root (e.g.
// internal/index, which is handed a bare root string) still share the same
// built-in set as Root.SkipDir.
func DefaultSkipName(name string) bool { return defaultSkipNames[name] }

// IsNestedGitBoundary reports whether dir is itself a separate git boundary:
// dir/.git exists, as either a directory (the main clone, or a nested/vendored
// clone) or a file (a linked worktree's or a submodule's `gitdir: ...`
// pointer). Any such subdirectory belongs to a different git checkout and
// every workspace walk must skip it wholesale — this is what a hardcoded
// '.claude' skip used to approximate (agent worktrees under
// .claude/worktrees/<name> happen to be linked git worktrees), generalized to
// any harness, any location, any worktree/clone/submodule layout.
func IsNestedGitBoundary(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// matchIgnoreGlob matches a config.yaml ignore glob against a repo-relative
// slash path. "**" matches everything; a "**/" prefix matches any (including
// zero) directory depth; a trailing "/**" matches the named directory itself
// and everything below it (so "bin/**" prunes the bin/ directory, not just
// files inside it); otherwise it's a plain path.Match.
func matchIgnoreGlob(g, p string) bool {
	if g == "**" {
		return true
	}
	if rest, ok := strings.CutPrefix(g, "**/"); ok {
		for {
			if ok, _ := path.Match(rest, p); ok {
				return true
			}
			i := strings.IndexByte(p, '/')
			if i < 0 {
				return false
			}
			p = p[i+1:]
		}
	}
	if base, ok := strings.CutSuffix(g, "/**"); ok {
		if p == base || strings.HasPrefix(p, base+"/") {
			return true
		}
	}
	ok, _ := path.Match(g, p)
	return ok
}

// gitIgnoreCache memoizes one ignore.Matcher per workspace root for the
// life of the process. SkipDir runs once per directory entry in every
// workspace walk (ContextDirs, spec.Load's cascade, the swarm freshness
// walk, ...), so a git subprocess per call is unacceptable — see
// internal/ignore.New, which is the one place the subprocess actually runs.
// Root carries no matcher of its own (it's a plain value copied through
// every walk), so the cache is keyed by Dir instead of hung off the struct.
//
// The tradeoff this buys: a .gitignore edited after a root's first walk in
// this process won't be seen again until the process restarts. That is the
// same staleness window every other in-process cache here already accepts,
// and it is strictly better than the pre-issue-26 behavior it replaces.
var gitIgnoreCache sync.Map // root dir (string) -> *ignore.Matcher

// defaultBranchCache memoizes one wt.DefaultBranch lookup per directory, for
// the same reason gitIgnoreCache exists a few lines below: load() runs on
// every workspace open, spec.Load reaches it from sixteen call sites in the
// server alone, and wt.DefaultBranch spawns a git subprocess.
//
// Measured before this cache: LoadRoot cost 9.25ms per call and DefaultBranch
// alone accounted for 9.30ms of it — the subprocess WAS the cost of loading a
// workspace, paid on every call, for a field only the git automation reads.
//
// The staleness tradeoff is the one this file already accepts for gitignore: a
// repository whose default branch is renamed mid-process keeps the old value
// until restart. That is a rename of the branch a task is pushed against —
// vanishingly rare, and cheap to recover from — against a subprocess on the
// hottest path in the server.
var defaultBranchCache sync.Map // dir (string) -> string

func defaultBranchCached(dir string) string {
	if v, ok := defaultBranchCache.Load(dir); ok {
		return v.(string)
	}
	b, err := wt.DefaultBranch(dir)
	if err != nil {
		b = "" // no repo, or an unborn HEAD: degrade quietly, as load() did before
	}
	defaultBranchCache.Store(dir, b)
	return b
}

func gitIgnoreFor(root string) *ignore.Matcher {
	if v, ok := gitIgnoreCache.Load(root); ok {
		return v.(*ignore.Matcher)
	}
	m := ignore.New(root)
	actual, _ := gitIgnoreCache.LoadOrStore(root, m)
	return actual.(*ignore.Matcher)
}

// SkipDir is the single entry point every workspace walk (ContextDirs,
// spec.Load, the coverage-gap walk, and — via DefaultSkipName /
// IsNestedGitBoundary — the indexer) shares to decide whether to prune a
// directory. rel is the repo-relative, slash-separated path of the directory
// ("" for the workspace root, which is never itself pruned); name is its
// basename. True when any of these hold:
//   - rel is a nested git boundary (IsNestedGitBoundary) — never checked for
//     the root itself;
//   - name is one of the built-in defaults (DefaultSkipName);
//   - rel matches a configured Config.Ignore glob;
//   - rel matches a configured Config.IgnoreRegex pattern;
//   - rel is excluded by git itself (internal/ignore) — checked last, after
//     every user-configurable and built-in rule, so config always wins and
//     the cheap checks above run before the (memoized, but still map-lookup
//     plus climb) git-backed one.
func (r Root) SkipDir(rel, name string) bool {
	if rel != "" && IsNestedGitBoundary(filepath.Join(r.Dir, filepath.FromSlash(rel))) {
		return true
	}
	if defaultSkipNames[name] {
		return true
	}
	for _, g := range r.Cfg.Ignore {
		if matchIgnoreGlob(g, rel) {
			return true
		}
	}
	for _, pat := range r.Cfg.IgnoreRegex {
		re, err := regexp.Compile(pat)
		if err != nil {
			continue // malformed patterns are rejected at load() time; ignore defensively here
		}
		if re.MatchString(rel) {
			return true
		}
	}
	if gitIgnoreFor(r.Dir).Ignored(rel) {
		return true
	}
	return false
}

// ContextDirs returns every repo-relative dir (incl. "" for root) that has a
// .spectackle folder with at least one bundle file, shallow before deep.
func (r Root) ContextDirs() ([]string, error) {
	var out []string
	err := filepath.WalkDir(r.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == Dot {
			ctx, _ := filepath.Rel(r.Dir, filepath.Dir(p))
			ctx = filepath.ToSlash(ctx)
			if ctx == "." {
				ctx = ""
			}
			for _, f := range []string{"spec.md", "work.md", "journal.ndjson", "bench.ndjson"} {
				if fileExists(filepath.Join(p, f)) {
					out = append(out, ctx)
					break
				}
			}
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(r.Dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		if r.SkipDir(rel, d.Name()) {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := strings.Count(out[i], "/"), strings.Count(out[j], "/")
		if out[i] == "" || out[j] == "" {
			return out[i] == ""
		}
		if di != dj {
			return di < dj
		}
		return out[i] < out[j]
	})
	return out, nil
}

// NearestContext returns the closest ancestor context dir for a repo-relative
// path (falls back to "" root). ctxs must come from ContextDirs.
func NearestContext(ctxs []string, rel string) string {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	best := ""
	for _, c := range ctxs {
		if c == "" {
			continue
		}
		if rel == c || strings.HasPrefix(rel, c+"/") {
			if len(c) > len(best) {
				best = c
			}
		}
	}
	return best
}

// EnsureScaffold creates the .spectackle folder of a context dir with its
// server-written housekeeping files. For the root it additionally writes
// config.yaml, .gitignore (cache/) and the cache dir.
func (r Root) EnsureScaffold(ctx string) error {
	dot := r.SpectackleDir(ctx)
	if err := os.MkdirAll(dot, 0o755); err != nil {
		return err
	}
	if err := ensureLines(filepath.Join(dot, ".gitattributes"), "journal.ndjson merge=union", "bench.ndjson merge=union"); err != nil {
		return err
	}
	if ctx != "" {
		return nil
	}
	// ensure-lines, not write-if-absent: repos scaffolded by older versions
	// have a .gitignore without wt/ and must not start committing worktrees.
	//
	// migrate-backup-*/ is the same story one release later. A successful
	// schema migration copies every bundle file into
	// .spectackle/migrate-backup-<from>/ and RETAINS it (migrate.go:84), so
	// without this line a migrated workspace grows a dozen untracked files
	// that wt.CommitRecords would sweep into a records commit and that the
	// scope gate would otherwise have to reason about. Ignoring them keeps
	// both out of the question; IsRecordsPath above still exempts the
	// subtree, because this line only reaches a workspace once something
	// calls EnsureScaffold and the backup can predate that (B-01KYSK7HQFFPM).
	if err := ensureLines(filepath.Join(dot, ".gitignore"), "cache/", "wt/", migrateBackupPrefix+"*/"); err != nil {
		return err
	}
	if !fileExists(filepath.Join(dot, "config.yaml")) {
		if err := os.WriteFile(filepath.Join(dot, "config.yaml"), scaffoldConfigYAML(), 0o644); err != nil {
			return err
		}
	}
	return os.MkdirAll(r.CacheDir(), 0o755)
}

// scaffoldConfigYAML renders a NEWLY created config.yaml documenting every
// setting with its default value and a short trailing comment, generated
// from defaultConfig() so the template can never drift from the actual
// defaults it describes. This only ever runs on the create path
// (EnsureScaffold's fileExists guard above) — an existing config.yaml is
// never regenerated or touched.
//
// `verify` is intentionally left out: unlike every other field it has no
// meaningful default (an empty list just means "no gate commands
// configured"), so there is nothing to document — users add it explicitly
// when they need a build/test gate. `ignore_regex` has no default patterns
// either but IS documented (as an empty list literal) since it is one of
// the two user-extensible prune mechanisms alongside `ignore`.
//
// The generated file must parse back through load() into a Config equal to
// defaultConfig() field by field — see
// TestEnsureScaffoldGeneratesSelfDocumentingConfig in workspace_test.go.
func scaffoldConfigYAML() []byte {
	d := defaultConfig()
	var b strings.Builder
	fmt.Fprintf(&b, "schema: %s  # server file-format stamp — do not edit; no migration exists, an unknown stamp is a tool error\n", d.Schema)
	b.WriteString("langs:  # languages the indexer parses (see internal/langspec for the registry)\n")
	for _, l := range d.Langs {
		fmt.Fprintf(&b, "  - %s\n", l)
	}
	b.WriteString("ignore:  # glob prune patterns, repo-relative slash paths, on top of the built-in skip list\n")
	for _, g := range d.Ignore {
		fmt.Fprintf(&b, "  - %s\n", g)
	}
	b.WriteString("ignore_regex: []  # RE2 prune patterns, repo-relative slash paths (none by default)\n")
	fmt.Fprintf(&b, "budget_default: %d  # default token budget for context-pack commands\n", d.BudgetDefault)
	b.WriteString("compact:\n")
	fmt.Fprintf(&b, "  journal_max: %d  # journal events since last compact before check/the swarm hint flags it due\n", d.Compact.JournalMax)
	fmt.Fprintf(&b, "  done_max: %d  # done-but-unarchived items before check flags it due\n", d.Compact.DoneMax)
	b.WriteString("swarm:\n")
	fmt.Fprintf(&b, "  lease_ttl: %d  # seconds a scope lease lives without refresh\n", d.Swarm.LeaseTTL)
	fmt.Fprintf(&b, "  agent_ttl: %d  # seconds without heartbeat before an agent counts as gone\n", d.Swarm.AgentTTL)
	fmt.Fprintf(&b, "  panel_max: %d  # cap on a per-item review panel (grill panel=n needs a live risk signal; config caps, never raises)\n", d.Swarm.PanelMax)
	b.WriteString("benchmarks:\n")
	fmt.Fprintf(&b, "  history: %d  # retained versions per benchmark key (bench tool); the latest is the head, older ones trim\n", d.Benchmarks.History)
	b.WriteString("feedback:\n")
	fmt.Fprintf(&b, "  max_rounds: %d  # reopen/gate-fail rounds before an item escalates to blocked\n", d.Feedback.MaxRounds)
	fmt.Fprintf(&b, "  grill: %q  # optional shell command producing grill feedback on reopen (none by default)\n", d.Feedback.Grill)
	fmt.Fprintf(&b, "  risk_files: %d  # landed-diff file count that requires a validation verdict even when validate is warn\n", d.Feedback.RiskFiles)
	b.WriteString("  dangerous_paths: []  # repo-relative globs whose landed changes require a validation verdict (empty by default)\n")
	fmt.Fprintf(&b, "worktrees_dir: %q  # override for .spectackle/wt (abs or root-relative); empty = default location\n", d.WorktreesDir)
	b.WriteString("git:\n")
	b.WriteString("  await_checks: 0  # seconds the archive closure waits for CI (0 = 12m default; set ABOVE your slowest CI run — a wait that expires on a green build costs a retry, and the retry restarts CI)\n")
	fmt.Fprintf(&b, "coverage_gate: %q  # \"package\": check counts internal/ and cmd/ packages without a binding contract as findings; empty = silent (visibility stays in state)\n", d.CoverageGate)
	return []byte(b.String())
}

func writeIfAbsent(path, content string) error {
	if fileExists(path) {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// ensureLines appends any of the given lines that are missing from the file.
func ensureLines(path string, lines ...string) error {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(raw), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	out := string(raw)
	changed := false
	for _, l := range lines {
		if !have[l] {
			if out != "" && !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			out += l + "\n"
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

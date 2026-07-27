// Package migrate upgrades a .spectackle workspace from an older schema stamp
// to the current one, in place, when the workspace is opened.
//
// # Why this is not the throwaway path T-0094 rejected
//
// T-0094 was rejected on the grounds that a one-off rewrite of one
// repository's records does not justify a permanent tool, and T-0137 was
// rejected in its favor. This is a different animal. An ID-scheme change
// (ADR-0013) reaches every workspace every user of a released tool already
// carries, and the schema stamp's documented answer to a mismatch is a hard
// error saying there is no migration (see workspace.SchemaStamp, spec.Load).
// Shipping the new scheme without a migration would turn every existing
// workspace into an unreadable one, and the honest advice to users would be to
// regenerate and lose their history.
//
// # Why it does not depend on the packages it rewrites
//
// The migration reads and writes raw bytes: work.md as text, journal.ndjson as
// generic JSON objects, spec.md and config.yaml as text with a front-matter
// stamp. It deliberately does not use internal/item, internal/journal or
// internal/spec, for two reasons that both matter.
//
// The first is ordering. Those packages refuse a mismatched stamp — that is
// the very behavior being fixed — so a migration built on them could not read
// its own input. Nothing above them can help either: workspace.Detect fails on
// the old config stamp, so the migration has to run before a Root exists, on
// nothing but a directory path.
//
// The second is fidelity. Parsing a journal event into the Go struct and
// re-marshalling it would silently drop any field the struct does not know and
// normalize the ones it does. A migration must be a rewrite of the fields it
// understands and a byte-faithful passthrough of everything else, so it
// decodes into an ordered generic form instead.
//
// # Properties
//
// Idempotent: a workspace already on the current stamp is untouched and
// reports nothing. Deterministic: new IDs are derived from each record's own
// creation timestamp in the journal plus its legacy ID as a seed (see
// ids.MintRecordIDFrom), so two clones migrating the same committed records
// converge instead of diverging and colliding on the next merge, and the
// archive keeps its chronology instead of collapsing into the migration
// moment. Atomic and recoverable: every new file content is computed in memory
// before anything is written, the originals are copied into a retained backup
// directory first, and a crash mid-write is detected and rolled back on the
// next open (see Recover).
package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jxsl13/spectackle/internal/ids"
)

// Dot mirrors workspace.Dot. It is duplicated rather than imported to keep
// this package free of the dependency that would make the import cycle (see
// the package comment: internal/item imports internal/workspace, and this
// package must not import either).
const Dot = ".spectackle"

// From is the stamp this migration upgrades, To the stamp it produces. A
// bundle on any other stamp is refused exactly as before — there is one
// migration, not a chain, and pretending otherwise would silently mangle a
// workspace from a future version.
const (
	From = "v0"
	To   = "v1"
)

// backupPrefix names the retained pre-migration copy. It sits inside the
// workspace's own root .spectackle folder so it travels with the workspace and
// cannot be mistaken for a sibling repository, and it is prefixed so Recover
// can find it without being told the stamp.
const backupPrefix = "migrate-backup-"

// doneMarker is written into the backup directory as the LAST step of a
// successful migration. Its absence beside an existing backup is what
// distinguishes "crashed partway" from "finished, backup retained".
const doneMarker = "COMPLETE"

// legacyIDRe matches a pre-ADR-0013 sequential record ID as a whole token.
// The boundaries matter: without them "P-0001" would also rewrite inside
// "P-00012", and prose routinely mentions IDs mid-sentence.
var legacyIDRe = regexp.MustCompile(`\b((?:ADR|[PTBRD])-\d{4})\b`)

// legacyHeadingRe matches a work.md item heading carrying a legacy ID.
var legacyHeadingRe = regexp.MustCompile(`^## +((?:ADR|[PTBRD])-\d{4}) `)

// kindOfLetter maps an ID letter to the item kind. D is the legacy letter for
// adr items, from before the decision->adr rename.
var kindOfLetter = map[string]string{
	"P": "proposal", "T": "task", "B": "bug", "R": "research",
	"ADR": "adr", "D": "adr",
}

// Report describes what a Run did, or would have done.
type Report struct {
	Migrated bool              // false = already current, nothing written
	Remap    map[string]string // legacy ID -> new ID, every record found
	Files    []string          // workspace-relative paths rewritten, sorted
	Events   int               // journal events whose content changed
	Backup   string            // workspace-relative path of the retained backup
}

// ErrUnknownStamp reports a bundle stamped with something this migration does
// not understand — a workspace written by a newer version, most likely.
var ErrUnknownStamp = errors.New("migrate: unknown schema stamp")

// rootProbes are the root bundle's own stamped files, in the order Needed
// checks them. Three small reads, no walk: this runs on every workspace open,
// including the ones spec.Load performs several times per tool call, so the
// already-migrated path must not cost a tree traversal.
var rootProbes = []string{"config.yaml", "spec.md", "work.md"}

// Needed reports whether the workspace at dir carries the old stamp, by
// probing only the root bundle. A nested bundle left behind at the old stamp
// can only happen if a write was interrupted, and that case is caught by
// Recover rather than by this probe.
func Needed(dir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(dir, Dot)); err != nil {
		return false, nil // not a workspace
	}
	for _, name := range rootProbes {
		raw, err := os.ReadFile(filepath.Join(dir, Dot, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if m := frontMatterStampRe.FindSubmatch(raw); m != nil && string(m[1]) == From {
			return true, nil
		}
	}
	// An interrupted attempt leaves new files beside old ones, so the probe
	// above can read a migrated root while nested bundles are still stale.
	if _, err := os.Stat(filepath.Join(dir, Dot, backupPrefix+From)); err == nil {
		if _, err := os.Stat(filepath.Join(dir, Dot, backupPrefix+From, doneMarker)); err != nil {
			return true, nil
		}
	}
	return false, nil
}

// Run migrates the workspace rooted at dir, if it needs it.
//
// It is safe and cheap to call on every open: the common case is a stamp scan
// over the bundle files and an immediate return with Migrated false.
func Run(dir string) (Report, error) {
	if rep, err := Recover(dir); err != nil || rep.Migrated {
		// A previous, interrupted attempt was rolled back. Fall through and
		// migrate again from the restored (old) state.
		if err != nil {
			return Report{}, err
		}
	}
	files, err := bundleFiles(dir)
	if err != nil {
		return Report{}, err
	}
	stamps, err := stampsOf(dir, files)
	if err != nil {
		return Report{}, err
	}
	switch {
	case len(stamps) == 0 || stamps[To] == len(files):
		return Report{}, nil // nothing stamped, or all current
	case stamps[From] == 0:
		for s := range stamps {
			if s != To {
				return Report{}, fmt.Errorf("%w %q: expected %q or %q", ErrUnknownStamp, s, From, To)
			}
		}
		return Report{}, nil
	}

	// ---- plan: everything is computed before anything is written ----
	ws := &workspaceText{dir: dir}
	if err := ws.read(files); err != nil {
		return Report{}, err
	}
	remap, err := ws.plan()
	if err != nil {
		return Report{}, err
	}
	rewritten, events := ws.rewrite(remap)

	// ---- commit: back up the originals, then write ----
	backup := backupPrefix + From
	backupAbs := filepath.Join(dir, Dot, backup)
	if err := os.RemoveAll(backupAbs); err != nil {
		return Report{}, err
	}
	for rel := range rewritten {
		if err := copyInto(filepath.Join(dir, rel), filepath.Join(backupAbs, rel)); err != nil {
			return Report{}, err
		}
	}
	paths := make([]string, 0, len(rewritten))
	for rel := range rewritten {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		if err := writeAtomic(filepath.Join(dir, rel), rewritten[rel]); err != nil {
			// Leave the backup and its missing marker behind: the next open
			// detects the incomplete attempt and rolls back.
			return Report{}, fmt.Errorf("migrate: writing %s: %w", rel, err)
		}
	}
	// COMPLETE is written only when every stamped file actually reads To:
	// a completion marker over a half-stamped bundle was the brick (issue
	// 178). Withholding it leaves the backup armed — the next open rolls
	// back and retries from the restored originals.
	if after, aerr := stampsOf(dir, files); aerr != nil {
		return Report{}, aerr
	} else {
		for stamp, n := range after {
			if stamp != To {
				return Report{}, fmt.Errorf("migrate: %d file(s) still stamped %q after rewrite — completion withheld, the next open rolls back and retries", n, stamp)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(backupAbs, doneMarker), []byte(To+"\n"), 0o644); err != nil {
		return Report{}, err
	}
	return Report{
		Migrated: true, Remap: remap, Files: paths, Events: events,
		Backup: filepath.Join(Dot, backup),
	}, nil
}

// Recover rolls back an interrupted migration: a retained backup with no
// completion marker means the process died between the first and last write,
// so the workspace is a mix of old and new files. Restoring the backup returns
// it to a state that loads, which Run then migrates again from scratch.
//
// Reported as Migrated when it actually restored something.
func Recover(dir string) (Report, error) {
	backupAbs := filepath.Join(dir, Dot, backupPrefix+From)
	if _, err := os.Stat(backupAbs); err != nil {
		return Report{}, nil // no attempt in flight
	}
	if _, err := os.Stat(filepath.Join(backupAbs, doneMarker)); err == nil {
		return Report{}, nil // finished; the backup is the retained copy
	}
	var restored []string
	err := filepath.WalkDir(backupAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(backupAbs, p)
		if relErr != nil {
			return relErr
		}
		raw, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		if writeErr := writeAtomic(filepath.Join(dir, rel), raw); writeErr != nil {
			return writeErr
		}
		restored = append(restored, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	if err := os.RemoveAll(backupAbs); err != nil {
		return Report{}, err
	}
	sort.Strings(restored)
	return Report{Migrated: len(restored) > 0, Files: restored}, nil
}

// ---- reading the bundle ----

// bundleFiles lists every server-written file in the workspace, as
// workspace-relative slash paths, sorted. It walks for Dot directories rather
// than asking a workspace.Root for its context dirs, since no Root can be
// loaded while the stamp is stale.
func bundleFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch {
			case p == dir:
				return nil
			case name == Dot:
				return nil // descend: the bundle files are in here
			case name == ".git" || name == "cache" || name == "wt" ||
				strings.HasPrefix(name, backupPrefix):
				return filepath.SkipDir
			}
			return nil
		}
		switch name {
		case "work.md", "spec.md", "journal.ndjson", "config.yaml", "anchors.tsv":
		default:
			return nil
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		if !strings.Contains(filepath.ToSlash(rel), Dot+"/") {
			return nil // a work.md outside a .spectackle folder is not ours
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// frontMatterStampRe pulls the `schema:` value out of a leading front matter
// block or, for config.yaml, out of the top-level mapping.
var frontMatterStampRe = regexp.MustCompile(`(?m)^schema:[ \t]*"?([A-Za-z0-9._-]+)"?[ \t]*\r?$`)

// stampsOf counts the stamps present across the bundle. Files carrying no
// stamp at all (anchors.tsv, a hand-created spec.md) are not counted: they are
// not evidence either way.
func stampsOf(dir string, files []string) (map[string]int, error) {
	out := map[string]int{}
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if m := frontMatterStampRe.FindSubmatch(raw); m != nil {
			out[string(m[1])]++
		}
	}
	return out, nil
}

// workspaceText is the whole bundle held as bytes, plus the journal decoded
// into ordered generic objects.
type workspaceText struct {
	dir     string
	files   []string
	content map[string][]byte
	journal map[string][]jsonObj // rel path -> one object per NDJSON line
}

func (w *workspaceText) read(files []string) error {
	w.files = files
	w.content = make(map[string][]byte, len(files))
	w.journal = map[string][]jsonObj{}
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(w.dir, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		w.content[rel] = raw
		if filepath.Base(rel) != "journal.ndjson" {
			continue
		}
		objs, err := parseNDJSON(raw)
		if err != nil {
			return fmt.Errorf("migrate: %s: %w", rel, err)
		}
		w.journal[rel] = objs
	}
	return nil
}

// plan builds the legacy-ID -> new-ID mapping.
//
// Each new ID is stamped with the record's own creation time, taken from the
// journal — the earliest event mentioning it, which for any record that was
// ever drafted is its create event. Minting from wall-clock-now instead would
// flatten the whole archive's chronology into the migration moment, and these
// IDs sort by their timestamp.
func (w *workspaceText) plan() (map[string]string, error) {
	kinds := map[string]string{} // legacy ID -> kind
	born := map[string]time.Time{}
	seen := map[string]bool{}

	// The sources are consulted in descending order of precision, and a later
	// source never overrides an earlier one. That ordering is the whole point:
	// work.md's `created:` is a DATE, so folding it in as "another sighting,
	// keep the earliest" would drag every record created on a given day back to
	// that day's midnight and destroy the intra-day ordering the journal knows.
	// Found by TestMigratePreservesChronology, where two records five minutes
	// apart ended up with identical timestamps and therefore an order decided by
	// their hash.
	note := func(id, k string, t time.Time, precise bool) {
		if _, ok := kindOfLetter[strings.SplitN(id, "-", 2)[0]]; !ok {
			return
		}
		seen[id] = true
		if k != "" && kinds[id] == "" {
			kinds[id] = k
		}
		if _, ok := born[id]; ok || !precise || t.IsZero() {
			return
		}
		born[id] = t
	}

	// 1. the journal's own create event: the record's actual birth, to the
	//    second. Sorted by timestamp first so "the earliest event mentioning
	//    it" is well defined even across context dirs, whose files are visited
	//    in map order.
	type ev struct {
		t time.Time
		o jsonObj
	}
	var events []ev
	for _, objs := range w.journal {
		for _, o := range objs {
			events = append(events, ev{o.time("t"), o})
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].t.Before(events[j].t) })
	for _, e := range events {
		if e.o.str("ev") != "create" {
			continue
		}
		if id := e.o.str("id"); id != "" {
			note(id, e.o.str("k"), e.t, true)
		}
	}
	// 2. any other journal event mentioning the ID — a record whose create
	//    event a compact folded away is only witnessed by its archive or reject
	//    tombstone, which is still a real timestamp.
	for _, e := range events {
		for _, id := range e.o.ids() {
			note(id, e.o.str("k"), e.t, true)
		}
	}
	// 3. work.md headings, for a record with no journal event at all (seeded
	//    directly, or drafted and never journaled). Date precision only.
	for _, rel := range w.files {
		if filepath.Base(rel) != "work.md" {
			continue
		}
		for _, it := range parseWorkItems(string(w.content[rel])) {
			note(it.id, it.kind, it.created, true)
		}
	}
	// 4. anything the text merely mentions — a ref to a record whose events
	//    were compacted away — still has to map, or the reference dangles after
	//    the rewrite. No timestamp; the sequence-number fallback below orders
	//    these.
	for _, rel := range w.files {
		for _, m := range legacyIDRe.FindAllStringSubmatch(string(w.content[rel]), -1) {
			note(m[1], "", time.Time{}, false)
		}
	}

	remap := make(map[string]string, len(seen))
	for id := range seen {
		t := born[id]
		kind := kinds[id]
		if kind == "" {
			kind = kindOfLetter[strings.SplitN(id, "-", 2)[0]]
		}
		letter := "P"
		for l, k := range kindOfLetter {
			if k == kind && l != "D" {
				letter = l
				break
			}
		}
		if t.IsZero() {
			// No timestamp anywhere. Deriving one from the sequence number
			// keeps such records in their original relative order instead of
			// bunching them at the epoch, which is all the ordering guarantee
			// they ever had.
			t = time.Unix(0, 0).UTC().Add(time.Duration(seqOf(id)) * time.Second)
		}
		remap[id] = letter + "-" + ids.MintRecordIDFrom(t, id).String()
	}
	// An empty remap is not an error. A workspace can legitimately carry the
	// old stamp with no records at all — a freshly scaffolded one, or one that
	// only ever held rules — and for those the migration is purely a restamp.
	return remap, nil
}

// rewrite produces the new content of every file that changes.
func (w *workspaceText) rewrite(remap map[string]string) (map[string][]byte, int) {
	out := map[string][]byte{}
	events := 0
	for _, rel := range w.files {
		if objs, ok := w.journal[rel]; ok {
			raw, n := rewriteJournal(objs, remap)
			events += n
			if !bytesEqual(raw, w.content[rel]) {
				out[rel] = raw
			}
			continue
		}
		raw := replaceIDs(w.content[rel], remap)
		raw = restamp(raw)
		if strings.HasSuffix(rel, "config.yaml") {
			raw = forceConfigStamp(raw)
		}
		if !bytesEqual(raw, w.content[rel]) {
			out[rel] = raw
		}
	}
	return out, events
}

// replaceIDs rewrites every whole-token legacy ID. This one function covers
// work.md headings, the parent/needs/refs header lines, the `blocks:` line of a
// decide option record, spec.md intent lines and any ID mentioned in prose —
// they are all the same edit, and enumerating the field names instead would
// leave whichever one nobody thought of dangling.
func replaceIDs(raw []byte, remap map[string]string) []byte {
	return legacyIDRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		if to, ok := remap[string(m)]; ok {
			return []byte(to)
		}
		return m
	})
}

// restamp rewrites a `schema: <From>` line to To, in front matter or in
// config.yaml's top-level mapping.
func restamp(raw []byte) []byte {
	return frontMatterStampRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		sub := frontMatterStampRe.FindSubmatch(m)
		if sub == nil || string(sub[1]) != From {
			return m
		}
		return []byte("schema: " + To)
	})
}

// forceConfigStamp guarantees config.yaml leaves the migration at To even
// when the generic front-matter regex misses its schema line — CRLF
// endings or a trailing comment escaped it, the migration wrote COMPLETE
// anyway, and the next open refused the half-stamped bundle (issue 178).
// The whole line is replaced (any comment on it describes the OLD stamp).
func forceConfigStamp(raw []byte) []byte {
	if m := frontMatterStampRe.FindSubmatch(raw); m != nil && string(m[1]) == To {
		return raw // already current, leave byte-identical
	}
	re := regexp.MustCompile("(?m)^schema:[^\r\n]*")
	if re.Match(raw) {
		return re.ReplaceAll(raw, []byte("schema: "+To))
	}
	return append([]byte("schema: "+To+"\n"), raw...)
}

// rewriteJournal rewrites the ID-bearing fields of every event and re-emits
// the file. Fields are handled by name where they hold an ID or a list of
// them, and by whole-token replacement where they hold free text that may
// mention one (note, sum, body, ti). Every other field passes through as it
// was decoded.
func rewriteJournal(objs []jsonObj, remap map[string]string) ([]byte, int) {
	var b strings.Builder
	changed := 0
	for _, o := range objs {
		before := o.encode()
		for _, k := range []string{"id", "par", "item"} {
			if v := o.str(k); v != "" {
				if to, ok := remap[v]; ok {
					o.set(k, to)
				}
			}
		}
		for _, k := range []string{"nd", "tg", "rules"} {
			if list, ok := o.strs(k); ok {
				next := make([]any, len(list))
				for i, v := range list {
					if to, ok := remap[v]; ok {
						next[i] = to
					} else {
						next[i] = v
					}
				}
				o.set(k, next)
			}
		}
		for _, k := range []string{"note", "sum", "body", "ti"} {
			if v := o.str(k); v != "" {
				if s := string(replaceIDs([]byte(v), remap)); s != v {
					o.set(k, s)
				}
			}
		}
		line := o.encode()
		if line != before {
			changed++
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return []byte(b.String()), changed
}

// ---- work.md item headers (read-only; the rewrite is textual) ----

type workItem struct {
	id      string
	kind    string
	created time.Time
}

var createdRe = regexp.MustCompile(`^created: *(\d{4}-\d{2}-\d{2})`)
var kindRe = regexp.MustCompile(`^kind: *(\w+)`)

// parseWorkItems reads just enough of a work.md to plan the remap: the legacy
// ID, its kind and its created date. It is deliberately not item.LoadWork —
// that would drag in the packages this one cannot depend on (see the package
// comment), and the fields needed here are three.
func parseWorkItems(src string) []workItem {
	var out []workItem
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		m := legacyHeadingRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		it := workItem{id: m[1]}
		for j := i + 1; j < len(lines) && !strings.HasPrefix(lines[j], "## "); j++ {
			if k := kindRe.FindStringSubmatch(lines[j]); k != nil && it.kind == "" {
				it.kind = k[1]
			}
			if c := createdRe.FindStringSubmatch(lines[j]); c != nil && it.created.IsZero() {
				if t, err := time.Parse("2006-01-02", c[1]); err == nil {
					it.created = t.UTC()
				}
			}
		}
		out = append(out, it)
	}
	return out
}

// seqOf returns the sequence number of a legacy ID, or 0.
func seqOf(id string) int {
	_, digits, ok := strings.Cut(id, "-")
	if !ok {
		return 0
	}
	n := 0
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0
		}
		n = n*10 + int(digits[i]-'0')
	}
	return n
}

// ---- file plumbing ----

// writeAtomic replaces path's contents via a temp file and a rename, so a
// reader never observes a partial file. Per-file atomicity is what makes the
// backup-and-marker scheme enough for whole-workspace atomicity: either a file
// is entirely old or entirely new, and the marker says whether every file got
// its turn.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func copyInto(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func bytesEqual(a, b []byte) bool { return string(a) == string(b) }

// ---- ordered generic JSON ----

// jsonObj is one journal event, decoded so that unknown fields survive and key
// order is preserved. json.Decoder with UseNumber keeps integers from being
// reformatted through float64, which would rewrite fields this migration has
// no business touching.
type jsonObj struct {
	keys []string
	vals map[string]any
}

func parseNDJSON(raw []byte) ([]jsonObj, error) {
	var out []jsonObj
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		o, err := parseObj(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		out = append(out, o)
	}
	return out, nil
}

func parseObj(line string) (jsonObj, error) {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return jsonObj{}, err
	}
	// Recover the on-disk key order: a Go map has none, and re-emitting the
	// keys sorted would rewrite every line of every journal in the workspace
	// as a side effect of migrating a handful of IDs.
	o := jsonObj{vals: m}
	var order []string
	d2 := json.NewDecoder(strings.NewReader(line))
	if _, err := d2.Token(); err == nil {
		for d2.More() {
			tok, err := d2.Token()
			if err != nil {
				break
			}
			key, ok := tok.(string)
			if !ok {
				break
			}
			order = append(order, key)
			var skip json.RawMessage
			if err := d2.Decode(&skip); err != nil {
				break
			}
		}
	}
	for _, k := range order {
		if _, ok := m[k]; ok {
			o.keys = append(o.keys, k)
		}
	}
	return o, nil
}

func (o jsonObj) str(k string) string {
	s, _ := o.vals[k].(string)
	return s
}

func (o jsonObj) strs(k string) ([]string, bool) {
	raw, ok := o.vals[k].([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func (o jsonObj) time(k string) time.Time {
	t, err := time.Parse(time.RFC3339, o.str(k))
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// ids returns every ID-shaped value in the fields that hold one.
func (o jsonObj) ids() []string {
	var out []string
	for _, k := range []string{"id", "par", "item"} {
		if v := o.str(k); v != "" {
			out = append(out, v)
		}
	}
	if nd, ok := o.strs("nd"); ok {
		out = append(out, nd...)
	}
	return out
}

func (o *jsonObj) set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// encode re-emits the object in its original key order, with the same escaping
// the journal writer uses (encoding/json, HTML escaping left on, no trailing
// newline).
func (o jsonObj) encode() string {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(o.vals[k])
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.String()
}

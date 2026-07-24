// Package coord is the shared multi-agent coordination DB — the "swarm
// brain". It lives at <main>/.spectacle/cache/coord.db and is shared by all
// spectacle processes (one per agent session) via WAL-mode SQLite.
//
// It owns: the agent registry (heartbeats), scope leases, the global ID
// counters (item AND rule IDs — the only collision-free mint across
// worktrees), the swarm event log (realtime learnings: rejections, rules,
// drift), replay bookkeeping (applied event IDs), worktree records and the
// global integrate lock.
//
// coord.db is unversioned but NOT knowledge: losing it loses only ephemeral
// coordination state. Counters are floor-seeded on every mint (max(stored,
// scan-derived floor)+1), so deletion can never regress IDs.
package coord

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const gen = "coord-v1"

const ddl = `
CREATE TABLE IF NOT EXISTS meta     (k TEXT PRIMARY KEY, v TEXT);
CREATE TABLE IF NOT EXISTS agents   (name TEXT PRIMARY KEY, pid INTEGER NOT NULL,
  started INTEGER NOT NULL, hb INTEGER NOT NULL,
  item TEXT NOT NULL DEFAULT '', wt TEXT NOT NULL DEFAULT '',
  cursor INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS counters (kind TEXT PRIMARY KEY, n INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS leases   (path TEXT PRIMARY KEY, agent TEXT NOT NULL,
  item TEXT NOT NULL DEFAULT '', exp INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS events   (seq INTEGER PRIMARY KEY AUTOINCREMENT,
  t INTEGER NOT NULL, agent TEXT NOT NULL, ev TEXT NOT NULL,
  ref TEXT NOT NULL DEFAULT '', msg TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS applied  (wt TEXT NOT NULL, eid TEXT NOT NULL, PRIMARY KEY(wt,eid));
CREATE TABLE IF NOT EXISTS worktrees(item TEXT PRIMARY KEY, agent TEXT NOT NULL,
  branch TEXT NOT NULL, root TEXT NOT NULL, base TEXT NOT NULL,
  state TEXT NOT NULL, created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS locks    (name TEXT PRIMARY KEY, agent TEXT NOT NULL, exp INTEGER NOT NULL);
`

// Lease is one scope reservation.
type Lease struct {
	Path  string // repo-relative dir/file prefix or item ID
	Agent string
	Item  string
	Exp   time.Time
}

// AgentRow is one registered agent.
type AgentRow struct {
	Name string
	Pid  int
	HB   time.Time
	Item string
	WT   string
}

// Event is one swarm learning/coordination notice.
type Event struct {
	Seq   int64
	T     time.Time
	Agent string
	Ev    string // reject|rule|drift|claim|release|start|submit|abort|expire|remap
	Ref   string
	Msg   string
}

// Worktree is the coordination record of an open worktree.
type Worktree struct {
	Item, Agent, Branch, Root, Base, State string
	Created                                time.Time
}

// DB wraps the shared coordination database for one agent.
type DB struct {
	db    *sql.DB
	Agent string
	pid   int // for heartbeat re-registration after a sweep
}

// Open opens (creating/rebuilding as needed) the coordination DB and
// registers the agent.
func Open(path, agent string, pid int) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// _txlock=immediate: every transaction takes the write lock up front,
	// avoiding the read->write upgrade deadlock that busy_timeout cannot
	// retry. WAL + NORMAL is the standard multi-process profile.
	dsn := "file:" + path + "?_txlock=immediate" +
		"&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	d := &DB{db: db, Agent: agent, pid: pid}
	if err := d.init(); err != nil {
		db.Close()
		return nil, err
	}
	now := time.Now().Unix()
	err = d.retry(func() error {
		_, err := d.db.Exec(`INSERT INTO agents(name,pid,started,hb,cursor)
			VALUES(?,?,?,?,COALESCE((SELECT MAX(seq) FROM events),0))
			ON CONFLICT(name) DO UPDATE SET pid=excluded.pid, hb=excluded.hb`, agent, pid, now, now)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) init() error {
	return d.retry(func() error {
		var have string
		err := d.db.QueryRow(`SELECT v FROM meta WHERE k='gen'`).Scan(&have)
		if err == nil && have == gen {
			return nil
		}
		for _, t := range []string{"meta", "agents", "counters", "leases", "events", "applied", "worktrees", "locks"} {
			if _, err := d.db.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
				return err
			}
		}
		if _, err := d.db.Exec(ddl); err != nil {
			return err
		}
		_, err = d.db.Exec(`INSERT INTO meta(k,v) VALUES('gen',?)`, gen)
		return err
	})
}

// Close closes the handle.
func (d *DB) Close() error { return d.db.Close() }

// retry re-runs f on SQLITE_BUSY/LOCKED with jittered backoff — cross-process
// write bursts are rare but real.
func (d *DB) retry(f func() error) error {
	var err error
	for i := 0; i < 4; i++ {
		if err = f(); err == nil {
			return nil
		}
		s := err.Error()
		if !strings.Contains(s, "SQLITE_BUSY") && !strings.Contains(s, "database is locked") &&
			!strings.Contains(s, "SQLITE_LOCKED") {
			return err
		}
		time.Sleep(time.Duration(10+rand.Intn(40)) * time.Millisecond)
	}
	return err
}

// Heartbeat marks this agent alive; called by the tool gate. It is an upsert:
// an agent whose row was removed by a sibling's sweep (idle past agent_ttl)
// silently re-registers on its next call instead of heartbeating into a void.
func (d *DB) Heartbeat() error {
	now := time.Now().Unix()
	return d.retry(func() error {
		_, err := d.db.Exec(`INSERT INTO agents(name,pid,started,hb,cursor)
			VALUES(?,?,?,?,COALESCE((SELECT MAX(seq) FROM events),0))
			ON CONFLICT(name) DO UPDATE SET hb=excluded.hb`, d.Agent, d.pid, now, now)
		return err
	})
}

// Deregister removes this agent's leases and registry row — the clean
// goodbye on server shutdown, so short-lived sessions never linger in the
// swarm view until the TTL sweep.
func (d *DB) Deregister() error {
	return d.retry(func() error {
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`DELETE FROM leases WHERE agent=?`, d.Agent); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM agents WHERE name=?`, d.Agent); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// SetActive records the agent's current item and worktree root.
func (d *DB) SetActive(item, wtRoot string) error {
	return d.retry(func() error {
		_, err := d.db.Exec(`UPDATE agents SET item=?, wt=? WHERE name=?`, item, wtRoot, d.Agent)
		return err
	})
}

// Sweep expires leases of agents whose heartbeat is older than agentTTL and
// emits an expire event per lease. Returns the expired leases.
func (d *DB) Sweep(agentTTL time.Duration) ([]Lease, error) {
	cutoff := time.Now().Add(-agentTTL).Unix()
	var expired []Lease
	err := d.retry(func() error {
		expired = expired[:0]
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		rows, err := tx.Query(`SELECT l.path, l.agent, l.item, l.exp FROM leases l
			JOIN agents a ON a.name = l.agent WHERE a.hb < ?`, cutoff)
		if err != nil {
			return err
		}
		for rows.Next() {
			var l Lease
			var exp int64
			if err := rows.Scan(&l.Path, &l.Agent, &l.Item, &exp); err != nil {
				rows.Close()
				return err
			}
			l.Exp = time.Unix(exp, 0)
			expired = append(expired, l)
		}
		rows.Close()
		if len(expired) > 0 {
			if _, err := tx.Exec(`DELETE FROM leases WHERE agent IN (SELECT name FROM agents WHERE hb < ?)`, cutoff); err != nil {
				return err
			}
		}
		// SPX-SWM-006: the registry row goes with the leases — the swarm
		// view shows who is actually there. Deleted unconditionally (a stale
		// agent usually holds no leases at all); a still-alive-but-idle agent
		// re-registers via its next Heartbeat (upsert).
		if _, err := tx.Exec(`DELETE FROM agents WHERE hb < ?`, cutoff); err != nil {
			return err
		}
		now := time.Now().Unix()
		for _, l := range expired {
			if _, err := tx.Exec(`INSERT INTO events(t,agent,ev,ref,msg) VALUES(?,?,?,?,?)`,
				now, d.Agent, "expire", l.Item, "lease "+l.Path+" of stale agent "+l.Agent+" expired"); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	return expired, err
}

// NextID atomically mints the next number for a counter kind ("item:P",
// "rule:CUDA-KRN"), never below floor+1 (floor = caller's file-scan max, so
// a deleted coord.db cannot regress IDs).
func (d *DB) NextID(kind string, floor int) (int, error) {
	var n int
	err := d.retry(func() error {
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var stored int
		_ = tx.QueryRow(`SELECT n FROM counters WHERE kind=?`, kind).Scan(&stored)
		if floor > stored {
			stored = floor
		}
		n = stored + 1
		if _, err := tx.Exec(`INSERT INTO counters(kind,n) VALUES(?,?)
			ON CONFLICT(kind) DO UPDATE SET n=excluded.n`, kind, n); err != nil {
			return err
		}
		return tx.Commit()
	})
	return n, err
}

// overlaps reports prefix overlap between two scope paths (item IDs only
// overlap on equality; path prefixes overlap in either direction).
func overlaps(a, b string) bool {
	if a == b {
		return true
	}
	return strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// Blocked returns the first live foreign lease overlapping any of the paths.
func (d *DB) Blocked(paths []string, agentTTL time.Duration) (*Lease, error) {
	live, err := d.liveLeases(agentTTL)
	if err != nil {
		return nil, err
	}
	for _, l := range live {
		if l.Agent == d.Agent {
			continue
		}
		for _, p := range paths {
			if overlaps(l.Path, p) {
				lease := l
				return &lease, nil
			}
		}
	}
	return nil, nil
}

// Claim reserves the paths for this agent, all-or-nothing. A conflicting
// live foreign lease is returned instead of an error.
func (d *DB) Claim(paths []string, item string, ttl, agentTTL time.Duration) (*Lease, error) {
	if conflict, err := d.Blocked(paths, agentTTL); conflict != nil || err != nil {
		return conflict, err
	}
	exp := time.Now().Add(ttl).Unix()
	err := d.retry(func() error {
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, p := range paths {
			if _, err := tx.Exec(`INSERT INTO leases(path,agent,item,exp) VALUES(?,?,?,?)
				ON CONFLICT(path) DO UPDATE SET agent=excluded.agent, item=excluded.item, exp=excluded.exp`,
				p, d.Agent, item, exp); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	return nil, err
}

// Release drops leases: the given paths, or all of this agent's when paths
// is nil. Only own leases are touched.
func (d *DB) Release(paths []string) error {
	return d.retry(func() error {
		if paths == nil {
			_, err := d.db.Exec(`DELETE FROM leases WHERE agent=?`, d.Agent)
			return err
		}
		for _, p := range paths {
			if _, err := d.db.Exec(`DELETE FROM leases WHERE path=? AND agent=?`, p, d.Agent); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReleaseItem drops all leases bound to an item (any holder — used when
// aborting an orphaned worktree of a dead agent).
func (d *DB) ReleaseItem(item string) error {
	return d.retry(func() error {
		_, err := d.db.Exec(`DELETE FROM leases WHERE item=?`, item)
		return err
	})
}

// RefreshLeases extends all own leases; called by the tool gate.
func (d *DB) RefreshLeases(ttl time.Duration) error {
	return d.retry(func() error {
		_, err := d.db.Exec(`UPDATE leases SET exp=? WHERE agent=?`, time.Now().Add(ttl).Unix(), d.Agent)
		return err
	})
}

func (d *DB) liveLeases(agentTTL time.Duration) ([]Lease, error) {
	now := time.Now()
	cutoff := now.Add(-agentTTL).Unix()
	var out []Lease
	err := d.retry(func() error {
		out = out[:0]
		rows, err := d.db.Query(`SELECT l.path, l.agent, l.item, l.exp FROM leases l
			JOIN agents a ON a.name = l.agent WHERE l.exp > ? AND a.hb >= ?`, now.Unix(), cutoff)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l Lease
			var exp int64
			if err := rows.Scan(&l.Path, &l.Agent, &l.Item, &exp); err != nil {
				return err
			}
			l.Exp = time.Unix(exp, 0)
			out = append(out, l)
		}
		return rows.Err()
	})
	return out, err
}

// Leases returns all live leases.
func (d *DB) Leases(agentTTL time.Duration) ([]Lease, error) { return d.liveLeases(agentTTL) }

// Agents returns the registry.
func (d *DB) Agents() ([]AgentRow, error) {
	var out []AgentRow
	err := d.retry(func() error {
		out = out[:0]
		rows, err := d.db.Query(`SELECT name, pid, hb, item, wt FROM agents ORDER BY hb DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a AgentRow
			var hb int64
			if err := rows.Scan(&a.Name, &a.Pid, &hb, &a.Item, &a.WT); err != nil {
				return err
			}
			a.HB = time.Unix(hb, 0)
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// Emit appends a swarm event (the realtime learning channel).
func (d *DB) Emit(ev, ref, msg string) error {
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return d.retry(func() error {
		_, err := d.db.Exec(`INSERT INTO events(t,agent,ev,ref,msg) VALUES(?,?,?,?,?)`,
			time.Now().Unix(), d.Agent, ev, ref, msg)
		return err
	})
}

// After returns up to limit events with seq > after, oldest first, skipping
// this agent's own events.
func (d *DB) After(after int64, limit int) ([]Event, error) {
	var out []Event
	err := d.retry(func() error {
		out = out[:0]
		rows, err := d.db.Query(`SELECT seq,t,agent,ev,ref,msg FROM events
			WHERE seq > ? AND agent != ? ORDER BY seq LIMIT ?`, after, d.Agent, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Event
			var ts int64
			if err := rows.Scan(&e.Seq, &ts, &e.Agent, &e.Ev, &e.Ref, &e.Msg); err != nil {
				return err
			}
			e.T = time.Unix(ts, 0)
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// MaxSeq returns the highest event sequence number.
func (d *DB) MaxSeq() (int64, error) {
	var n sql.NullInt64
	err := d.retry(func() error { return d.db.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&n) })
	return n.Int64, err
}

// Cursor / SetCursor track this agent's last-delivered event seq.
func (d *DB) Cursor() (int64, error) {
	var c int64
	err := d.retry(func() error {
		return d.db.QueryRow(`SELECT cursor FROM agents WHERE name=?`, d.Agent).Scan(&c)
	})
	return c, err
}

func (d *DB) SetCursor(c int64) error {
	return d.retry(func() error {
		_, err := d.db.Exec(`UPDATE agents SET cursor=? WHERE name=?`, c, d.Agent)
		return err
	})
}

// SearchEvents does a LIKE search over the swarm event log (unioned into
// find scope=rejection|history so sibling learnings surface pre-merge).
func (d *DB) SearchEvents(q string, kinds []string, k int) ([]Event, error) {
	var out []Event
	like := "%" + strings.ToLower(strings.TrimSpace(q)) + "%"
	err := d.retry(func() error {
		out = out[:0]
		query := `SELECT seq,t,agent,ev,ref,msg FROM events WHERE (lower(msg) LIKE ? OR lower(ref) LIKE ?)`
		args := []any{like, like}
		if len(kinds) > 0 {
			query += ` AND ev IN (` + strings.Repeat("?,", len(kinds)-1) + `?)`
			for _, kd := range kinds {
				args = append(args, kd)
			}
		}
		query += ` ORDER BY seq DESC LIMIT ?`
		args = append(args, k)
		rows, err := d.db.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Event
			var ts int64
			if err := rows.Scan(&e.Seq, &ts, &e.Agent, &e.Ev, &e.Ref, &e.Msg); err != nil {
				return err
			}
			e.T = time.Unix(ts, 0)
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// MarkApplied / Applied track replayed event IDs per worktree item.
func (d *DB) MarkApplied(item string, eids []string) error {
	return d.retry(func() error {
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, eid := range eids {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO applied(wt,eid) VALUES(?,?)`, item, eid); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

func (d *DB) Applied(item string) (map[string]bool, error) {
	out := map[string]bool{}
	err := d.retry(func() error {
		rows, err := d.db.Query(`SELECT eid FROM applied WHERE wt=?`, item)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var eid string
			if err := rows.Scan(&eid); err != nil {
				return err
			}
			out[eid] = true
		}
		return rows.Err()
	})
	return out, err
}

// LockIntegrate acquires the global integration lock (one submit merges at a
// time). Returns (false, holder) when busy; a lock whose ttl expired counts
// as free (crashed integrator).
func (d *DB) LockIntegrate(ttl time.Duration) (bool, string, error) {
	now := time.Now().Unix()
	exp := time.Now().Add(ttl).Unix()
	ok := false
	holder := ""
	err := d.retry(func() error {
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var a string
		var e int64
		errRow := tx.QueryRow(`SELECT agent, exp FROM locks WHERE name='integrate'`).Scan(&a, &e)
		if errRow == nil && e > now && a != d.Agent {
			ok, holder = false, a
			return tx.Commit()
		}
		if _, err := tx.Exec(`INSERT INTO locks(name,agent,exp) VALUES('integrate',?,?)
			ON CONFLICT(name) DO UPDATE SET agent=excluded.agent, exp=excluded.exp`, d.Agent, exp); err != nil {
			return err
		}
		ok, holder = true, d.Agent
		return tx.Commit()
	})
	return ok, holder, err
}

// UnlockIntegrate releases the integration lock if held by this agent.
func (d *DB) UnlockIntegrate() error {
	return d.retry(func() error {
		_, err := d.db.Exec(`DELETE FROM locks WHERE name='integrate' AND agent=?`, d.Agent)
		return err
	})
}

// PutWorktree / GetWorktree / DelWorktree / Worktrees manage worktree records.
func (d *DB) PutWorktree(w Worktree) error {
	if w.Created.IsZero() {
		w.Created = time.Now()
	}
	return d.retry(func() error {
		_, err := d.db.Exec(`INSERT INTO worktrees(item,agent,branch,root,base,state,created)
			VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(item) DO UPDATE SET agent=excluded.agent, branch=excluded.branch,
			root=excluded.root, base=excluded.base, state=excluded.state`,
			w.Item, w.Agent, w.Branch, w.Root, w.Base, w.State, w.Created.Unix())
		return err
	})
}

func (d *DB) GetWorktree(item string) (Worktree, bool, error) {
	var w Worktree
	var created int64
	found := false
	err := d.retry(func() error {
		err := d.db.QueryRow(`SELECT item,agent,branch,root,base,state,created FROM worktrees WHERE item=?`, item).
			Scan(&w.Item, &w.Agent, &w.Branch, &w.Root, &w.Base, &w.State, &created)
		if err == sql.ErrNoRows {
			found = false
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		w.Created = time.Unix(created, 0)
		return nil
	})
	return w, found, err
}

func (d *DB) DelWorktree(item string) error {
	return d.retry(func() error {
		_, err := d.db.Exec(`DELETE FROM worktrees WHERE item=?`, item)
		return err
	})
}

func (d *DB) Worktrees() ([]Worktree, error) {
	var out []Worktree
	err := d.retry(func() error {
		out = out[:0]
		rows, err := d.db.Query(`SELECT item,agent,branch,root,base,state,created FROM worktrees`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var w Worktree
			var created int64
			if err := rows.Scan(&w.Item, &w.Agent, &w.Branch, &w.Root, &w.Base, &w.State, &created); err != nil {
				return err
			}
			w.Created = time.Unix(created, 0)
			out = append(out, w)
		}
		return rows.Err()
	})
	return out, err
}

// GenName generates an agent name when SPECTACLE_AGENT is unset.
func GenName() string { return fmt.Sprintf("ag-%04x", rand.Intn(1<<16)) }

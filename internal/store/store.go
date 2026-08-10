package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kevinburke/ssh_config"
	_ "modernc.org/sqlite"
)

// Host represents a saved SSH connection target.
type Host struct {
	ID           int64
	Alias        string
	HostName     string
	User         string
	Port         int
	IdentityFile string
	Tags         []string
	Group        string
	Visits       int
	LastConnect  int64
	// Pinned lifts a host out of the frecency order into the PINNED section at the
	// top of the list; PinOrder is its place inside that section, 1-based and dense
	// (see renumberPins). PinOrder is meaningless — and zero — on an unpinned host.
	Pinned   bool
	PinOrder int

	// Forwards are the TCP tunnels defined for this host. They are loaded with the
	// host so the dashboard and tunnel manager can render without querying from
	// View, and are stored in their own table so editing a host never rewrites them.
	Forwards []Forward
}

// ForwardKind is which side of the SSH connection owns the listening socket.
// A local forward listens on the machine running hop and dials its target through
// SSH; a remote forward listens on the server and dials its target from hop.
type ForwardKind string

const (
	ForwardLocal  ForwardKind = "local"
	ForwardRemote ForwardKind = "remote"
)

// Forward is one persisted TCP port-forwarding definition.
type Forward struct {
	ID         int64
	HostID     int64
	Kind       ForwardKind
	BindHost   string
	BindPort   int
	TargetHost string
	TargetPort int
}

// Validate rejects definitions that cannot name TCP endpoints. BindHost may be
// blank: the runtime applies the safe loopback default for the forward's side.
func (f Forward) Validate() error {
	if f.Kind != ForwardLocal && f.Kind != ForwardRemote {
		return fmt.Errorf("forward kind must be local or remote")
	}
	if f.BindPort < 1 || f.BindPort > 65535 {
		return fmt.Errorf("bind port must be between 1 and 65535")
	}
	if strings.TrimSpace(f.TargetHost) == "" {
		return fmt.Errorf("target host can't be empty")
	}
	if f.TargetPort < 1 || f.TargetPort > 65535 {
		return fmt.Errorf("target port must be between 1 and 65535")
	}
	return nil
}

// Store wraps the SQLite database holding hosts.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS hosts (
	id            INTEGER PRIMARY KEY,
	alias         TEXT UNIQUE NOT NULL,
	hostname      TEXT,
	user          TEXT,
	port          INTEGER DEFAULT 22,
	identity_file TEXT,
	tags          TEXT,
	grp           TEXT,
	visits        INTEGER DEFAULT 0,
	last_connect  INTEGER DEFAULT 0,
	pinned        INTEGER DEFAULT 0,
	pin_order     INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS forwards (
	id          INTEGER PRIMARY KEY,
	host_id     INTEGER NOT NULL,
	kind        TEXT NOT NULL CHECK (kind IN ('local', 'remote')),
	bind_host   TEXT,
	bind_port   INTEGER NOT NULL,
	target_host TEXT NOT NULL,
	target_port INTEGER NOT NULL,
	FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
	UNIQUE (host_id, kind, bind_host, bind_port)
);`

// addedColumns are the columns that arrived after the first release. CREATE TABLE
// IF NOT EXISTS is a no-op on a database that already has the table, so a schema
// that only grows there is a schema no existing install ever gets — hence the
// ALTER pass in migrate.
var addedColumns = []struct{ name, ddl string }{
	{"pinned", `ALTER TABLE hosts ADD COLUMN pinned INTEGER DEFAULT 0`},
	{"pin_order", `ALTER TABLE hosts ADD COLUMN pin_order INTEGER DEFAULT 0`},
}

// migrate adds any column in addedColumns the table does not have yet. It asks
// PRAGMA table_info rather than running the ALTERs and swallowing the duplicate
// column error, which is a driver-specific string and would hide real failures.
func migrate(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(hosts)`)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for rows.Next() {
		var (
			cid       int
			name, typ string
			notNull   int
			dflt      sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, c := range addedColumns {
		if have[c.name] {
			continue
		}
		if _, err := db.Exec(c.ddl); err != nil {
			return err
		}
	}
	return nil
}

// Open opens (creating if needed) the hop database at
// <UserConfigDir>/hop/hop.db and ensures the schema exists.
func Open() (*Store, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cfgDir, "hop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return OpenAt(filepath.Join(dir, "hop.db"))
}

// OpenAt opens (creating if needed) the hop database at path and ensures the
// schema exists.
func OpenAt(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	// Drop the table behind the withdrawn "recent directories" feature, so a
	// database written by an older build does not keep its browsing history.
	if _, err := db.Exec(`DROP TABLE IF EXISTS dirs`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Hosts returns all hosts: the pinned ones first in the order the user put them
// in, then the rest sorted by Visits desc then LastConnect desc.
func (s *Store) Hosts() ([]Host, error) {
	rows, err := s.db.Query(`
		SELECT id, alias, hostname, user, port, identity_file, tags, grp, visits, last_connect,
		       pinned, pin_order
		FROM hosts
		ORDER BY pinned DESC, pin_order ASC, visits DESC, last_connect DESC`)
	if err != nil {
		return nil, err
	}
	var hosts []Host
	for rows.Next() {
		var (
			h    Host
			tags string
		)
		if err := rows.Scan(
			&h.ID, &h.Alias, &h.HostName, &h.User, &h.Port,
			&h.IdentityFile, &tags, &h.Group, &h.Visits, &h.LastConnect,
			&h.Pinned, &h.PinOrder,
		); err != nil {
			return nil, err
		}
		h.Tags = splitTags(tags)
		hosts = append(hosts, h)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	byID := make(map[int64]int, len(hosts))
	for i := range hosts {
		byID[hosts[i].ID] = i
	}
	forwardRows, err := s.db.Query(`
		SELECT id, host_id, kind, bind_host, bind_port, target_host, target_port
		FROM forwards
		ORDER BY host_id, id`)
	if err != nil {
		return nil, err
	}
	defer forwardRows.Close()
	for forwardRows.Next() {
		var f Forward
		if err := forwardRows.Scan(&f.ID, &f.HostID, &f.Kind, &f.BindHost, &f.BindPort, &f.TargetHost, &f.TargetPort); err != nil {
			return nil, err
		}
		if i, ok := byID[f.HostID]; ok {
			hosts[i].Forwards = append(hosts[i].Forwards, f)
		}
	}
	if err := forwardRows.Err(); err != nil {
		return nil, err
	}
	return hosts, nil
}

// Upsert inserts or updates a host keyed by its Alias and returns the row id.
func (s *Store) Upsert(h Host) (int64, error) {
	port := h.Port
	if port == 0 {
		port = 22
	}
	tags := joinTags(h.Tags)

	_, err := s.db.Exec(`
		INSERT INTO hosts (alias, hostname, user, port, identity_file, tags, grp, visits, last_connect)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(alias) DO UPDATE SET
			hostname      = excluded.hostname,
			user          = excluded.user,
			port          = excluded.port,
			identity_file = excluded.identity_file,
			tags          = excluded.tags,
			grp           = excluded.grp`,
		h.Alias, h.HostName, h.User, port, h.IdentityFile, tags, h.Group, h.Visits, h.LastConnect,
	)
	if err != nil {
		return 0, err
	}

	// On an ON CONFLICT update LastInsertId is unreliable, so resolve the id
	// authoritatively by alias.
	var rowID int64
	if qerr := s.db.QueryRow(`SELECT id FROM hosts WHERE alias = ?`, h.Alias).Scan(&rowID); qerr != nil {
		return 0, qerr
	}
	return rowID, nil
}

// Add inserts a new host, failing when the alias is already taken. Unlike Upsert
// it never overwrites: it is the path for "create a host the user believes is new",
// so a stale in-memory list cannot silently clobber a host that was added since —
// from the CLI, say, while the TUI was open. The UNIQUE constraint on alias is the
// real guarantee; the pre-check is only there to turn a driver-specific constraint
// error into a message worth reading. Returns the new row id.
func (s *Store) Add(h Host) (int64, error) {
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM hosts WHERE alias = ?`, h.Alias).Scan(&exists); err == nil {
		return 0, fmt.Errorf("host %q already exists", h.Alias)
	} else if err != sql.ErrNoRows {
		return 0, err
	}

	port := h.Port
	if port == 0 {
		port = 22
	}

	res, err := s.db.Exec(`
		INSERT INTO hosts (alias, hostname, user, port, identity_file, tags, grp, visits, last_connect)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.Alias, h.HostName, h.User, port, h.IdentityFile, joinTags(h.Tags), h.Group, h.Visits, h.LastConnect,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Delete removes the host with the given alias, closing the hole a pinned host
// leaves behind in the pin order.
func (s *Store) Delete(alias string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM forwards WHERE host_id = (SELECT id FROM hosts WHERE alias = ?)`, alias); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM hosts WHERE alias = ?`, alias); err != nil {
		return err
	}
	if err := renumberPins(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// AddForward persists a new forwarding definition for hostID. A host cannot have
// two forwards competing for the same listener on the same side.
func (s *Store) AddForward(hostID int64, f Forward) (int64, error) {
	f = normalizeForward(hostID, f)
	if err := f.Validate(); err != nil {
		return 0, err
	}
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM hosts WHERE id = ?`, hostID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("forward: no such host")
		}
		return 0, err
	}
	res, err := s.db.Exec(`
		INSERT INTO forwards (host_id, kind, bind_host, bind_port, target_host, target_port)
		VALUES (?, ?, ?, ?, ?, ?)`,
		hostID, f.Kind, f.BindHost, f.BindPort, f.TargetHost, f.TargetPort)
	if err != nil {
		return 0, fmt.Errorf("add forward: %w", err)
	}
	return res.LastInsertId()
}

// UpdateForward replaces an existing definition, preserving its identity so a
// running tunnel can be matched and stopped before the new definition takes over.
func (s *Store) UpdateForward(f Forward) error {
	f = normalizeForward(f.HostID, f)
	if err := f.Validate(); err != nil {
		return err
	}
	res, err := s.db.Exec(`
		UPDATE forwards
		SET kind = ?, bind_host = ?, bind_port = ?, target_host = ?, target_port = ?
		WHERE id = ? AND host_id = ?`,
		f.Kind, f.BindHost, f.BindPort, f.TargetHost, f.TargetPort, f.ID, f.HostID)
	if err != nil {
		return fmt.Errorf("update forward: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("update forward: no such forward")
	}
	return nil
}

func normalizeForward(hostID int64, f Forward) Forward {
	f.HostID = hostID
	f.BindHost = strings.TrimSpace(f.BindHost)
	if f.BindHost == "" {
		f.BindHost = "127.0.0.1"
	}
	if f.BindHost == "*" {
		f.BindHost = "0.0.0.0"
	}
	f.TargetHost = strings.TrimSpace(f.TargetHost)
	return f
}

// upsertImportedForward syncs one OpenSSH LocalForward/RemoteForward by its
// listening endpoint. User-created definitions use AddForward and still get a
// duplicate error; re-importing config is allowed to update the target behind an
// existing listener.
func (s *Store) upsertImportedForward(hostID int64, f Forward) error {
	f = normalizeForward(hostID, f)
	if err := f.Validate(); err != nil {
		return err
	}
	_, err := s.db.Exec(`
		INSERT INTO forwards (host_id, kind, bind_host, bind_port, target_host, target_port)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(host_id, kind, bind_host, bind_port) DO UPDATE SET
			target_host = excluded.target_host,
			target_port = excluded.target_port`,
		hostID, f.Kind, f.BindHost, f.BindPort, f.TargetHost, f.TargetPort)
	return err
}

// DeleteForward removes one definition belonging to hostID.
func (s *Store) DeleteForward(hostID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM forwards WHERE id = ? AND host_id = ?`, id, hostID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("delete forward: no such forward")
	}
	return nil
}

// Rename changes a host's alias from oldAlias to newAlias, preserving its visit
// count and connect history (a plain Upsert of a new alias would start them from
// zero). It is a no-op when the two are equal, and fails when newAlias is already
// taken or oldAlias does not exist.
func (s *Store) Rename(oldAlias, newAlias string) error {
	if oldAlias == newAlias {
		return nil
	}

	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM hosts WHERE alias = ?`, newAlias).Scan(&exists); err == nil {
		return fmt.Errorf("rename: host %q already exists", newAlias)
	} else if err != sql.ErrNoRows {
		return err
	}

	res, err := s.db.Exec(`UPDATE hosts SET alias = ? WHERE alias = ?`, newAlias, oldAlias)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("rename: no such host %q", oldAlias)
	}
	return nil
}

// SetPinned pins or unpins a host. A newly pinned host goes to the *end* of the
// pinned section — pinning is "keep this one where I can find it", not "make this
// the first thing", and a pin that reshuffled the section every time would fight
// the manual order the user set with MovePin. It fails when there is no such host.
func (s *Store) SetPinned(alias string, pinned bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var (
		id  int64
		was bool
	)
	if err := tx.QueryRow(`SELECT id, pinned FROM hosts WHERE alias = ?`, alias).Scan(&id, &was); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("pin: no such host %q", alias)
		}
		return err
	}
	if was == pinned {
		return nil
	}

	if pinned {
		var next int
		if err := tx.QueryRow(`SELECT COALESCE(MAX(pin_order), 0) + 1 FROM hosts WHERE pinned = 1`).Scan(&next); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE hosts SET pinned = 1, pin_order = ? WHERE id = ?`, next, id); err != nil {
			return err
		}
	} else if _, err := tx.Exec(`UPDATE hosts SET pinned = 0, pin_order = 0 WHERE id = ?`, id); err != nil {
		return err
	}

	if err := renumberPins(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// MovePin moves a pinned host delta places within the pinned section — -1 is up,
// +1 is down — and reports whether it actually moved. An unpinned host, or one
// already at the end it is being pushed against, is a no-op rather than an error:
// it is a held-down key hitting the edge of the list, which the caller shows as
// nothing happening.
func (s *Store) MovePin(alias string, delta int) (bool, error) {
	if delta == 0 {
		return false, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// The section in the order it is drawn in, so "up" here is up on screen.
	rows, err := tx.Query(`SELECT id, alias FROM hosts WHERE pinned = 1 ORDER BY pin_order ASC, visits DESC, last_connect DESC`)
	if err != nil {
		return false, err
	}
	var (
		ids []int64
		at  = -1
	)
	for rows.Next() {
		var (
			id int64
			a  string
		)
		if err := rows.Scan(&id, &a); err != nil {
			rows.Close()
			return false, err
		}
		if a == alias {
			at = len(ids)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()

	if at < 0 {
		return false, nil
	}
	to := at + delta
	if to < 0 || to >= len(ids) {
		return false, nil
	}

	id := ids[at]
	ids = append(ids[:at], ids[at+1:]...)
	ids = append(ids[:to], append([]int64{id}, ids[to:]...)...)

	if err := writePinOrder(tx, ids); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// renumberPins rewrites pin_order as 1..n over the pinned hosts in their current
// order, so a delete or an unpin cannot leave a hole for MovePin's arithmetic to
// trip over. A host pinned before this column existed sorts by frecency, which is
// the order it was already in.
func renumberPins(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id FROM hosts WHERE pinned = 1 ORDER BY pin_order ASC, visits DESC, last_connect DESC`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	return writePinOrder(tx, ids)
}

// writePinOrder stamps ids with pin_order 1..n, in the order given.
func writePinOrder(tx *sql.Tx, ids []int64) error {
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE hosts SET pin_order = ? WHERE id = ?`, i+1, id); err != nil {
			return err
		}
	}
	return nil
}

// Touch increments the visit count and records the current connect time.
func (s *Store) Touch(alias string) error {
	_, err := s.db.Exec(
		`UPDATE hosts SET visits = visits + 1, last_connect = ? WHERE alias = ?`,
		time.Now().Unix(), alias,
	)
	return err
}

// ImportSSHConfig parses an OpenSSH config file and upserts each concrete
// Host alias (wildcard patterns containing '*' or '?' are skipped).
// It returns the number of hosts imported.
func (s *Store) ImportSSHConfig(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, host := range cfg.Hosts {
		for _, pat := range host.Patterns {
			alias := pat.String()
			if alias == "" || strings.ContainsAny(alias, "*?") {
				continue
			}

			hostName, _ := cfg.Get(alias, "HostName")
			user, _ := cfg.Get(alias, "User")
			portStr, _ := cfg.Get(alias, "Port")
			identity, _ := cfg.Get(alias, "IdentityFile")

			port := 22
			if portStr != "" {
				if p, perr := strconv.Atoi(strings.TrimSpace(portStr)); perr == nil && p > 0 {
					port = p
				}
			}
			if hostName == "" {
				hostName = alias
			}

			hostID, err := s.Upsert(Host{
				Alias:        alias,
				HostName:     hostName,
				User:         user,
				Port:         port,
				IdentityFile: identity,
			})
			if err != nil {
				return count, err
			}
			for _, directive := range []struct {
				key  string
				kind ForwardKind
			}{{"LocalForward", ForwardLocal}, {"RemoteForward", ForwardRemote}} {
				values, _ := cfg.GetAll(alias, directive.key)
				for _, value := range values {
					forward, ok := parseSSHForward(value, directive.kind)
					if !ok {
						continue // dynamic and Unix-socket forwarding are not TCP tunnels
					}
					if err := s.upsertImportedForward(hostID, forward); err != nil {
						return count, err
					}
				}
			}
			count++
		}
	}
	return count, nil
}

// parseSSHForward accepts OpenSSH's TCP forwarding shape:
// [bind_address:]port host:hostport. Socket-path and dynamic forms are left to
// OpenSSH rather than being misrepresented as TCP definitions in hop.
func parseSSHForward(value string, kind ForwardKind) (Forward, bool) {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return Forward{}, false
	}
	bindHost, bindPort, ok := splitForwardEndpoint(fields[0], true)
	if !ok {
		return Forward{}, false
	}
	targetHost, targetPort, ok := splitForwardEndpoint(fields[1], false)
	if !ok {
		return Forward{}, false
	}
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	return Forward{Kind: kind, BindHost: bindHost, BindPort: bindPort, TargetHost: targetHost, TargetPort: targetPort}, true
}

func splitForwardEndpoint(value string, portOnly bool) (string, int, bool) {
	if portOnly {
		if port, err := strconv.Atoi(value); err == nil && port >= 1 && port <= 65535 {
			return "", port, true
		}
	}
	host, portText, err := netSplitHostPortLoose(value)
	if err != nil {
		return "", 0, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || (!portOnly && strings.TrimSpace(host) == "") {
		return "", 0, false
	}
	return host, port, true
}

// netSplitHostPortLoose is net.SplitHostPort plus OpenSSH's common unbracketed
// hostname:port spelling. IPv6 remains bracketed, as OpenSSH documents it.
func netSplitHostPortLoose(value string) (string, string, error) {
	if strings.HasPrefix(value, "[") {
		end := strings.LastIndex(value, "]:")
		if end < 0 {
			return "", "", fmt.Errorf("missing port")
		}
		return value[1:end], value[end+2:], nil
	}
	i := strings.LastIndex(value, ":")
	if i < 0 {
		return "", "", fmt.Errorf("missing port")
	}
	return value[:i], value[i+1:], nil
}

func joinTags(tags []string) string {
	return strings.Join(tags, ",")
}

func splitTags(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

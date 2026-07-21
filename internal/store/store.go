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
	last_connect  INTEGER DEFAULT 0
);`

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

// Hosts returns all hosts sorted by Visits desc then LastConnect desc.
func (s *Store) Hosts() ([]Host, error) {
	rows, err := s.db.Query(`
		SELECT id, alias, hostname, user, port, identity_file, tags, grp, visits, last_connect
		FROM hosts
		ORDER BY visits DESC, last_connect DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []Host
	for rows.Next() {
		var (
			h    Host
			tags string
		)
		if err := rows.Scan(
			&h.ID, &h.Alias, &h.HostName, &h.User, &h.Port,
			&h.IdentityFile, &tags, &h.Group, &h.Visits, &h.LastConnect,
		); err != nil {
			return nil, err
		}
		h.Tags = splitTags(tags)
		hosts = append(hosts, h)
	}
	if err := rows.Err(); err != nil {
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

// Delete removes the host with the given alias.
func (s *Store) Delete(alias string) error {
	_, err := s.db.Exec(`DELETE FROM hosts WHERE alias = ?`, alias)
	return err
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

			if _, err := s.Upsert(Host{
				Alias:        alias,
				HostName:     hostName,
				User:         user,
				Port:         port,
				IdentityFile: identity,
			}); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
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

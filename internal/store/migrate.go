package store

// The one-way migration from the SQLite database older versions of hop kept.
//
// It runs once, on the first start after the upgrade, and is deliberately conservative:
// it reads the database with the dependency-free reader in sqlitefile.go, writes the two
// new files, and only then moves the database aside — under a .bak name, never deleted,
// so a migration that turns out to have missed something is still recoverable by hand.
//
// Anything it does not fully understand is an error that stops hop from starting, rather
// than a partial import that silently loses hosts. A user who sees that error still has
// their database exactly as it was.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// migrateLegacyDB converts the hop.db at dbPath into the hosts file at hostsPath and the
// metadata sidecar at metaPath. It is a no-op when there is no database, or when the
// hosts file already exists — the migration has then already run, and the config file is
// the newer truth.
//
// dbPath and hostsPath may be the same file: OpenAt hands both the same path when it is
// asked to open what turns out to be a database.
func migrateLegacyDB(dbPath, hostsPath, metaPath string) error {
	if dbPath == "" || !isSQLiteFile(dbPath) {
		return nil
	}
	if dbPath != hostsPath {
		if _, err := os.Stat(hostsPath); err == nil {
			return nil // already migrated; the database is a leftover
		}
	}

	hosts, m, err := readLegacyDB(dbPath)
	if err != nil {
		return fmt.Errorf("migrating %s: %w\n\nYour database has not been changed. Please report this with the error above; the previous hop release can still read it.", dbPath, err)
	}

	// The database moves aside first when it is in the way of its own replacement.
	backup := dbPath + ".bak"
	if err := os.Rename(dbPath, backup); err != nil {
		return fmt.Errorf("migrating %s: %w", dbPath, err)
	}
	if err := writeHosts(hostsPath, hosts); err != nil {
		return fmt.Errorf("migrating %s: %w", dbPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o700); err != nil {
		return fmt.Errorf("migrating %s: %w", dbPath, err)
	}
	if err := m.save(metaPath); err != nil {
		return fmt.Errorf("migrating %s: %w", dbPath, err)
	}
	return nil
}

// readLegacyDB lifts the hosts and forwards tables out of a hop.db.
func readLegacyDB(path string) ([]Host, *meta, error) {
	db, err := openSQLite(path)
	if err != nil {
		return nil, nil, err
	}
	tables, err := db.tables()
	if err != nil {
		return nil, nil, err
	}
	hostRows, err := readSQLiteTable(db, tables, "hosts")
	if err != nil {
		return nil, nil, err
	}
	forwardRows, err := readSQLiteTable(db, tables, "forwards")
	if err != nil {
		return nil, nil, err
	}

	m := &meta{Version: metaVersion, Hosts: map[string]*hostMeta{}}
	hosts := make([]Host, 0, len(hostRows))
	byID := map[int64]int{}
	for _, r := range hostRows {
		alias := text(r["alias"])
		if alias == "" {
			continue // a row with no alias names no host
		}
		id, _ := toInt(r["id"])
		port := 22
		if p, ok := toInt(r["port"]); ok && p > 0 && p <= 65535 {
			port = int(p)
		}
		visits, _ := toInt(r["visits"])
		lastConnect, _ := toInt(r["last_connect"])
		pinOrder, _ := toInt(r["pin_order"])
		pinned, _ := toInt(r["pinned"])

		h := Host{
			ID:           id,
			Alias:        alias,
			HostName:     text(r["hostname"]),
			User:         text(r["user"]),
			Port:         port,
			IdentityFile: text(r["identity_file"]),
			Tags:         splitTags(text(r["tags"])),
			Group:        text(r["grp"]),
			Visits:       int(visits),
			LastConnect:  lastConnect,
			DefaultDir:   text(r["default_dir"]),
			ProxyCommand: normalizeProxyCommand(text(r["proxy_command"])),
			ProxyJump:    text(r["proxy_jump"]),
			Pinned:       pinned != 0,
			PinOrder:     int(pinOrder),
		}
		if h.HostName == "" {
			h.HostName = alias
		}
		byID[id] = len(hosts)
		hosts = append(hosts, h)

		if m.NextID <= id {
			m.NextID = id + 1
		}
		m.Hosts[alias] = &hostMeta{
			ID:          id,
			Tags:        h.Tags,
			Group:       h.Group,
			Visits:      h.Visits,
			LastConnect: h.LastConnect,
			Pinned:      h.Pinned,
			PinOrder:    h.PinOrder,
			DefaultDir:  h.DefaultDir,
		}
	}

	for _, r := range forwardRows {
		hostID, ok := toInt(r["host_id"])
		if !ok {
			continue
		}
		at, ok := byID[hostID]
		if !ok {
			continue // a forward whose host is gone was already dead weight
		}
		bindPort, _ := toInt(r["bind_port"])
		targetPort, _ := toInt(r["target_port"])
		id, _ := toInt(r["id"])
		kind := ForwardKind(text(r["kind"]))
		if kind != ForwardLocal && kind != ForwardRemote {
			continue
		}
		f := normalizeForward(hostID, Forward{
			ID:         id,
			Kind:       kind,
			BindHost:   text(r["bind_host"]),
			BindPort:   int(bindPort),
			TargetHost: text(r["target_host"]),
			TargetPort: int(targetPort),
		})
		if err := f.Validate(); err != nil {
			continue // a definition that cannot name endpoints could never have run
		}
		hosts[at].Forwards = append(hosts[at].Forwards, f)
	}

	// Write the file in id order, which is roughly the order the hosts were added.
	sort.SliceStable(hosts, func(i, j int) bool { return hosts[i].ID < hosts[j].ID })
	return hosts, m, nil
}

// text narrows a decoded value to a string, tolerating the NULL that an ALTER-added
// column has in rows written before it existed.
func text(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return ""
	}
}

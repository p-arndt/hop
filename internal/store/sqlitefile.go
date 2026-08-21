package store

// A dependency-free, read-only reader for the subset of the SQLite file format a legacy
// hop.db uses (https://sqlite.org/fileformat.html): table b-trees, no indexes, no WAL
// replay. It errors rather than guesses; a wrong answer here is silent data loss.

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const sqliteMagic = "SQLite format 3\x00"

// isSQLiteFile tells a legacy hop.db from an OpenSSH config without trusting the name.
func isSQLiteFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [16]byte
	if _, err := f.Read(head[:]); err != nil {
		return false
	}
	return string(head[:]) == sqliteMagic
}

// sqliteDB is a database file held in memory.
type sqliteDB struct {
	data     []byte
	pageSize int
	// usable is pageSize minus the reserved per-page tail; every payload threshold uses it.
	usable int
}

// openSQLite reads path into memory and validates the header fields the reader relies on.
func openSQLite(path string) (*sqliteDB, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 100 || string(data[:16]) != sqliteMagic {
		return nil, fmt.Errorf("not a SQLite database")
	}

	// Page size is a big-endian u16 at offset 16, where the value 1 encodes 65536.
	pageSize := int(binary.BigEndian.Uint16(data[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	if pageSize < 512 || pageSize&(pageSize-1) != 0 {
		return nil, fmt.Errorf("bad page size %d", pageSize)
	}
	reserved := int(data[20])
	usable := pageSize - reserved
	if usable < 480 {
		return nil, fmt.Errorf("bad reserved size %d", reserved)
	}
	if len(data)%pageSize != 0 || len(data) < pageSize {
		return nil, fmt.Errorf("truncated database: %d bytes at page size %d", len(data), pageSize)
	}

	// Offsets 18/19 are the write and read format versions: 1 rollback journal, 2 WAL.
	if data[18] > 2 || data[19] > 2 {
		return nil, fmt.Errorf("unsupported file format version %d/%d", data[18], data[19])
	}

	// A WAL or hot journal holds committed data this reader cannot replay; migrating
	// without it would silently drop the last session's writes.
	for _, sidecar := range []string{path + "-wal", path + "-journal"} {
		if info, err := os.Stat(sidecar); err == nil && info.Size() > 0 {
			return nil, fmt.Errorf("database has pending %s; open it once with the previous hop to flush it", filepath.Ext(sidecar)[1:])
		}
	}
	return &sqliteDB{data: data, pageSize: pageSize, usable: usable}, nil
}

// page returns page n, 1-based as SQLite numbers them.
func (db *sqliteDB) page(n int) ([]byte, error) {
	if n < 1 || n*db.pageSize > len(db.data) {
		return nil, fmt.Errorf("page %d out of range", n)
	}
	return db.data[(n-1)*db.pageSize : n*db.pageSize], nil
}

// sqliteTable is one table's schema as recorded in sqlite_master.
type sqliteTable struct {
	name     string
	rootPage int
	columns  []string
	// rowidAlias indexes an INTEGER PRIMARY KEY column (-1 if none): it is absent from the
	// record and must be filled in from the rowid, or every id reads NULL.
	rowidAlias int
}

// tables reads sqlite_master, whose b-tree always starts on page 1.
func (db *sqliteDB) tables() (map[string]sqliteTable, error) {
	rows, err := db.readTree(1, 0)
	if err != nil {
		return nil, err
	}
	out := make(map[string]sqliteTable, len(rows))
	for _, r := range rows {
		// sqlite_master is (type, name, tbl_name, rootpage, sql).
		if len(r.values) < 5 {
			continue
		}
		kind, _ := r.values[0].(string)
		name, _ := r.values[1].(string)
		if kind != "table" {
			continue
		}
		root, ok := toInt(r.values[3])
		if !ok || root <= 0 {
			continue
		}
		sql, _ := r.values[4].(string)
		cols, alias := parseCreateTable(sql)
		if len(cols) == 0 {
			continue
		}
		out[name] = sqliteTable{name: name, rootPage: int(root), columns: cols, rowidAlias: alias}
	}
	return out, nil
}

// readSQLiteTable returns every row keyed by column name; a missing table is not an error,
// since a hop.db written before forwards existed has none.
func readSQLiteTable(db *sqliteDB, tables map[string]sqliteTable, name string) ([]map[string]any, error) {
	t, ok := tables[name]
	if !ok {
		return nil, nil
	}
	rows, err := db.readTree(t.rootPage, 0)
	if err != nil {
		return nil, fmt.Errorf("table %s: %w", name, err)
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		m := make(map[string]any, len(t.columns))
		for i, col := range t.columns {
			switch {
			case i == t.rowidAlias:
				m[col] = r.rowid
			case i < len(r.values):
				m[col] = r.values[i]
			default:
				// A column ALTER-added after this row was written is absent from its
				// record; every column hop added defaults to the zero value.
				m[col] = nil
			}
		}
		out = append(out, m)
	}
	return out, nil
}

type sqliteRow struct {
	rowid  int64
	values []any
}

// maxTreeDepth bounds the walk: a corrupt file can point a page at an ancestor.
const maxTreeDepth = 32

// readTree walks the table b-tree rooted at page n and returns its rows in key order.
func (db *sqliteDB) readTree(n, depth int) ([]sqliteRow, error) {
	if depth > maxTreeDepth {
		return nil, fmt.Errorf("b-tree too deep: cycle in page %d", n)
	}
	page, err := db.page(n)
	if err != nil {
		return nil, err
	}
	// Page 1 carries the 100-byte file header before its b-tree header.
	offset := 0
	if n == 1 {
		offset = 100
	}
	if offset+12 > len(page) {
		return nil, fmt.Errorf("page %d too small", n)
	}

	kind := page[offset]
	cells := int(binary.BigEndian.Uint16(page[offset+3 : offset+5]))
	// The header is 8 bytes for a leaf, 12 for an interior page (the extra 4 are its
	// rightmost child), then the cell pointer array.
	headerSize := 8
	if kind == 0x05 || kind == 0x02 {
		headerSize = 12
	}
	pointers := offset + headerSize

	switch kind {
	case 0x0D: // table leaf
		var rows []sqliteRow
		for i := 0; i < cells; i++ {
			at := pointers + i*2
			if at+2 > len(page) {
				return nil, fmt.Errorf("page %d: cell pointer %d out of range", n, i)
			}
			cell := int(binary.BigEndian.Uint16(page[at : at+2]))
			row, err := db.readLeafCell(page, cell)
			if err != nil {
				return nil, fmt.Errorf("page %d cell %d: %w", n, i, err)
			}
			rows = append(rows, row)
		}
		return rows, nil

	case 0x05: // table interior
		var rows []sqliteRow
		for i := 0; i < cells; i++ {
			at := pointers + i*2
			if at+2 > len(page) {
				return nil, fmt.Errorf("page %d: cell pointer %d out of range", n, i)
			}
			cell := int(binary.BigEndian.Uint16(page[at : at+2]))
			if cell+4 > len(page) {
				return nil, fmt.Errorf("page %d: interior cell %d out of range", n, i)
			}
			child := int(binary.BigEndian.Uint32(page[cell : cell+4]))
			sub, err := db.readTree(child, depth+1)
			if err != nil {
				return nil, err
			}
			rows = append(rows, sub...)
		}
		// The rightmost child hangs off the header, not the cell array.
		right := int(binary.BigEndian.Uint32(page[offset+8 : offset+12]))
		sub, err := db.readTree(right, depth+1)
		if err != nil {
			return nil, err
		}
		return append(rows, sub...), nil

	default:
		// 0x0A and 0x02 are index pages, which hop's two tables never root.
		return nil, fmt.Errorf("page %d: unsupported page type 0x%02x", n, kind)
	}
}

// readLeafCell decodes one table-leaf cell: payload length, rowid, then the record.
func (db *sqliteDB) readLeafCell(page []byte, at int) (sqliteRow, error) {
	if at < 0 || at >= len(page) {
		return sqliteRow{}, fmt.Errorf("cell offset out of range")
	}
	payloadLen, n1 := getVarint(page[at:])
	if n1 == 0 {
		return sqliteRow{}, fmt.Errorf("bad payload length")
	}
	rowid, n2 := getVarint(page[at+n1:])
	if n2 == 0 {
		return sqliteRow{}, fmt.Errorf("bad rowid")
	}
	body := at + n1 + n2

	payload, err := db.readPayload(page, body, int(payloadLen))
	if err != nil {
		return sqliteRow{}, err
	}
	values, err := decodeRecord(payload)
	if err != nil {
		return sqliteRow{}, err
	}
	return sqliteRow{rowid: rowid, values: values}, nil
}

// readPayload assembles a record spanning overflow pages; the split point is SQLite's,
// derived from the usable page size.
func (db *sqliteDB) readPayload(page []byte, at, total int) ([]byte, error) {
	if total < 0 {
		return nil, fmt.Errorf("negative payload length")
	}
	u := db.usable
	maxLocal := u - 35
	if total <= maxLocal {
		if at+total > len(page) {
			return nil, fmt.Errorf("payload runs past page end")
		}
		return page[at : at+total], nil
	}

	minLocal := ((u-12)*32)/255 - 23
	local := minLocal + (total-minLocal)%(u-4)
	if local > maxLocal {
		local = minLocal
	}
	if at+local+4 > len(page) {
		return nil, fmt.Errorf("overflow payload runs past page end")
	}
	out := make([]byte, 0, total)
	out = append(out, page[at:at+local]...)

	next := int(binary.BigEndian.Uint32(page[at+local : at+local+4]))
	// Each overflow page is a 4-byte next pointer then data; bound the chain by the page
	// count so a cycle errors rather than hangs.
	for hops := 0; next != 0; hops++ {
		if hops > len(db.data)/db.pageSize+1 {
			return nil, fmt.Errorf("overflow chain does not terminate")
		}
		op, err := db.page(next)
		if err != nil {
			return nil, err
		}
		next = int(binary.BigEndian.Uint32(op[0:4]))
		take := u - 4
		if remaining := total - len(out); take > remaining {
			take = remaining
		}
		if 4+take > len(op) {
			return nil, fmt.Errorf("overflow page short")
		}
		out = append(out, op[4:4+take]...)
		if len(out) >= total {
			break
		}
	}
	if len(out) != total {
		return nil, fmt.Errorf("payload short: got %d of %d", len(out), total)
	}
	return out, nil
}

// decodeRecord splits a record into its values, per the serial-type encoding.
func decodeRecord(rec []byte) ([]any, error) {
	headerLen, n := getVarint(rec)
	if n == 0 || int(headerLen) > len(rec) || headerLen < int64(n) {
		return nil, fmt.Errorf("bad record header")
	}
	var (
		types []int64
		at    = n
	)
	for at < int(headerLen) {
		t, m := getVarint(rec[at:])
		if m == 0 {
			return nil, fmt.Errorf("bad serial type")
		}
		types = append(types, t)
		at += m
	}

	values := make([]any, 0, len(types))
	body := int(headerLen)
	for _, t := range types {
		size, err := serialSize(t)
		if err != nil {
			return nil, err
		}
		if body+size > len(rec) {
			return nil, fmt.Errorf("record value runs past end")
		}
		raw := rec[body : body+size]
		body += size

		switch {
		case t == 0:
			values = append(values, nil)
		case t >= 1 && t <= 6:
			values = append(values, beInt(raw))
		case t == 7:
			values = append(values, math.Float64frombits(binary.BigEndian.Uint64(raw)))
		case t == 8:
			values = append(values, int64(0))
		case t == 9:
			values = append(values, int64(1))
		case t >= 12 && t%2 == 0:
			values = append(values, append([]byte(nil), raw...))
		case t >= 13 && t%2 == 1:
			values = append(values, string(raw))
		default:
			// Serial types 10 and 11 are internal and never appear in a table row.
			return nil, fmt.Errorf("reserved serial type %d", t)
		}
	}
	return values, nil
}

func serialSize(t int64) (int, error) {
	switch {
	case t == 0, t == 8, t == 9:
		return 0, nil
	case t >= 1 && t <= 4:
		return int(t), nil
	case t == 5:
		return 6, nil
	case t == 6, t == 7:
		return 8, nil
	case t >= 12:
		if t%2 == 0 {
			return int(t-12) / 2, nil
		}
		return int(t-13) / 2, nil
	default:
		return 0, fmt.Errorf("reserved serial type %d", t)
	}
}

// beInt reads a big-endian two's-complement integer of 1..8 bytes.
func beInt(b []byte) int64 {
	var v int64
	if len(b) > 0 && b[0]&0x80 != 0 {
		v = -1 // sign-extend by starting from all ones
	}
	for _, c := range b {
		v = v<<8 | int64(c)
	}
	return v
}

// getVarint decodes SQLite's base-128 varint: eight bytes of seven bits, a ninth of eight;
// it returns the width consumed, or 0 when the buffer ends mid-value.
func getVarint(b []byte) (int64, int) {
	var v uint64
	for i := 0; i < 8; i++ {
		if i >= len(b) {
			return 0, 0
		}
		v = v<<7 | uint64(b[i]&0x7f)
		if b[i]&0x80 == 0 {
			return int64(v), i + 1
		}
	}
	if len(b) < 9 {
		return 0, 0
	}
	return int64(v<<8 | uint64(b[8])), 9
}

func toInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}

// parseCreateTable returns the column names in declaration order plus the index of the
// INTEGER PRIMARY KEY rowid alias (-1 for none); it only handles statements hop wrote.
func parseCreateTable(sql string) ([]string, int) {
	open := strings.Index(sql, "(")
	close := strings.LastIndex(sql, ")")
	if open < 0 || close < open {
		return nil, -1
	}
	var (
		cols  []string
		alias = -1
		depth int
		start = open + 1
		items []string
	)
	// Split on top-level commas only: a CHECK (kind IN (...)) carries its own.
	for i := open + 1; i <= close; i++ {
		switch {
		case sql[i] == '(':
			depth++
		case sql[i] == ')' && depth > 0:
			depth--
		case (sql[i] == ',' && depth == 0) || i == close:
			items = append(items, sql[start:i])
			start = i + 1
		}
	}

	for _, item := range items {
		fields := strings.Fields(strings.TrimSpace(item))
		if len(fields) == 0 {
			continue
		}
		name := strings.Trim(fields[0], "\"`[]")
		switch strings.ToUpper(name) {
		case "FOREIGN", "UNIQUE", "PRIMARY", "CHECK", "CONSTRAINT":
			continue // a table constraint, not a column
		}
		rest := strings.ToUpper(strings.Join(fields[1:], " "))
		if strings.HasPrefix(rest, "INTEGER PRIMARY KEY") {
			alias = len(cols)
		}
		cols = append(cols, name)
	}
	return cols, alias
}

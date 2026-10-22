package main

// A read only SQLite reader, because three of the files this program has
// to open are SQLite databases and it has no dependencies.
//
// It reads the file, not the database. There is no query engine here and
// there is no locking: it walks the b-tree of a table and hands back the
// rows. That is all that is needed, and it is also the only honest way to
// do this, because opening the real library would mean writing to the
// file. SQLite takes a lock and can roll back a hot journal on open, and a
// program that promises to leave your browser history alone must not be
// the thing that rewrites it.
//
// What it gives up by not being SQLite: the write ahead log. Recent rows
// can be sitting in a -wal file that has not been folded into the database
// yet, and those are not read. That makes this reader miss things rather
// than invent them, which is the right direction for the mistake, and it
// is said out loud in the report rather than left for somebody to work out.

import (
	"encoding/binary"
	"fmt"
	"strings"
)

type sqlite struct {
	body     []byte
	pageSize int
	usable   int
	utf16    bool
}

type table struct {
	name    string
	root    int
	columns []string
	// rowid is the column declared INTEGER PRIMARY KEY, or minus one.
	// A column declared that way is an alias for the row id, so SQLite
	// stores a null in the record and keeps the real value in the cell
	// header. Reading the record alone gives zero for every row, which is
	// silent and wrong: it is what joins Firefox's annotation tables to
	// each other, so every download came back as belonging to attribute
	// zero and nothing was ever found.
	rowid int
}

// value is one cell. Numbers come back as int64 and everything else as
// text, because every column this program reads is either a path, a name
// or a timestamp, and a float that is really a date is easier to handle as
// a number than as an interface worth of type switches at every call.
type value struct {
	num  int64
	text string
	null bool
}

func (v value) String() string { return v.text }

func openSQLite(body []byte) (*sqlite, error) {
	if len(body) < 100 || string(body[:15]) != "SQLite format 3" {
		return nil, fmt.Errorf("not a SQLite database")
	}
	s := &sqlite{body: body}
	s.pageSize = int(binary.BigEndian.Uint16(body[16:]))
	if s.pageSize == 1 {
		s.pageSize = 65536 // the one page size that does not fit the field
	}
	if s.pageSize < 512 || s.pageSize&(s.pageSize-1) != 0 {
		return nil, fmt.Errorf("page size %d is not a power of two", s.pageSize)
	}
	s.usable = s.pageSize - int(body[20])
	if s.usable < 480 {
		return nil, fmt.Errorf("the reserved space at the end of each page is too big to be real")
	}
	s.utf16 = binary.BigEndian.Uint32(body[56:]) != 1
	return s, nil
}

func (s *sqlite) page(n int) []byte {
	if n < 1 {
		return nil
	}
	start := (n - 1) * s.pageSize
	if start < 0 || start+s.usable > len(s.body) {
		return nil
	}
	return s.body[start : start+s.usable]
}

// tables reads the schema, which is itself a table rooted at page one.
func (s *sqlite) tables() map[string]table {
	out := map[string]table{}
	for _, row := range s.rows(1, 5, -1) {
		if len(row) < 5 || row[0].text != "table" {
			continue
		}
		name := strings.ToLower(row[2].text)
		columns, rowid := columnNames(row[4].text)
		out[name] = table{
			name:    row[2].text,
			root:    int(row[3].num),
			columns: columns,
			rowid:   rowid,
		}
	}
	return out
}

// read is the ordinary way to get at a table: every column, with the row
// id put back into the column that aliases it.
func (s *sqlite) read(t table) [][]value {
	return s.rows(t.root, len(t.columns), t.rowid)
}

// rows walks a table b-tree and returns every row. want is how many
// columns the caller needs; a row with fewer is dropped rather than
// indexed into, because a database with an older schema has genuinely
// short rows and a panic in the middle of somebody's browser history is
// not an acceptable way to find that out.
func (s *sqlite) rows(root, want, rowidAt int) [][]value {
	var out [][]value
	seen := map[int]bool{}
	var walk func(n int, depth int)
	walk = func(n, depth int) {
		// A corrupt or hostile file can point a page at itself or build a
		// cycle out of several. The depth limit is the belt to that
		// braces: a b-tree over any real database is a handful of levels.
		if n < 1 || seen[n] || depth > 32 || len(out) > 500000 {
			return
		}
		seen[n] = true
		p := s.page(n)
		if p == nil {
			return
		}
		at := 0
		if n == 1 {
			at = 100 // page one carries the database header in front of it
		}
		if at+8 > len(p) {
			return
		}
		kind := p[at]
		cells := int(binary.BigEndian.Uint16(p[at+3:]))
		headerLen := 8
		if kind == 0x02 || kind == 0x05 {
			headerLen = 12
		}
		pointers := at + headerLen
		if kind != 0x0D && kind != 0x05 {
			return // an index page, which no table this reads is rooted at
		}
		if kind == 0x05 {
			right := int(binary.BigEndian.Uint32(p[at+8:]))
			for i := 0; i < cells; i++ {
				off := pointers + i*2
				if off+2 > len(p) {
					break
				}
				cell := int(binary.BigEndian.Uint16(p[off:]))
				if cell+4 > len(p) {
					continue
				}
				walk(int(binary.BigEndian.Uint32(p[cell:])), depth+1)
			}
			walk(right, depth+1)
			return
		}
		for i := 0; i < cells; i++ {
			off := pointers + i*2
			if off+2 > len(p) {
				break
			}
			cell := int(binary.BigEndian.Uint16(p[off:]))
			if cell < 0 || cell >= len(p) {
				continue
			}
			row, id := s.readCell(p[cell:], want)
			if row != nil {
				if rowidAt >= 0 && rowidAt < len(row) {
					row[rowidAt] = value{num: id}
				}
				out = append(out, row)
			}
		}
	}
	walk(root, 0)
	return out
}

func (s *sqlite) readCell(cell []byte, want int) ([]value, int64) {
	payloadLen, n := varint(cell)
	if n == 0 || payloadLen < 0 {
		return nil, 0
	}
	id, m := varint(cell[n:])
	if m == 0 {
		return nil, 0
	}
	body := cell[n+m:]

	// How much of the payload lives on this page. The rest is on a chain
	// of overflow pages, and the formula is the one in the file format:
	// it exists so that a row is never split in a way that leaves a
	// useless few bytes on the page.
	maxLocal := s.usable - 35
	local := int(payloadLen)
	if local > maxLocal {
		minLocal := ((s.usable-12)*32)/255 - 23
		local = minLocal + (int(payloadLen)-minLocal)%(s.usable-4)
		if local > maxLocal {
			local = minLocal
		}
	}
	if local > len(body) {
		return nil, id
	}
	payload := body[:local]

	if int64(local) < payloadLen {
		if local+4 > len(body) {
			return nil, id
		}
		next := int(binary.BigEndian.Uint32(body[local:]))
		payload = s.followOverflow(payload, next, payloadLen)
	}
	return s.readRecord(payload, want), id
}

func (s *sqlite) followOverflow(payload []byte, next int, total int64) []byte {
	out := make([]byte, len(payload), total)
	copy(out, payload)
	seen := map[int]bool{}
	for next > 0 && !seen[next] && int64(len(out)) < total {
		seen[next] = true
		p := s.page(next)
		if len(p) < 4 {
			break
		}
		take := s.usable - 4
		if left := total - int64(len(out)); int64(take) > left {
			take = int(left)
		}
		if 4+take > len(p) {
			break
		}
		out = append(out, p[4:4+take]...)
		next = int(binary.BigEndian.Uint32(p))
	}
	return out
}

func (s *sqlite) readRecord(payload []byte, want int) []value {
	headerLen, n := varint(payload)
	if n == 0 || headerLen <= 0 || int(headerLen) > len(payload) {
		return nil
	}
	types := payload[n:headerLen]
	at := int(headerLen)
	out := make([]value, 0, want)
	for len(types) > 0 && len(out) < want {
		serial, used := varint(types)
		if used == 0 {
			break
		}
		types = types[used:]
		v, size := s.readValue(payload, at, serial)
		if size < 0 {
			break
		}
		at += size
		out = append(out, v)
	}
	// A row can be short. SQLite leaves the trailing columns out of the
	// record when they are null and the table grew by ALTER TABLE ADD
	// COLUMN, which is how browsers change their schema, so the rows
	// written before the column was added have fewer fields than the table
	// has columns. Dropping those rows lost real history; padding them
	// with nulls is what SQLite itself does when it reads them.
	for len(out) < want {
		out = append(out, value{null: true})
	}
	return out
}

func (s *sqlite) readValue(payload []byte, at int, serial int64) (value, int) {
	read := func(n int) []byte {
		if at < 0 || at+n > len(payload) {
			return nil
		}
		return payload[at : at+n]
	}
	switch {
	case serial == 0:
		return value{null: true}, 0
	case serial >= 1 && serial <= 6:
		sizes := []int{0, 1, 2, 3, 4, 6, 8}
		n := sizes[serial]
		b := read(n)
		if b == nil {
			return value{}, -1
		}
		return value{num: signed(b)}, n
	case serial == 7:
		b := read(8)
		if b == nil {
			return value{}, -1
		}
		// A float column here is always a date, and every one of them is
		// read as a whole number of seconds, so the bits are turned into
		// an integer rather than carrying a float through the program.
		return value{num: int64(float64FromBits(binary.BigEndian.Uint64(b)))}, 8
	case serial == 8:
		return value{num: 0}, 0
	case serial == 9:
		return value{num: 1}, 0
	case serial >= 12 && serial%2 == 0:
		n := int((serial - 12) / 2)
		b := read(n)
		if b == nil {
			return value{}, -1
		}
		return value{text: string(b)}, n
	case serial >= 13:
		n := int((serial - 13) / 2)
		b := read(n)
		if b == nil {
			return value{}, -1
		}
		return value{text: printable(s.decode(b))}, n
	}
	return value{null: true}, 0
}

func (s *sqlite) decode(b []byte) string {
	if !s.utf16 {
		return string(b)
	}
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(b[i:]))
	}
	return decodeUTF16(units)
}

func signed(b []byte) int64 {
	var n int64
	if len(b) > 0 && b[0]&0x80 != 0 {
		n = -1 // sign extend, which matters for the negative row ids
	}
	for _, x := range b {
		n = n<<8 | int64(x)
	}
	return n
}

func float64FromBits(bits uint64) float64 {
	sign := 1.0
	if bits>>63 != 0 {
		sign = -1
	}
	exponent := int((bits >> 52) & 0x7FF)
	mantissa := bits & 0xFFFFFFFFFFFFF
	if exponent == 0 {
		return sign * float64(mantissa) * pow2(-1074)
	}
	if exponent == 0x7FF {
		return 0 // an infinity or a not a number is not a date
	}
	return sign * (1 + float64(mantissa)*pow2(-52)) * pow2(exponent-1023)
}

func pow2(n int) float64 {
	out := 1.0
	for ; n > 0; n-- {
		out *= 2
	}
	for ; n < 0; n++ {
		out /= 2
	}
	return out
}

// varint is the big endian seven bits per byte number the format uses,
// with a ninth byte that contributes all eight of its bits.
func varint(b []byte) (int64, int) {
	var n int64
	for i := 0; i < 8 && i < len(b); i++ {
		n = n<<7 | int64(b[i]&0x7F)
		if b[i]&0x80 == 0 {
			return n, i + 1
		}
	}
	if len(b) >= 9 {
		return n<<8 | int64(b[8]), 9
	}
	return 0, 0
}

// columnNames pulls the names out of the CREATE TABLE that SQLite keeps
// beside every table. Reading them by position instead would mean this
// program breaks quietly on the day a browser adds a column in the middle,
// and it would break by reading the wrong field rather than by failing.
func columnNames(create string) ([]string, int) {
	rowid := -1
	open := strings.Index(create, "(")
	if open < 0 {
		return nil, rowid
	}
	body := create[open+1:]
	if close := strings.LastIndex(body, ")"); close > 0 {
		body = body[:close]
	}
	var (
		out   []string
		depth int
		field strings.Builder
	)
	flush := func() {
		whole := strings.TrimSpace(field.String())
		field.Reset()
		if whole == "" {
			return
		}
		name := whole
		rest := ""
		if cut := strings.IndexAny(name, " \t\n"); cut > 0 {
			name, rest = name[:cut], strings.ToUpper(strings.Join(strings.Fields(name[cut:]), " "))
		}
		name = strings.Trim(name, "\"`[]")
		upper := strings.ToUpper(name)
		// A table can carry constraints in the same list as its columns.
		// Taking those as column names shifts every index after them.
		switch upper {
		case "PRIMARY", "UNIQUE", "CHECK", "FOREIGN", "CONSTRAINT":
			return
		}
		// Only a column whose declared type is exactly INTEGER aliases the
		// row id. INT PRIMARY KEY and BIGINT PRIMARY KEY do not, and are
		// stored in the record like anything else, so widening this test
		// would put a row id where a real value is.
		if strings.HasPrefix(rest, "INTEGER PRIMARY KEY") {
			rowid = len(out)
		}
		out = append(out, strings.ToLower(name))
	}
	for _, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				flush()
				continue
			}
		}
		field.WriteRune(r)
	}
	flush()
	return out, rowid
}

// column finds a column by name and returns its position, or minus one.
func (t table) column(name string) int {
	for i, c := range t.columns {
		if c == name {
			return i
		}
	}
	return -1
}

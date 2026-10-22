package main

// The SQLite reader, checked against databases written by SQLite.
//
// Fixtures built by hand only prove that the reader agrees with whoever
// wrote the fixture. What has to be true is that it agrees with the thing
// that writes the files in the real world, so these tests ask Python,
// which carries SQLite in its standard library, to build a database and
// then compare row for row.
//
// The shapes here are chosen to reach the parts of the format that a small
// database never touches: enough rows to push the b-tree past one page and
// force interior pages, a value long enough to spill onto overflow pages,
// a column added later so the older rows are short, and every type the
// format has. A reader that only ever sees a ten row table passes without
// having read any of that.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func python(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python", "py"} {
		if path, err := exec.LookPath(name); err == nil {
			// The Windows app execution alias is a stub that opens the
			// Microsoft Store instead of running anything, so the
			// interpreter is asked to prove it works before it is used.
			if err := exec.Command(path, "-c", "import sqlite3").Run(); err == nil {
				return path
			}
		}
	}
	t.Skip("no working Python with sqlite3, so the reader cannot be checked against real databases")
	return ""
}

func build(t *testing.T, script string) []byte {
	t.Helper()
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	full := "import sqlite3\nc = sqlite3.connect(" + quote(db) + ")\n" + script + "\nc.commit()\nc.close()\n"
	out, err := exec.Command(python(t), "-c", full).CombinedOutput()
	if err != nil {
		t.Fatalf("building the database failed: %v\n%s", err, out)
	}
	body, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "\\", "\\\\") + "'" }

func TestReadsEveryRowOfARealDatabase(t *testing.T) {
	const rows = 2000
	body := build(t, `
c.execute("CREATE TABLE downloads (id INTEGER PRIMARY KEY, target_path TEXT, tab_url TEXT, total_bytes INTEGER)")
for i in range(`+itoa(rows)+`):
    c.execute("INSERT INTO downloads VALUES (?,?,?,?)", (i, "/home/x/file%d.pdf" % i, "https://example.test/%d" % i, i*1000))
`)
	db, err := openSQLite(body)
	if err != nil {
		t.Fatal(err)
	}
	table, ok := db.tables()["downloads"]
	if !ok {
		t.Fatal("the downloads table was not found in the schema")
	}
	want := []string{"id", "target_path", "tab_url", "total_bytes"}
	if got := strings.Join(table.columns, ","); got != strings.Join(want, ",") {
		t.Fatalf("column names came back as %q", got)
	}

	got := db.read(table)
	if len(got) != rows {
		t.Fatalf("read %d rows out of a database with %d, so the b-tree walk is missing pages", len(got), rows)
	}
	seen := map[string]bool{}
	for _, row := range got {
		seen[row[1].text] = true
		if row[3].num != row[0].num*1000 {
			t.Fatalf("row %d has total_bytes %d", row[0].num, row[3].num)
		}
	}
	for i := 0; i < rows; i++ {
		if !seen[fmt.Sprintf("/home/x/file%d.pdf", i)] {
			t.Fatalf("file%d.pdf never came back", i)
		}
	}
}

// A payload longer than a page is split across overflow pages, and the
// formula for how much stays behind is the one part of the record format
// that cannot be worked out by looking at a small file.
func TestReadsValuesThatSpillOntoOverflowPages(t *testing.T) {
	body := build(t, `
c.execute("CREATE TABLE files (folder TEXT, file_name TEXT)")
c.execute("INSERT INTO files VALUES (?,?)", ("/" + "deep/"*4000, "x.png"))
c.execute("INSERT INTO files VALUES (?,?)", ("/home/y", "short.png"))
`)
	db, err := openSQLite(body)
	if err != nil {
		t.Fatal(err)
	}
	table := db.tables()["files"]
	rows := db.read(table)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	var long string
	for _, row := range rows {
		if len(row[0].text) > len(long) {
			long = row[0].text
		}
	}
	if want := 1 + len("deep/")*4000; len(long) != want {
		t.Fatalf("the long path came back %d bytes long and it is %d", len(long), want)
	}
	if !strings.HasSuffix(long, "deep/") {
		t.Fatalf("the end of the overflow chain is wrong: %q", long[max(0, len(long)-20):])
	}
}

// SQLite leaves trailing nulls out of a record when a column was added
// after the row was written, which is exactly what a browser does to its
// own history between versions. Dropping the short rows lost real data.
func TestRowsWrittenBeforeAColumnWasAddedStillCount(t *testing.T) {
	body := build(t, `
c.execute("CREATE TABLE downloads (id INTEGER PRIMARY KEY, target_path TEXT)")
c.execute("INSERT INTO downloads VALUES (1, '/home/x/old.pdf')")
c.execute("ALTER TABLE downloads ADD COLUMN tab_url TEXT")
c.execute("INSERT INTO downloads VALUES (2, '/home/x/new.pdf', 'https://example.test/')")
`)
	db, err := openSQLite(body)
	if err != nil {
		t.Fatal(err)
	}
	table := db.tables()["downloads"]
	if got := table.column("tab_url"); got != 2 {
		t.Fatalf("tab_url is at column %d", got)
	}
	rows := db.read(table)
	if len(rows) != 2 {
		t.Fatalf("expected both rows, got %d", len(rows))
	}
	for _, row := range rows {
		if row[0].num == 1 && row[1].text != "/home/x/old.pdf" {
			t.Errorf("the row written before the column was added came back as %q", row[1].text)
		}
	}
}

func TestReadsEveryTypeTheFormatHas(t *testing.T) {
	body := build(t, `
c.execute("CREATE TABLE t (a, b, c, d, e)")
c.execute("INSERT INTO t VALUES (?,?,?,?,?)", (None, 1, -2000000000, 1.5, "text"))
c.execute("INSERT INTO t VALUES (?,?,?,?,?)", (0, 255, 9007199254740993, 0.0, b"blob"))
`)
	db, err := openSQLite(body)
	if err != nil {
		t.Fatal(err)
	}
	rows := db.read(db.tables()["t"])
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	first := rows[0]
	if !first[0].null {
		t.Error("a null did not come back as null")
	}
	if first[2].num != -2000000000 {
		t.Errorf("a negative integer came back as %d, so the sign is not being extended", first[2].num)
	}
	if first[3].num != 1 {
		t.Errorf("the float 1.5 came back as %d rather than truncated to 1", first[3].num)
	}
	if first[4].text != "text" {
		t.Errorf("text came back as %q", first[4].text)
	}
	if rows[1][2].num != 9007199254740993 {
		t.Errorf("an eight byte integer came back as %d", rows[1][2].num)
	}
}

// A database file is somebody else's and is allowed to be nonsense. What
// it must not do is hang, panic, or read past the end of the slice.
func TestBrokenDatabasesStillFinish(t *testing.T) {
	body := build(t, `
c.execute("CREATE TABLE t (a TEXT)")
for i in range(500):
    c.execute("INSERT INTO t VALUES (?)", ("row %d" % i,))
`)
	for name, broken := range map[string][]byte{
		"cut in half":     body[:len(body)/2],
		"tail only":       body[len(body)/2:],
		"one byte":        body[:1],
		"zeroed pages":    append(append([]byte{}, body[:100]...), make([]byte, 4096)...),
		"page size zero":  withByte(body, 16, 0),
		"reserved is max": withByte(body, 20, 0xFF),
	} {
		db, err := openSQLite(broken)
		if err != nil {
			continue
		}
		for _, table := range db.tables() {
			db.read(table)
		}
		t.Logf("%s: read without falling over", name)
	}
}

func withByte(body []byte, at int, b byte) []byte {
	out := append([]byte{}, body...)
	if at < len(out) {
		out[at] = b
	}
	return out
}

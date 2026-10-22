package main

// The .DS_Store reader.
//
// The fixture below is built here, and on its own that only proves the
// reader agrees with the builder, which is the trap this whole project is
// meant to avoid. The check that counts runs in the workflow: it fetches
// the test file from the reference implementation of this format and
// requires the exact three names that implementation's own test suite
// asserts on. What is here is the shape of the container and the ways it
// can be broken, which are the parts a real sample does not exercise.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// makeDSStore writes a minimum buddy allocator holding one leaf node.
//
// The layout: a 36 byte header, the block table at 0x800, the tree header
// at 0x1000 and the leaf at 0x2000. Every address in the table has the log
// of its own size in the low five bits, and every address counts from four
// bytes into the file rather than from the start of it.
func makeDSStore(names []string) []byte {
	const (
		infoAt   = 0x800
		infoSize = 0x800
		masterAt = 0x1000
		leafAt   = 0x2000
		leafSize = 0x1000
	)
	body := make([]byte, leafAt+4+leafSize)
	be := binary.BigEndian

	be.PutUint32(body, 1)
	copy(body[4:], "Bud1")
	be.PutUint32(body[8:], infoAt)
	be.PutUint32(body[12:], infoSize)
	be.PutUint32(body[16:], infoAt)

	info := body[infoAt+4:]
	be.PutUint32(info, 2) // two blocks in the table
	be.PutUint32(info[4:], 0)
	be.PutUint32(info[8:], masterAt|5) // 32 bytes
	be.PutUint32(info[12:], leafAt|12) // 4096 bytes
	dir := info[8+256*4:]              // the table is padded to 256 slots
	be.PutUint32(dir, 1)               // one directory entry
	dir[4] = 4                         // the length of its name
	copy(dir[5:], "DSDB")
	be.PutUint32(dir[9:], 0) // which lives in block zero

	master := body[masterAt+4:]
	be.PutUint32(master, 1) // the root node is block one
	be.PutUint32(master[4:], 0)
	be.PutUint32(master[8:], uint32(len(names)))
	be.PutUint32(master[12:], 1)
	be.PutUint32(master[16:], 0x1000)

	leaf := body[leafAt+4:]
	be.PutUint32(leaf, 0) // a leaf has no rightmost child
	be.PutUint32(leaf[4:], uint32(len(names)))
	at := 8
	for _, name := range names {
		units := utf16.Encode([]rune(name))
		be.PutUint32(leaf[at:], uint32(len(units)))
		at += 4
		for _, u := range units {
			be.PutUint16(leaf[at:], u)
			at += 2
		}
		copy(leaf[at:], "Iloc")
		copy(leaf[at+4:], "blob")
		be.PutUint32(leaf[at+8:], 16)
		at += 12 + 16
	}
	return body
}

func TestReadsTheNamesOutOfADSStore(t *testing.T) {
	want := []string{"budget.xlsx", "deleted photo.jpg", "niña.png", "notes"}
	got, err := dsStoreNames(makeDSStore(want))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, wanted %v", got, want)
	}
}

// The same name appears once per setting the Finder remembers about it, so
// a folder of four files can hold a dozen records. The report counts files.
func TestRepeatedNamesCountOnce(t *testing.T) {
	got, err := dsStoreNames(makeDSStore([]string{"a.txt", "a.txt", "a.txt", "b.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("four records over two files came back as %v", got)
	}
}

// These arrive from repositories and shared drives, so a broken one has to
// come back as an error rather than as a panic or a hang.
func TestBrokenDSStoresStillFinish(t *testing.T) {
	good := makeDSStore([]string{"a.txt", "b.txt"})
	broken := map[string][]byte{
		"empty":       {},
		"header only": good[:36],
		"cut in half": good[:len(good)/2],
		"no magic":    append([]byte("XXXXXXXX"), good[8:]...),
		"table off the end": func() []byte {
			b := append([]byte{}, good...)
			binary.BigEndian.PutUint32(b[8:], 0xFFFFFF)
			return b
		}(),
		"absurd block count": func() []byte {
			b := append([]byte{}, good...)
			binary.BigEndian.PutUint32(b[0x804:], 0xFFFFFFF)
			return b
		}(),
		"a node that points at itself": func() []byte {
			b := append([]byte{}, good...)
			binary.BigEndian.PutUint32(b[0x2004:], 1) // the leaf claims a child
			return b
		}(),
	}
	for name, body := range broken {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s made it panic: %v", name, r)
				}
			}()
			dsStoreNames(body)
		}()
	}
}

// The finding is the difference between what the file remembers and what
// the folder holds, so the test has to have both halves.
func TestNamesTheFilesAFolderNoLongerHas(t *testing.T) {
	dir := t.TempDir()
	defer setHomeTo(t, dir)()
	walkedOnce = false

	folder := filepath.Join(dir, "Documents")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "kept.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := makeDSStore([]string{"kept.txt", "severance agreement.pdf", ".DS_Store"})
	if err := os.WriteFile(filepath.Join(folder, ".DS_Store"), store, 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Report{}
	scanDSStore(r)
	out := renderTo(r)

	if !strings.Contains(out, "1 file is named in a .DS_Store") {
		t.Errorf("the deleted file was not reported on its own:\n%s", out)
	}
	if strings.Contains(out, "kept.txt") || strings.Contains(out, "severance") {
		t.Errorf("a name got out without --reveal:\n%s", out)
	}
	// The file naming itself is not a deleted file, and neither is the one
	// that is still sitting there.
	if strings.Contains(out, "2 files") || strings.Contains(out, "3 files") {
		t.Errorf("the count includes something it should not:\n%s", out)
	}
}

// One inside a repository is a different finding, because it is a commit
// away from being public and it is invisible on the machine carrying it.
func TestOneInsideARepositoryIsLouder(t *testing.T) {
	dir := t.TempDir()
	defer setHomeTo(t, dir)()
	walkedOnce = false

	project := filepath.Join(dir, "Documents", "project")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := makeDSStore([]string{"draft.md", "internal pricing.xlsx"})
	if err := os.WriteFile(filepath.Join(project, ".DS_Store"), store, 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Report{}
	scanDSStore(r)
	out := renderTo(r)
	if !strings.Contains(out, "OUTSIDE") || !strings.Contains(out, "inside a repository") {
		t.Errorf("a .DS_Store in a repository was not reported as the louder finding:\n%s", out)
	}
}

// The real samples, when the workflow has fetched them. This is the check
// that means anything: three files written by real Macs, one of which has
// a published expected output and one of which the reference
// implementation's own tests assert on.
func TestAgainstRealDSStoreFiles(t *testing.T) {
	dir := os.Getenv("AFTERIMAGE_DSSTORE_DIR")
	if dir == "" {
		t.Skip("no real samples fetched, so only the built fixtures were checked")
	}
	for name, want := range map[string][]string{
		// From the test suite of the reference implementation, which
		// asserts on exactly these three.
		"reference.DS_Store": {"bam", "bar", "baz"},
		// The published output in the readme of an independent parser.
		"ctf.DS_Store": {"favicon.ico", "flag", "static", "templates", "vulnerable.py", "vulnerable.wsgi"},
	} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got, err := dsStoreNames(body)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s came back as %v and another parser says %v", name, got, want)
			continue
		}
		t.Logf("%s: %d names, matching an independent parser exactly", name, len(got))
	}
}

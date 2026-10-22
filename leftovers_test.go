package main

// Editor leftovers.
//
// The vim swap file is the one worth getting right, because it is the only
// leftover that holds the text of the document rather than a reference to
// it. Its header is a fixed layout with three strings in it: who was
// editing, on what machine, and which file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func vimSwap(user, host, file string) []byte {
	body := make([]byte, 4096)
	copy(body, "b0VIM 9.0\x00")
	copy(body[28:68], user)
	copy(body[68:108], host)
	copy(body[108:1006], file)
	return body
}

func TestReadsAVimSwapHeader(t *testing.T) {
	l := leftover{}
	readVimSwap(vimSwap("marisol", "workshop", "/home/marisol/notes/plan.md"), &l)
	if l.who != "marisol" {
		t.Errorf("the account name came back as %q", l.who)
	}
	if l.host != "workshop" {
		t.Errorf("the host name came back as %q", l.host)
	}
	if l.original != filepath.Clean("/home/marisol/notes/plan.md") {
		t.Errorf("the document came back as %q", l.original)
	}
}

func TestVimSwapHeadersThatAreNotOneAreIgnored(t *testing.T) {
	for name, body := range map[string][]byte{
		"empty":        {},
		"too short":    vimSwap("x", "y", "/a/b")[:100],
		"wrong magic":  append([]byte("XXnotvim"), make([]byte, 4000)...),
		"no file name": vimSwap("x", "y", ""),
	} {
		l := leftover{}
		readVimSwap(body, &l)
		if l.original != "" {
			t.Errorf("%s produced the document %q", name, l.original)
		}
	}
}

func TestAnOrphanedSwapFileIsFound(t *testing.T) {
	dir := t.TempDir()
	defer setHomeTo(t, dir)()
	docs := filepath.Join(dir, "Documents")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}

	// One swap beside a document that is still there, and one beside a
	// document that is not. Only the second is a finding.
	kept := filepath.Join(docs, "kept.md")
	if err := os.WriteFile(kept, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, ".kept.md.swp"),
		vimSwap("marisol", "workshop", kept), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, ".vanished.md.swp"),
		vimSwap("marisol", "workshop", filepath.Join(docs, "vanished.md")), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Report{}
	scanLeftovers(r)
	out := renderTo(r)

	if !strings.Contains(out, "1 editor leftover belongs to a document that is not there") {
		t.Errorf("the orphaned swap file was not reported on its own:\n%s", out)
	}
	if !strings.Contains(out, "account name is written inside") {
		t.Errorf("the account name in the header was not reported:\n%s", out)
	}
	if strings.Contains(out, "marisol") {
		t.Errorf("the account name was printed in full without --reveal:\n%s", out)
	}
}

func TestReadsTheNameOutOfAnOfficeLockFile(t *testing.T) {
	name := "Marisol Iglesias"
	body := append([]byte{byte(len(name))}, name...)
	body = append(body, make([]byte, 60)...)
	if got := officeOwner(body); got != name {
		t.Errorf("the owner came back as %q", got)
	}
	for label, junk := range map[string][]byte{
		"empty":          {},
		"length is zero": {0, 'x'},
		"length too big": {200, 'x'},
		"control bytes":  {4, 1, 2, 3, 4},
	} {
		if got := officeOwner(junk); got != "" {
			t.Errorf("%s came back as the name %q", label, got)
		}
	}
}

// The walk has a floor on it, and a report that quietly stopped early
// would look exactly like a clean one, so it has to say so.
func TestAWalkThatStopsEarlySaysSo(t *testing.T) {
	dir := t.TempDir()
	defer setHomeTo(t, dir)()

	// One folder per level, deeper than the walk goes, with a leftover at
	// the bottom that must not be found.
	deep := dir
	for i := 0; i < maxDepth+3; i++ {
		deep = filepath.Join(deep, "level")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, ".buried.md.swp"),
		vimSwap("x", "y", filepath.Join(deep, "buried.md")), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Report{}
	scanLeftovers(r)
	if len(r.Findings) != 0 {
		t.Errorf("something below the depth limit was reported: %v", r.Findings)
	}
}

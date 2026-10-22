package main

// The promises on the front of the README, tested by reading the source.
//
// Every one of these could be broken by a change that compiles and passes
// everything else. The masking one especially: it is the difference
// between a report somebody can paste into an issue and a report that
// hands over a list of their files, and nothing about the code makes it
// obvious which of the two it currently is.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sources(t *testing.T) map[string]string {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(body)
	}
	if len(out) < 12 {
		t.Fatalf("only found %d source files, so this is not looking at the program", len(out))
	}
	return out
}

func TestItNeverWritesAnything(t *testing.T) {
	forbidden := []string{
		"os.WriteFile", "os.Create", "os.Remove", "os.RemoveAll", "os.Rename",
		"os.Mkdir", "os.MkdirAll", "os.MkdirTemp", "os.Truncate", "os.Chmod",
		"os.OpenFile", "os.Symlink", "os.Link", "io.Copy", "os.Chtimes",
	}
	for name, body := range sources(t) {
		for _, call := range forbidden {
			if strings.Contains(body, call) {
				t.Errorf("%s calls %s, and this program only reads", name, call)
			}
		}
	}
}

func TestItNeverRunsAnythingOrOpensASocket(t *testing.T) {
	for name, body := range sources(t) {
		for _, bad := range []string{"os/exec", "exec.Command", `"net"`, `"net/http"`, `"net/url"`} {
			if strings.Contains(body, bad) {
				t.Errorf("%s has %s in it", name, bad)
			}
		}
	}
}

func TestNoDependencies(t *testing.T) {
	body, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "require") {
		t.Errorf("go.mod has a dependency in it:\n%s", body)
	}
}

// The one thing a program that reads a UNC path must not do is stat it,
// because that opens a connection to whatever is on the other end. It
// would break the promise about sockets and it would tell a file server
// that somebody is looking for a file they deleted.
func TestSharesAreNeverReachedFor(t *testing.T) {
	for _, path := range []string{
		`\\fileserver\finance\budget.xlsx`,
		`\\10.0.0.4\share\thing.txt`,
		"//nas/media/film.mkv",
	} {
		if got, why := stillThere(path); got != unreachable || !strings.Contains(why, "share") {
			t.Errorf("%s came back as %v (%s) rather than being left alone", path, got, why)
		}
	}
}

// A profile with known names in it, run through every scanner, to check
// that nothing gets out by default and that --reveal does let it out.
func TestNamesDoNotGetOutWithoutReveal(t *testing.T) {
	dir := t.TempDir()
	defer setHomeTo(t, dir)()

	const secret = "quarterly-redundancies"
	cache := filepath.Join(dir, ".cache", "thumbnails", "normal")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	uri := "file:///" + strings.TrimPrefix(filepath.ToSlash(filepath.Join(dir, secret+".pdf")), "/")
	body := thumbnailPNG(t, map[string]string{"Thumb::URI": uri})
	if err := os.WriteFile(filepath.Join(cache, "x.png"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	run := func() string {
		r := &Report{}
		for _, look := range []func(*Report){scanThumbnails, scanRecentlyUsed, scanLeftovers} {
			look(r)
		}
		var buf bytes.Buffer
		r.render(&buf, "test")
		return buf.String()
	}

	reveal = false
	covered := run()
	if strings.Contains(covered, secret) {
		t.Errorf("the file name reached the report with masking on:\n%s", covered)
	}
	if !strings.Contains(covered, dots) {
		t.Errorf("nothing in the report was masked at all:\n%s", covered)
	}

	reveal = true
	defer func() { reveal = false }()
	if shown := run(); !strings.Contains(shown, secret) {
		t.Errorf("--reveal did not show the name:\n%s", shown)
	}
}

// The account name is on the front of nearly every line, and it is the one
// string that ties a whole report to a person.
func TestTheAccountNameIsCoveredOver(t *testing.T) {
	oldHome, oldUser := home, userName
	home, userName = filepath.FromSlash("/home/marisol"), "marisol"
	defer func() { home, userName = oldHome, oldUser }()

	reveal = false
	for _, path := range []string{
		filepath.FromSlash("/home/marisol/Documents/x.txt"),
		filepath.FromSlash("/home/marisol"),
		`C:\Users\marisol\Desktop`,
	} {
		if got := maskPath(path); strings.Contains(got, "marisol") {
			t.Errorf("%s came back as %s", path, got)
		}
	}
	// A folder that merely contains the name as part of a longer word is
	// not the account and must not be rewritten.
	if got := maskUser(filepath.FromSlash("/home/marisolete/")); !strings.Contains(got, "marisolete") {
		t.Errorf("a different name was rewritten: %s", got)
	}
}

func TestAGapIsNotAPass(t *testing.T) {
	r := &Report{}
	r.gap("the thumbnail cache could not be opened")
	out := renderTo(r)
	if !strings.Contains(out, "could not be read") {
		t.Errorf("a gap did not reach the report:\n%s", out)
	}
	if strings.Contains(out, "Nothing turned up") {
		t.Errorf("a run that could not read something said nothing turned up:\n%s", out)
	}
}

func TestReadmeMatchesTheFlags(t *testing.T) {
	body, err := os.ReadFile("README.md")
	if err != nil {
		t.Skip("no README yet")
	}
	readme := string(body)
	for _, flag := range []string{"--reveal", "--all", "--version", "AFTERIMAGE_HOME"} {
		if !strings.Contains(readme, flag) {
			t.Errorf("%s is in the program and the README does not mention it", flag)
		}
	}
	for _, promise := range []string{"only reads", "write ahead log", ".ds_store"} {
		if !strings.Contains(strings.ToLower(readme), promise) {
			t.Errorf("the README no longer says %q", promise)
		}
	}
}

// Every scanner has to cope with a profile that has none of what it is
// looking for, which is the normal case for two thirds of them on any one
// machine, and with a profile that does not exist at all.
func TestAnEmptyProfileIsQuiet(t *testing.T) {
	dir := t.TempDir()
	defer setHomeTo(t, dir)()

	r := &Report{}
	for _, look := range []func(*Report){
		scanRecent, scanJumpLists, scanThumbcache,
		// Two are left out, and for the same reason: they do not live
		// inside the profile, so pointing this at an empty directory does
		// not make them empty.
		//
		// The recycle bin belongs to the volume, so a temporary profile on
		// the real C: drive still finds the real bin.
		//
		// The Quick Look cache belongs to the account on the machine. It
		// sits under /var/folders in a directory named from a hash of the
		// user, nowhere near the home directory, and there is no way to
		// read the one belonging to some other profile. So on a real Mac
		// this finds the real cache no matter what home is set to. That was
		// always true and this test never noticed, because until the cache
		// moved and was found again, this read nothing anywhere.
		scanThumbnails, scanRecentlyUsed, scanDownloads, scanLeftovers,
	} {
		look(r)
	}
	if len(r.Findings) != 0 {
		t.Errorf("an empty profile produced %d findings", len(r.Findings))
	}
	if len(r.Gaps) != 0 {
		t.Errorf("an empty profile produced gaps: %v", r.Gaps)
	}
}

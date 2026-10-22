package main

// What editors leave behind when they are interrupted.
//
// These are not caches and nothing made them on purpose. A swap file, a
// lock file, an autosave: each one is written next to the document it
// belongs to so that a crash does not lose your work, and each one is
// supposed to be removed when the editor exits cleanly. When it does not
// exit cleanly, the leftover stays, and it goes on staying after somebody
// deletes the document.
//
// The vim swap file is the one that matters. It is not a reference to the
// document, it holds the text of it, along with the account name and the
// host name of whoever was editing. So an orphaned .swp is a copy of a
// file that is not there any more, sitting in the same folder under a name
// that starts with a dot.
//
// This walks a bounded set of folders rather than the whole disk. Where it
// looks is said in the report, because a search that quietly stops at
// a few levels down and reports nothing looks exactly like a clean result.

import (
	"os"
	"path/filepath"
	"strings"
)

type leftover struct {
	at       string // the leftover itself
	original string // the document it belongs to
	kind     string
	who      string
	host     string
}

func scanLeftovers(r *Report) {
	candidates, stopped := walkDocuments()
	var found []leftover
	for _, f := range candidates {
		if l, ok := readLeftover(f.dir, f.name); ok {
			found = append(found, l)
		}
	}

	if stopped {
		r.gap("the walk for editor leftovers stopped after " + itoa(maxDirs) +
			" folders, so this part of the report is not a complete answer for this account")
	}
	if len(found) == 0 {
		return
	}

	var (
		orphans group
		live    int
		people  = map[string]bool{}
	)
	for _, l := range found {
		if l.who != "" {
			people[l.who] = true
		}
		if l.original == "" {
			continue
		}
		if judge(l.original, 0) != nowGone {
			live++
			continue
		}
		right := l.kind
		if l.who != "" {
			right += ", left by " + maskName(l.who)
		}
		orphans.add(maskName(filepath.Base(l.original)), right)
	}

	if orphans.count > 0 {
		r.add(Finding{
			Severity: Copied,
			Title: plural(orphans.count, "editor leftover belongs to a document that is not there",
				"editor leftovers belong to documents that are not there") + " any more",
			Lines: orphans.top(12),
			Where: maskUser(home) + " and the document folders under it, " + itoa(maxDepth) + " levels deep",
			Advice: "A vim swap file is not a marker, it is the text of the document, and the account name " +
				"and host name of whoever was editing are in the header. Recovering one is vim -r on the swap " +
				"file. Deleting one is safe once you are sure the document is really gone, and these are the " +
				"files that make deleting a document not enough.",
		})
	}

	if len(people) > 0 {
		var g group
		for _, who := range sorted(map[string]bool(people)) {
			g.add("  "+maskName(who), "named in a leftover file")
		}
		r.add(Finding{
			Severity: Tells,
			Title:    plural(len(people), "account name is written inside a leftover file", "account names are written inside leftover files"),
			Lines:    g.top(8),
			Where:    maskUser(home),
			Advice: "Editors write who was editing into their lock and swap files so that a second person " +
				"opening the same document is told who has it. Send the folder to somebody and you send that too.",
		})
	}

	if live > 0 && orphans.count == 0 {
		r.add(Finding{
			Severity: Note,
			Title:    plural(live, "editor leftover is sitting next to its document", "editor leftovers are sitting next to their documents"),
			Where:    maskUser(home),
			Advice:   "These belong to files that are still here, so they are untidy rather than revealing.",
		})
	}
}

// readLeftover recognises one file by the shape of its name and then, for
// the ones that carry more than a name, reads what is inside it.
func readLeftover(dir, name string) (leftover, bool) {
	full := filepath.Join(dir, name)
	lower := strings.ToLower(name)

	switch {
	case strings.HasPrefix(name, ".") && isVimSwapName(lower):
		l := leftover{at: full, kind: "a vim swap file, with the text of the document in it"}
		if body, err := os.ReadFile(full); err == nil {
			readVimSwap(body, &l)
		}
		if l.original == "" {
			// vim names the swap after the document, so the name is the
			// fallback when the header could not be read.
			l.original = filepath.Join(dir, strings.TrimSuffix(strings.TrimPrefix(name, "."), filepath.Ext(name)))
		}
		return l, true

	case strings.HasPrefix(name, "~$"):
		l := leftover{at: full, kind: "an Office lock file", original: filepath.Join(dir, name[2:])}
		if body, err := os.ReadFile(full); err == nil {
			l.who = officeOwner(body)
		}
		// Word truncates the first two characters of a long name when it
		// builds the lock file name, so the document is looked for under
		// both spellings before deciding it is gone.
		if judge(l.original, 0) == nowGone {
			if match := firstMatch(dir, "??"+name[2:]); match != "" {
				l.original = match
			}
		}
		return l, true

	case strings.HasPrefix(lower, ".~lock.") && strings.HasSuffix(name, "#"):
		l := leftover{
			at:       full,
			kind:     "a LibreOffice lock file",
			original: filepath.Join(dir, strings.TrimSuffix(name[len(".~lock."):], "#")),
		}
		if body, err := os.ReadFile(full); err == nil {
			parts := strings.Split(string(body), ",")
			if len(parts) > 0 {
				l.who = printable(strings.TrimSpace(parts[0]))
			}
			if len(parts) > 2 {
				l.host = printable(strings.TrimSpace(parts[2]))
			}
		}
		return l, true

	case len(name) > 2 && strings.HasPrefix(name, "#") && strings.HasSuffix(name, "#"):
		return leftover{
			at:       full,
			kind:     "an emacs autosave, holding unsaved changes",
			original: filepath.Join(dir, strings.Trim(name, "#")),
		}, true

	case strings.HasPrefix(name, ".goutputstream-"):
		// These are what GIO writes while replacing a file. One left
		// behind is a copy of a write that did not finish, and its name
		// says nothing about what it was, so there is no original to
		// look for and it is reported only through the account names.
		return leftover{at: full, kind: "an interrupted save from a GNOME application"}, true
	}
	return leftover{}, false
}

func isVimSwapName(lower string) bool {
	for _, ext := range []string{".swp", ".swo", ".swn", ".swm", ".swl"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// readVimSwap reads the first block of a swap file. The numbers in the
// header are written in the byte order of whichever machine made it, which
// is why none of them are read: the three fields worth having are strings.
func readVimSwap(body []byte, l *leftover) {
	if len(body) < 1008 || body[0] != 'b' || body[1] != '0' || string(body[2:6]) != "VIM " {
		return
	}
	l.who = printable(trimNull(body[28:68]))
	l.host = printable(trimNull(body[68:108]))
	name := printable(trimNull(body[108:1006]))
	if name == "" {
		return
	}
	// vim writes the path with a leading ~/ when it is under the home
	// directory of the account that was editing.
	if strings.HasPrefix(name, "~/") {
		name = filepath.Join(home, name[2:])
	}
	// Absolute in either style, not just the style of the machine doing
	// the reading. A swap file written on Linux and read from Windows has
	// a path filepath.IsAbs says nothing about, and dropping it there
	// loses every finding when reading somebody else's profile.
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") || windowsPath(name) {
		l.original = filepath.Clean(name)
	}
}

// officeOwner reads the name Word and Excel write into their lock files so
// that the second person to open a document is told who has it. It is a
// length byte and then the name, and the same name follows again in wide
// characters, which is not read because one copy is enough.
func officeOwner(body []byte) string {
	if len(body) < 2 {
		return ""
	}
	n := int(body[0])
	if n < 1 || n > 52 || 1+n > len(body) {
		return ""
	}
	name := printable(strings.TrimRight(string(body[1:1+n]), "\x00 "))
	for _, r := range name {
		if r < 0x20 {
			return ""
		}
	}
	return name
}

func trimNull(b []byte) string {
	if cut := indexByte(b, 0); cut >= 0 {
		return string(b[:cut])
	}
	return string(b)
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func firstMatch(dir, pattern string) string {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

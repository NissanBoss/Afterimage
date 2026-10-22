package main

// The one walk of the document folders, shared by everything that needs
// it.
//
// Two scanners want the same tree: the one looking for editor leftovers
// and the one looking for .DS_Store files. Walking twice took twice as
// long for no reason and, worse, could report two different answers about
// where the walk stopped. So the walk happens once, keyed by the profile
// it was done for, and both scanners read the result.
//
// It is bounded on purpose and the bound is reported. A search that
// quietly stops a few levels down and finds nothing looks exactly like a
// clean result, which is the failure this program is least able to afford.

import (
	"os"
	"path/filepath"
	"strings"
)

type foundFile struct {
	dir  string
	name string
}

// The folders that get walked. Anything below a home directory that is not
// a cache of somebody else's making, and nothing outside it.
func documentRoots() []string {
	var out []string
	for _, name := range []string{
		"", "Desktop", "Documents", "Downloads", "Pictures", "Escritorio", "Documentos", "Descargas", "Imágenes",
	} {
		dir := home
		if name != "" {
			dir = under(name)
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			out = append(out, dir)
		}
	}
	return out
}

// Folders that are somebody else's business: package trees, version
// control, and the application data folders this program reads elsewhere
// with readers that understand them.
var skipDirs = map[string]bool{
	"appdata": true, "node_modules": true, ".git": true, ".svn": true, "library": true,
	".cargo": true, ".rustup": true, ".nuget": true, ".gradle": true, "__pycache__": true,
	"site-packages": true, ".next": true, ".terraform": true, "bower_components": true,
	"venv": true, ".venv": true, "vendor": true, "target": true, ".cache": true,
	"onedrive": true, "$recycle.bin": true, ".local": true, "go": true,
}

// How far the walk goes. The ceiling is high enough that an ordinary
// account never reaches it and low enough that a home directory with a
// source tree in it cannot turn this into a disk crawl. Four thousand was
// the first figure and a normal machine went straight through it, which
// left the report carrying a gap on every run and taught nobody anything.
const (
	maxDepth = 5
	maxDirs  = 40000
)

var (
	walkedFor  string
	walkFound  []foundFile
	walkStop   bool
	walkInGit  map[string]bool
	walkedOnce bool
)

// interesting is the filter. Keeping it here rather than in each scanner
// means the walk collects once for everybody, and it is deliberately
// loose: deciding what a file really is costs a read, and that only
// happens for the handful that get through this.
func interesting(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case lower == ".ds_store":
		return true
	case strings.HasPrefix(name, "~$"):
		return true
	case strings.HasPrefix(name, ".") && isVimSwapName(lower):
		return true
	case strings.HasPrefix(lower, ".~lock.") && strings.HasSuffix(name, "#"):
		return true
	case len(name) > 2 && strings.HasPrefix(name, "#") && strings.HasSuffix(name, "#"):
		return true
	case strings.HasPrefix(name, ".goutputstream-"):
		return true
	}
	return false
}

func walkDocuments() ([]foundFile, bool) {
	if walkedOnce && walkedFor == home {
		return walkFound, walkStop
	}
	walkedOnce, walkedFor = true, home
	walkFound, walkStop = nil, false
	walkInGit = map[string]bool{}

	visited := 0
	done := map[string]bool{}

	var walk func(dir string, depth int, inGit bool)
	walk = func(dir string, depth int, inGit bool) {
		// Folders already walked, not roots already started. Documents
		// sits under the home directory, so walking both as roots visited
		// everything below it twice and reported every leftover twice.
		if depth > maxDepth || walkStop || done[dir] {
			return
		}
		done[dir] = true
		if visited++; visited > maxDirs {
			walkStop = true
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		// A folder inside a repository is a folder whose contents are one
		// commit away from being published, which changes what a stray
		// file in it means. The .git directory itself is never walked.
		if !inGit {
			if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
				inGit = true
			}
		}
		if inGit {
			walkInGit[dir] = true
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				if !skipDirs[strings.ToLower(name)] {
					walk(filepath.Join(dir, name), depth+1, inGit)
				}
				continue
			}
			if interesting(name) {
				walkFound = append(walkFound, foundFile{dir: dir, name: name})
			}
		}
	}
	for _, root := range documentRoots() {
		walk(root, 0, false)
	}
	return walkFound, walkStop
}

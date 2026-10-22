package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Apple moved this cache. It used to sit straight under the per user caches
// directory; on newer systems the thumbnail service got a directory of its
// own and the cache went inside it. A build that only knew the old place
// read a Mac with a full cache on it and reported nothing at all, which is
// the worst way for this program to be wrong: not a wrong answer, a
// confident empty one.
//
// The real path cannot be built here, because it lives under /var/folders
// in a directory named from a hash of the user. But the branch that finds
// it from TMPDIR only cares about the shape of the path, so a whole fake
// tree can be laid out in a temporary directory and pointed at.
func TestQuickLookIsFoundInBothPlacesAppleHasPutIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("these paths are a Unix shape")
	}

	for _, caso := range []struct {
		nombre string
		under  string
	}{
		{"the place it used to be", "com.apple.QuickLook.thumbnailcache"},
		{
			"the place it is now",
			filepath.Join("com.apple.quicklook.ThumbnailsAgent", "com.apple.QuickLook.thumbnailcache"),
		},
	} {
		t.Run(caso.nombre, func(t *testing.T) {
			raiz := t.TempDir()
			// The same shape as the real one: .../var/folders/xx/yy/T for
			// the temporary files and .../C alongside it for the caches.
			porUsuario := filepath.Join(raiz, "var", "folders", "d8", "hvxvltxn", "")
			temporal := filepath.Join(porUsuario, "T")
			cache := filepath.Join(porUsuario, "C", caso.under)
			if err := os.MkdirAll(temporal, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(cache, 0o755); err != nil {
				t.Fatal(err)
			}

			t.Setenv("TMPDIR", temporal+string(filepath.Separator))

			encontrados := quickLookDirs()
			for _, d := range encontrados {
				if d == cache {
					return
				}
			}
			t.Errorf("the cache is at %q and what was looked at was %v", cache, encontrados)
		})
	}
}

// A directory that is not there is not a directory to read. Without this,
// every run on a machine with no cache would try to open a file inside a
// path that does not exist.
func TestQuickLookDoesNotReportPlacesThatAreNotThere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("these paths are a Unix shape")
	}
	raiz := t.TempDir()
	temporal := filepath.Join(raiz, "var", "folders", "d8", "hvxvltxn", "T")
	if err := os.MkdirAll(temporal, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", temporal+string(filepath.Separator))

	for _, d := range quickLookDirs() {
		if strings.HasPrefix(d, raiz) {
			t.Errorf("nothing was created under %q and %q came back", raiz, d)
		}
	}
}

package main

// The macOS Quick Look cache, which is the worst of these by some way.
//
// Press space on a file in the Finder and macOS draws a preview. To draw
// it quickly next time it keeps the picture, and beside the picture it
// keeps a SQLite database with the folder, the file name and the size of
// everything it has ever previewed. The database lives in a per user
// directory under /var/folders that nothing cleans, and the entries stay
// after the file is gone.
//
// The part that has been reported repeatedly since at least 2010 and is
// still true: browsing a mounted encrypted volume in the Finder puts the
// previews and the file names of what is inside it into this cache, on the
// unencrypted disk. Unmount the volume, lock it, put the password away,
// and the pictures of what was in it are still sitting out here.
//
// The layout of the tables is read from the schema rather than assumed,
// because this database has changed shape between releases and reading it
// by column position is how a tool starts confidently printing the wrong
// field.

import (
	"os"
	"path/filepath"
	"strings"
)

func scanQuickLook(r *Report) {
	for _, dir := range quickLookDirs() {
		path := filepath.Join(dir, "index.sqlite")
		body, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				r.gap("the Quick Look cache is there and could not be read: " + err.Error())
			}
			continue
		}
		db, err := openSQLite(body)
		if err != nil {
			r.gap("the Quick Look cache could not be read: " + err.Error())
			continue
		}
		readQuickLook(db, dir, r)
	}
}

// Where the cache sits under the per user caches directory.
//
// It used to be straight under C/. On newer systems the thumbnail service
// was given a directory of its own and the cache moved inside it, so both
// have to be looked at: a Mac that has been upgraded can still have the old
// one sitting there with everything that was in it.
//
// This is not a guess. A macOS runner drawing previews in 2026 put its
// cache in the second one, and a build that only knew the first read a
// machine with a cache on it and reported nothing.
var quickLookUnderCaches = []string{
	"com.apple.QuickLook.thumbnailcache",
	filepath.Join("com.apple.quicklook.ThumbnailsAgent", "com.apple.QuickLook.thumbnailcache"),
}

// quickLookDirs finds the cache. The directory under /var/folders is named
// from a hash of the user, so it is found rather than built: TMPDIR points
// into the same per user tree, one letter along.
func quickLookDirs() []string {
	var out []string
	if tmp := os.Getenv("TMPDIR"); strings.Contains(tmp, "/var/folders/") {
		// .../C/ is the caches sibling of .../T/, which is what TMPDIR is.
		base := filepath.Dir(strings.TrimSuffix(tmp, string(filepath.Separator)))
		for _, under := range quickLookUnderCaches {
			out = append(out, filepath.Join(base, "C", under))
		}
	}
	for _, under := range quickLookUnderCaches {
		matches, _ := filepath.Glob(filepath.Join("/var/folders/*/*/C", under))
		out = append(out, matches...)
	}

	seen := map[string]bool{}
	var unique []string
	for _, d := range out {
		if seen[d] {
			continue
		}
		seen[d] = true
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			unique = append(unique, d)
		}
	}
	return unique
}

func readQuickLook(db *sqlite, dir string, r *Report) {
	files, ok := db.tables()["files"]
	if !ok {
		r.gap("the Quick Look cache has no files table in it, so its shape is not one this understands")
		return
	}
	folder := files.column("folder")
	name := files.column("file_name")
	if folder < 0 || name < 0 {
		r.gap("the Quick Look cache does not name its folder and file name columns the way this expects")
		return
	}

	var (
		missing  group
		outside  group
		unable   int
		recycled int
		total    int
	)
	for _, row := range db.read(files) {
		path := filepath.Join(row[folder].text, row[name].text)
		if row[folder].text == "" || row[name].text == "" {
			continue
		}
		total++
		switch judge(path, 0) {
		case nowGone:
			missing.add(maskName(row[name].text), maskUser(row[folder].text))
		case inTheBin:
			recycled++
		case offTheMachine:
			outside.add(maskName(row[name].text), maskUser(row[folder].text))
		case cannotSay:
			unable++
		}
	}
	if total == 0 {
		return
	}
	if unable > 0 {
		r.gap(plural(unable, "Quick Look entry names a file this run could not check",
			"Quick Look entries name files this run could not check") + ". None of those are counted as gone.")
	}

	if missing.count > 0 {
		lines := missing.top(12)
		if recycled > 0 {
			lines = append(lines, "")
			lines = append(lines, wrapText(plural(recycled, "other entry names a file in the trash",
				"other entries name files in the trash")+", so those are not counted here.", wrap+10)...)
		}
		r.add(Finding{
			Severity: Copied,
			Title: plural(missing.count, "file you previewed is named in the Quick Look cache and is gone",
				"files you previewed are named in the Quick Look cache and are gone"),
			Lines: lines,
			Where: maskUser(dir),
			Advice: "The picture is in thumbnails.data beside this database and the name and folder are in the " +
				"database itself. Nothing prunes either. qlmanage -r cache empties it, and it fills again the next " +
				"time you press space on anything.",
		})
	}

	if outside.count > 0 {
		r.add(Finding{
			Severity: Outside,
			Title: plural(outside.count, "file on a volume that is not mounted is pictured in the Quick Look cache",
				"files on volumes that are not mounted are pictured in the Quick Look cache"),
			Lines: outside.top(12),
			Where: maskUser(dir),
			Advice: "This is the one worth acting on. Previewing a file inside a mounted encrypted volume " +
				"copies the preview and the name out here onto the ordinary disk, and locking the volume again does " +
				"not take them back. The same is true of anything you unplugged.",
		})
	}
}

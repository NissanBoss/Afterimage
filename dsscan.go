package main

// Reading the .DS_Store files found in the walk, and saying what they know
// that the folder no longer does.
//
// The cross check here is the tightest one in the program, because both
// halves are right in front of it: the file lists names, the folder lists
// names, and a name in the first and not the second is a file that was in
// this folder and is not now. Nothing has to be inferred and no timestamp
// has to be trusted.
//
// The second half is where it travels to. A .DS_Store inside a repository
// is one commit from being public, and it is invisible on Windows and
// Linux to whoever is carrying it around, so that is reported separately
// and louder.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func scanDSStore(r *Report) {
	candidates, _ := walkDocuments()

	var (
		ghosts    group
		inRepo    group
		files     int
		unreadble int
		total     int
	)
	for _, f := range candidates {
		if !strings.EqualFold(f.name, ".DS_Store") {
			continue
		}
		files++
		body, err := os.ReadFile(filepath.Join(f.dir, f.name))
		if err != nil {
			unreadble++
			continue
		}
		names, err := dsStoreNames(body)
		if err != nil {
			unreadble++
			continue
		}

		// What the folder holds now, matched without case because the
		// filesystem the .DS_Store was written on did not care either.
		here := map[string]bool{}
		if entries, err := os.ReadDir(f.dir); err == nil {
			for _, e := range entries {
				here[strings.ToLower(e.Name())] = true
			}
		} else {
			unreadble++
			continue
		}

		var gone []string
		for _, name := range names {
			// The Finder writes a record about the folder's own settings
			// under a name that is not a file, and a .DS_Store naming
			// itself is not a deleted file either.
			if name == "" || name == "." || strings.EqualFold(name, ".DS_Store") {
				continue
			}
			if !here[strings.ToLower(name)] {
				gone = append(gone, name)
			}
		}
		sort.Strings(gone)
		total += len(gone)

		if len(gone) == 0 {
			continue
		}
		row := &ghosts
		note := maskUser(f.dir)
		if walkInGit[f.dir] {
			row = &inRepo
			note = maskUser(f.dir) + ", inside a repository"
		}
		for _, name := range gone {
			row.add(maskName(name), note)
		}
	}

	if files == 0 {
		return
	}
	if unreadble > 0 {
		r.gap(plural(unreadble, "a .DS_Store could not be read", "some .DS_Store files could not be read") +
			", so what those folders used to hold was not checked")
	}

	if ghosts.count > 0 {
		r.add(Finding{
			Severity: Named,
			Title: plural(ghosts.count, "file is named in a .DS_Store and is not in the folder",
				"files are named in .DS_Store files and are not in their folders") + " any more",
			Lines: ghosts.top(12),
			Where: plural(files, ".DS_Store file", ".DS_Store files") + " under " + maskUser(home),
			Advice: "The Finder keeps a record per file so it can put the icons back where you left them, " +
				"and it does not remove the record when the file goes. Deleting the .DS_Store is safe: the folder " +
				"loses its arrangement and nothing else. On a Mac, defaults write com.apple.desktopservices " +
				"DSDontWriteNetworkStores -bool true at least stops them being written onto shared drives.",
		})
	}

	if inRepo.count > 0 {
		r.add(Finding{
			Severity: Outside,
			Title: plural(inRepo.count, "file named in a .DS_Store inside a repository is not in the folder",
				"files named in .DS_Store files inside repositories are not in their folders") + " any more",
			Lines: inRepo.top(12),
			Where: maskUser(home),
			Advice: "A .DS_Store in a repository is one commit from being published, and on Windows and Linux " +
				"it is invisible to whoever is carrying it. Anybody who clones it can read the names of the files " +
				"that were in that folder, including the ones that were taken out before it was made public. " +
				"Putting .DS_Store in .gitignore stops the next one; the ones already committed are still in the " +
				"history after the file is removed.",
		})
	}
}

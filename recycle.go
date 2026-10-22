package main

// The recycle bin, which keeps two files per deleted file.
//
// The one called $R is the file itself. The one called $I beside it is the
// record: the size the file was, the moment it was deleted to the
// hundred nanosecond, and its original full path. People know the first
// one is there. The second is the one that says where the file used to
// live, and it is the reason this program can tell the difference between
// a file you deleted and a file you deleted twice.
//
// The pair can come apart. Delete the $R and the $I stays behind holding
// the path of something that is no longer anywhere on the machine, which
// is the strongest form of the thing this program looks for: a full record
// of a file, and no file.

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type binned struct {
	path    string
	size    uint64
	deleted time.Time
	body    bool // whether the $R half is still beside it
}

var (
	binLoaded string // the home the bin was last read for, or empty
	binned_   []binned
	binPath   map[string]bool
	binGaps   []string
)

// loadBin is read once per profile and used by several scanners. The
// Recent folder needs it to avoid calling a file gone when it is sitting
// in the bin with its name still on it, which is a different thing to say
// to somebody.
//
// It keys off the home directory rather than running once, because the
// tests point the program at one temporary profile after another and a
// single sync.Once left the second run looking at the first one's bin.
func loadBin() {
	if binLoaded == home && binPath != nil {
		return
	}
	binLoaded, binned_, binGaps = home, nil, nil
	binPath = map[string]bool{}
	readBin(binRoot())
}

// binRoot is the bin of the volume the profile is on. The recycle bin
// belongs to the volume rather than to the account, so a machine with a
// second disk has a second bin, and only this one is read. That is said
// in the README rather than left as a surprise.
func binRoot() string {
	return filepath.Join(filepath.VolumeName(home)+string(filepath.Separator), "$Recycle.Bin")
}

func readBin(root string) {
	if binPath == nil {
		binPath = map[string]bool{}
	}
	users, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, u := range users {
		if !u.IsDir() {
			continue
		}
		dir := filepath.Join(root, u.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			// Every account has a folder in here and you can only read
			// your own. That is not a gap in the report, it is the
			// permissions working.
			continue
		}
		present := map[string]bool{}
		for _, f := range files {
			if n := f.Name(); strings.HasPrefix(n, "$R") {
				present[strings.TrimPrefix(n, "$R")] = true
			}
		}
		for _, f := range files {
			name := f.Name()
			if !strings.HasPrefix(name, "$I") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				binGaps = append(binGaps, "a record in the recycle bin could not be read: "+err.Error())
				continue
			}
			b, ok := parseRecycleRecord(body)
			if !ok {
				continue
			}
			b.body = present[strings.TrimPrefix(name, "$I")]
			binned_ = append(binned_, b)
			binPath[strings.ToLower(b.path)] = true
		}
	}
}

func parseRecycleRecord(body []byte) (binned, bool) {
	c := newCursor(body)
	version := c.u64()
	b := binned{size: c.u64(), deleted: filetime(c.u64())}
	switch version {
	case 1:
		// Everything up to Windows 8 wrote a fixed 260 character field
		// and padded it, so the length has to come from the terminator.
		b.path = printable(c.wfixed(260))
	case 2:
		n := int(c.u32())
		if n <= 0 || n > 32768 {
			return b, false
		}
		b.path = printable(c.wfixed(n))
	default:
		return b, false
	}
	if c.bad || b.path == "" {
		return b, false
	}
	return b, true
}

// inBin says whether a path is sitting in the recycle bin right now.
func inBin(path string) bool {
	loadBin()
	return binPath[strings.ToLower(path)]
}

func scanRecycleBin(r *Report) {
	loadBin()
	for _, g := range binGaps {
		r.gap(g)
	}
	if len(binned_) == 0 {
		return
	}

	var (
		waiting group
		orphans group
		oldest  time.Time
		bytes   uint64
	)
	for _, b := range binned_ {
		if !b.deleted.IsZero() && (oldest.IsZero() || b.deleted.Before(oldest)) {
			oldest = b.deleted
		}
		row := maskName(filepath.Base(b.path))
		// The date goes first and the folder after a word, because the
		// wrapper works on words and collapses a run of spaces, so two
		// spaces used as a column separator inside the right hand side
		// come out as one and the date reads as part of the path.
		when := "at some unrecorded time"
		if !b.deleted.IsZero() {
			when = b.deleted.Format("2 Jan 2006")
		}
		if b.body {
			bytes += b.size
			waiting.add(row, when+", from "+maskUser(filepath.Dir(b.path)))
			continue
		}
		orphans.add(row, when+", from "+maskUser(filepath.Dir(b.path)))
	}

	if orphans.count > 0 {
		lines := orphans.top(12)
		r.add(Finding{
			Severity: Named,
			Title:    plural(orphans.count, "record in the recycle bin has lost its file and kept the description", "records in the recycle bin have lost their files and kept the descriptions"),
			Lines:    lines,
			Where:    maskUser(binRoot()),
			Advice: "The contents are gone and the original full path, the size and the moment of deletion are " +
				"still written down. This happens when something removes the $R half and leaves the $I half, which " +
				"is what a cleaner that deletes by pattern usually does.",
		})
	}

	if waiting.count > 0 {
		lines := waiting.top(8)
		title := plural(waiting.count, "file is in the recycle bin", "files are in the recycle bin") + ", " + megabytes(int64(bytes))
		if !oldest.IsZero() {
			title += ", the oldest deleted " + oldest.Format("2 January 2006")
		}
		r.add(Finding{
			Severity: Note,
			Title:    title,
			Lines:    lines,
			Where:    maskUser(binRoot()),
			Advice: "Each of these has a second file next to it holding the path the file came from and the " +
				"second it was deleted at. That is worth knowing before handing the machine to anybody.",
		})
	}
}

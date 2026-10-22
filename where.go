package main

// Where to look, and how to tell whether what a record points at is still
// there.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	home     string
	userName string
)

// setHome works out whose machine to read. AFTERIMAGE_HOME is how the
// tests work and how you read a Windows profile from inside WSL, which is
// the only way to check the Windows half of this on a machine that is
// running something else.
func setHome() error {
	if set := os.Getenv("AFTERIMAGE_HOME"); set != "" {
		home = filepath.Clean(set)
		userName = filepath.Base(home)
		return nil
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	home = filepath.Clean(dir)
	userName = filepath.Base(home)
	return nil
}

// The Windows paths are built out of the home directory rather than out of
// %APPDATA%, so that pointing AFTERIMAGE_HOME at /mnt/c/Users/somebody
// from inside WSL reads that profile properly instead of quietly reading
// nothing.
func roaming(rest ...string) string {
	return filepath.Join(append([]string{home, "AppData", "Roaming"}, rest...)...)
}

func local(rest ...string) string {
	return filepath.Join(append([]string{home, "AppData", "Local"}, rest...)...)
}

func under(rest ...string) string {
	return filepath.Join(append([]string{home}, rest...)...)
}

// A place is one file or folder that keeps records of files. Every one of
// them says which system it belongs to, because a Windows folder missing
// on Linux is not news and a Windows folder missing on Windows might be.
type Place struct {
	System string // "windows", "linux", "darwin", or "" for anywhere
	Path   string
	What   string
}

// Whether the record still matches something on the disk. The three
// answers are kept apart on purpose: the first version of this had two,
// and an unplugged drive came out as "the file is gone", which is the one
// thing it must not say.
type presence int

const (
	here presence = iota
	gone
	unreachable
)

// stillThere never touches the network and never walks onto a drive that
// is not mounted. A stat on a UNC path opens a connection to whatever is
// on the other end, which would make a tool that promises to open no
// sockets do exactly that, and would tell a file server that you are
// looking for a file you deleted.
func stillThere(path string) (presence, string) {
	if path == "" {
		return unreachable, "there is no path in the record"
	}
	if strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//") {
		return unreachable, "it is on a share, and checking would mean reaching out to it"
	}
	if vol := filepath.VolumeName(path); vol != "" && len(vol) == 2 {
		if _, err := os.Stat(vol + "\\"); err != nil {
			return unreachable, "drive " + vol + " is not mounted here"
		}
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return gone, ""
		}
		// Permission denied is not absence. Something is there and this
		// user cannot see it, which is a different sentence.
		return unreachable, "it could not be checked: " + err.Error()
	}
	return here, ""
}

// windowsPath is true for a path that came out of a Windows record, which
// decides whether stillThere can be trusted on the machine doing the
// reading. Reading a Windows profile from WSL means C:\ is not a thing
// here, and saying a file is gone because the drive letter does not exist
// on Linux would be a lie in every single row.
func windowsPath(path string) bool {
	return len(path) > 2 && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

// canCheck says whether an absence found on this machine means anything.
func canCheck(path string) bool {
	if windowsPath(path) && runtime.GOOS != "windows" {
		return false
	}
	if !windowsPath(path) && runtime.GOOS == "windows" && strings.HasPrefix(path, "/") {
		return false
	}
	return true
}

// A verdict on one record. Keeping in the bin apart from gone is the
// difference between telling somebody their file is unrecoverable and
// telling them it is sitting where they put it. The first run of this
// reported thirty seven deleted files on a machine where most of them were
// still in the recycle bin with their names on, which is the kind of wrong
// that costs a report the rest of its credibility.
type verdict int

const (
	stillHere verdict = iota
	nowGone
	inTheBin
	offTheMachine
	cannotSay
)

// judge decides what one record's target amounts to. driveType is the one
// out of a shell link, or zero where the record does not carry one.
func judge(path string, driveType uint32) verdict {
	if path == "" {
		return cannotSay
	}
	if !canCheck(path) {
		return cannotSay
	}
	switch state, why := stillThere(path); state {
	case here:
		return stillHere
	case gone:
		if inBin(path) {
			return inTheBin
		}
		// On Windows an unplugged drive is caught above, because the
		// letter is not there to stat. On Linux and macOS the mount point
		// is an ordinary directory, so a file on a stick that is not
		// plugged in stats exactly like a file somebody deleted. Asking
		// whether the mount point itself is there is what tells the two
		// apart, and getting it wrong reports the contents of a drive you
		// still own as gone for good.
		if root, ok := removableRoot(path); ok && !isDir(root) {
			return offTheMachine
		}
		return nowGone
	default:
		if driveType == 2 || driveType == 5 || removable(path) || strings.Contains(why, "not mounted") {
			return offTheMachine
		}
		return cannotSay
	}
}

// removable is where a path lives outside the machine, judged by name
// alone. The drive type in a Windows shortcut is better evidence and is
// used where there is one; this is for the records that do not carry one.
func removable(path string) bool {
	_, ok := removableRoot(path)
	return ok
}

// The places a removable volume gets mounted, and how many path elements
// make up the mount point, counting from the root. Both shapes of /media
// are in use: one distribution mounts at /media/LABEL and another at
// /media/user/LABEL, so both are tried and the volume counts as present if
// either of them is a directory.
//
// The counts include the prefix itself. Leaving it out made the deepest
// fallback for /media come out as /media, which exists on every Linux
// machine whether or not anything is mounted under it, so a file on an
// unplugged stick was reported as deleted rather than as out of reach.
var mountPoints = []struct {
	prefix string
	deep   []int
}{
	{"/media/", []int{3, 2}},
	{"/run/media/", []int{4}},
	{"/mnt/", []int{2}},
	{"/volumes/", []int{2}},
}

// removableRoot gives back the mount point a path sits under, and whether
// it is under one at all.
func removableRoot(path string) (string, bool) {
	slashed := filepath.ToSlash(path)
	lower := strings.ToLower(slashed)
	for _, m := range mountPoints {
		if !strings.HasPrefix(lower, m.prefix) {
			continue
		}
		parts := strings.Split(strings.Trim(slashed, "/"), "/")
		for _, deep := range m.deep {
			if len(parts) >= deep {
				root := "/" + strings.Join(parts[:deep], "/")
				if isDir(root) {
					return root, true
				}
			}
		}
		// None of them are there, so hand back the shallowest candidate:
		// the caller only wants to know that nothing is mounted.
		if deep := m.deep[len(m.deep)-1]; len(parts) >= deep {
			return "/" + strings.Join(parts[:deep], "/"), true
		}
		return strings.TrimSuffix(m.prefix, "/"), true
	}
	return "", false
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

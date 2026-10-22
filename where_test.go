package main

// Telling a deleted file apart from a file that is simply not reachable.
//
// This is the part of the program that decides what every finding says, so
// it is the part that can be wrong in the most damaging way: telling
// somebody a file is gone for good when it is on a stick in a drawer.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAFileOnAnUnmountedVolumeIsNotCalledDeleted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("these mount points are a Unix shape")
	}
	// Nothing is mounted at any of these on a test machine, so each one is
	// a file that cannot be reached rather than a file that was deleted.
	for _, path := range []string{
		"/media/somebody/HOLIDAY 2019/DSC_0413.jpg",
		"/media/BACKUP/archive.tar.gz",
		"/run/media/somebody/VERACRYPT/keys.kdbx",
		"/mnt/nothing-is-here/thing.txt",
		"/Volumes/Time Machine/photos/a.jpg",
	} {
		if got := judge(path, 0); got != offTheMachine {
			t.Errorf("%s was judged %v and it should be off the machine", path, got)
		}
	}
}

// The other half: a file under a mount point that really is mounted, and
// really is not there, is deleted and has to be reported as such.
func TestAFileUnderAMountedVolumeThatIsGoneIsDeleted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("these mount points are a Unix shape")
	}
	// The temporary directory is mounted by definition, so a missing file
	// under it is a missing file.
	missing := filepath.Join(t.TempDir(), "not-here.jpg")
	if got := judge(missing, 0); got != nowGone {
		t.Errorf("a missing file on a mounted filesystem was judged %v", got)
	}
	// And one that exists is left alone.
	here := filepath.Join(t.TempDir(), "here.jpg")
	if err := os.WriteFile(here, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := judge(here, 0); got != stillHere {
		t.Errorf("a file that is there was judged %v", got)
	}
}

func TestTheMountPointIsWorkedOutFromThePath(t *testing.T) {
	for path, want := range map[string]string{
		"/media/somebody/STICK/a.jpg":     "/media/somebody",
		"/run/media/somebody/STICK/a.jpg": "/run/media/somebody/STICK",
		"/mnt/data/a.jpg":                 "/mnt/data",
		"/Volumes/Backup/a.jpg":           "/Volumes/Backup",
	} {
		got, ok := removableRoot(path)
		if !ok {
			t.Errorf("%s was not recognised as being under a mount point", path)
			continue
		}
		// The shallow /media shape falls back to the two element form when
		// neither candidate is a directory, which is the case on any
		// machine running these tests.
		if got != want {
			t.Errorf("%s gave the mount point as %s, expected %s", path, got, want)
		}
	}
	for _, path := range []string{
		"/home/somebody/Documents/a.jpg",
		`C:\Users\somebody\Desktop\a.jpg`,
		"/usr/share/a.jpg",
		"",
	} {
		if root, ok := removableRoot(path); ok {
			t.Errorf("%s was taken for a removable volume at %s", path, root)
		}
	}
}

// A record that cannot be judged from the machine doing the reading must
// come back as unknown rather than as gone. Reading a Windows profile from
// Linux is the case this exists for: none of those drive letters are here.
func TestRecordsThatCannotBeJudgedAreNotCalledDeleted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows these paths can be judged")
	}
	for _, path := range []string{
		`C:\Users\somebody\Desktop\report.docx`,
		`D:\photos\a.jpg`,
	} {
		if got := judge(path, 0); got != cannotSay {
			t.Errorf("%s was judged %v from a machine that has no drive letters", path, got)
		}
	}
}

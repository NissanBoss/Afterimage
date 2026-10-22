package main

// Afterimage says what your machine still remembers about files that are
// not on it any more.
//
// Deleting a file does not delete the picture of it. Every desktop keeps a
// cache of thumbnails so that folders open quickly, and none of them are
// swept when the file goes: the thumbnail stays, and on Linux it stays
// with the full path of the original written inside the PNG. Alongside
// those are the lists nobody prunes, which keep the name, the folder, the
// size, the volume and the hour of everything you have opened, long after
// the thing itself is gone.
//
// The name is from the mark a bright image leaves on the eye after you
// look away. The file is gone and you are still looking at it.
//
// Two things separate this from the forensics tools that read the same
// files. The first is the direction: those exist to take evidence off
// somebody else's machine, and this exists to show you your own. The
// second is the cross check. A list of four thousand cached thumbnails is
// not a finding. The finding is which of them no longer match anything on
// the disk, because those are the ones you thought you had got rid of.
//
// It only reads. Nothing is written, moved, cleaned or deleted, and the
// tests read the source to keep it that way.

import (
	"flag"
	"fmt"
	"os"
)

var version = "unreleased"

const (
	exitQuiet  = 0
	exitDecide = 1 // something here is a decision, not a fact
	exitBroken = 2
	exitUnread = 3 // part of it could not be read, so nothing here is a pass
)

var showAll bool

func main() {
	flag.BoolVar(&reveal, "reveal", false, "print names, paths and hardware addresses in full")
	flag.BoolVar(&showAll, "all", false, "print every row instead of the first few of each kind")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Println("Afterimage " + version)
		return
	}
	if flag.NArg() > 0 {
		usage()
		os.Exit(exitBroken)
	}
	if err := setHome(); err != nil {
		fmt.Fprintf(os.Stderr, "afterimage: %v\n", err)
		os.Exit(exitBroken)
	}

	r := &Report{}
	for _, look := range []func(*Report){
		scanRecent,
		scanJumpLists,
		scanThumbcache,
		scanRecycleBin,
		scanThumbnails,
		scanRecentlyUsed,
		scanDSStore,
		scanQuickLook,
		scanDownloads,
		scanLeftovers,
	} {
		look(r)
	}

	r.render(os.Stdout, "Afterimage  ("+maskUser(home)+")")

	switch {
	case r.worst() >= Named:
		os.Exit(exitDecide)
	case len(r.Gaps) > 0:
		os.Exit(exitUnread)
	}
	os.Exit(exitQuiet)
}

func usage() {
	fmt.Fprintln(os.Stderr, `Afterimage - what your machine still remembers about files you deleted

  afterimage              read this account
  afterimage --reveal     with the names and the paths in full
  afterimage --all        every row, not the first few of each kind
  afterimage --version

Deleting a file does not delete the thumbnail of it, and nothing sweeps
the caches when the file goes. Around them sit the lists nobody prunes:
one record per file you have ever opened, holding the name, the folder,
the size, the volume it was on and the hour you opened it.

This reads those caches and lists, and then does the part that makes them
mean something: it checks each record against the disk. What comes out is
the set of files your machine can still name, or still show, and that you
no longer have.

Set AFTERIMAGE_HOME to read a different profile, which is how you check a
Windows account from inside WSL.

Nothing is written, moved, cleaned or deleted.`)
}

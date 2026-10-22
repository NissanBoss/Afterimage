# Changelog

## v1.1

Published again, from a repository of its own with a fresh history; v1
lived in the one before.

- **macOS: the Quick Look cache is found again on current systems.** Apple
  moved it. It used to sit straight under the per user caches directory,
  and now the thumbnail service has a directory of its own with the cache
  inside it. Both are looked at, because a Mac that has been upgraded can
  still have the old one sitting there full of what was in it. Until this,
  a current Mac with a cache on it was read and reported as clean, which is
  the worst way for this to be wrong: not a wrong answer, a confident empty
  one. Caught by running the tests against a real macOS machine for the
  first time.

## v1

The first one.

Reads the caches and lists that keep a record of every file you open, and
then checks each record against the disk, so what comes out is the set of
files the machine can still name or still show and that are not there any
more.

- **Windows.** The Recent folder, with shell links parsed by hand down to
  the tracker block, which carries the name of the machine and the hardware
  address of a card in it. The jump lists, read out of the compound files
  they live in. The thumbnail cache. The recycle bin, including the records
  whose file has been taken out from underneath them.
- **Linux.** The freedesktop thumbnail cache, where every thumbnail carries
  the address of its original inside the PNG, plus the fail directory and
  the GTK and KDE recent document lists.
- **macOS.** The Quick Look cache, which keeps the name and the folder of
  everything ever previewed, including what was inside an encrypted volume.
- **Everywhere.** Download history from seven browsers, and editor
  leftovers: vim swap files, which hold the text of the document, and the
  Office and LibreOffice lock files, which hold a person's name.
- **And `.DS_Store`**, which the Finder leaves in every folder it shows and
  which keeps a record per file that nothing removes when the file goes.
  These travel: into repositories, onto memory sticks, across shared
  drives, invisible on Windows and Linux to whoever is carrying them. One
  found inside a repository is reported louder than the rest.

No dependencies. Every format is read here, including SQLite, because
opening the real library would mean writing to the file to read it.

Four piles that never mix: a copy that got off the machine, a copy of what
was inside a file, a name with no file, and what the records say about you
rather than about a file. A file sitting in the recycle bin is counted as
none of those.

Names are covered over unless you ask for them with `--reveal`.

# Afterimage

What your machine still remembers about the files you deleted.

```
    [OUTSIDE ] 2 pictures are of files on volumes that are not mounted
              DS•••••.jpg                       E:\photos
              DS•••••.jpg                       E:\photos

              The thumbnail is on the internal disk and the file is on
              something you unplugged, locked or unmounted. This is the case
              the caches are worst at: opening an encrypted volume in a file
              manager once is enough to leave pictures of what was inside it
              out here in the clear.

    [OUTSIDE ] 3 files named in .DS_Store files inside repositories are not
              in their folders any more
              bo•••••.numbers                   C:\demo\<you>\Documents\entrega, inside a
                                                repository
              ca•••••.docx                      C:\demo\<you>\Documents\entrega, inside a
                                                repository

    [COPIED  ] 2 pictures are here of files that are not on the disk any more
              ca•••••.png                       C:\demo\<you>\Desktop
              co•••••.pdf                       C:\demo\<you>\Desktop

    [COPIED  ] 1 editor leftover belongs to a document that is not there any more
              in•••••.md                        a vim swap file, with the text of the
                                                document in it, left by a•••••

    [NAMED   ] 5 files are named in Recent and are not on the disk any more
              ba•••••                           C:\demo\<you>\Desktop
              ca•••••                           C:\demo\<you>\Desktop
              co•••••                           C:\demo\<you>\Desktop

    [TELLS   ] Every shortcut in Recent carries the name of this machine, and
              most carry the address of a card in it
                machine name                    ••••• (6 records)
                hardware address                04:**:**:**:**:** (6 records)
```

Deleting a file does not delete the picture of it.

Every desktop keeps a cache of thumbnails so that folders open quickly, and
none of them are swept when the file goes. The photograph you deleted last
year is still in there at up to 1280 pixels on the long side, which is not
a thumbnail in any useful sense of the word: it is large enough to read a
document over somebody's shoulder or recognise a face. Emptying the recycle
bin does not touch it. Nothing in normal use ever does.

Around the pictures sit the lists nobody prunes. Windows writes a shortcut
into a folder every time anything opens anything, and the shortcut is named
after the file, so the folder listing on its own is a plain text list of
what you have opened. Inside each one is the full path, the size the file
was, the three timestamps it had, the label and serial number of the volume
it was on, the name of the machine, and the address of a network card in
it. The browser keeps every download with the address of the page you got
it from. The recycle bin keeps a second file per deletion holding the
original path and the second it happened.

All of it outlives the file.

## The part nobody else does

A list of four thousand cached thumbnails is not a finding. Anybody can
count files.

The finding is which of them no longer match anything on the disk, because
those are the ones you thought you had got rid of. So every record this
reads gets checked against the filesystem, and the answer goes into one of
four piles that are never mixed:

- **OUTSIDE** is a copy that got out of the place that was holding it. The
  stick you unplugged, the volume you locked, the share you left. The
  original is behind something and this is not. It is the worst one, and it
  is the reason to run this at all.
- **COPIED** is a copy of what was inside a file that is not there any
  more. A picture of it, or in the case of an abandoned editor swap file,
  the text of it.
- **NAMED** is the name and the full path of something that is gone,
  sitting in a file nothing prunes.
- **TELLS** is what these records say about you rather than about a file.
  The file is still there. You are the thing that leaked.

There is a fifth answer and it is the one that took the most care: a file
sitting in the recycle bin is **not** counted as gone. The first run of
this reported thirty seven deleted files on a machine where most of them
were still in the bin with their names on. Telling somebody their file is
unrecoverable when it is where they put it is the kind of wrong that costs
a report the rest of its credibility.

## What it reads

**Windows.** The Recent folder, one shell link per file ever opened, parsed
by hand: the path, the volume, the timestamps and the tracker block. The
jump lists, which are a second and separate set of recent files kept per
application inside compound files. The thumbnail cache. The recycle bin,
including the records whose file has been removed from underneath them.

**Linux.** The freedesktop thumbnail cache, which is the strongest case
here, because the standard says each thumbnail must carry the address of
its original in a text chunk inside the PNG. So unlike the Windows cache,
which can only be counted, this one names its own originals. Also the fail
directory, which keeps an empty PNG and a full path for everything a
thumbnailer choked on, and the GTK and KDE recent document lists, which
record which program opened which file how many times.

**macOS.** The Quick Look cache, where pressing space on a file puts the
preview and the name and the folder into a database under `/var/folders`
that nothing cleans. Browsing a mounted encrypted volume in the Finder puts
the contents of it out here in the clear, which has been
[written up](https://objective-see.org/blog/blog_0x30.html)
[repeatedly](https://wojciechregula.blog/post/your-encrypted-photos-in-macos-cache/)
since at least 2010 and is still true.

**Everywhere.** Download history from Chrome, Edge, Brave, Chromium,
Vivaldi, Opera and Firefox, which is a list of files with the address each
one came from. Editor leftovers: vim swap files, which hold the text of the
document along with the account and host name of whoever was editing,
Office and LibreOffice lock files, which hold a person's name, and emacs
autosaves.

And `.DS_Store`, which is the one that travels. The Finder drops one into
every folder it has ever shown you, to remember where the icons were, and
to do that it keeps a record per file, keyed by the file's name. Nothing
removes the record when the file goes. So a `.DS_Store` is a list of what
used to be in a folder, and the cross check here is the tightest in the
program: the file lists names, the folder lists names, and a name in the
first and not the second is a file that was there and is not.

They get committed to repositories, copied onto memory sticks and left on
shared drives, and on Windows and Linux they are invisible to whoever is
carrying them around. One inside a repository is reported louder than the
rest, because it is a commit away from telling everybody who clones it what
was in that folder before it was made public.

No dependencies. It reads every one of those formats itself, including
SQLite, because opening the real library would mean writing to somebody's
browser history to read it.

## What already exists

Two kinds of thing, and this is neither.

**Forensics tools**, which exist to take evidence off somebody else's
machine. [LECmd](https://github.com/EricZimmerman/LECmd) and
[JLECmd](https://github.com/EricZimmerman/JLECmd) parse shell links and
jump lists properly and are the reference for those formats;
[Thumbcache Viewer](https://github.com/thumbcacheviewer/thumbcacheviewer)
pulls the pictures out of a Windows cache. They are good at what they do,
which is to dump one artefact into a spreadsheet for an investigator who
already knows what they are looking for. None of them check anything
against the disk, none of them read the other artefacts, and none of them
are pointed at you.

**Cleaners**, like [BleachBit](https://github.com/bleachbit/bleachbit),
which delete these caches without telling you what was in them. That is a
reasonable thing to want and it is the opposite end of the job: you have to
know what is there before deciding it should go, and a tool that empties
your thumbnail cache tells you nothing about the two years it was holding.

This is the other direction. Same files, read for the person they are
about, with the one step that makes them mean something.

## What it does not cover

The part that makes the rest believable.

**The Windows thumbnail cache cannot name its pictures.** From Windows 8
onwards each entry is keyed by a hash of the file's path and that hash is
the only identifier stored, so this can tell you there are 1732 readable
pictures sitting in 22 MB and it cannot tell you which file any one of them
came from. Anything that claims otherwise is reading the search index,
which is a different file and a different promise. The Linux cache does
carry paths, which is why the two are reported so differently.

**The write ahead log is not read.** A browser's most recent rows can be
sitting in a `-wal` file that has not been folded into the database yet.
Reading it properly would mean replaying a journal, and reading it badly
would mean inventing rows. So this misses recent downloads rather than
making them up, which is the right direction for the mistake.

**A `.DS_Store` is only read for names.** It also holds icon positions,
window sizes, sort orders and in some versions a modification date per
file, none of which are reported. The names are the part that says what was
there. And only the ones inside the walked folders are found, for the same
reason as the editor leftovers below.

**macOS recent documents are not read**: the `.sfl2`
files are keyed archives inside binary property lists, and that is two
formats deep before reaching a filename.

**Only one recycle bin.** It belongs to the volume rather than to the
account, so a machine with a second disk has a second bin and this reads
the one the profile is on.

**The walk goes five levels down and forty thousand folders in**, not
across the whole disk. That is the one walk that finds both the editor
leftovers and the `.DS_Store` files, and the report says where it looked
and says so if it stopped early.

**Nothing in the registry**, so the Office and Run dialog recent lists are
not covered. That would mean a dependency and it is not worth one.

**The jump list application is a hash.** Windows names each list after a
hash of the path of the executable. The published tables of those hashes
are collected by hand and get things wrong, and a report that says Notepad
when it means something else is worse than one that says it does not know.

## The rules it follows

**It only reads.** Nothing is written, moved, cleaned or deleted. This is
not a promise in a readme: tests read the source and fail the build if a
write call ever turns up in it.

**It opens no sockets, and it never stats a network path.** A stat on a
`\\server\share` path opens a connection to whatever is on the other end,
which would break the first rule and would tell a file server that somebody
is looking for a file they deleted. Those records are reported as unchecked
and left alone.

**Names are covered over by default.** This is a report about the files you
keep, which makes the report itself a list of the files you keep, and
people paste these into issues and screenshots. The finding survives the
masking: forty forgotten files in Downloads is the same finding whether or
not you can read their names. `--reveal` is there for the one person who
needs to know which forty. What is never masked is anything that names a
product rather than a person, because a row that will not say which browser
to go and clear is a row nobody can act on.

**A gap is not a pass.** What could not be read prints first, before the
findings, because it changes how everything under it should be taken. A
locked browser database, a folder without permission, a record pointing at
a drive that is not mounted: each of those is said out loud rather than
quietly counted as clean.

**It does not tell you to fix things it cannot see the cost of.** Emptying
these caches is safe and the report says how. It stops there. Turning every
one of them off makes a machine that behaves like nobody else's, and unique
is traceable.

## Running it

```bash
afterimage
```

```bash
afterimage --reveal
```

`--all` prints every row instead of the first few of each kind. `--version`
prints the version.

Set `AFTERIMAGE_HOME` to read a different profile, which is how you check a
Windows account from inside WSL:

```bash
AFTERIMAGE_HOME=/mnt/c/Users/somebody afterimage
```

Exit code is 1 when something is named or pictured, 3 when part of it could
not be read, 0 when nothing turned up.

Windows marks anything that came from the internet, so the first run may
bring up a blue "Windows protected your PC" box. Right click the zip,
Properties, tick Unblock at the bottom, and then extract it.

## Building

Needs [Go](https://go.dev) and nothing else. No dependencies at all.

```bash
go build -o afterimage .
```

```bash
go test ./...
```

The tests check the readers against files written by the things that write
them in the real world: shortcuts made by Windows through the same COM
object every installer uses, and SQLite databases built by Python, with
enough rows to force interior pages and values long enough to spill onto
overflow pages. Fixtures written by hand only prove that a reader agrees
with whoever wrote the fixture.

## Licence

MIT. Do what you like with it.

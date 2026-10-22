package main

// Windows shell links, which are the reason this program started.
//
// A .lnk is not a link. It is a record of an opening: the full path, the
// size and the three timestamps the file had at the time, the serial
// number and the label of the volume it was on, and in most of them the
// NetBIOS name of the machine and a pair of identifiers that are version 1
// UUIDs, which means the last six bytes of each one are the hardware
// address of a network card in the machine that made it.
//
// Windows writes one of these into Recent every time anything opens
// anything, and nothing prunes the folder. So the record outlives the
// file, outlives the drive it was on, and keeps the name of the file in
// the clear in the name of the .lnk itself.
//
// The layout is MS-SHLLINK. What is read here is the header, the LinkInfo
// with the path and the volume, the string data and the extra data
// blocks. The LinkTargetIDList is walked far enough to be skipped: it is
// a stack of shell items with their own formats per item type, and the
// path is already in the LinkInfo of anything that has one.

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	lnkHeaderSize   = 0x4C
	hasTargetIDList = 1 << 0
	hasLinkInfo     = 1 << 1
	hasName         = 1 << 2
	hasRelativePath = 1 << 3
	hasWorkingDir   = 1 << 4
	hasArguments    = 1 << 5
	hasIconLocation = 1 << 6
	isUnicode       = 1 << 7
)

type Link struct {
	// Path is the local path out of the LinkInfo, which is the one that
	// says where the file actually was.
	Path    string
	NetPath string

	Size     uint32
	Attrs    uint32
	Written  time.Time
	Created  time.Time
	Accessed time.Time

	DriveType uint32
	Serial    uint32
	Label     string

	Relative   string
	WorkingDir string
	Arguments  string
	Name       string

	Machine  string
	MAC      string
	BirthMAC string

	Gaps []string
}

func (l *Link) note(format string, args ...any) {
	l.Gaps = append(l.Gaps, fmt.Sprintf(format, args...))
}

// Where returns the best path the record carries. A shortcut to something
// on a share has no local path at all, and one to a virtual folder such as
// This PC has neither, which is not damage and is not reported as any.
func (l *Link) Where() string {
	if l.Path != "" {
		return l.Path
	}
	return l.NetPath
}

func (l *Link) drive() string {
	switch l.DriveType {
	case 2:
		return "a removable drive"
	case 3:
		return "a fixed drive"
	case 4:
		return "a network drive"
	case 5:
		return "a cd or dvd"
	case 6:
		return "a ram disk"
	}
	return ""
}

func (l *Link) isDir() bool { return l.Attrs&0x10 != 0 }

func readLink(path string) (*Link, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseLink(body)
}

func parseLink(body []byte) (*Link, error) {
	c := newCursor(body)
	if c.u32() != lnkHeaderSize {
		return nil, fmt.Errorf("no shell link header")
	}
	clsid := c.take(16)
	// 00021401-0000-0000-C000-000000000046. Checking it keeps this from
	// trying to read every stray file in a folder as a shortcut, which
	// matters because it walks folders it did not choose.
	if clsid[0] != 0x01 || clsid[1] != 0x14 || clsid[2] != 0x02 || clsid[8] != 0xC0 || clsid[15] != 0x46 {
		return nil, fmt.Errorf("not a shell link")
	}

	l := &Link{}
	flags := c.u32()
	l.Attrs = c.u32()
	l.Created = filetime(c.u64())
	l.Accessed = filetime(c.u64())
	l.Written = filetime(c.u64())
	l.Size = c.u32()
	c.skip(4)  // icon index
	c.skip(4)  // show command
	c.skip(2)  // hotkey
	c.skip(10) // three reserved fields
	if c.bad {
		return nil, fmt.Errorf("the header stops in the middle")
	}

	if flags&hasTargetIDList != 0 {
		size := int(c.u16())
		c.skip(size)
		if c.bad {
			l.note("the list of shell items runs past the end of the file")
			return l, nil
		}
	}

	if flags&hasLinkInfo != 0 {
		l.readLinkInfo(c)
	}

	wide := flags&isUnicode != 0
	for _, s := range []struct {
		flag uint32
		into *string
	}{
		{hasName, &l.Name},
		{hasRelativePath, &l.Relative},
		{hasWorkingDir, &l.WorkingDir},
		{hasArguments, &l.Arguments},
		{hasIconLocation, new(string)},
	} {
		if flags&s.flag == 0 {
			continue
		}
		n := int(c.u16())
		if wide {
			*s.into = printable(c.wfixed(n))
			continue
		}
		*s.into = printable(string(c.take(n)))
	}
	if c.bad {
		l.note("the strings at the end of the shortcut are cut short")
		return l, nil
	}

	l.readExtra(c)
	return l, nil
}

// readLinkInfo is where the path lives, along with the volume it was on.
// Everything in it is addressed by an offset from the start of the
// structure rather than laid out in order, so it is read with a second
// cursor over the same bytes.
func (l *Link) readLinkInfo(c *cursor) {
	start := c.pos
	size := int(c.u32())
	if size < 0x1C || c.pos-4+size > len(c.b) {
		l.note("the block holding the path is the wrong size")
		c.bad = true
		return
	}
	// The whole structure is bounded by its own size field, so the
	// offsets inside it are held to it rather than to the end of the
	// file. An offset that reaches past the block into the extra data
	// would otherwise read a machine identifier as a path.
	info := newCursor(c.b[start : start+size])
	c.seek(start + size)

	info.seek(4)
	headerSize := int(info.u32())
	flags := info.u32()
	volumeOffset := int(info.u32())
	basePathOffset := int(info.u32())
	netOffset := int(info.u32())
	suffixOffset := int(info.u32())
	basePathUnicode, suffixUnicode := 0, 0
	if headerSize >= 0x24 {
		basePathUnicode = int(info.u32())
		suffixUnicode = int(info.u32())
	}
	if info.bad {
		l.note("the header of the path block is cut short")
		return
	}

	suffix := ""
	if suffixUnicode > 0 {
		suffix = info.jump(suffixUnicode).wstring()
	} else if suffixOffset > 0 {
		suffix = info.jump(suffixOffset).cstring()
	}

	if flags&1 != 0 {
		base := ""
		if basePathUnicode > 0 {
			base = info.jump(basePathUnicode).wstring()
		} else if basePathOffset > 0 {
			base = info.jump(basePathOffset).cstring()
		}
		l.Path = printable(base + suffix)
		l.readVolume(info.jump(volumeOffset))
	}

	if flags&2 != 0 && netOffset > 0 {
		l.readNetwork(info.jump(netOffset), suffix)
	}
}

func (l *Link) readVolume(v *cursor) {
	size := int(v.u32())
	l.DriveType = v.u32()
	l.Serial = v.u32()
	labelOffset := int(v.u32())
	// 0x14 is the signal that the ASCII label is empty and a unicode one
	// follows. Reading the ASCII field anyway gives an empty label on
	// every volume whose name is not plain ASCII, which on a Spanish
	// machine is most of the interesting ones.
	if labelOffset == 0x14 {
		labelOffsetUnicode := int(v.u32())
		if labelOffsetUnicode > 0 && labelOffsetUnicode < size {
			l.Label = printable(v.jump(v.pos - 20 + labelOffsetUnicode).wstring())
		}
		return
	}
	if labelOffset > 0 && labelOffset < size {
		l.Label = printable(v.jump(v.pos - 16 + labelOffset).cstring())
	}
}

func (l *Link) readNetwork(n *cursor, suffix string) {
	base := n.pos
	n.skip(4) // size
	flags := n.u32()
	nameOffset := int(n.u32())
	if nameOffset == 0 {
		return
	}
	name := ""
	if nameOffset > 0x14 {
		n.skip(8) // device name offset, network provider type
		if unicodeOffset := int(n.u32()); unicodeOffset > 0 {
			name = n.jump(base + unicodeOffset).wstring()
		}
	}
	if name == "" {
		name = n.jump(base + nameOffset).cstring()
	}
	_ = flags
	if name != "" {
		l.NetPath = printable(strings.TrimRight(name, "\\") + "\\" + strings.TrimLeft(suffix, "\\"))
	}
}

// readExtra walks the blocks on the end. The one that matters is the
// tracker, which exists so that Windows can find a file that has been
// renamed or moved, and which pays for that by writing down the machine
// and the card in it.
func (l *Link) readExtra(c *cursor) {
	for c.left() >= 4 {
		size := int(c.u32())
		if size < 4 {
			return // the terminal block, whose size is under four
		}
		if size-4 > c.left() {
			l.note("an extra block claims more bytes than the file has")
			return
		}
		body := newCursor(c.take(size - 4))
		switch body.u32() {
		case 0xA0000003:
			l.readTracker(body)
		}
	}
}

func (l *Link) readTracker(t *cursor) {
	t.skip(4) // length
	t.skip(4) // version
	l.Machine = printable(strings.TrimRight(string(t.take(16)), "\x00"))
	l.MAC = macFromUUID(t.take(16 + 16)[16:])
	l.BirthMAC = macFromUUID(t.take(16 + 16)[16:])
	if t.bad {
		l.MAC, l.BirthMAC = "", ""
		l.note("the tracker block is shorter than it says it is")
	}
}

// macFromUUID pulls the hardware address out of a version 1 UUID. The
// droid identifiers in a shortcut are generated the old way, from a
// timestamp and the address of a card, which is the same property that
// put a name to the author of the Melissa document in 1999.
//
// It returns nothing unless the UUID really is version 1 with the right
// variant bits. Windows falls back to a random identifier on machines
// with no network card at all, and printing six random bytes as somebody's
// hardware address would be a confident lie.
func macFromUUID(g []byte) string {
	if len(g) < 16 {
		return ""
	}
	version := g[7] >> 4
	variant := g[8] & 0xC0
	if version != 1 || variant != 0x80 {
		return ""
	}
	node := g[10:16]
	// The multicast bit is set on a node that was made up rather than read
	// off a card, which is what the specification says to do when there is
	// no address to be had.
	if node[0]&0x01 != 0 {
		return ""
	}
	if node[0]|node[1]|node[2]|node[3]|node[4]|node[5] == 0 {
		return ""
	}
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", node[0], node[1], node[2], node[3], node[4], node[5])
}

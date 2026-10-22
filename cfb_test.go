package main

// The compound file reader.
//
// The fixture is built here because nothing on a Linux or macOS runner can
// produce one, and the shape that matters is the small stream area: every
// entry in a real jump list is a few hundred bytes, which is under the
// cutoff, so they all live in the mini stream rather than in ordinary
// sectors. A reader that only handles ordinary sectors opens every jump
// list on the machine and finds a directory full of empty streams, with no
// error anywhere.

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"unicode/utf16"
)

const sectorSize = 512

// makeCompound writes a v3 compound file with every stream in the mini
// stream, which is what a jump list looks like.
func makeCompound(streams map[string][]byte) []byte {
	names := make([]string, 0, len(streams))
	for name := range streams {
		names = append(names, name)
	}
	sort.Strings(names)

	// Lay the streams out in 64 byte mini sectors, back to back.
	var mini []byte
	starts := map[string]uint32{}
	for _, name := range names {
		starts[name] = uint32(len(mini) / miniSectorLen)
		body := streams[name]
		mini = append(mini, body...)
		for len(mini)%miniSectorLen != 0 {
			mini = append(mini, 0)
		}
	}
	miniSectors := len(mini) / miniSectorLen
	miniStreamSectors := (len(mini) + sectorSize - 1) / sectorSize
	if miniStreamSectors == 0 {
		miniStreamSectors = 1
	}

	header := make([]byte, sectorSize)
	binary.LittleEndian.PutUint64(header, 0xE11AB1A1E011CFD0)
	binary.LittleEndian.PutUint16(header[0x18:], 0x3E)
	binary.LittleEndian.PutUint16(header[0x1A:], 3)
	binary.LittleEndian.PutUint16(header[0x1C:], 0xFFFE)
	binary.LittleEndian.PutUint16(header[0x1E:], 9) // 512 byte sectors
	binary.LittleEndian.PutUint16(header[0x20:], 6) // 64 byte mini sectors
	binary.LittleEndian.PutUint32(header[0x2C:], 1) // one sector of table
	binary.LittleEndian.PutUint32(header[0x30:], 1) // the directory is sector 1
	binary.LittleEndian.PutUint32(header[0x38:], 4096)
	binary.LittleEndian.PutUint32(header[0x3C:], 2) // the mini table is sector 2
	binary.LittleEndian.PutUint32(header[0x40:], 1)
	binary.LittleEndian.PutUint32(header[0x44:], endOfChain)
	for i := 0; i < 109; i++ {
		binary.LittleEndian.PutUint32(header[0x4C+i*4:], freeSector)
	}
	binary.LittleEndian.PutUint32(header[0x4C:], 0) // sector 0 holds the table

	// The allocation table. Sector 0 is itself, 1 is the directory, 2 is
	// the mini table, and 3 onwards is the mini stream.
	fat := make([]byte, sectorSize)
	put := func(b []byte, i int, v uint32) { binary.LittleEndian.PutUint32(b[i*4:], v) }
	for i := 0; i < sectorSize/4; i++ {
		put(fat, i, freeSector)
	}
	put(fat, 0, fatSector)
	put(fat, 1, endOfChain)
	put(fat, 2, endOfChain)
	for i := 0; i < miniStreamSectors; i++ {
		if i == miniStreamSectors-1 {
			put(fat, 3+i, endOfChain)
			break
		}
		put(fat, 3+i, uint32(4+i))
	}

	// The mini table: each stream's sectors run on until the last one.
	miniFAT := make([]byte, sectorSize)
	for i := 0; i < sectorSize/4; i++ {
		put(miniFAT, i, freeSector)
	}
	for _, name := range names {
		start := int(starts[name])
		used := (len(streams[name]) + miniSectorLen - 1) / miniSectorLen
		for i := 0; i < used; i++ {
			if i == used-1 {
				put(miniFAT, start+i, endOfChain)
				continue
			}
			put(miniFAT, start+i, uint32(start+i+1))
		}
	}

	entries := make([]byte, 0, (len(names)+1)*dirEntrySize)
	entry := func(name string, kind byte, start uint32, size uint64) []byte {
		e := make([]byte, dirEntrySize)
		units := utf16.Encode([]rune(name))
		for i, u := range units {
			binary.LittleEndian.PutUint16(e[i*2:], u)
		}
		binary.LittleEndian.PutUint16(e[0x40:], uint16(len(units)*2+2))
		e[0x42] = kind
		e[0x43] = 1 // black
		binary.LittleEndian.PutUint32(e[0x44:], 0xFFFFFFFF)
		binary.LittleEndian.PutUint32(e[0x48:], 0xFFFFFFFF)
		binary.LittleEndian.PutUint32(e[0x4C:], 0xFFFFFFFF)
		binary.LittleEndian.PutUint32(e[0x74:], start)
		binary.LittleEndian.PutUint64(e[0x78:], size)
		return e
	}
	entries = append(entries, entry("Root Entry", 5, 3, uint64(miniSectors*miniSectorLen))...)
	for _, name := range names {
		entries = append(entries, entry(name, 2, starts[name], uint64(len(streams[name])))...)
	}
	for len(entries)%sectorSize != 0 {
		entries = append(entries, 0)
	}

	out := append([]byte{}, header...)
	out = append(out, fat...)
	out = append(out, entries[:sectorSize]...) // one sector of directory is enough here
	out = append(out, miniFAT...)
	out = append(out, mini...)
	for len(out)%sectorSize != 0 {
		out = append(out, 0)
	}
	return out
}

func TestReadsStreamsOutOfTheMiniStream(t *testing.T) {
	link := makeLink(linkParts{path: `C:\Users\x\Desktop\notes.txt`, label: "OS", driveType: 3, machine: "ka"})
	long := bytes.Repeat([]byte("abcdefgh"), 200) // several mini sectors
	c, err := openCompound(makeCompound(map[string][]byte{
		"1":        link,
		"DestList": long,
	}))
	if err != nil {
		t.Fatal(err)
	}

	got := map[string][]byte{}
	for _, e := range c.entries {
		if e.Kind == 2 {
			got[e.Name] = c.stream(e)
		}
	}
	if len(got) != 2 {
		t.Fatalf("found %d streams, not 2: %v", len(got), got)
	}
	if !bytes.Equal(got["1"], link) {
		t.Errorf("the shortcut stream came back %d bytes long and it is %d", len(got["1"]), len(link))
	}
	if !bytes.Equal(got["DestList"], long) {
		t.Errorf("a stream spanning several mini sectors came back wrong")
	}
	if l, err := parseLink(got["1"]); err != nil {
		t.Errorf("the stream did not parse as a shortcut: %v", err)
	} else if l.Path != `C:\Users\x\Desktop\notes.txt` {
		t.Errorf("the path came back as %q", l.Path)
	}
}

// A jump list is a file from the machine and a corrupt one must not put
// the reader into a loop or past the end of the buffer.
func TestBrokenCompoundFilesStillFinish(t *testing.T) {
	body := makeCompound(map[string][]byte{"1": bytes.Repeat([]byte("x"), 300)})
	broken := map[string][]byte{
		"cut in half": body[:len(body)/2],
		"no header":   body[512:],
		"empty":       {},
	}
	// A table that points a sector at itself, which is the shape that
	// turns a chain walk into a loop.
	loop := append([]byte{}, body...)
	binary.LittleEndian.PutUint32(loop[512+3*4:], 3)
	broken["a sector chain that eats itself"] = loop

	for name, b := range broken {
		c, err := openCompound(b)
		if err != nil {
			continue
		}
		for _, e := range c.entries {
			c.stream(e)
		}
		t.Logf("%s: read without falling over", name)
	}
}

// A sanity check against the real thing, when the machine has one. This
// does not assert on the contents, because they belong to whoever is
// running it: it asserts that the reader gets streams out at all, which
// is what would break silently if the mini stream handling regressed.
func TestReadsARealJumpListWhenThereIsOne(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("jump lists only exist on Windows")
	}
	if err := setHome(); err != nil {
		t.Skip(err)
	}
	dir := roaming("Microsoft", "Windows", "Recent", "AutomaticDestinations")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Skip("this account has no jump lists")
	}
	streams, links := 0, 0
	for _, e := range entries {
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".automaticdestinations-ms") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		c, err := openCompound(body)
		if err != nil {
			t.Errorf("%s did not open as a compound file: %v", e.Name(), err)
			continue
		}
		for _, entry := range c.entries {
			if entry.Kind != 2 {
				continue
			}
			streams++
			if s := c.stream(entry); len(s) > 0 {
				if _, err := parseLink(s); err == nil {
					links++
				}
			}
		}
	}
	if streams == 0 {
		t.Skip("no streams in any jump list on this account")
	}
	if links == 0 {
		t.Errorf("read %d streams out of the real jump lists and not one parsed as a shortcut", streams)
	}
	t.Logf("%d of %d streams in the real jump lists parsed as shortcuts", links, streams)
}

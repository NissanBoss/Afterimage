package main

// Shell links, checked two ways.
//
// The first is a link built here byte by byte, which is the only way to
// test the parts Windows will not produce on demand: a removable volume
// with a label, a tracker block with a known hardware address in it, and
// the several ways the structure can be cut short.
//
// The second is a link made by Windows itself through the same COM object
// every installer uses. That is the one that catches a misread of the real
// format rather than agreement with the fixture builder, and it is skipped
// everywhere except on Windows.

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

type linkParts struct {
	path      string
	label     string
	driveType uint32
	serial    uint32
	machine   string
	mac       []byte
	directory bool
}

func makeLink(p linkParts) []byte {
	var out []byte
	u16 := func(v uint16) { out = binary.LittleEndian.AppendUint16(out, v) }
	u32 := func(v uint32) { out = binary.LittleEndian.AppendUint32(out, v) }
	u64 := func(v uint64) { out = binary.LittleEndian.AppendUint64(out, v) }

	u32(lnkHeaderSize)
	out = append(out, 0x01, 0x14, 0x02, 0x00, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0, 0x46)
	flags := uint32(hasLinkInfo | isUnicode)
	u32(flags)
	attrs := uint32(0x20)
	if p.directory {
		attrs = 0x10
	}
	u32(attrs)
	for i := 0; i < 3; i++ {
		u64(133700000000000000) // creation, access and write, all the same
	}
	u32(4096) // size
	u32(0)    // icon index
	u32(1)    // show command
	u16(0)    // hot key
	out = append(out, make([]byte, 10)...)

	// LinkInfo, with every offset counted from the front of it.
	label := append([]byte(p.label), 0)
	path := append([]byte(p.path), 0)
	const headerSize = 0x1C
	volumeSize := 16 + len(label)
	volumeOffset := headerSize
	pathOffset := volumeOffset + volumeSize
	suffixOffset := pathOffset + len(path)
	total := suffixOffset + 1

	u32(uint32(total))
	u32(headerSize)
	u32(1) // there is a volume id and a local path
	u32(uint32(volumeOffset))
	u32(uint32(pathOffset))
	u32(0) // no network link
	u32(uint32(suffixOffset))

	u32(uint32(volumeSize))
	u32(p.driveType)
	u32(p.serial)
	u32(0x10) // the label sits straight after this field
	out = append(out, label...)
	out = append(out, path...)
	out = append(out, 0) // the empty common path suffix

	if p.machine != "" || p.mac != nil {
		u32(0x60)
		u32(0xA0000003)
		u32(0x58)
		u32(0)
		machine := make([]byte, 16)
		copy(machine, p.machine)
		out = append(out, machine...)
		for i := 0; i < 2; i++ {
			out = append(out, make([]byte, 16)...) // the volume identifier
			out = append(out, droid(p.mac)...)
		}
	}
	u32(0) // the terminal block
	return out
}

// droid builds a version 1 UUID with a given node, in the byte order
// Windows writes a GUID in: the first three fields little endian and the
// last eight bytes in order.
func droid(mac []byte) []byte {
	g := make([]byte, 16)
	copy(g, []byte{0x67, 0x91, 0x06, 0xa8, 0xf1, 0x11})
	g[6] = 0xf1
	g[7] = 0x11 // the high nibble of the third field is the version
	g[8] = 0x82 // the variant bits
	g[9] = 0xfb
	if mac == nil {
		mac = []byte{0x04, 0x7F, 0x0E, 0x03, 0xC7, 0xEF}
	}
	copy(g[10:], mac)
	return g
}

func TestReadsAShellLinkItBuilt(t *testing.T) {
	body := makeLink(linkParts{
		path:      `D:\photos\holiday.jpg`,
		label:     "MY STICK",
		driveType: 2,
		serial:    0xA1B2C3D4,
		machine:   "workshop",
	})
	l, err := parseLink(body)
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != `D:\photos\holiday.jpg` {
		t.Errorf("path came back as %q", l.Path)
	}
	if l.Label != "MY STICK" {
		t.Errorf("volume label came back as %q", l.Label)
	}
	if l.DriveType != 2 {
		t.Errorf("drive type came back as %d, so a removable drive would not be spotted", l.DriveType)
	}
	if got := serialString(l.Serial); got != "A1B2-C3D4" {
		t.Errorf("serial came back as %s", got)
	}
	if l.Machine != "workshop" {
		t.Errorf("machine name came back as %q", l.Machine)
	}
	if l.MAC != "04:7F:0E:03:C7:EF" {
		t.Errorf("hardware address came back as %q", l.MAC)
	}
}

// The tracker block is only worth reporting when the identifier really is
// the kind that carries an address. Windows makes up a random one on a
// machine with no card, and calling that somebody's hardware address is
// the worst thing this program could get wrong.
func TestOnlyRealHardwareAddressesAreReported(t *testing.T) {
	for name, g := range map[string][]byte{
		"version 4, so random": {0, 0, 0, 0, 0, 0, 0, 0x41, 0x82, 0, 1, 2, 3, 4, 5, 6},
		"wrong variant bits":   {0, 0, 0, 0, 0, 0, 0, 0x11, 0x00, 0, 1, 2, 3, 4, 5, 6},
		"multicast, so made up": {0, 0, 0, 0, 0, 0, 0, 0x11, 0x82, 0,
			0x01, 2, 3, 4, 5, 6},
		"all zeroes": {0, 0, 0, 0, 0, 0, 0, 0x11, 0x82, 0, 0, 0, 0, 0, 0, 0},
		"too short":  {0, 0, 0},
	} {
		if got := macFromUUID(g); got != "" {
			t.Errorf("%s: came back as the address %q", name, got)
		}
	}
	real := []byte{0, 0, 0, 0, 0, 0, 0, 0x11, 0x82, 0, 0xBC, 0xFC, 0xE7, 0x70, 0x1F, 0xC1}
	if got := macFromUUID(real); got != "BC:FC:E7:70:1F:C1" {
		t.Errorf("a real address came back as %q", got)
	}
}

// A shortcut can come from anywhere, including out of an archive somebody
// sent. Every prefix of a valid one has to come back as an error or a
// partial record, never as a panic.
func TestCutShortLinksDoNotFallOver(t *testing.T) {
	body := makeLink(linkParts{path: `C:\a\b.txt`, label: "OS", driveType: 3, machine: "ka"})
	for i := 0; i < len(body); i++ {
		if l, err := parseLink(body[:i]); err == nil && l != nil {
			l.Where()
		}
	}
	for _, junk := range [][]byte{
		{}, {0x4C}, make([]byte, 76), append([]byte{0x4C, 0, 0, 0}, make([]byte, 200)...),
	} {
		if l, err := parseLink(junk); err == nil && l != nil {
			l.Where()
		}
	}
}

// An offset inside the path block that reaches past the block would read
// the tracker data as if it were a path. The block is bounded by its own
// size field for exactly this reason.
func TestAnOffsetPastTheEndOfThePathBlockReadsNothing(t *testing.T) {
	body := makeLink(linkParts{path: `C:\a\b.txt`, label: "OS", driveType: 3, machine: "secret-machine"})
	at := 0x4C + 16 // the local base path offset inside the LinkInfo header
	binary.LittleEndian.PutUint32(body[at:], 0xFFFF)
	l, err := parseLink(body)
	if err != nil {
		return
	}
	if strings.Contains(l.Path, "secret") {
		t.Errorf("an out of range offset read past the block and came back with %q", l.Path)
	}
}

func TestReadsAShortcutWindowsMade(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows can be asked to make a real shortcut")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "a document.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "a document.lnk")
	script := "$s = New-Object -ComObject WScript.Shell; " +
		"$l = $s.CreateShortcut('" + link + "'); $l.TargetPath = '" + target + "'; $l.Save()"
	if out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput(); err != nil {
		t.Skipf("could not make a shortcut: %v\n%s", err, out)
	}

	l, err := readLink(link)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(l.Path, target) && !sameFile(l.Path, target) {
		t.Fatalf("Windows wrote a shortcut to %q and this read %q, "+
			"and those are two different files", target, l.Path)
	}
	if l.Machine == "" {
		t.Error("a shortcut made by Windows had no machine name in it, which it should")
	}
	// The file is right there, so a program that thinks it is gone has the
	// cross check backwards, which is the whole point of the tool.
	if judge(l.Path, l.DriveType) != stillHere {
		t.Error("a file that exists was not judged to be there")
	}
}

// Two names can be written differently and still be the same file, so the
// check above asks the filesystem rather than comparing text.
//
// This is not hypothetical. On a machine whose account name is longer than
// eight characters, the temporary directory comes back in its short form,
// C:\Users\RUNNER~1\..., while a shortcut made by Windows stores the long
// one, C:\Users\runneradmin\.... Comparing the two strings fails there
// while the reader is doing exactly the right thing, which is the worst
// kind of failing test: one that goes red at correct behaviour.
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// The helper above is the whole reason the shortcut test stopped going red
// at correct behaviour, so it gets checked rather than trusted.
func TestSameFileLooksAtTheFileAndNotAtTheName(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "a document.txt")
	if err := os.WriteFile(one, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "another.txt")
	if err := os.WriteFile(other, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The same file written two ways: straight, and through a detour that
	// no string comparison would forgive.
	detour := filepath.Join(dir, ".", "sub", "..", "a document.txt")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !sameFile(one, detour) {
		t.Errorf("%q and %q are the same file and were not seen as one", one, detour)
	}

	// Two files with the same contents are still two files.
	if sameFile(one, other) {
		t.Errorf("%q and %q are different files and were taken for one", one, other)
	}

	// And a name that leads nowhere is not the same as anything, least of
	// all as another name that leads nowhere.
	gone := filepath.Join(dir, "not here.txt")
	if sameFile(one, gone) || sameFile(gone, gone) {
		t.Error("a path to nothing was matched with something")
	}
}

func TestVolumeLabelsThatAreNotASCII(t *testing.T) {
	// The unicode label offset is the branch that a label with an accent
	// in it takes, and getting it wrong loses the label on every volume
	// named in any language but English.
	var out []byte
	u32 := func(v uint32) { out = binary.LittleEndian.AppendUint32(out, v) }
	label := utf16.Encode([]rune("COPIA SEGURIDAD ñ"))
	var wide []byte
	for _, u := range append(label, 0) {
		wide = binary.LittleEndian.AppendUint16(wide, u)
	}
	u32(uint32(20 + len(wide)))
	u32(2)          // removable
	u32(0x11223344) // serial
	u32(0x14)       // the ascii label is empty and a wide one follows
	u32(20)
	out = append(out, wide...)

	l := &Link{}
	l.readVolume(newCursor(out))
	if l.Label != "COPIA SEGURIDAD ñ" {
		t.Errorf("a label with an accent came back as %q", l.Label)
	}
}

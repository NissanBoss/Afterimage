package main

// Compound File Binary, which is the container a jump list is kept in.
//
// It is a filesystem in a file: a header, a table telling you which sector
// follows which, a directory of named streams, and a second smaller
// filesystem inside the first one for streams under four kilobytes. Almost
// every stream in a jump list is under four kilobytes, so the mini stream
// is not an edge case here, it is the normal path and everything that
// matters goes through it.
//
// This reads it rather than using a library because there is no compound
// file reader in the standard library and this program has no
// dependencies. It only needs to list the streams and hand back their
// bytes, so the red black tree the directory is really organised as is
// walked as a flat array: the tree exists to look a name up quickly, and
// here every stream is wanted anyway.

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"
)

const (
	freeSector    = 0xFFFFFFFF
	endOfChain    = 0xFFFFFFFE
	fatSector     = 0xFFFFFFFD
	difatSector   = 0xFFFFFFFC
	maxRegSector  = 0xFFFFFFFA
	dirEntrySize  = 128
	miniSectorLen = 64
)

type compound struct {
	body       []byte
	sectorSize int
	cutoff     uint32
	fat        []uint32
	miniFAT    []uint32
	mini       []byte
	entries    []cfbEntry
}

type cfbEntry struct {
	Name  string
	Kind  byte // 1 storage, 2 stream, 5 root
	Start uint32
	Size  uint64
}

func openCompound(body []byte) (*compound, error) {
	if len(body) < 512 {
		return nil, fmt.Errorf("too short to be a compound file")
	}
	sig := binary.LittleEndian.Uint64(body)
	if sig != 0xE11AB1A1E011CFD0 {
		return nil, fmt.Errorf("not a compound file")
	}
	c := &compound{body: body}
	shift := binary.LittleEndian.Uint16(body[0x1E:])
	if shift != 9 && shift != 12 {
		return nil, fmt.Errorf("sector size 2^%d is not one this understands", shift)
	}
	c.sectorSize = 1 << shift
	c.cutoff = binary.LittleEndian.Uint32(body[0x38:])
	if c.cutoff == 0 {
		c.cutoff = 4096
	}

	if err := c.readFAT(); err != nil {
		return nil, err
	}
	if err := c.readDirectory(); err != nil {
		return nil, err
	}
	c.readMiniFAT()
	return c, nil
}

// sector returns the bytes of one sector. The header occupies the first
// sector in a 512 byte file, so sector zero starts one sector in.
func (c *compound) sector(n uint32) []byte {
	if n > maxRegSector {
		return nil
	}
	start := (int(n) + 1) * c.sectorSize
	if start < 0 || start+c.sectorSize > len(c.body) {
		return nil
	}
	return c.body[start : start+c.sectorSize]
}

// chain follows a sector chain and refuses to go round in a circle. A file
// whose table points a sector at itself would otherwise read for ever, and
// these files come from wherever the machine got them.
func (c *compound) chain(start uint32, limit int) []uint32 {
	var out []uint32
	seen := map[uint32]bool{}
	for n := start; n <= maxRegSector; {
		if seen[n] || len(out) > limit {
			break
		}
		seen[n] = true
		out = append(out, n)
		if int(n) >= len(c.fat) {
			break
		}
		n = c.fat[n]
	}
	return out
}

func (c *compound) readFAT() error {
	// The first 109 table sectors are listed in the header. The rest are
	// in a chain of their own, which a jump list never needs and which is
	// followed anyway because a big enough one would otherwise lose most
	// of its streams silently.
	var numbers []uint32
	for i := 0; i < 109; i++ {
		n := binary.LittleEndian.Uint32(c.body[0x4C+i*4:])
		if n > maxRegSector {
			break
		}
		numbers = append(numbers, n)
	}
	next := binary.LittleEndian.Uint32(c.body[0x44:])
	guard := map[uint32]bool{}
	for next <= maxRegSector && !guard[next] {
		guard[next] = true
		sec := c.sector(next)
		if sec == nil {
			break
		}
		per := c.sectorSize/4 - 1
		for i := 0; i < per; i++ {
			if n := binary.LittleEndian.Uint32(sec[i*4:]); n <= maxRegSector {
				numbers = append(numbers, n)
			}
		}
		next = binary.LittleEndian.Uint32(sec[per*4:])
	}

	for _, n := range numbers {
		sec := c.sector(n)
		if sec == nil {
			continue
		}
		for i := 0; i+4 <= len(sec); i += 4 {
			c.fat = append(c.fat, binary.LittleEndian.Uint32(sec[i:]))
		}
	}
	if len(c.fat) == 0 {
		return fmt.Errorf("the sector table is empty")
	}
	return nil
}

func (c *compound) readDirectory() error {
	first := binary.LittleEndian.Uint32(c.body[0x30:])
	for _, n := range c.chain(first, len(c.fat)+1) {
		sec := c.sector(n)
		for i := 0; i+dirEntrySize <= len(sec); i += dirEntrySize {
			e := sec[i : i+dirEntrySize]
			kind := e[0x42]
			if kind != 1 && kind != 2 && kind != 5 {
				continue
			}
			// The name length is in bytes and counts the terminator, so a
			// name of four characters says ten. Trusting it without the
			// bound check puts whatever follows the name into the name on
			// a file that lies about it.
			nameLen := int(binary.LittleEndian.Uint16(e[0x40:]))
			if nameLen < 2 || nameLen > 64 {
				nameLen = 64
			}
			units := make([]uint16, 0, nameLen/2)
			for j := 0; j+1 < nameLen-2; j += 2 {
				units = append(units, binary.LittleEndian.Uint16(e[j:]))
			}
			c.entries = append(c.entries, cfbEntry{
				Name:  printable(string(utf16.Decode(units))),
				Kind:  kind,
				Start: binary.LittleEndian.Uint32(e[0x74:]),
				Size:  binary.LittleEndian.Uint64(e[0x78:]),
			})
		}
	}
	if len(c.entries) == 0 {
		return fmt.Errorf("there is no directory in it")
	}
	return nil
}

// readMiniFAT loads the small stream area. The root entry's own stream is
// the backing store for every stream under the cutoff, which is the part
// that is easy to miss: without it every jump list reads as a file full of
// empty streams, because the sizes are right there in the directory and
// the bytes are somewhere else entirely.
func (c *compound) readMiniFAT() {
	first := binary.LittleEndian.Uint32(c.body[0x3C:])
	for _, n := range c.chain(first, len(c.fat)+1) {
		sec := c.sector(n)
		for i := 0; i+4 <= len(sec); i += 4 {
			c.miniFAT = append(c.miniFAT, binary.LittleEndian.Uint32(sec[i:]))
		}
	}
	for _, e := range c.entries {
		if e.Kind == 5 {
			c.mini = c.read(e.Start, e.Size, false)
			return
		}
	}
}

func (c *compound) read(start uint32, size uint64, small bool) []byte {
	if size > uint64(len(c.body)) {
		size = uint64(len(c.body))
	}
	out := make([]byte, 0, size)
	if small {
		seen := map[uint32]bool{}
		for n := start; n <= maxRegSector && uint64(len(out)) < size; n = c.miniFAT[n] {
			if seen[n] || int(n) >= len(c.miniFAT) {
				break
			}
			seen[n] = true
			at := int(n) * miniSectorLen
			if at+miniSectorLen > len(c.mini) {
				break
			}
			out = append(out, c.mini[at:at+miniSectorLen]...)
		}
	} else {
		for _, n := range c.chain(start, len(c.fat)+1) {
			sec := c.sector(n)
			if sec == nil {
				break
			}
			out = append(out, sec...)
			if uint64(len(out)) >= size {
				break
			}
		}
	}
	if uint64(len(out)) > size {
		out = out[:size]
	}
	return out
}

// stream hands back the bytes of one named entry.
func (c *compound) stream(e cfbEntry) []byte {
	if e.Kind != 2 || e.Size == 0 {
		return nil
	}
	return c.read(e.Start, e.Size, e.Size < uint64(c.cutoff))
}

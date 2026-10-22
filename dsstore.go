package main

// .DS_Store, which is a database the Finder leaves in every folder it has
// ever shown you.
//
// It is there to remember how you had the folder arranged: the icon
// positions, the window size, the sort order. To do that it keeps one or
// more records per file, keyed by the file's name, and the names are what
// this is after. Nothing removes a record when the file goes, so a
// .DS_Store is a list of what was in a folder rather than of what is, and
// the difference is the finding.
//
// It matters far beyond macOS because these travel. They get committed to
// repositories, copied onto memory sticks and left on shared drives, and
// on Windows and Linux they are invisible to the person carrying them
// around. A .DS_Store in a folder you sent somebody is a list of the other
// things that were in that folder.
//
// The container is a buddy allocator, which is a small filesystem: a table
// of numbered blocks, a directory naming a few of them, and inside the one
// called DSDB a B-tree whose records are the interesting part. Everything
// in it is big endian, and every offset in the block table is relative to
// four bytes into the file, which is the one detail that makes a reader
// built from the description alone come out pointing at nothing.

import (
	"encoding/binary"
	"fmt"
	"sort"
	"unicode/utf16"
)

type dsStore struct {
	body   []byte
	blocks []uint32 // packed address and size, one per block number
}

// Names comes back sorted and without repeats. A file with an icon
// position and a window setting has two records under the same name, and
// the report counts files rather than records.
func dsStoreNames(body []byte) ([]string, error) {
	d, err := openDSStore(body)
	if err != nil {
		return nil, err
	}
	root, err := d.rootNode()
	if err != nil {
		return nil, err
	}

	found := map[string]bool{}
	seen := map[uint32]bool{}
	var walk func(id uint32, depth int)
	walk = func(id uint32, depth int) {
		// These come off other people's machines and out of repositories,
		// so a node that points at itself has to stop rather than spin.
		if seen[id] || depth > 32 {
			return
		}
		seen[id] = true
		block := d.block(id)
		if len(block) < 8 {
			return
		}
		c := newCursor(block)
		next := c.be32()
		count := int(c.be32())
		if count < 0 || count > len(block)/8+1 {
			return
		}
		for i := 0; i < count; i++ {
			if next != 0 {
				// An internal node puts a child before every record.
				walk(c.be32(), depth+1)
			}
			name, ok := d.record(c)
			if !ok {
				return
			}
			if name != "" {
				found[name] = true
			}
		}
		if next != 0 {
			walk(next, depth+1)
		}
	}
	walk(root, 0)

	out := make([]string, 0, len(found))
	for name := range found {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func openDSStore(body []byte) (*dsStore, error) {
	if len(body) < 36 || binary.BigEndian.Uint32(body) != 1 || string(body[4:8]) != "Bud1" {
		return nil, fmt.Errorf("not a .DS_Store")
	}
	d := &dsStore{body: body}
	offset := int(binary.BigEndian.Uint32(body[8:]))
	size := int(binary.BigEndian.Uint32(body[12:]))
	if offset <= 0 || size <= 0 || offset+4+size > len(body) {
		return nil, fmt.Errorf("the block table is outside the file")
	}

	c := newCursor(body[offset+4 : offset+4+size])
	count := int(c.be32())
	c.skip(4)
	if count < 0 || count > 1<<20 {
		return nil, fmt.Errorf("the block table claims %d blocks", count)
	}
	for i := 0; i < count; i++ {
		d.blocks = append(d.blocks, c.be32())
	}
	if c.bad {
		return nil, fmt.Errorf("the block table is cut short")
	}
	return d, nil
}

// block turns a block number into its bytes. The low five bits of the
// table entry are the log of the size and the rest is the address, and the
// address counts from four bytes into the file.
func (d *dsStore) block(id uint32) []byte {
	if int(id) >= len(d.blocks) {
		return nil
	}
	entry := d.blocks[id]
	at := int(entry &^ 0x1F)
	size := 1 << (entry & 0x1F)
	if at < 0 || size <= 0 || at+4+size > len(d.body) {
		return nil
	}
	return d.body[at+4 : at+4+size]
}

// rootNode finds the tree. The directory after the block table names a
// handful of blocks, and the one called DSDB holds the header that points
// at the root.
func (d *dsStore) rootNode() (uint32, error) {
	offset := int(binary.BigEndian.Uint32(d.body[8:]))
	size := int(binary.BigEndian.Uint32(d.body[12:]))
	c := newCursor(d.body[offset+4 : offset+4+size])
	c.skip(8)
	c.skip(len(d.blocks) * 4)
	// The table is padded out to a fixed number of slots, so the directory
	// starts at a rounded position rather than right after the entries
	// that are in use.
	c.seek(8 + 256*4)

	entries := int(c.be32())
	if entries < 0 || entries > 4096 {
		return 0, fmt.Errorf("the directory claims %d entries", entries)
	}
	for i := 0; i < entries; i++ {
		n := int(c.u8())
		name := string(c.take(n))
		id := c.be32()
		if c.bad {
			break
		}
		if name != "DSDB" {
			continue
		}
		master := d.block(id)
		if len(master) < 20 {
			return 0, fmt.Errorf("the tree header is too short")
		}
		return binary.BigEndian.Uint32(master), nil
	}
	return 0, fmt.Errorf("there is no DSDB directory in it")
}

// record reads one entry and gives back the file name it is about. The
// value has to be walked past even though nothing here wants it, because
// the records sit one after another and the length of this one is what
// says where the next begins.
func (d *dsStore) record(c *cursor) (string, bool) {
	n := int(c.be32())
	if c.bad || n < 0 || n > 4096 {
		return "", false
	}
	units := make([]uint16, 0, n)
	for i := 0; i < n; i++ {
		units = append(units, c.be16())
	}
	name := printable(string(utf16.Decode(units)))
	c.skip(4) // the structure id, which says what the record is about
	kind := string(c.take(4))
	if c.bad {
		return "", false
	}

	switch kind {
	case "bool":
		c.skip(1)
	case "long", "shor", "type":
		c.skip(4)
	case "comp", "dutc":
		c.skip(8)
	case "blob":
		c.skip(int(c.be32()))
	case "ustr":
		c.skip(int(c.be32()) * 2)
	default:
		// An unknown type means the length of this record is unknown, and
		// carrying on would read the middle of it as the next name. The
		// names found so far are kept and the walk of this node stops.
		return name, false
	}
	if c.bad {
		return name, false
	}
	return name, true
}

package main

// A cursor over a byte slice that runs off the end instead of panicking.
//
// Everything this program opens was written by something else, and several
// of the formats carry offsets that point wherever they like. A short read
// has to come back as a gap in the report rather than as a stack trace, so
// nothing in here returns an error and everything checks its own bounds:
// once bad is set the reads keep returning zero and the caller finds out
// when it is finished rather than at every single line.

import (
	"encoding/binary"
	"strings"
	"time"
	"unicode/utf16"
)

type cursor struct {
	b   []byte
	pos int
	bad bool
}

func newCursor(b []byte) *cursor { return &cursor{b: b} }

func (c *cursor) left() int {
	if c.bad || c.pos > len(c.b) {
		return 0
	}
	return len(c.b) - c.pos
}

func (c *cursor) take(n int) []byte {
	if n < 0 || c.bad || c.pos+n > len(c.b) {
		c.bad = true
		return make([]byte, max(n, 0))
	}
	out := c.b[c.pos : c.pos+n]
	c.pos += n
	return out
}

func (c *cursor) u8() byte     { return c.take(1)[0] }
func (c *cursor) u16() uint16  { return binary.LittleEndian.Uint16(c.take(2)) }
func (c *cursor) u32() uint32  { return binary.LittleEndian.Uint32(c.take(4)) }
func (c *cursor) u64() uint64  { return binary.LittleEndian.Uint64(c.take(8)) }
func (c *cursor) skip(n int)   { c.take(n) }
func (c *cursor) seek(off int) { c.pos = off; c.bad = c.bad || off < 0 || off > len(c.b) }

// jump makes a second cursor over the same bytes at an absolute offset,
// for formats that scatter their strings by offset rather than in order.
func (c *cursor) jump(off int) *cursor {
	if off < 0 || off > len(c.b) {
		return &cursor{bad: true}
	}
	return &cursor{b: c.b, pos: off}
}

// cstring reads up to a zero byte. A record with no terminator in it is
// truncated rather than run on into whatever is next, because a name that
// swallowed the following field is worse than a name cut short.
func (c *cursor) cstring() string {
	start := c.pos
	for c.pos < len(c.b) && c.b[c.pos] != 0 {
		c.pos++
	}
	out := string(c.b[start:min(c.pos, len(c.b))])
	if c.pos < len(c.b) {
		c.pos++
	}
	return out
}

// wstring reads UTF-16 little endian up to a zero unit.
func (c *cursor) wstring() string {
	var units []uint16
	for c.pos+1 < len(c.b) {
		u := binary.LittleEndian.Uint16(c.b[c.pos:])
		c.pos += 2
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	return string(utf16.Decode(units))
}

// wfixed reads a known number of UTF-16 units and stops at the first zero,
// which several of these formats pad with rather than trimming.
func (c *cursor) wfixed(units int) string {
	raw := c.take(units * 2)
	out := make([]uint16, 0, units)
	for i := 0; i+1 < len(raw); i += 2 {
		u := binary.LittleEndian.Uint16(raw[i:])
		if u == 0 {
			break
		}
		out = append(out, u)
	}
	return string(utf16.Decode(out))
}

// filetime is 100 nanosecond ticks since 1601. Zero means unset rather
// than the year 1601, and treating it as a date puts an impossible
// timestamp in the report, which was the first thing that made one of
// these reports look wrong.
func filetime(ticks uint64) time.Time {
	if ticks == 0 {
		return time.Time{}
	}
	const toUnix = 11644473600
	sec := int64(ticks/10000000) - toUnix
	nsec := int64(ticks%10000000) * 100
	t := time.Unix(sec, nsec).UTC()
	if t.Year() < 1970 || t.Year() > 2200 {
		return time.Time{}
	}
	return t
}

// printable throws out the control characters a corrupt record can leave
// in a name. Writing raw bytes to a terminal is how a report ends up
// moving the cursor around or setting a colour that never gets reset.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// decodeUTF16 is here rather than at each call site because two of the
// readers need it and neither wants an import for one line.
func decodeUTF16(units []uint16) string { return string(utf16.Decode(units)) }

// Big endian reads, for the formats that use it: PNG chunks and the buddy
// allocator inside a .DS_Store. Keeping them on the same cursor means one
// set of bounds checks rather than two.
func (c *cursor) be16() uint16 { return binary.BigEndian.Uint16(c.take(2)) }
func (c *cursor) be32() uint32 { return binary.BigEndian.Uint32(c.take(4)) }

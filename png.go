package main

// Just enough PNG to read the text chunks, which is where the thumbnail
// specification puts the path of the file the thumbnail was made from.
//
// This decodes no pixels. A PNG is a signature followed by a run of
// chunks, each with a length, a four letter type and a checksum, and the
// three that can hold text are tEXt, zTXt and iTXt. Walking that far into
// the file costs nothing and stops at the header of the image data, so a
// folder of ten thousand thumbnails is read in the time it takes to open
// ten thousand files and read a few hundred bytes of each.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"strings"
)

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// pngText pulls out the keyword and value pairs. It stops at IDAT because
// everything the specification puts in a thumbnail is written before the
// pixels, and reading past it would mean walking megabytes of compressed
// image to find nothing.
func pngText(body []byte) map[string]string {
	if len(body) < 8 || !bytes.Equal(body[:8], pngMagic) {
		return nil
	}
	out := map[string]string{}
	at := 8
	for at+12 <= len(body) {
		length := int(binary.BigEndian.Uint32(body[at:]))
		kind := string(body[at+4 : at+8])
		if length < 0 || at+12+length > len(body) {
			return out
		}
		data := body[at+8 : at+8+length]
		switch kind {
		case "tEXt":
			if key, value, ok := splitKeyword(data); ok {
				out[key] = printable(value)
			}
		case "zTXt":
			if key, rest, ok := splitKeyword(data); ok && len(rest) > 1 {
				// One byte of compression method, then a zlib stream.
				if text := inflate(rest[1:]); text != "" {
					out[key] = printable(text)
				}
			}
		case "iTXt":
			if key, value, ok := readInternational(data); ok {
				out[key] = printable(value)
			}
		case "IDAT", "IEND":
			return out
		}
		at += 12 + length
	}
	return out
}

func splitKeyword(data []byte) (string, string, bool) {
	cut := bytes.IndexByte(data, 0)
	if cut <= 0 {
		return "", "", false
	}
	return string(data[:cut]), string(data[cut+1:]), true
}

// readInternational handles iTXt, which puts a compression flag, a
// compression method, a language tag and a translated keyword between the
// keyword and the text. GNOME writes tEXt, but a thumbnail can be written
// by anything that registers itself as a thumbnailer.
func readInternational(data []byte) (string, string, bool) {
	cut := bytes.IndexByte(data, 0)
	if cut <= 0 || cut+3 > len(data) {
		return "", "", false
	}
	key := string(data[:cut])
	compressed := data[cut+1] != 0
	rest := data[cut+3:]
	for i := 0; i < 2; i++ { // the language tag and the translated keyword
		next := bytes.IndexByte(rest, 0)
		if next < 0 {
			return "", "", false
		}
		rest = rest[next+1:]
	}
	if compressed {
		if text := inflate(string(rest)); text != "" {
			return key, text, true
		}
		return "", "", false
	}
	return key, string(rest), true
}

func inflate(body string) string {
	z, err := zlib.NewReader(strings.NewReader(body))
	if err != nil {
		return ""
	}
	// A thumbnail's text is a path and a few numbers. Anything claiming to
	// be more than this is either broken or trying something, and either
	// way it is not a path.
	out, err := io.ReadAll(io.LimitReader(z, 1<<16))
	if err != nil && len(out) == 0 {
		return ""
	}
	return string(out)
}

// fileURI turns the file:// URI in a thumbnail back into a path. The
// specification says the URI is escaped, so a file with a space or an
// accent in its name comes back percent encoded, and leaving it that way
// means the path never matches anything on the disk and every accented
// filename is reported as deleted.
func fileURI(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return ""
	}
	path := unescape(strings.TrimPrefix(uri, "file://"))
	if path == "" || path[0] != '/' {
		// Anything else is a host name, which makes it a share rather
		// than a local file, and this program does not reach out to those.
		return ""
	}
	// A Windows path is written with a third slash in front of the drive
	// letter, so file:///C:/x is C:\x. Leaving the slash on makes the path
	// match nothing, which reads as every file having been deleted.
	if len(path) > 2 && path[2] == ':' {
		return path[1:]
	}
	return path
}

func unescape(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, ok1 := hexDigit(s[i+1])
			lo, ok2 := hexDigit(s[i+2])
			if ok1 && ok2 {
				out = append(out, hi<<4|lo)
				i += 2
				continue
			}
		}
		out = append(out, s[i])
	}
	return string(out)
}

func hexDigit(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

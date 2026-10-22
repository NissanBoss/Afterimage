package main

// The thumbnail cache, end to end.
//
// The fixtures here are real PNG files: the image is encoded by the
// standard library and the text chunk is spliced in with its own length
// and checksum, and then the whole thing is handed back to the standard
// library to decode. If image/png can still read it, the chunk is well
// formed, which is what makes this a check of the reader rather than an
// agreement between two things written on the same afternoon.

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func thumbnailPNG(t *testing.T, text map[string]string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()

	// The signature and the IHDR chunk come first and everything else can
	// follow, so the text goes in right after them.
	const afterHeader = 8 + 25
	var chunks []byte
	for key, value := range text {
		data := append(append([]byte(key), 0), []byte(value)...)
		payload := append([]byte("tEXt"), data...)
		chunk := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
		chunk = append(chunk, payload...)
		chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(payload))
		chunks = append(chunks, chunk...)
	}
	out := append([]byte{}, body[:afterHeader]...)
	out = append(out, chunks...)
	out = append(out, body[afterHeader:]...)

	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("the fixture is not a valid PNG, so it proves nothing: %v", err)
	}
	return out
}

func TestReadsTheAddressOutOfAThumbnail(t *testing.T) {
	body := thumbnailPNG(t, map[string]string{
		"Thumb::URI":          "file:///home/somebody/Pictures/a%20holiday%20photo.jpg",
		"Thumb::Image::Width": "4032",
	})
	text := pngText(body)
	if got := text["Thumb::Image::Width"]; got != "4032" {
		t.Errorf("the width came back as %q", got)
	}
	got := fileURI(text["Thumb::URI"])
	if want := "/home/somebody/Pictures/a holiday photo.jpg"; got != want {
		t.Errorf("the path came back as %q and it is %q", got, want)
	}
}

// A path that is left percent encoded never matches anything on the disk,
// so every file with a space or an accent in its name comes back as
// deleted. That is a whole class of false findings from one missing step.
func TestPercentEncodingIsUndone(t *testing.T) {
	for encoded, want := range map[string]string{
		"file:///home/x/a%20b.jpg":        "/home/x/a b.jpg",
		"file:///home/x/ni%C3%B1a.png":    "/home/x/niña.png",
		"file:///home/x/100%25.png":       "/home/x/100%.png",
		"file:///media/stick/plain.jpg":   "/media/stick/plain.jpg",
		"file:///home/x/half%2.png":       "/home/x/half%2.png",
		"http://example.test/not-a-file":  "",
		"file://server/share/thing.jpg":   "",
		"/home/x/no-scheme-at-all.jpg":    "",
		"file:///home/x/tra%C3%A7o%20.md": "/home/x/traço .md",
	} {
		if got := fileURI(encoded); got != want {
			t.Errorf("%s came back as %q and it should be %q", encoded, got, want)
		}
	}
}

func TestAThumbnailOfADeletedFileIsFound(t *testing.T) {
	dir := t.TempDir()
	defer setHomeTo(t, dir)()

	kept := filepath.Join(dir, "still-here.jpg")
	if err := os.WriteFile(kept, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, ".cache", "thumbnails", "large")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, uri string) {
		body := thumbnailPNG(t, map[string]string{"Thumb::URI": uri})
		if err := os.WriteFile(filepath.Join(cache, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aaa.png", "file:///"+strings.TrimPrefix(filepath.ToSlash(kept), "/"))
	write("bbb.png", "file:///"+strings.TrimPrefix(filepath.ToSlash(filepath.Join(dir, "long-gone.jpg")), "/"))

	r := &Report{}
	scanThumbnails(r)
	out := renderTo(r)

	if !strings.Contains(out, "long-gone") && !strings.Contains(out, "lo•") {
		t.Errorf("the thumbnail of the deleted file was not reported:\n%s", out)
	}
	if strings.Contains(out, "still-here") {
		t.Errorf("a file that is still on the disk was reported as gone:\n%s", out)
	}
	if !strings.Contains(out, "1 picture is here") {
		t.Errorf("expected exactly one finding for one deleted file:\n%s", out)
	}
}

// setHomeTo points the whole program at a temporary profile and gives back
// the function that puts it back, so one test cannot leak into the next.
func setHomeTo(t *testing.T, dir string) func() {
	t.Helper()
	oldHome, oldUser := home, userName
	home, userName = dir, filepath.Base(dir)
	t.Setenv("AFTERIMAGE_HOME", dir)
	return func() { home, userName = oldHome, oldUser }
}

func renderTo(r *Report) string {
	var buf bytes.Buffer
	r.render(&buf, "test")
	return buf.String()
}

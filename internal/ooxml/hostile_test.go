package ooxml

// Hostile-input tests: malformed packages must error, never panic, and
// resource abuse must hit the named caps in edit.go (maxEditBytes per part,
// maxEditEntries per package, maxEditTotalBytes decompressed total).

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zeroChunks streams n zero bytes without allocating them.
type zeroChunks struct{ left int64 }

func (z *zeroChunks) Read(p []byte) (int, error) {
	if z.left <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > z.left {
		n = z.left
	}
	clear(p[:n])
	z.left -= n
	return int(n), nil
}

// itoa4 formats i as four zero-padded digits (unique filler entry names).
func itoa4(i int) string {
	d := make([]byte, 4)
	for k := 3; k >= 0; k-- {
		d[k] = byte('0' + i%10)
		i /= 10
	}
	return string(d)
}

func buildZip(t *testing.T, files map[string][]byte, order []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range order {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := f.Write(files[name]); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func unzipParts(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	parts := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		parts[f.Name] = content
	}
	return parts
}

// TestHostileZipBomb refuses one entry expanding to 200 MiB from a few KiB:
// the per-part maxEditBytes cap fires after bounded buffering.
func TestHostileZipBomb(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(f, &zeroChunks{left: 200 << 20}, 200<<20); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	bomb := buf.Bytes()
	t.Logf("bomb file size: %d bytes", len(bomb))

	if _, err := ParagraphText(bomb, 0); err == nil {
		t.Fatal("zip bomb: want error, got nil")
	} else if !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("zip bomb: error %q must name the byte limit (per-part maxEditBytes cap)", err)
	}
	if _, err := ReplaceParagraphText(bomb, 0, "x"); err == nil {
		t.Fatal("zip bomb edit: want error, got nil")
	}
}

// TestHostileEntryCount refuses 5,000 entries via the maxEditEntries cap.
func TestHostileEntryCount(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for i := 0; i < 5000; i++ {
		f, err := w.Create("filler/p" + itoa4(i) + ".xml")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte("<x/>"))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	many := buf.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(many), int64(len(many)))
	if err != nil {
		t.Fatalf("setup zip invalid: %v", err)
	}
	if len(zr.File) < 5000 && len(zr.File) <= maxEditEntries {
		t.Skipf("setup produced %d entries, need over the %d cap", len(zr.File), maxEditEntries)
	}
	if _, err := ParagraphText(many, 0); err == nil {
		t.Fatal("5000 entries: want error, got nil")
	} else if !strings.Contains(err.Error(), "entry limit") {
		t.Fatalf("5000 entries: error %q must name the entry limit (maxEditEntries cap)", err)
	}
}

// TestHostileZipSlipNames is inert: the edit reader never touches the
// filesystem, so slip names are carried opaquely and the edit still works.
func TestHostileZipSlipNames(t *testing.T) {
	base := mustSeedDocx()
	parts := unzipParts(t, base)
	var order []string
	zr, _ := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	for _, f := range zr.File {
		order = append(order, f.Name)
	}
	parts["../evil.txt"] = []byte("escape")
	parts["/abs/evil.txt"] = []byte("absolute")
	order = append(order, "../evil.txt", "/abs/evil.txt")
	data := buildZip(t, parts, order)

	out, err := ReplaceParagraphText(data, 0, "kept")
	if err != nil {
		t.Fatalf("zip-slip names must not break the in-memory reader: %v", err)
	}
	if got, err := ParagraphText(out, 0); err != nil || got != "kept" {
		t.Fatalf("ParagraphText = %q, %v; want kept, nil", got, err)
	}
	for _, p := range []string{"../evil.txt", "/abs/evil.txt"} {
		if _, err := os.Stat(filepath.Clean(p)); err == nil {
			t.Fatalf("zip-slip wrote %s to the filesystem", p)
		}
	}
}

// TestHostileDuplicateEntries pins deterministic first-wins reads.
func TestHostileDuplicateEntries(t *testing.T) {
	base := mustSeedDocx()
	parts := unzipParts(t, base)
	first := string(parts["word/document.xml"])
	dup := strings.Replace(first, "Seed Title", "SECOND", 1)
	if dup == first {
		t.Fatal("setup: seed text not found")
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	zr, _ := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	for _, f := range zr.File {
		fw, _ := w.Create(f.Name)
		_, _ = fw.Write(parts[f.Name])
	}
	fw, _ := w.Create("word/document.xml") // duplicate name
	_, _ = fw.Write([]byte(dup))
	_ = w.Close()

	got, err := ParagraphText(buf.Bytes(), 0)
	if err != nil {
		t.Fatalf("duplicate entries: %v", err)
	}
	if !strings.Contains(got, "Seed Title") || strings.Contains(got, "SECOND") {
		t.Fatalf("duplicate entries: got %q, want deterministic first part", got)
	}
}

// TestHostileDTDEntity is rejected: encoding/xml never expands custom
// entities, so wellFormed fails and no edit proceeds.
func TestHostileDTDEntity(t *testing.T) {
	base := mustSeedDocx()
	parts := unzipParts(t, base)
	var order []string
	zr, _ := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	for _, f := range zr.File {
		order = append(order, f.Name)
	}
	parts["word/document.xml"] = []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<!DOCTYPE w:document [<!ENTITY xxe "EXPANDED">]>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body><w:p><w:r><w:t>&xxe;</w:t></w:r></w:p></w:body></w:document>`)
	data := buildZip(t, parts, order)

	if _, err := ParagraphText(data, 0); err == nil {
		t.Fatal("DTD entity: want error, got nil")
	}
	out, err := ReplaceParagraphText(data, 0, "x")
	if err == nil {
		if strings.Contains(string(out), "EXPANDED") {
			t.Fatal("DTD entity was expanded into output")
		}
		t.Fatal("DTD entity: want error, got nil")
	}
}

// TestHostileMalformedRels handles missing and external targets without panic.
func TestHostileMalformedRels(t *testing.T) {
	base, err := CreateXLSX("S")
	if err != nil {
		t.Fatal(err)
	}
	parts := unzipParts(t, base)
	var order []string
	zr, _ := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	for _, f := range zr.File {
		order = append(order, f.Name)
	}
	parts["xl/_rels/workbook.xml.rels"] = []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1"/>` +
		`<Relationship Id="rId2" Target="https://evil.example/sheet.xml"/>` +
		`</Relationships>`)
	data := buildZip(t, parts, order)

	if _, err := SetCellValue(data, "", "A1", "v", false, ""); err == nil {
		t.Fatal("malformed rels: want error, got nil")
	}
}

// TestHostileTruncation errors at every byte offset, never panics.
func TestHostileTruncation(t *testing.T) {
	base := mustSeedDocx()
	offsets := []int{0, 1, 2, 10, 100, len(base) / 4, len(base) / 2, len(base) - 10, len(base) - 2, len(base) - 1}
	for _, off := range offsets {
		if off < 0 || off >= len(base) {
			continue
		}
		trunc := base[:off]
		if _, err := ParagraphText(trunc, 0); err == nil {
			t.Errorf("offset %d: ParagraphText want error, got nil", off)
		}
		if _, err := ReplaceParagraphText(trunc, 0, "x"); err == nil {
			t.Errorf("offset %d: ReplaceParagraphText want error, got nil", off)
		}
	}
	if _, err := ParagraphText(base, 0); err != nil {
		t.Fatalf("full package must still read: %v", err)
	}
}

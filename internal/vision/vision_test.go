package vision

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestHeroIDOf(t *testing.T) {
	cases := map[string]int{
		"7":          7,
		"7_persona1": 7,
		"1_alt":      1,
		"npc_hero":   0,
		"0":          0,
		"-3":         0,
		"":           0,
	}
	for stem, want := range cases {
		if got := HeroIDOf(stem); got != want {
			t.Errorf("HeroIDOf(%q) = %d, want %d", stem, got, want)
		}
	}
}

func TestSlotsLayout(t *testing.T) {
	slots := Slots(1920, 1080)
	if len(slots) != 10 {
		t.Fatalf("got %d slots, want 10", len(slots))
	}
	if slots[0].Name() != "left1" || slots[9].Name() != "right5" {
		t.Errorf("unexpected slot names: %s ... %s", slots[0].Name(), slots[9].Name())
	}
	for _, s := range slots {
		if s.Box.Empty() {
			t.Errorf("slot %s is empty", s.Name())
		}
		if s.Box.Min.X < 0 || s.Box.Max.X > 1920 || s.Box.Max.Y > 1080 {
			t.Errorf("slot %s out of bounds: %v", s.Name(), s.Box)
		}
	}
	// 124px pitch, 116px wide portraits.
	if w := slots[0].Box.Dx(); w != 116 {
		t.Errorf("portrait width = %d, want 116", w)
	}
	if slots[1].Box.Min.X-slots[0].Box.Min.X != 124 {
		t.Errorf("pitch = %d, want 124", slots[1].Box.Min.X-slots[0].Box.Min.X)
	}
}

func TestDescriptorFlatIsZero(t *testing.T) {
	flat := image.NewRGBA(image.Rect(0, 0, 50, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 50; x++ {
			flat.SetRGBA(x, y, color.RGBA{120, 120, 120, 255})
		}
	}
	d := descriptor(flat)
	for i, v := range d {
		if v != 0 {
			t.Fatalf("flat descriptor[%d] = %v, want 0", i, v)
		}
	}
}

func TestDescriptorUnitNorm(t *testing.T) {
	img := splitImage(color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255})
	d := descriptor(img)
	var norm2 float64
	for _, v := range d {
		norm2 += float64(v) * float64(v)
	}
	if norm2 < 0.99 || norm2 > 1.01 {
		t.Fatalf("descriptor norm^2 = %v, want ~1", norm2)
	}
}

func splitImage(left, right color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			if x < 20 {
				img.SetRGBA(x, y, left)
			} else {
				img.SetRGBA(x, y, right)
			}
		}
	}
	return img
}

func TestRecogniserMatchesAndRefuses(t *testing.T) {
	dir := t.TempDir()
	one := splitImage(color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255})
	two := splitImage(color.RGBA{0, 255, 0, 255}, color.RGBA{255, 255, 0, 255})
	// A hero with a persona: same id, different art.
	twoAlt := splitImage(color.RGBA{0, 128, 255, 255}, color.RGBA{255, 0, 255, 255})
	writePNG(t, filepath.Join(dir, "1.png"), one)
	writePNG(t, filepath.Join(dir, "2.png"), two)
	writePNG(t, filepath.Join(dir, "2_persona1.png"), twoAlt)

	r := NewRecogniser(dir)
	n, err := r.Load()
	if err != nil || n != 3 {
		t.Fatalf("Load = %d, %v; want 3, nil", n, err)
	}

	match := r.Identify(one, nil)
	if match.HeroID != 1 {
		t.Errorf("identify hero1 = %+v, want HeroID 1", match)
	}
	if match.Margin < MinMargin {
		t.Errorf("margin = %v, want >= %v", match.Margin, MinMargin)
	}

	match = r.Identify(two, nil)
	if match.HeroID != 2 {
		t.Errorf("identify hero2 = %+v, want HeroID 2", match)
	}

	// Excluding the true hero must not silently return the runner-up as if it
	// were certain; the margin collapses or the best is excluded.
	if m := r.Identify(one, map[int]bool{1: true}); m.HeroID == 1 {
		t.Error("excluded hero was still returned")
	}

	// A flat crop carries no structure and must be refused.
	flat := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			flat.SetRGBA(x, y, color.RGBA{90, 90, 90, 255})
		}
	}
	if m := r.Identify(flat, nil); m.HeroID != 0 {
		t.Errorf("flat crop matched hero %d, want refusal", m.HeroID)
	}
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// fakeIdentifier maps the top-left pixel of a crop to a hero, so the reader can
// be tested without reference art.
type fakeIdentifier struct{ byPixel map[color.RGBA]int }

func (f fakeIdentifier) Identify(crop image.Image, exclude map[int]bool) Match {
	b := crop.Bounds()
	c := color.RGBAModel.Convert(crop.At(b.Min.X+1, b.Min.Y+1)).(color.RGBA)
	id, ok := f.byPixel[c]
	if !ok || exclude[id] {
		return Match{}
	}
	return Match{HeroID: id, Similarity: 1, Margin: 1}
}

func TestDraftReaderSplitsSides(t *testing.T) {
	colors := []color.RGBA{
		{10, 0, 0, 255}, {20, 0, 0, 255}, {30, 0, 0, 255}, {40, 0, 0, 255}, {50, 0, 0, 255},
		{0, 10, 0, 255}, {0, 20, 0, 255}, {0, 30, 0, 255}, {0, 40, 0, 255}, {0, 50, 0, 255},
	}
	byPixel := map[color.RGBA]int{}
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for i, slot := range Slots(1920, 1080) {
		byPixel[colors[i]] = 100 + i
		fill(img, slot.Box, colors[i])
	}

	ident := fakeIdentifier{byPixel: byPixel}
	reader := newDraftReaderWith(ident, VotesNeeded, History)

	// First pass records votes but settles nothing yet.
	if r := reader.Read(img, 101, "left", nil); len(r.Allies)+len(r.Enemies) != 0 {
		t.Fatalf("first pass settled %d slots, want 0", len(r.Allies)+len(r.Enemies))
	}

	r := reader.Read(img, 101, "left", nil)
	if !r.Usable() || r.OwnSide != "left" {
		t.Fatalf("reading = %+v, want usable left", r)
	}
	if len(r.Allies) != 5 || len(r.Enemies) != 5 {
		t.Fatalf("allies=%d enemies=%d, want 5/5", len(r.Allies), len(r.Enemies))
	}
	for _, id := range r.Allies {
		if id < 100 || id >= 105 {
			t.Errorf("ally %d is not a left-side hero", id)
		}
	}
	for _, id := range r.Enemies {
		if id < 105 || id >= 110 {
			t.Errorf("enemy %d is not a right-side hero", id)
		}
	}
}

func TestDraftReaderConflict(t *testing.T) {
	colors := make([]color.RGBA, 10)
	for i := range colors {
		colors[i] = color.RGBA{uint8(i + 1), 0, 0, 255}
	}
	byPixel := map[color.RGBA]int{}
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	slots := Slots(1920, 1080)
	for i, slot := range slots {
		byPixel[colors[i]] = 200 + i
		fill(img, slot.Box, colors[i])
	}

	ident := fakeIdentifier{byPixel: byPixel}
	reader := newDraftReaderWith(ident, 1, History)

	// GSI says we are on the right, but our hero (200) sits on the left.
	r := reader.Read(img, 200, "right", nil)
	if !r.Conflict {
		t.Fatalf("reading = %+v, want conflict", r)
	}
	if r.Usable() {
		t.Error("conflicting reading must not be usable")
	}
}

func TestVPKPreloadAndInline(t *testing.T) {
	tree := buildVPKTree([]string{
		"panorama/images/heroes/preload_hero.vtex_c",
		"panorama/images/heroes/inline_hero.vtex_c",
	}, []byte("ABCD"), 4)

	data := append([]byte("WXYZ"), 0, 0, 0, 0) // pad so inline offset 0 is safe
	file := buildVPKFile(tree, data)

	path := filepath.Join(t.TempDir(), "pak01_dir.vpk")
	if err := os.WriteFile(path, file, 0o644); err != nil {
		t.Fatal(err)
	}
	pak, err := OpenVPK(filepath.Dir(path))
	if err != nil {
		t.Fatalf("OpenVPK: %v", err)
	}
	defer pak.Close()

	if got := len(pak.Entries()); got != 2 {
		t.Fatalf("entries = %d, want 2", got)
	}
	got, err := pak.Read("panorama/images/heroes/preload_hero.vtex_c")
	if err != nil || string(got) != "ABCD" {
		t.Errorf("preload read = %q, %v; want ABCD", got, err)
	}
	// The second entry points into the file data section at offset 0.
	got, err = pak.Read("panorama/images/heroes/inline_hero.vtex_c")
	if err != nil || string(got) != "WXYZ" {
		t.Errorf("inline read = %q, %v; want WXYZ", got, err)
	}
}

// block builds one DXT5 block: eight bytes of alpha (unused), two RGB565
// endpoints (c0 pure red, c1 pure blue) and sixteen 2-bit indices.
func dxt5Block(indices uint32) []byte {
	b := make([]byte, 8, 16)
	b = append(b, 0x00, 0xF8, 0x1F, 0x00)
	var idx [4]byte
	binary.LittleEndian.PutUint32(idx[:], indices)
	return append(b, idx[:]...)
}

func TestDecodeDXT5(t *testing.T) {
	pixel := func(img image.Image, x, y int) color.RGBA {
		return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
	}

	img, err := decodeDXT5(dxt5Block(0x00000000), 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got := pixel(img, 0, 0); got.R != 255 || got.G != 0 || got.B != 0 {
		t.Errorf("index 0 = %+v, want red", got)
	}

	// 0b01 repeated sixteen times: the second endpoint.
	img, _ = decodeDXT5(dxt5Block(0x55555555), 4, 4)
	if got := pixel(img, 2, 2); got.R != 0 || got.G != 0 || got.B != 255 {
		t.Errorf("index 1 = %+v, want blue", got)
	}

	// 0b10 repeated: two thirds of the way from c0 to c1. BC3 always
	// interpolates four colours; alpha has its own half of the block.
	img, _ = decodeDXT5(dxt5Block(0xAAAAAAAA), 4, 4)
	if got := pixel(img, 1, 1); got.R != 170 || got.G != 0 || got.B != 85 {
		t.Errorf("index 2 = %+v, want (170,0,85)", got)
	}

	// An image smaller than a block is cropped, not refused.
	img, err = decodeDXT5(dxt5Block(0), 3, 2)
	if err != nil || img.Bounds().Dx() != 3 || img.Bounds().Dy() != 2 {
		t.Errorf("crop = %v, err %v; want 3x2", img.Bounds().Size(), err)
	}

	// A short buffer must be refused rather than misread.
	if _, err := decodeDXT5(dxt5Block(0)[:8], 4, 4); err == nil {
		t.Error("short DXT5 buffer was accepted")
	}
}

func TestDecodeBGRA8888(t *testing.T) {
	// Two pixels, BGRA order.
	pixels := []byte{30, 20, 10, 255, 60, 50, 40, 255}
	img, err := decodeBGRA8888(pixels, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	c := color.RGBAModel.Convert(img.At(0, 0)).(color.RGBA)
	if c.R != 10 || c.G != 20 || c.B != 30 {
		t.Errorf("pixel = %+v, want R10 G20 B30", c)
	}
}

func TestDecodeVTEXEmbeddedPNG(t *testing.T) {
	// A single-pixel PNG.
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{200, 100, 50, 255})
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, src); err != nil {
		t.Fatal(err)
	}
	raw := buildVTEXEmbeddedPNG(pngBuf.Bytes(), 2, 2)
	img, err := decodeVTEX(raw)
	if err != nil {
		t.Fatalf("decodeVTEX: %v", err)
	}
	c := color.RGBAModel.Convert(img.At(0, 0)).(color.RGBA)
	if c.R != 200 || c.G != 100 || c.B != 50 {
		t.Errorf("pixel = %+v, want R200 G100 B50", c)
	}
}

// buildVTEXEmbeddedPNG fabricates a minimal .vtex_c wrapping a PNG, following
// the same offsets the decoder reads.
func buildVTEXEmbeddedPNG(pngBytes []byte, width, height uint16) []byte {
	const headerSize = 32
	const blockOffset = 16
	const position = 8 + blockOffset
	const dataStart = position + 4 + 8 // offset field is 8
	raw := make([]byte, dataStart+headerSize+len(pngBytes))
	binary.LittleEndian.PutUint16(raw[4:6], 12) // version
	binary.LittleEndian.PutUint32(raw[8:12], blockOffset)
	binary.LittleEndian.PutUint32(raw[12:16], 1) // block count
	copy(raw[position:position+4], "DATA")
	binary.LittleEndian.PutUint32(raw[position+4:position+8], 8) // offset
	binary.LittleEndian.PutUint32(raw[position+8:position+12], headerSize)
	binary.LittleEndian.PutUint16(raw[dataStart+20:dataStart+22], width)
	binary.LittleEndian.PutUint16(raw[dataStart+22:dataStart+24], height)
	raw[dataStart+26] = formatPNGRGBA8888
	copy(raw[dataStart+headerSize:], pngBytes)
	return raw
}

// buildVPKTree builds the string table for two entries under the heroes path.
// The first entry carries preload data; the second points into the file data
// section (archive index 0x7fff, offset 0).
func buildVPKTree(paths []string, preload []byte, inlineLength uint32) []byte {
	var b bytes.Buffer
	writeCString(&b, "vtex_c") // the tree stores extensions without the dot
	writeCString(&b, "panorama/images/heroes")

	writeCString(&b, "preload_hero")
	writeEntry(&b, uint16(len(preload)), archiveIndexDir, 0, 0)
	b.Write(preload)

	writeCString(&b, "inline_hero")
	writeEntry(&b, 0, archiveIndexDir, 0, inlineLength)

	writeCString(&b, "") // end names
	writeCString(&b, "") // end paths
	writeCString(&b, "") // end extensions
	return b.Bytes()
}

func writeEntry(b *bytes.Buffer, preloadBytes uint16, archiveIndex uint16, offset, length uint32) {
	var e [18]byte
	copy(e[0:4], []byte{0, 0, 0, 0}) // CRC
	binary.LittleEndian.PutUint16(e[4:6], preloadBytes)
	binary.LittleEndian.PutUint16(e[6:8], archiveIndex)
	binary.LittleEndian.PutUint32(e[8:12], offset)
	binary.LittleEndian.PutUint32(e[12:16], length)
	binary.LittleEndian.PutUint16(e[16:18], vpkEntryTerminal)
	b.Write(e[:])
}

func writeCString(b *bytes.Buffer, s string) {
	b.WriteString(s)
	b.WriteByte(0)
}

func buildVPKFile(tree, fileData []byte) []byte {
	header := make([]byte, 28)
	binary.LittleEndian.PutUint32(header[0:4], vpkMagic)
	binary.LittleEndian.PutUint32(header[4:8], 2) // version
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(tree)))
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(fileData)))
	out := append([]byte{}, header...)
	out = append(out, tree...)
	out = append(out, fileData...)
	return out
}

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

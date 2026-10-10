package vision

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Image format ids from Source 2's texture header. Only the three that appear
// in panorama/images/heroes/ are handled; anything else raises rather than
// returning a picture that is quietly wrong.
const (
	formatPNGRGBA8888 = 16
	formatDXT5        = 2
	formatBGRA8888    = 28
)

// VPKHeroDir is the flat portrait directory inside Dota's asset archive. The
// icons/ and selection/ folders hold the same heroes drawn for other panels, at
// shapes the top bar never uses.
const VPKHeroDir = "panorama/images/heroes"

// errUnsupportedTexture marks a texture format this reader does not decode.
type unsupportedTexture struct{ msg string }

func (e unsupportedTexture) Error() string { return e.msg }

// decodeVTEX turns the bytes of one .vtex_c resource into an image.
func decodeVTEX(data []byte) (image.Image, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("vtex: file too short (%d bytes)", len(data))
	}
	version := binary.LittleEndian.Uint16(data[4:6])
	if version != 12 {
		return nil, fmt.Errorf("vtex: unexpected resource header version %d", version)
	}
	blockOffset := binary.LittleEndian.Uint32(data[8:12])
	blockCount := binary.LittleEndian.Uint32(data[12:16])

	position := 8 + int(blockOffset)
	for i := uint32(0); i < blockCount; i++ {
		if position+12 > len(data) {
			break
		}
		name := data[position : position+4]
		offset := binary.LittleEndian.Uint32(data[position+4 : position+8])
		length := binary.LittleEndian.Uint32(data[position+8 : position+12])

		if string(name) == "DATA" {
			start := position + 4 + int(offset)
			if start+27 > len(data) {
				return nil, fmt.Errorf("vtex: DATA block truncated")
			}
			width := int(binary.LittleEndian.Uint16(data[start+20 : start+22]))
			height := int(binary.LittleEndian.Uint16(data[start+22 : start+24]))
			format := data[start+26]
			// The block length covers the header; the pixels follow it.
			pixelStart := start + int(length)
			if pixelStart > len(data) {
				pixelStart = len(data)
			}
			pixels := data[pixelStart:]

			switch format {
			case formatPNGRGBA8888:
				img, err := png.Decode(bytes.NewReader(pixels))
				if err != nil {
					return nil, fmt.Errorf("vtex: decode embedded PNG: %w", err)
				}
				return img, nil
			case formatDXT5:
				return decodeDXT5(pixels, width, height)
			case formatBGRA8888:
				return decodeBGRA8888(pixels, width, height)
			default:
				return nil, unsupportedTexture{fmt.Sprintf("vtex: image format %d is not handled", format)}
			}
		}
		position += 12
	}
	return nil, unsupportedTexture{"vtex: no DATA block in this texture"}
}

func rgb565(v uint16) (uint8, uint8, uint8) {
	r := (v >> 11) & 0x1f
	g := (v >> 5) & 0x3f
	b := v & 0x1f
	return uint8((r << 3) | (r >> 2)), uint8((g << 2) | (g >> 4)), uint8((b << 3) | (b >> 2))
}

// decodeDXT5 decodes the colour half of a DXT5 (BC3) block, skipping alpha:
// matching works on RGB, and these portraits are opaque where it counts. BC3
// always uses the four-colour interpolation, unlike bare BC1, where c0 <= c1
// selects a punch-through mode.
func decodeDXT5(data []byte, width, height int) (image.Image, error) {
	blocksX, blocksY := (width+3)/4, (height+3)/4
	expected := blocksX * blocksY * 16
	if len(data) < expected {
		return nil, unsupportedTexture{fmt.Sprintf("vtex: DXT5 needs %d bytes for %dx%d, got %d", expected, width, height, len(data))}
	}

	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for by := 0; by < blocksY; by++ {
		for bx := 0; bx < blocksX; bx++ {
			block := data[(by*blocksX+bx)*16 : (by*blocksX+bx)*16+16]
			c0 := uint16(block[8]) | uint16(block[9])<<8
			c1 := uint16(block[10]) | uint16(block[11])<<8
			r0, g0, b0 := rgb565(c0)
			r1, g1, b1 := rgb565(c1)

			var pal [4][3]uint8
			pal[0] = [3]uint8{r0, g0, b0}
			pal[1] = [3]uint8{r1, g1, b1}
			pal[2] = [3]uint8{
				uint8(math.Round((2*float64(r0) + float64(r1)) / 3)),
				uint8(math.Round((2*float64(g0) + float64(g1)) / 3)),
				uint8(math.Round((2*float64(b0) + float64(b1)) / 3)),
			}
			pal[3] = [3]uint8{
				uint8(math.Round((float64(r0) + 2*float64(r1)) / 3)),
				uint8(math.Round((float64(g0) + 2*float64(g1)) / 3)),
				uint8(math.Round((float64(b0) + 2*float64(b1)) / 3)),
			}

			bits := binary.LittleEndian.Uint32(block[12:16])
			for p := 0; p < 16; p++ {
				idx := (bits >> (2 * uint(p))) & 0b11
				row, col := p/4, p%4
				x, y := bx*4+col, by*4+row
				if x >= width || y >= height {
					continue
				}
				c := pal[idx]
				out.SetRGBA(x, y, color.RGBA{R: c[0], G: c[1], B: c[2], A: 255})
			}
		}
	}
	return out, nil
}

// decodeBGRA8888 decodes uncompressed Direct3D-order pixels.
func decodeBGRA8888(pixels []byte, width, height int) (image.Image, error) {
	needed := width * height * 4
	if len(pixels) < needed {
		return nil, unsupportedTexture{fmt.Sprintf("vtex: BGRA8888 needs %d bytes, got %d", needed, len(pixels))}
	}
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		out.Pix[i*4] = pixels[i*4+2]
		out.Pix[i*4+1] = pixels[i*4+1]
		out.Pix[i*4+2] = pixels[i*4]
		out.Pix[i*4+3] = pixels[i*4+3]
	}
	return out, nil
}

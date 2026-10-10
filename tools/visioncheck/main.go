// Command visioncheck is a diagnostic for the screen reader. It grabs the
// primary display once (or reads a saved capture), saves an annotated copy with
// the ten portrait boxes and what each was matched to, and prints the boxes and
// scores that decided it.
//
//	go run ./tools/visioncheck --own 1 --side left
//	go run ./tools/visioncheck --file capture.png --own 1 --side right
//
// Run it while a draft is on screen. If the red boxes in the saved image do not
// sit on the portraits, the layout fractions are wrong for this resolution; if
// they do and slots are still refused, the matching or the thresholds are.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"counterdraft/internal/heroes"
	"counterdraft/internal/vision"
)

type result struct {
	slot  vision.Slot
	match vision.Match
	name  string
}

func main() {
	own := flag.Int("own", 0, "your hero id, if you have picked")
	side := flag.String("side", "", "your side: left or right")
	file := flag.String("file", "", "read this image instead of capturing the screen")
	out := flag.String("out", vision.CaptureDir(), "directory for the saved capture")
	portraits := flag.String("portraits", vision.PortraitDir(), "reference portrait directory")
	minSim := flag.Float64("min-sim", vision.MinSimilarity, "accept a slot if similarity is at least this")
	minMargin := flag.Float64("min-margin", vision.MinMargin, "accept a slot if its margin over the runner-up is at least this")
	flag.Parse()
	vision.MinSimilarity = *minSim
	vision.MinMargin = *minMargin

	var img image.Image
	if *file != "" {
		f, err := os.Open(*file)
		if err != nil {
			fmt.Printf("could not open %s: %v\n", *file, err)
			os.Exit(1)
		}
		img, _, err = image.Decode(f)
		f.Close()
		if err != nil {
			fmt.Printf("could not decode %s: %v\n", *file, err)
			os.Exit(1)
		}
		fmt.Printf("loaded %s\n", *file)
	} else {
		img = vision.GrabWithTimeout(3 * time.Second)
		if img == nil {
			fmt.Println("could not capture the screen (timed out, or no display)")
			os.Exit(1)
		}
		fmt.Println("captured the screen")
	}
	b := img.Bounds()
	fmt.Printf("image %dx%d\n", b.Dx(), b.Dy())

	rec := vision.NewRecogniser(*portraits)
	n, err := rec.Load()
	if err != nil || n == 0 {
		fmt.Printf("no reference portraits in %s (loaded %d): %v\n", *portraits, n, err)
		fmt.Println("start the overlay once to download them, or run the portraits build")
		os.Exit(1)
	}
	fmt.Printf("reference templates: %d\n", n)
	fmt.Printf("thresholds: similarity >= %.2f, margin >= %.2f\n\n", vision.MinSimilarity, vision.MinMargin)

	all, _ := heroes.Load()
	nameOf := func(id int) string {
		if id == 0 {
			return "-"
		}
		if all != nil {
			return all.Name(id)
		}
		return fmt.Sprintf("#%d", id)
	}

	// Recognise every slot first, so the saved image can be labelled with the
	// answers and the table can be printed.
	var results []result
	found := map[int]bool{}
	heroSide := ""
	for _, slot := range vision.Slots(b.Dx(), b.Dy()) {
		m := rec.Identify(vision.Crop(img, slot.Box), found)
		res := result{slot: slot, match: m, name: "not recognised"}
		if m.HeroID != 0 {
			res.name = nameOf(m.HeroID)
			found[m.HeroID] = true
			if m.HeroID == *own {
				heroSide = slot.Team
			}
		}
		results = append(results, res)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "SLOT\tBOX (x,y wxh)\tHERO\tSIMILARITY\tMARGIN\tRUNNER-UP")
	for _, r := range results {
		box := r.slot.Box
		fmt.Fprintf(w, "%s\t(%d,%d %dx%d)\t%s\t%.3f\t%.3f\t%s\n",
			r.slot.Name(), box.Min.X, box.Min.Y, box.Dx(), box.Dy(),
			r.name, r.match.Similarity, r.match.Margin, nameOf(r.match.RunnerUp))
	}
	w.Flush()

	annotated := annotate(img, results)
	path, err := savePNG(annotated, *out, "visioncheck")
	if err == nil {
		fmt.Printf("\nannotated capture: %s\n", path)
	} else {
		fmt.Printf("\ncould not save capture: %v\n", err)
	}

	// A crop of just the top bar, so the boxes are easy to read at a glance.
	stripH := 0
	for _, r := range results {
		if r.slot.Box.Max.Y > stripH {
			stripH = r.slot.Box.Max.Y
		}
	}
	stripH = clamp(stripH+24, 1, b.Dy())
	strip := annotated.SubImage(image.Rect(0, 0, b.Dx(), stripH))
	if p, err := savePNG(strip, *out, "visioncheck-strip"); err == nil {
		fmt.Printf("top strip:         %s\n", p)
	}

	fmt.Printf("recognised %d of 10 slots\n", len(found))
	if heroSide != "" {
		fmt.Printf("your hero %d sits on the %s\n", *own, heroSide)
	}
	if *side != "" {
		fmt.Printf("GSI reports your side as %s\n", *side)
	} else {
		fmt.Println("(pass --side/--own to see the side cross-check)")
	}
}

// annotate returns a copy of the capture with every slot box outlined and the
// guessed hero written beside it.
func annotate(img image.Image, results []result) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)

	red := color.RGBA{255, 40, 40, 255}
	ok := color.RGBA{255, 230, 120, 255}
	bad := color.RGBA{255, 120, 120, 255}

	for _, r := range results {
		box := r.slot.Box.Intersect(image.Rect(0, 0, b.Dx(), b.Dy()))
		outline(out, box, red, 2)

		txt := fmt.Sprintf("%s %.2f", r.name, r.match.Similarity)
		if r.match.HeroID == 0 {
			txt = fmt.Sprintf("%s %.2f", "?", r.match.Similarity)
		}
		fg := ok
		if r.match.HeroID == 0 {
			fg = bad
		}
		label(out, box.Min.X+2, box.Min.Y-3, txt, fg)
	}
	return out
}

// label draws text with a dark backdrop so it stays readable over any art.
func label(img *image.RGBA, x, y int, s string, c color.RGBA) {
	f := basicfont.Face7x13
	width := font.MeasureString(f, s).Ceil()
	height := f.Metrics().Height.Ceil()
	if y-height < 0 {
		y = height
	}
	bg := image.Rect(x, y-height, x+width+2, y+2).Intersect(img.Bounds())
	draw.Draw(img, bg, image.NewUniform(color.RGBA{0, 0, 0, 200}), image.Point{}, draw.Over)
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: f, Dot: fixed.P(x+1, y-1)}
	d.DrawString(s)
}

func outline(img *image.RGBA, r image.Rectangle, c color.RGBA, thickness int) {
	for t := 0; t < thickness; t++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, clamp(r.Min.Y+t, 0, img.Bounds().Dy()-1), c)
			img.SetRGBA(x, clamp(r.Max.Y-1-t, 0, img.Bounds().Dy()-1), c)
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			img.SetRGBA(clamp(r.Min.X+t, 0, img.Bounds().Dx()-1), y, c)
			img.SetRGBA(clamp(r.Max.X-1-t, 0, img.Bounds().Dx()-1), y, c)
		}
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func savePNG(img image.Image, dir, prefix string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.png", prefix, time.Now().Format("20060102-150405")))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", err
	}
	return path, nil
}

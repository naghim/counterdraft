package vision

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
)

// Descriptor size. Small enough that the hero's silhouette and colour blocks
// dominate over cosmetic detail, large enough to tell similar heroes apart.
const (
	descriptorW = 64
	descriptorH = 40
)

// A match must clear both. Scored over 30 slots from three real 1920x1080
// captures: real portraits 0.778-0.935 similarity and 0.354-0.631 margin,
// empty slots 0.102-0.364 and 0.001-0.050. Two clean gaps, because the bar
// draws Valve's own art: a real portrait is near-identical to the reference,
// while an empty panel correlates with nothing in particular.
//
// The two errors are not symmetric. Refusing a real portrait costs the user one
// typed name; accepting an empty slot silently poisons every recommendation
// that follows. So when in doubt, refuse.
const (
	MinSimilarity = 0.55
	MinMargin     = 0.18
)

// Below this, a crop carries no structure worth matching. An empty slot is a
// flat panel, and dividing its float rounding error by its own tiny norm yields
// a full-strength unit vector made of nothing but noise. Measured over the 127
// reference portraits the pre-normalisation norm runs 11.0 to 28.0, while a
// flat image gives 0.00017 - about 66,000x smaller.
const MinStructure = 0.01

// Match is what the recogniser thinks a crop is, and how sure it is. A HeroID
// of 0 means the recogniser refused rather than guessed.
type Match struct {
	HeroID     int
	Similarity float64
	Margin     float64
	RunnerUp   int
}

// Confident reports whether the recogniser accepted the best candidate.
func (m Match) Confident() bool { return m.HeroID != 0 }

// HeroIDOf returns the hero a reference file belongs to, or 0 if the name is
// not one. "7.png" is Earthshaker and "7_persona1.png" is still Earthshaker: a
// persona is different art for the same hero, never a hero of its own.
func HeroIDOf(stem string) int {
	head := stem
	if i := strings.IndexByte(stem, '_'); i >= 0 {
		head = stem[:i]
	}
	id, err := strconv.Atoi(head)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// descriptor returns mean-subtracted, unit-length pixels, so a dot product is a
// cosine similarity. Brightness and overall tint drop out; structure decides.
func descriptor(img image.Image) []float32 {
	dst := image.NewRGBA(image.Rect(0, 0, descriptorW, descriptorH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)

	n := descriptorW * descriptorH
	out := make([]float32, n*3)
	var sum [3]float64
	idx := 0
	for y := 0; y < descriptorH; y++ {
		for x := 0; x < descriptorW; x++ {
			c := color.NRGBAModel.Convert(dst.At(x, y)).(color.NRGBA)
			r := float64(c.R) / 255
			g := float64(c.G) / 255
			b := float64(c.B) / 255
			out[idx] = float32(r)
			out[idx+1] = float32(g)
			out[idx+2] = float32(b)
			sum[0] += r
			sum[1] += g
			sum[2] += b
			idx += 3
		}
	}
	mean := [3]float64{sum[0] / float64(n), sum[1] / float64(n), sum[2] / float64(n)}

	var norm2 float64
	for i := 0; i < n; i++ {
		v := [3]float64{
			float64(out[i*3]) - mean[0],
			float64(out[i*3+1]) - mean[1],
			float64(out[i*3+2]) - mean[2],
		}
		out[i*3] = float32(v[0])
		out[i*3+1] = float32(v[1])
		out[i*3+2] = float32(v[2])
		norm2 += v[0]*v[0] + v[1]*v[1] + v[2]*v[2]
	}
	norm := math.Sqrt(norm2)
	if norm < MinStructure {
		return make([]float32, len(out))
	}
	scale := float32(1 / norm)
	for i := range out {
		out[i] *= scale
	}
	return out
}

// Recogniser matches portrait crops against the stored reference art.
type Recogniser struct {
	dir     string
	ids     []int
	matrix  [][]float32
	loaded  bool
	loadErr error
}

// NewRecogniser creates a recogniser reading reference art from dir. An empty
// dir uses PortraitDir().
func NewRecogniser(dir string) *Recogniser {
	if dir == "" {
		dir = PortraitDir()
	}
	return &Recogniser{dir: dir}
}

// Load reads the reference art into memory once. It returns how many templates
// were loaded. A second call is a no-op.
func (r *Recogniser) Load() (int, error) {
	if r.loaded {
		return len(r.ids), r.loadErr
	}
	r.loaded = true

	paths, err := filepath.Glob(filepath.Join(r.dir, "*.png"))
	if err != nil {
		r.loadErr = err
		return 0, err
	}
	sort.Strings(paths)

	for _, path := range paths {
		heroID := HeroIDOf(strings.TrimSuffix(filepath.Base(path), ".png"))
		if heroID == 0 {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			continue
		}
		r.ids = append(r.ids, heroID)
		r.matrix = append(r.matrix, descriptor(img))
	}
	return len(r.ids), nil
}

// Ready reports whether Load has completed and produced at least one template.
func (r *Recogniser) Ready() bool { return len(r.matrix) > 0 }

// Identify returns the best hero for a crop, or a Match with HeroID 0 when
// unsure. exclude drops heroes that cannot be in the slot - already banned, or
// already found elsewhere in the bar. Narrowing the field is free accuracy.
//
// The margin is measured against a *different hero*: a hero with a persona has
// two templates, and its own second template is often its closest rival. What
// the margin asks is "could this be someone else", so the comparison is between
// heroes, not templates.
func (r *Recogniser) Identify(crop image.Image, exclude map[int]bool) Match {
	if len(r.matrix) == 0 {
		return Match{}
	}
	d := descriptor(crop)

	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, len(r.ids))
	for i, row := range r.matrix {
		s := float32(0)
		for j := range row {
			s += row[j] * d[j]
		}
		scores[i] = scored{idx: i, score: float64(s)}
	}
	sort.SliceStable(scores, func(a, b int) bool { return scores[a].score > scores[b].score })

	// Apply exclusion while walking the sorted order so the runner-up is the
	// best non-excluded, different hero.
	var best scored
	var haveBest bool
	var runner float64
	var runnerID int
	var haveRunner bool

	for _, sc := range scores {
		heroID := r.ids[sc.idx]
		if exclude != nil && exclude[heroID] {
			continue
		}
		if !haveBest {
			best = sc
			haveBest = true
			continue
		}
		if heroID == r.ids[best.idx] {
			continue
		}
		runner = sc.score
		runnerID = heroID
		haveRunner = true
		break
	}
	if !haveBest {
		return Match{}
	}

	top := best.score
	if math.IsNaN(top) {
		return Match{}
	}
	bestID := r.ids[best.idx]

	margin := top
	if haveRunner {
		margin = top - runner
	}
	confident := top >= MinSimilarity && margin >= MinMargin

	m := Match{
		Similarity: round4(top),
		Margin:     round4(margin),
	}
	if haveRunner {
		m.RunnerUp = runnerID
	}
	if confident {
		m.HeroID = bestID
	}
	return m
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

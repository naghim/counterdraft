package vision

import (
	"image"
	"strings"
	"sync"
)

// Voting across frames. A single frame can be caught mid-animation, mid-fade,
// or with a tooltip over a portrait. The draft lasts a minute and the screen can
// be read several times a second, so a slot is only accepted once the same hero
// has won it repeatedly. This costs a little latency and removes a whole class
// of one-frame errors.
const (
	VotesNeeded = 2
	History     = 4
)

// DraftReading is what one pass over the screen concluded.
type DraftReading struct {
	OwnSide string // "left", "right", or "" when unknown
	Allies  []int
	Enemies []int
	// Slots the recogniser could not settle. An unpicked slot and an
	// unrecognised portrait cannot be told apart, so this is a count, not an
	// error.
	Unresolved []string
	// Conflict is true when the two side anchors disagreed. Distinct from "no
	// side known": this means a stated assumption has just been contradicted.
	Conflict bool
}

// Usable is false when the side is unknown, which makes allies and enemies
// meaningless.
func (r DraftReading) Usable() bool { return r.OwnSide != "" }

// Identifier matches a portrait crop to a hero id. It exists so the reader can
// be tested without a directory of reference art.
type Identifier interface {
	Identify(crop image.Image, exclude map[int]bool) Match
}

// DraftReader recognises all ten portrait slots, with voting across frames.
type DraftReader struct {
	ident       Identifier
	votesNeeded int
	history     int

	mu    sync.Mutex
	votes map[string][]int
}

// NewDraftReader creates a reader. A nil recogniser creates one reading the
// default portrait directory.
func NewDraftReader(recogniser *Recogniser) *DraftReader {
	if recogniser == nil {
		recogniser = NewRecogniser("")
	}
	return &DraftReader{
		ident:       recogniser,
		votesNeeded: VotesNeeded,
		history:     History,
		votes:       map[string][]int{},
	}
}

// newDraftReaderWith creates a reader over an arbitrary identifier, with
// explicit voting parameters. It is for tests.
func newDraftReaderWith(ident Identifier, votesNeeded, history int) *DraftReader {
	return &DraftReader{
		ident:       ident,
		votesNeeded: votesNeeded,
		history:     history,
		votes:       map[string][]int{},
	}
}

// Load reads the reference art into the underlying recogniser, if it is one.
// It returns how many templates are available.
func (d *DraftReader) Load() (int, error) {
	if r, ok := d.ident.(*Recogniser); ok {
		return r.Load()
	}
	return 1, nil
}

// Reset forgets the vote history. Call when a new match starts.
func (d *DraftReader) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.votes = map[string][]int{}
}

// vote records a reading (0 meaning "not recognised") and returns the hero the
// slot has settled on, or 0 if it has not.
func (d *DraftReader) vote(slot string, heroID int) int {
	seen := append(d.votes[slot], heroID)
	if len(seen) > d.history {
		seen = seen[len(seen)-d.history:]
	}
	d.votes[slot] = seen

	// Ties go to the hero that was seen first, mirroring Python's
	// Counter.most_common(1) on an insertion-ordered counter.
	counts := map[int]int{}
	order := make([]int, 0, len(seen))
	for _, h := range seen {
		if h == 0 {
			continue
		}
		if _, ok := counts[h]; !ok {
			order = append(order, h)
		}
		counts[h]++
	}
	best, bestCount := 0, 0
	for _, h := range order {
		if counts[h] > bestCount {
			best, bestCount = h, counts[h]
		}
	}
	if bestCount >= d.votesNeeded {
		return best
	}
	return 0
}

// Read recognises every slot and splits the result into allies and enemies.
//
// ownSide is "left" or "right" and comes from GSI's team name; ownHeroID is the
// player's hero, if they have one yet. Each settles the sides on its own, and
// disagreement reports nothing.
//
// exclude should carry the heroes that cannot appear - the bans GSI reports even
// in all-pick. Narrowing the field turns near-ties into clear answers.
func (d *DraftReader) Read(img image.Image, ownHeroID int, ownSide string, exclude []int) DraftReading {
	d.mu.Lock()
	defer d.mu.Unlock()

	blocked := make(map[int]bool, len(exclude)+TeamSize)
	for _, id := range exclude {
		if id > 0 {
			blocked[id] = true
		}
	}

	settled := map[string]int{}
	var unresolved []string

	for _, slot := range Slots(img.Bounds().Dx(), img.Bounds().Dy()) {
		match := d.ident.Identify(cropImage(img, slot.Box), blocked)
		heroID := d.vote(slot.Name(), match.HeroID)
		if heroID == 0 {
			unresolved = append(unresolved, slot.Name())
			continue
		}
		settled[slot.Name()] = heroID
		// A hero occupies one slot, so finding it here rules it out elsewhere
		// in the same pass.
		blocked[heroID] = true
	}

	heroSide := ""
	if ownHeroID != 0 {
		for name, heroID := range settled {
			if heroID == ownHeroID {
				if strings.HasPrefix(name, "left") {
					heroSide = "left"
				} else {
					heroSide = "right"
				}
				break
			}
		}
	}

	// Where the two anchors meet. Only a real disagreement is a conflict: the
	// hero simply not being on the bar yet is the normal early-draft case, not
	// evidence against the GSI team.
	conflict := ownSide != "" && heroSide != "" && ownSide != heroSide
	settledSide := ""
	if !conflict {
		if ownSide != "" {
			settledSide = ownSide
		} else {
			settledSide = heroSide
		}
	}

	reading := DraftReading{OwnSide: settledSide, Unresolved: unresolved, Conflict: conflict}
	if settledSide == "" {
		return reading
	}

	for name, heroID := range settled {
		side := "right"
		if strings.HasPrefix(name, "left") {
			side = "left"
		}
		if side == settledSide {
			reading.Allies = append(reading.Allies, heroID)
		} else {
			reading.Enemies = append(reading.Enemies, heroID)
		}
	}
	return reading
}

// cropImage returns the sub-image inside box, clamped to the image bounds.
func cropImage(img image.Image, box image.Rectangle) image.Image {
	box = box.Intersect(img.Bounds())
	if box.Empty() {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if s, ok := img.(subImager); ok {
		return s.SubImage(box)
	}
	return img
}

// Package vision reads the draft off the screen, because no API reports enemy
// picks in all-pick modes. It captures the Dota top bar, matches each portrait
// against reference hero art, and reports which five heroes sit on each side.
//
// The hard boundary, and the reason this is safe: only the user's own screen is
// read, exactly as OBS or any recorder does. Nothing here touches Dota's
// process, its memory or its files, and nothing automates input.
package vision

import (
	"fmt"
	"image"
)

// TeamSize is the number of portrait slots per side.
const TeamSize = 5

// Fractions of screen width/height, measured at 1920x1080 by finding the
// portrait edges in a real capture rather than by eye. Pitch and width differ:
// the portraits sit 124px apart but are only 116px wide, with an 8px gap.
const (
	portraitWidthFrac  = 116.0 / 1920.0
	slotPitchFrac      = 124.0 / 1920.0
	leftTeamStartFrac  = 210.0 / 1920.0
	rightTeamStartFrac = 1098.0 / 1920.0

	// Rows 6 to 72. The lower edge is where variance drops from 43 to 15 and
	// the colour turns from hero art to the panel behind the team bar. Six
	// pixels sound negligible and are not: cropping the frame into every
	// template costs more than it looks.
	slotTopFrac        = 6.0 / 1080.0
	portraitHeightFrac = 66.0 / 1080.0
)

// Slot is one player's portrait box, in absolute pixels for a given screen.
type Slot struct {
	Team  string // "left" or "right"
	Index int    // 0-4, in the order the bar shows them
	Box   image.Rectangle
}

// Name is the stable identifier for a slot, e.g. "left1" or "right3".
func (s Slot) Name() string { return fmt.Sprintf("%s%d", s.Team, s.Index+1) }

// Slots returns the ten portrait boxes for a screen of this size.
//
// The two teams are returned in bar order, left first. Which side is the enemy
// is not decided here: GSI reports the player's own team, so the caller knows,
// and a layout module has no business guessing it.
func Slots(width, height int) []Slot {
	portraitW := round(portraitWidthFrac * float64(width))
	pitch := slotPitchFrac * float64(width)
	portraitH := round(portraitHeightFrac * float64(height))
	top := round(slotTopFrac * float64(height))

	out := make([]Slot, 0, TeamSize*2)
	for _, team := range []struct {
		name  string
		start float64
	}{
		{"left", leftTeamStartFrac},
		{"right", rightTeamStartFrac},
	} {
		origin := team.start * float64(width)
		for index := 0; index < TeamSize; index++ {
			left := round(origin + float64(index)*pitch)
			out = append(out, Slot{
				Team:  team.name,
				Index: index,
				Box:   image.Rect(left, top, left+portraitW, top+portraitH),
			})
		}
	}
	return out
}

func round(v float64) int {
	if v < 0 {
		return int(v - 0.5)
	}
	return int(v + 0.5)
}

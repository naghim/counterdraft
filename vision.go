package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"counterdraft/internal/config"
	"counterdraft/internal/gsi"
	"counterdraft/internal/heroes"
	"counterdraft/internal/vision"
)

// Vision tuning, mirroring the Python session.
//
// Dota only shows the picks on the bar during hero selection; every other state
// is either too early to matter or already covered by GSI. A quarter second
// poll is 14% of one core while a draft is on screen and returns before
// capturing otherwise.
const (
	heroSelectionState = "DOTA_GAMERULES_STATE_HERO_SELECTION"
	visionPollEvery    = 250 * time.Millisecond

	// How long a hero must be absent from the bar before what the screen reader
	// put in the draft is taken back out. Written as a duration so changing the
	// poll rate cannot quietly change the rule.
	visionDropSeconds = 2.0
	visionDropAfter   = 8 // max(2, round(2.0/0.25))

	// DraftState enforces these too; repeated so vision can stop before raising.
	visionEnemyLimit = 5
	visionOwnLimit   = 4
)

// ensurePortraits builds the reference art the screen reader matches against,
// on its own goroutine: the first run downloads 127 images.
func (a *App) ensurePortraits() {
	if a.ix == nil {
		return
	}
	all := a.ix.All()
	list := make([]vision.HeroImage, 0, len(all))
	for _, h := range all {
		list = append(list, vision.HeroImage{ID: h.ID, Name: h.Name})
	}
	dotaPath, _ := config.FindDotaPath()

	build := vision.Ensure(list, vision.PortraitDir(), dotaPath)

	a.mu.Lock()
	defer a.mu.Unlock()
	a.visionAvailable = build.Usable()
	switch {
	case !build.Usable():
		a.visionStatus = "Screen reading unavailable: no reference portraits could be loaded."
	case !build.DotaFound:
		// The defaults are enough for most slots; the missing half is only the
		// arcanas and personas, which exist nowhere but the installed game.
		a.visionStatus = fmt.Sprintf(
			"Screen reading ready: %d heroes. Dota 2 was not found, so arcana and persona portraits are missing; those slots are refused rather than guessed.",
			build.Heroes)
	case build.Error != "":
		a.visionStatus = fmt.Sprintf("Screen reading ready (%d portraits); alternates incomplete: %s", build.Heroes, build.Error)
	default:
		a.visionStatus = fmt.Sprintf("Screen reading ready: %d heroes, %d alternates.", build.Heroes, build.Variants)
	}
	if build.Changed() {
		// Drop the reader so the next pass builds one that loads the art that
		// just landed. HeroRecogniser reads the directory once and holds it.
		a.visionRebuild = true
	}
	a.recomputeLocked()
	log.Printf("counterdraft: portraits: %d heroes, %d alternates, %d downloaded, %d extracted, dotaFound=%t, err=%q",
		build.Heroes, build.Variants, build.Downloaded, build.Extracted, build.DotaFound, build.Error)
}

// visionLoop reads the screen while a draft is on it, until the context ends.
func (a *App) visionLoop(ctx context.Context) {
	ticker := time.NewTicker(visionPollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollVision()
		}
	}
}

// pollVision reads enemy picks off the screen. It returns nothing; the draft
// change is emitted from recomputeLocked inside the lock.
//
// This is the one thing no API reports, so it is also the one thing with no
// second opinion to check against. Three conditions before it runs at all, each
// of which would otherwise produce confident nonsense:
//
//   - Dota must say a draft is on screen, or the bar holds something else;
//   - a side must be known, or which half is the enemy is a guess;
//   - the recogniser must be sure, which it decides per slot.
func (a *App) pollVision() {
	a.mu.Lock()
	enabled := a.visionEnabled
	state := a.gameState
	ownHeroID := a.ownHeroID
	ownSide := a.ownSide
	bans := append([]int(nil), a.bannedIDs...)
	rebuild := a.visionRebuild
	reset := a.visionReset
	a.visionRebuild = false
	a.visionReset = false
	a.mu.Unlock()

	if !enabled {
		return
	}
	if rebuild && a.visionReader != nil {
		a.visionReader = nil
	}
	if reset && a.visionReader != nil {
		a.visionReader.Reset()
	}
	if state != heroSelectionState {
		return
	}
	// Either anchor will do. Requiring the hero was the bug that made this
	// useless in all-pick: enemies are on the bar before the player picks.
	if ownSide == "" && ownHeroID == 0 {
		return
	}

	reader := a.visionReader
	if reader == nil {
		candidate := vision.NewDraftReader(nil)
		n, err := candidate.Load()
		if err != nil || n == 0 {
			// No reference art yet - the portrait build is very likely still
			// fetching it. Say nothing and try again next pass.
			return
		}
		reader = candidate
		a.visionReader = reader
	}

	image := vision.GrabWithTimeout(vision.DefaultTimeout)
	if image == nil {
		// A timed-out capture is not "Dota is not on screen", so this retries
		// next pass rather than disabling vision.
		return
	}

	reading := reader.Read(image, ownHeroID, ownSide, bans)

	if reading.Conflict {
		a.setVisionStatus("Screen reading paused: Dota's team and the bar disagree.")
		return
	}
	if !reading.Usable() {
		return
	}
	// Nothing recognised anywhere on the bar almost always means Dota is not the
	// window on screen - the user alt-tabbed - rather than that ten heroes
	// vanished at once, so believing it would retract the whole draft.
	if len(reading.Allies) == 0 && len(reading.Enemies) == 0 {
		return
	}

	a.mu.Lock()
	a.applyReadingLocked(reading, ownHeroID)
	a.mu.Unlock()
}

// applyReadingLocked merges one screen reading into the vision-owned picks.
// Callers must hold a.mu.
func (a *App) applyReadingLocked(reading vision.DraftReading, ownHeroID int) {
	enemyWanted := dedupeIDs(reading.Enemies)
	var ownWanted []int
	for _, id := range dedupeIDs(reading.Allies) {
		if id != ownHeroID {
			ownWanted = append(ownWanted, id)
		}
	}

	wanted := make(map[int]string, len(enemyWanted)+len(ownWanted))
	for _, id := range enemyWanted {
		wanted[id] = sideEnemy
	}
	for _, id := range ownWanted {
		wanted[id] = sideOwn
	}

	// Enemy slots the reader could not settle. Deliberately not called a
	// failure: an unpicked slot and an unrecognised portrait cannot be told
	// apart, and an empty panel overlaps a real portrait in structure.
	enemyPrefix := "right"
	if reading.OwnSide == "right" {
		enemyPrefix = "left"
	}
	unread := 0
	for _, name := range reading.Unresolved {
		if strings.HasPrefix(name, enemyPrefix) {
			unread++
		}
	}
	a.visionUnread = unread

	a.retractLocked(wanted)
	a.absorbLocked(enemyWanted, ownWanted)

	a.visionStatus = fmt.Sprintf("Reading the top bar: %d enemy, %d ally slots read.", len(enemyWanted), len(ownWanted))
	if unread > 0 {
		a.visionStatus += fmt.Sprintf(" %d enemy slot(s) unrecognised.", unread)
	}
	a.recomputeLocked()
}

// retractLocked takes back heroes the screen reader added that the bar no
// longer shows. Only what vision added is ever removed - a hero the user typed
// is theirs to keep. Callers must hold a.mu.
func (a *App) retractLocked(wanted map[int]string) []string {
	var removed []string
	for id, side := range a.visionAdded {
		if wanted[id] == side {
			delete(a.visionMissing, id)
			continue
		}
		a.visionMissing[id]++
		if a.visionMissing[id] < visionDropAfter {
			continue
		}
		if side == sideEnemy {
			a.visionEnemy = removeID(a.visionEnemy, id)
		} else {
			a.visionOwn = removeID(a.visionOwn, id)
		}
		delete(a.visionAdded, id)
		delete(a.visionMissing, id)
		removed = append(removed, a.ix.Name(id))
	}
	return removed
}

// absorbLocked adds the heroes the bar shows, as far as the slot limits allow.
// Callers must hold a.mu.
func (a *App) absorbLocked(enemyWanted, ownWanted []int) []string {
	var added []string
	absorb := func(id int, side string) {
		if id <= 0 || a.visionRejected[id] {
			return
		}
		target := &a.visionEnemy
		limit := visionEnemyLimit
		if side == sideOwn {
			target = &a.visionOwn
			limit = visionOwnLimit
		}
		if len(*target) >= limit || a.heroKnownLocked(id) {
			return
		}
		*target = append(*target, id)
		a.visionAdded[id] = side
		added = append(added, a.ix.Name(id))
	}
	for _, id := range enemyWanted {
		absorb(id, sideEnemy)
	}
	for _, id := range ownWanted {
		absorb(id, sideOwn)
	}
	return added
}

// heroKnownLocked reports whether a hero is already in the draft (from GSI or
// vision), so vision does not add a duplicate.
func (a *App) heroKnownLocked(id int) bool {
	for _, existing := range append(append([]int(nil), a.detectedEnemy...), a.detectedOwn...) {
		if existing == id {
			return true
		}
	}
	for _, existing := range append(append([]int(nil), a.visionEnemy...), a.visionOwn...) {
		if existing == id {
			return true
		}
	}
	return false
}

// setVisionStatus updates the status line without touching the draft, emitting
// a fresh snapshot so the UI reflects it.
func (a *App) setVisionStatus(status string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.visionStatus = status
	a.recomputeLocked()
}

// SetVision enables or disables screen reading and remembers the choice.
func (a *App) SetVision(enabled bool) error {
	a.mu.Lock()
	a.visionEnabled = enabled
	a.base.VisionEnabled = enabled
	a.recomputeLocked()
	a.mu.Unlock()

	s := loadSettings()
	s.Vision = enabled
	if err := saveSettings(s); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

// resetVisionLocked forgets everything the screen reader believed. Callers must
// hold a.mu.
func (a *App) resetVisionLocked() {
	a.visionEnemy = nil
	a.visionOwn = nil
	a.visionAdded = map[int]string{}
	a.visionMissing = map[int]int{}
	a.visionRejected = map[int]bool{}
	a.visionUnread = 0
	a.visionReset = true
}

// banIDs collects every hero banned by either side. GSI reports bans even in
// all-pick, and a banned hero cannot be on the bar, so passing these to the
// reader turns near-ties into answers for free.
func banIDs(p gsi.Payload, ix *heroes.Index) []int {
	if ix == nil {
		return nil
	}
	seen := map[int]bool{}
	var out []int
	add := func(id int) {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, d := range []gsi.TeamDraft{p.Draft.Team2, p.Draft.Team3} {
		for _, id := range d.Bans() {
			add(id)
		}
		for _, name := range d.BanClasses() {
			if h, ok := ix.ByNpcName(name); ok {
				add(h.ID)
			}
		}
	}
	return out
}

func dedupeIDs(ids []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func unionIDs(a, b []int) []int {
	if len(b) == 0 {
		return append([]int(nil), a...)
	}
	seen := map[int]bool{}
	out := make([]int, 0, len(a)+len(b))
	for _, id := range a {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range b {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

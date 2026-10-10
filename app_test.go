package main

import (
	"testing"

	"counterdraft/internal/gsi"
	"counterdraft/internal/heroes"
	"counterdraft/internal/rules"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	ix, err := heroes.Load()
	if err != nil {
		t.Fatalf("load heroes: %v", err)
	}
	return &App{
		ix:     ix,
		engine: rules.New(ix),
		base:   Snapshot{Status: "test"},
	}
}

func TestHandleGSISplitsTeamsAndRecommends(t *testing.T) {
	a := newTestApp(t)

	var p gsi.Payload
	p.Player.Team = 2
	p.Draft.Team2.Pick0ID = 1 // Anti-Mage (ally)
	p.Draft.Team3.Pick0ID = 44
	p.Draft.Team3.Pick1ID = 32
	p.Draft.Team3.Pick2ID = 59

	a.handleGSI(p)

	if len(a.base.Own) != 1 || a.base.Own[0].ID != 1 {
		t.Errorf("own = %+v, want [Anti-Mage]", a.base.Own)
	}
	if len(a.base.Enemy) != 3 {
		t.Errorf("enemy = %+v, want 3 heroes", a.base.Enemy)
	}
	if len(a.base.Recommendations) == 0 {
		t.Fatal("expected recommendations")
	}
	for _, r := range a.base.Recommendations {
		if r.HeroID == 44 || r.HeroID == 32 || r.HeroID == 59 || r.HeroID == 1 {
			t.Errorf("recommended already-picked hero %s", r.Name)
		}
	}
}

func idsOf(refs []HeroRef) []int {
	out := make([]int, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.ID)
	}
	return out
}

func contains(ids []int, want int) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func draftWithEnemy(t *testing.T) gsi.Payload {
	t.Helper()
	var p gsi.Payload
	p.Player.Team = 2
	p.Draft.Team2.Pick0ID = 1  // ally Anti-Mage
	p.Draft.Team3.Pick0ID = 44 // enemy Phantom Assassin
	return p
}

func TestAddPickOverridesDetection(t *testing.T) {
	a := newTestApp(t)
	a.handleGSI(draftWithEnemy(t))
	if got := idsOf(a.base.Enemy); len(got) != 1 || got[0] != 44 {
		t.Fatalf("detected enemy = %v, want [44]", got)
	}

	if err := a.AddPick("enemy", 32); err != nil { // Riki
		t.Fatalf("AddPick: %v", err)
	}
	if !a.base.EnemyManual {
		t.Error("EnemyManual should be set after an edit")
	}
	got := idsOf(a.base.Enemy)
	if !contains(got, 44) || !contains(got, 32) {
		t.Errorf("enemy = %v, want it to contain 44 and 32", got)
	}
	for _, r := range a.base.Recommendations {
		if r.HeroID == 32 || r.HeroID == 44 {
			t.Errorf("recommended picked hero %s", r.Name)
		}
	}
}

func TestManualRemovalIsStickyAcrossUpdates(t *testing.T) {
	a := newTestApp(t)
	a.handleGSI(draftWithEnemy(t))
	if err := a.RemovePick("enemy", 44); err != nil {
		t.Fatalf("RemovePick: %v", err)
	}
	if got := idsOf(a.base.Enemy); len(got) != 0 {
		t.Fatalf("enemy after removal = %v, want empty", got)
	}

	// A fresh GSI update still reports PA; manual mode must ignore it.
	a.handleGSI(draftWithEnemy(t))
	if got := idsOf(a.base.Enemy); len(got) != 0 {
		t.Errorf("enemy = %v, want empty (manual override sticky)", got)
	}
}

func TestResetSideReturnsToDetection(t *testing.T) {
	a := newTestApp(t)
	a.handleGSI(draftWithEnemy(t))
	if err := a.AddPick("enemy", 32); err != nil {
		t.Fatal(err)
	}
	if err := a.ResetSide("enemy"); err != nil {
		t.Fatalf("ResetSide: %v", err)
	}
	if a.base.EnemyManual {
		t.Error("EnemyManual should be cleared after reset")
	}
	if got := idsOf(a.base.Enemy); len(got) != 1 || got[0] != 44 {
		t.Errorf("enemy = %v, want detected [44]", got)
	}
}

func TestAddPickRejectsUnknownHeroAndSide(t *testing.T) {
	a := newTestApp(t)
	if err := a.AddPick("enemy", 999999); err == nil {
		t.Error("expected error for unknown hero id")
	}
	if err := a.AddPick("nope", 1); err == nil {
		t.Error("expected error for unknown side")
	}
}

func TestHeroesSortedByName(t *testing.T) {
	a := newTestApp(t)
	hs := a.Heroes()
	if len(hs) == 0 {
		t.Fatal("expected heroes")
	}
	for i := 1; i < len(hs); i++ {
		if hs[i-1].Name > hs[i].Name {
			t.Fatalf("heroes not sorted: %q before %q", hs[i-1].Name, hs[i].Name)
		}
	}
}

func TestPicksForTeamUnionsDraftAndMinimap(t *testing.T) {
	a := newTestApp(t)

	var p gsi.Payload
	p.Draft.Team3.Pick0ID = 44 // Phantom Assassin from the draft block
	p.Draft.Team3.Pick1Class = "npc_dota_hero_riki"
	p.Minimap = map[string]gsi.MinimapElement{
		"0": {UnitName: "npc_dota_hero_axe", Team: "dire"},
		"1": {UnitName: "npc_dota_hero_axe", Team: "dire"}, // duplicate ignored
		"2": {UnitName: "npc_dota_hero_lina", Team: "radiant"},
	}
	got := picksForTeam(p, 3, a.ix)
	if !contains(got, 44) || !contains(got, 32) || !contains(got, 2) {
		t.Errorf("picksForTeam(3) = %v, want it to contain 44 (draft id), 32 (draft class), 2 (minimap)", got)
	}
	if contains(got, 5) { // Lina is radiant; must not appear on dire
		t.Errorf("picksForTeam(3) leaked a radiant hero: %v", got)
	}
}

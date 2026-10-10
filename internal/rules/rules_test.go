package rules

import (
	"strings"
	"testing"

	"counterdraft/internal/heroes"
)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	ix, err := heroes.Load()
	if err != nil {
		t.Fatalf("load heroes: %v", err)
	}
	return New(ix)
}

func TestEvasionAndInvisCounters(t *testing.T) {
	e := newEngine(t)
	// Phantom Assassin (1vasion), Riki (invis), Huskar (self_sustain).
	recs := e.Recommend([]int{44, 32, 59}, nil, 20)
	if len(recs) == 0 {
		t.Fatal("expected recommendations, got none")
	}
	reasons := map[string]bool{}
	for _, r := range recs {
		for _, why := range r.Reasons {
			reasons[why] = true
		}
	}
	for _, want := range []string{
		"Enemy relies on evasion — magic/pure damage and Break bypass it",
		"Enemy goes invisible — bring detection and AoE",
	} {
		found := false
		for why := range reasons {
			if strings.Contains(why, strings.SplitN(want, " — ", 2)[0]) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing expected reason containing %q", want)
		}
	}
}

func TestPickedHeroesExcluded(t *testing.T) {
	e := newEngine(t)
	enemy := []int{44, 32, 59}
	recs := e.Recommend(enemy, nil, 200)
	picked := map[int]bool{44: true, 32: true, 59: true}
	for _, r := range recs {
		if picked[r.HeroID] {
			t.Errorf("recommended already-picked hero %s", r.Name)
		}
	}
}

func TestSynergyFillsMissingDisable(t *testing.T) {
	e := newEngine(t)
	// Anti-Mage, Spectre, Phantom Lancer: carries with no hard control between
	// them (Medusa would not work here because Stone Gaze is a stun).
	own := []int{1, 67, 12}
	recs := e.Recommend(nil, own, 50)
	if len(recs) == 0 {
		t.Fatal("expected synergy recommendations")
	}
	addressed := false
	for _, r := range recs {
		for _, why := range r.Reasons {
			if strings.Contains(why, "hard disable") {
				addressed = true
			}
		}
	}
	if !addressed {
		t.Error("expected at least one recommendation to address the missing hard disable")
	}
}

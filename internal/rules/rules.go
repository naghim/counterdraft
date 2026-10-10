// Package rules implements the tag-based counter/synergy recommender.
//
// The engine is deliberately heuristic and explainable: it reasons about
// mechanical capabilities ("enemy has evasion") rather than match win rates,
// so recommendations are not confounded by player skill or game outcomes.
package rules

import (
	"sort"
	"strings"

	"counterdraft/internal/heroes"
)

// groups lets rules refer to a class of tags. A hero "has" a group when it
// carries at least one of the group's concrete tags.
var groups = map[string][]string{
	"hard_control":   {"stun", "hex", "fear", "sleep", "taunt", "knockback"},
	"disable":        {"stun", "hex", "fear", "sleep", "taunt", "knockback", "root", "silence", "disarm"},
	"sustain":        {"heal", "lifesteal", "self_sustain", "regen", "shield_ally", "save"},
	"reliable_setup": {"stun", "root", "sleep", "taunt", "aoe"},
	"damage":         {"physical", "magical", "pure", "burst"},
	"early_pressure": {"early_game", "gank", "roam", "push"},
}

// expand turns a query tag into the concrete tags it matches.
func expand(tag string) []string {
	if members, ok := groups[tag]; ok {
		return members
	}
	return []string{tag}
}

// CounterRule fires when the enemy team's aggregated tags satisfy All/Any and
// recommends candidate heroes carrying at least one Answer tag.
type CounterRule struct {
	ID     string
	All    []string // every tag required on the enemy team
	Any    []string // at least one of these required on the enemy team
	Answer []string
	Reason string
	Weight int
}

// SynergyRule fires against your own team's tags and recommends candidates
// that complement it.
type SynergyRule struct {
	ID      string
	Missing []string // your team must have none of these
	Require []string // your team must have at least one of these (optional)
	Want    []string
	Reason  string
	Weight  int
}

var counterRules = []CounterRule{
	{ID: "evasion", All: []string{"evasion"}, Answer: []string{"magical", "pure", "break"},
		Reason: "Enemy relies on evasion — magic/pure damage and Break bypass it", Weight: 3},
	{ID: "high_armor", All: []string{"high_armor"}, Answer: []string{"magical", "pure", "armor_reduction"},
		Reason: "Enemy stacks armor — lean on magic/pure damage or armor shred", Weight: 3},
	{ID: "sustain", Any: []string{"heal", "lifesteal", "self_sustain"}, Answer: []string{"anti_heal", "burst", "single_target"},
		Reason: "Enemy heals a lot — bring anti-heal or burst to outpace it", Weight: 4},
	{ID: "invis", All: []string{"invis"}, Answer: []string{"detection", "aoe", "global_vision"},
		Reason: "Enemy goes invisible — bring detection and AoE", Weight: 4},
	{ID: "illusions", All: []string{"illusions"}, Answer: []string{"aoe", "wave_clear"},
		Reason: "Enemy floods illusions — AoE and wave clear shred them", Weight: 3},
	{ID: "summons", All: []string{"summons"}, Answer: []string{"aoe", "wave_clear"},
		Reason: "Enemy relies on summons — AoE clears them", Weight: 3},
	{ID: "channeled", All: []string{"channeled"}, Answer: []string{"stun", "silence", "hex", "fear", "taunt", "knockback"},
		Reason: "Enemy channels key spells — interrupts shut them down", Weight: 3},
	{ID: "single_target", All: []string{"single_target"}, Answer: []string{"save", "shield_ally", "heal"},
		Reason: "Enemy wins through single-target burst — saves and shields negate it", Weight: 2},
	{ID: "spell_immune", Any: []string{"spell_immune"}, Answer: []string{"bkb_pierce", "escape", "mobility"},
		Reason: "Enemy can become spell immune — BKB-piercing control or kite them", Weight: 3},
	{ID: "mobility", Any: []string{"blink", "mobility"}, Answer: []string{"root", "hex", "stun", "silence"},
		Reason: "Enemy is highly mobile — roots and instant disables lock them down", Weight: 2},
	{ID: "melee_heavy", All: []string{"melee_heavy"}, Answer: []string{"ranged", "slow", "aoe", "wave_clear"},
		Reason: "Enemy is melee-heavy — ranged kiting and AoE punish clumping", Weight: 1},
	{ID: "squishy", Any: []string{"squishy"}, Answer: []string{"burst"},
		Reason: "Enemy has squishy targets — burst them before fights develop", Weight: 1},
	{ID: "late_game", Any: []string{"late_game"}, Answer: []string{"early_game", "gank", "push", "roam"},
		Reason: "Enemy outscales late — add early pressure and end before they come online", Weight: 2},
	{ID: "push", Any: []string{"push", "split_push"}, Answer: []string{"wave_clear", "aoe"},
		Reason: "Enemy pushes fast — wave clear and depush to hold towers", Weight: 3},
	{ID: "save", All: []string{"save"}, Answer: []string{"silence", "hex", "stun", "burst"},
		Reason: "Enemy has saves — disable the saver or burst through before it lands", Weight: 2},
	{ID: "teamfight", All: []string{"teamfight"}, Answer: []string{"counter_initiate", "save", "aoe"},
		Reason: "Enemy wants 5v5 — counter-initiation and saves blunt their wombo", Weight: 1},
	{ID: "escape", All: []string{"escape"}, Answer: []string{"root", "hex", "stun", "silence"},
		Reason: "Enemy is elusive — roots and hex prevent escapes", Weight: 2},
	{ID: "physical", Any: []string{"physical"}, Answer: []string{"evasion", "disarm", "high_armor"},
		Reason: "Enemy leans on physical damage — evasion, disarms and armor reduce it", Weight: 1},
}

var synergyRules = []SynergyRule{
	{ID: "no_hard_control", Missing: []string{"hard_control"}, Want: []string{"hard_control"},
		Reason: "Your team lacks a reliable hard disable — add stuns/holds", Weight: 3},
	{ID: "no_disable", Missing: []string{"disable"}, Want: []string{"disable"},
		Reason: "Your team has almost no disable — add control to fight", Weight: 3},
	{ID: "no_wave_clear", Missing: []string{"wave_clear"}, Want: []string{"wave_clear", "aoe"},
		Reason: "No wave clear — you'll struggle to defend pushes", Weight: 2},
	{ID: "no_sustain", Missing: []string{"sustain"}, Want: []string{"heal", "save", "shield_ally", "self_sustain"},
		Reason: "No sustain or saves — add a defensive/save support", Weight: 2},
	{ID: "no_initiation", Missing: []string{"initiator"}, Want: []string{"initiator", "teamfight"},
		Reason: "No initiation — add a teamfight starter", Weight: 2},
	{ID: "setup_no_burst", Require: []string{"reliable_setup"}, Missing: []string{"burst"}, Want: []string{"burst", "damage"},
		Reason: "You have setup but little burst — add follow-up damage", Weight: 2},
	{ID: "greedy", Require: []string{"late_game"}, Missing: []string{"early_pressure"}, Want: []string{"early_game", "gank", "push", "roam"},
		Reason: "Greedy late-game lineup — add early pressure so you survive to it", Weight: 2},
	{ID: "no_detection", Missing: []string{"detection"}, Want: []string{"detection", "aoe", "global_vision"},
		Reason: "No detection — add a way to reveal invis heroes", Weight: 2},
	{ID: "no_push", Missing: []string{"push"}, Want: []string{"push", "wave_clear"},
		Reason: "Little pushing power — add a hero that takes objectives", Weight: 1},
}

// recommendation is the internal scored candidate.
type recommendation struct {
	HeroID  int
	Score   int
	Reasons []string
}

// Recommendation is a scored hero suggestion returned to the UI.
type Recommendation struct {
	HeroID  int      `json:"heroId"`
	Name    string   `json:"name"`
	Tags    []string `json:"tags"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
}

// Engine scores hero candidates against the current draft.
type Engine struct {
	ix *heroes.Index
}

// New creates an engine backed by the given hero index.
func New(ix *heroes.Index) *Engine { return &Engine{ix: ix} }

// aggregate returns the union of concrete tags carried by the given heroes.
func (e *Engine) aggregate(picks []int) map[string]bool {
	out := map[string]bool{}
	for _, id := range picks {
		if h, ok := e.ix.ByID(id); ok {
			for _, t := range h.Tags {
				out[t] = true
			}
		}
	}
	return out
}

// hasTag reports whether the aggregated tag set satisfies a query tag,
// expanding groups (any member counts).
func hasTag(set map[string]bool, query string) bool {
	for _, t := range expand(query) {
		if set[t] {
			return true
		}
	}
	return false
}

func allTags(set map[string]bool, queries []string) bool {
	for _, q := range queries {
		if !hasTag(set, q) {
			return false
		}
	}
	return true
}

func anyTags(set map[string]bool, queries []string) bool {
	for _, q := range queries {
		if hasTag(set, q) {
			return true
		}
	}
	return false
}

// Recommend scores every unpicked hero against the draft and returns the top N.
func (e *Engine) Recommend(enemy, own []int, topN int) []Recommendation {
	enemyTags := e.aggregate(enemy)
	ownTags := e.aggregate(own)
	picked := map[int]bool{}
	for _, id := range enemy {
		picked[id] = true
	}
	for _, id := range own {
		picked[id] = true
	}

	// Saturation penalties keep the draft balanced rather than suggesting five
	// carries.
	ownCarries, ownSupports := 0, 0
	for _, id := range own {
		if h, ok := e.ix.ByID(id); ok {
			for _, r := range h.Roles {
				if r == "Carry" {
					ownCarries++
				}
				if r == "Support" {
					ownSupports++
				}
			}
		}
	}

	var scored []recommendation
	for _, h := range e.ix.All() {
		if picked[h.ID] {
			continue
		}
		score := 0
		seen := map[string]bool{}
		var reasons []string
		add := func(reason string) {
			if !seen[reason] {
				seen[reason] = true
				reasons = append(reasons, reason)
			}
		}

		for _, r := range counterRules {
			if !ruleMatches(r.All, r.Any, enemyTags) {
				continue
			}
			if !heroHasAny(e.ix, h.ID, r.Answer) {
				continue
			}
			score += r.Weight
			add(r.Reason)
		}
		for _, r := range synergyRules {
			if !synergyMatches(r, ownTags) {
				continue
			}
			if !heroHasAny(e.ix, h.ID, r.Want) {
				continue
			}
			score += r.Weight
			add(r.Reason)
		}

		for _, role := range h.Roles {
			if role == "Carry" && ownCarries >= 2 {
				score -= 2
				add("Already carry-heavy — reduced priority")
			}
			if role == "Support" && ownSupports >= 2 {
				score -= 1
				add("Already support-heavy — reduced priority")
			}
		}

		if score <= 0 {
			continue
		}
		scored = append(scored, recommendation{HeroID: h.ID, Score: score, Reasons: reasons})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return e.ix.Name(scored[i].HeroID) < e.ix.Name(scored[j].HeroID)
	})

	if topN > 0 && len(scored) > topN {
		scored = scored[:topN]
	}

	out := make([]Recommendation, 0, len(scored))
	for _, s := range scored {
		h, _ := e.ix.ByID(s.HeroID)
		out = append(out, Recommendation{
			HeroID:  s.HeroID,
			Name:    h.LocalizedName,
			Tags:    h.Tags,
			Score:   s.Score,
			Reasons: s.Reasons,
		})
	}
	return out
}

func ruleMatches(all, any []string, set map[string]bool) bool {
	if len(all) > 0 && !allTags(set, all) {
		return false
	}
	if len(any) > 0 && !anyTags(set, any) {
		return false
	}
	return len(all) > 0 || len(any) > 0
}

func synergyMatches(r SynergyRule, set map[string]bool) bool {
	if len(r.Missing) > 0 && anyTags(set, r.Missing) {
		return false
	}
	if len(r.Require) > 0 && !anyTags(set, r.Require) {
		return false
	}
	return true
}

func heroHasAny(ix *heroes.Index, id int, queries []string) bool {
	for _, q := range queries {
		for _, t := range expand(q) {
			if ix.HasTag(id, t) {
				return true
			}
		}
	}
	return false
}

// Describe returns a human-readable summary of tags, used for logging.
func Describe(tags []string) string { return strings.Join(tags, ", ") }

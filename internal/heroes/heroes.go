// Package heroes embeds hero metadata and the hand-authored mechanical tag
// table used by the recommendation engine. No match/win-rate statistics are
// involved; tags describe what a hero's kit does mechanically.
package heroes

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed heroes.json
var raw []byte

// Hero is a single Dota 2 hero with its mechanical capability tags.
type Hero struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	LocalizedName string   `json:"localized_name"`
	Attr          string   `json:"attr"`
	AttackType    string   `json:"attack_type"`
	Roles         []string `json:"roles"`
	Tags          []string `json:"tags"`
}

// Index is a loaded, queryable hero table.
type Index struct {
	all    []*Hero
	byID   map[int]*Hero
	byName map[string]*Hero
	tags   map[int]map[string]bool
}

// Load parses the embedded hero table.
func Load() (*Index, error) {
	var list []*Hero
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("heroes: decode: %w", err)
	}
	ix := &Index{
		all:    list,
		byID:   make(map[int]*Hero, len(list)),
		byName: make(map[string]*Hero, len(list)),
		tags:   make(map[int]map[string]bool, len(list)),
	}
	for _, h := range list {
		ix.byID[h.ID] = h
		ix.byName[h.LocalizedName] = h
		set := make(map[string]bool, len(h.Tags))
		for _, t := range h.Tags {
			set[t] = true
		}
		ix.tags[h.ID] = set
	}
	return ix, nil
}

// All returns every hero in the index.
func (ix *Index) All() []*Hero { return ix.all }

// ByID looks up a hero by its stable Dota hero id.
func (ix *Index) ByID(id int) (*Hero, bool) {
	h, ok := ix.byID[id]
	return h, ok
}

// ByName looks up a hero by its localized (display) name.
func (ix *Index) ByName(name string) (*Hero, bool) {
	h, ok := ix.byName[name]
	return h, ok
}

// HasTag reports whether the hero with the given id carries a concrete tag.
func (ix *Index) HasTag(id int, tag string) bool {
	return ix.tags[id][tag]
}

// Name returns a display name for an id, falling back to a placeholder.
func (ix *Index) Name(id int) string {
	if h, ok := ix.byID[id]; ok {
		return h.LocalizedName
	}
	return fmt.Sprintf("Hero #%d", id)
}

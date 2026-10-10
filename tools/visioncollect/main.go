// Command visioncollect captures labeled screenshots of real drafts.
//
//	go run ./tools/visioncollect --port 3000
//
// It runs its own GSI listener and, whenever Dota reports hero selection or
// strategy time, saves a screenshot beside a JSON sidecar of what GSI said at
// that moment. Play normally; Ctrl+C stops it.
//
// The labels are what the recogniser cannot give itself. At strategy time the
// minimap block names all ten heroes, and in Captain's Mode the draft block
// names each side's picks and bans outright. Even where only the set of ten is
// known, ten crops matched to ten known heroes is an assignment problem rather
// than an open-ended guess, which is enough to build templates from.
//
// The sidecar cannot always say which side a hero is on: the minimap entry's
// team field belongs to the widget, not the hero. The draft block, when
// present, can.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"counterdraft/internal/config"
	"counterdraft/internal/gsi"
	"counterdraft/internal/heroes"
	"counterdraft/internal/vision"
)

const (
	heroSelection = "DOTA_GAMERULES_STATE_HERO_SELECTION"
	strategyTime  = "DOTA_GAMERULES_STATE_STRATEGY_TIME"
	heroPrefix    = "npc_dota_hero_"
)

// label is the sidecar written next to each capture.
type label struct {
	CapturedAt    string    `json:"captured_at"`
	GameState     string    `json:"game_state"`
	MatchID       string    `json:"match_id"`
	OwnTeam       string    `json:"own_team"`
	OwnHeroID     int       `json:"own_hero_id"`
	OwnHeroName   string    `json:"own_hero_name"`
	MinimapHeroes []string  `json:"minimap_heroes"`
	Team2         teamLabel `json:"team2"`
	Team3         teamLabel `json:"team3"`
}

type teamLabel struct {
	Picks       []int    `json:"picks"`
	PickClasses []string `json:"pick_classes"`
	Bans        []int    `json:"bans"`
	BanClasses  []string `json:"ban_classes"`
}

func main() {
	port := flag.Int("port", 3000, "GSI listen port")
	out := flag.String("out", vision.CaptureDir(), "directory for captures and sidecars")
	every := flag.Float64("every", 4.0, "seconds between captures while a draft is on screen")
	install := flag.Bool("install", false, "write the Dota GSI config pointing at this listener")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "cannot create %s: %v\n", *out, err)
		os.Exit(1)
	}

	ix, err := heroes.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load heroes: %v\n", err)
		os.Exit(1)
	}

	var mu sync.Mutex
	var latest *gsi.Payload

	srv := gsi.New("127.0.0.1", *port, func(p gsi.Payload) {
		cp := p
		mu.Lock()
		latest = &cp
		mu.Unlock()
	})
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot listen on port %d: %v\n", *port, err)
		fmt.Fprintln(os.Stderr, "the overlay may already be running - close it first")
		os.Exit(1)
	}
	defer srv.Close()

	if *install {
		path, err := config.Install(srv.URL(), "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "install config: %v\n", err)
		} else {
			fmt.Printf("installed GSI config: %s\n", path)
		}
	}

	fmt.Printf("listening on %s\n", srv.URL())
	fmt.Printf("saving captures to %s\n", *out)
	fmt.Println("play normally; captures happen during hero selection and strategy time. Ctrl+C to stop.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	interval := time.Duration(*every * float64(time.Second))
	last := time.Time{}
	count := 0

	for {
		select {
		case <-stop:
			fmt.Printf("\nstopped; %d captures written to %s\n", count, *out)
			return
		case <-ticker.C:
			mu.Lock()
			p := latest
			mu.Unlock()
			if p == nil {
				continue
			}
			state := p.Map.GameState
			if state != heroSelection && state != strategyTime {
				continue
			}
			if !last.IsZero() && time.Since(last) < interval {
				continue
			}
			img := vision.GrabWithTimeout(3 * time.Second)
			if img == nil {
				continue
			}
			last = time.Now()
			if err := write(*out, state, p, ix, img); err != nil {
				fmt.Printf("  save failed: %v\n", err)
				continue
			}
			count++
		}
	}
}

func write(dir, state string, p *gsi.Payload, ix *heroes.Index, img image.Image) error {
	stamp := time.Now().Format("20060102-150405")
	short := strings.ToLower(state[strings.LastIndex(state, "_")+1:])
	base := filepath.Join(dir, fmt.Sprintf("draft-%s-%s", short, stamp))

	raw, err := os.Create(base + ".png")
	if err != nil {
		return err
	}
	if err := png.Encode(raw, img); err != nil {
		raw.Close()
		return err
	}
	raw.Close()

	heroName := ""
	if p.Hero.ID > 0 {
		if h, ok := ix.ByID(p.Hero.ID); ok {
			heroName = h.Name
		}
	}
	lbl := label{
		CapturedAt:    time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		GameState:     state,
		MatchID:       p.Map.MatchID,
		OwnTeam:       p.Player.TeamName,
		OwnHeroID:     p.Hero.ID,
		OwnHeroName:   heroName,
		MinimapHeroes: minimapHeroes(p),
		Team2:         teamLabelOf(p.Draft.Team2),
		Team3:         teamLabelOf(p.Draft.Team3),
	}
	data, err := json.MarshalIndent(lbl, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(base+".json", data, 0o644); err != nil {
		return err
	}

	label := "no hero labels yet"
	if len(lbl.MinimapHeroes) > 0 {
		label = fmt.Sprintf("%d heroes named", len(lbl.MinimapHeroes))
	} else if n := len(lbl.Team2.Picks) + len(lbl.Team3.Picks); n > 0 {
		label = fmt.Sprintf("%d picks in the draft block", n)
	}
	fmt.Printf("  saved %s  (%s)\n", filepath.Base(base+".png"), label)
	return nil
}

func minimapHeroes(p *gsi.Payload) []string {
	set := map[string]bool{}
	for _, e := range p.Minimap {
		if strings.HasPrefix(e.UnitName, heroPrefix) {
			set[e.UnitName] = true
		}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func teamLabelOf(t gsi.TeamDraft) teamLabel {
	return teamLabel{
		Picks:       t.Picks(),
		PickClasses: t.PickClasses(),
		Bans:        t.Bans(),
		BanClasses:  t.BanClasses(),
	}
}

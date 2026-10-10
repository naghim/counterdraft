// Package gsi implements the server side of Dota 2's official Game State
// Integration (GSI). The game POSTs JSON snapshots to a local HTTP endpoint
// declared in a .cfg file; no process injection or memory reading is involved.
package gsi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Payload is the subset of the GSI document this app consumes.
type Payload struct {
	Provider struct {
		Name  string `json:"name"`
		AppID int    `json:"appid"`
	} `json:"provider"`
	Map struct {
		Name      string `json:"name"`
		MatchID   string `json:"matchid"`
		GameState string `json:"game_state"`
	} `json:"map"`
	Player struct {
		SteamID  string `json:"steamid"`
		Name     string `json:"name"`
		Team     int    `json:"team"`
		TeamName string `json:"team_name"`
	} `json:"player"`
	Hero struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"hero"`
	Draft   Draft                     `json:"draft"`
	Minimap map[string]MinimapElement `json:"minimap"`
}

// MinimapElement is one unit drawn on the minimap. Only units whose unitname
// starts with "npc_dota_hero_" are heroes; team is "radiant" or "dire".
type MinimapElement struct {
	UnitName string `json:"unitname"`
	Team     string `json:"team"`
}

// Draft holds both teams' picks and bans as reported during the drafting phase.
type Draft struct {
	ActiveTeam int       `json:"activeteam"`
	Pick       bool      `json:"pick"`
	Team2      TeamDraft `json:"team2"`
	Team3      TeamDraft `json:"team3"`
}

// TeamDraft is a team's picks/bans. Team 2 is Radiant, team 3 is Dire.
// Dota reports each slot both as a numeric hero id (pick0_id) and as an
// internal hero name (pick0_class); different modes/game versions populate
// one or the other, so both are parsed.
type TeamDraft struct {
	Pick0ID int `json:"pick0_id"`
	Pick1ID int `json:"pick1_id"`
	Pick2ID int `json:"pick2_id"`
	Pick3ID int `json:"pick3_id"`
	Pick4ID int `json:"pick4_id"`
	Ban0ID  int `json:"ban0_id"`
	Ban1ID  int `json:"ban1_id"`
	Ban2ID  int `json:"ban2_id"`
	Ban3ID  int `json:"ban3_id"`
	Ban4ID  int `json:"ban4_id"`
	Ban5ID  int `json:"ban5_id"`

	Pick0Class string `json:"pick0_class"`
	Pick1Class string `json:"pick1_class"`
	Pick2Class string `json:"pick2_class"`
	Pick3Class string `json:"pick3_class"`
	Pick4Class string `json:"pick4_class"`
	Ban0Class  string `json:"ban0_class"`
	Ban1Class  string `json:"ban1_class"`
	Ban2Class  string `json:"ban2_class"`
	Ban3Class  string `json:"ban3_class"`
	Ban4Class  string `json:"ban4_class"`
	Ban5Class  string `json:"ban5_class"`
}

// Picks returns the non-empty pick ids for the team.
func (t TeamDraft) Picks() []int {
	return nonZero(t.Pick0ID, t.Pick1ID, t.Pick2ID, t.Pick3ID, t.Pick4ID)
}

// PickClasses returns the non-empty pick hero names for the team, e.g.
// "npc_dota_hero_axe".
func (t TeamDraft) PickClasses() []string {
	return nonEmpty(t.Pick0Class, t.Pick1Class, t.Pick2Class, t.Pick3Class, t.Pick4Class)
}

// Bans returns the non-empty ban ids for the team. Dota reports bans even in
// all-pick modes, which is what lets the screen reader rule banned heroes out.
func (t TeamDraft) Bans() []int {
	return nonZero(t.Ban0ID, t.Ban1ID, t.Ban2ID, t.Ban3ID, t.Ban4ID, t.Ban5ID)
}

// BanClasses returns the non-empty ban hero names for the team.
func (t TeamDraft) BanClasses() []string {
	return nonEmpty(t.Ban0Class, t.Ban1Class, t.Ban2Class, t.Ban3Class, t.Ban4Class, t.Ban5Class)
}

func nonZero(ids ...int) []int {
	out := ids[:0:0]
	for _, id := range ids {
		if id > 0 {
			out = append(out, id)
		}
	}
	return out
}

func nonEmpty(names ...string) []string {
	out := names[:0:0]
	for _, n := range names {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// MinimapHeroes returns the unique hero unit names seen on the minimap for
// each team. During a normal (non-draft) match this is the only place GSI
// exposes the other players' heroes: Dota sends a "players" block only to
// spectators, and the draft block only in draft modes such as Captain's Mode.
// Team 2 is Radiant, team 3 is Dire.
func (p Payload) MinimapHeroes() (team2, team3 []string) {
	seen2 := map[string]bool{}
	seen3 := map[string]bool{}
	for _, e := range p.Minimap {
		if !strings.HasPrefix(e.UnitName, "npc_dota_hero_") {
			continue
		}
		switch strings.ToLower(e.Team) {
		case "radiant", "team2":
			if !seen2[e.UnitName] {
				seen2[e.UnitName] = true
				team2 = append(team2, e.UnitName)
			}
		case "dire", "team3":
			if !seen3[e.UnitName] {
				seen3[e.UnitName] = true
				team3 = append(team3, e.UnitName)
			}
		}
	}
	return team2, team3
}

// OwnTeam resolves which team the local player is on. Dota reports the local
// team as a string in player.team_name ("radiant"/"dire"); the numeric
// player.team field is only present in some payload variants, so it is used
// first when available.
func (p Payload) OwnTeam() int {
	if p.Player.Team == 2 || p.Player.Team == 3 {
		return p.Player.Team
	}
	switch strings.ToLower(strings.TrimSpace(p.Player.TeamName)) {
	case "radiant", "team2":
		return 2
	case "dire", "team3":
		return 3
	}
	return 0
}

// Handler is invoked for every valid snapshot the game sends.
type Handler func(Payload)

// Server receives GSI snapshots on a local port.
type Server struct {
	addr    string
	handler Handler
	http    *http.Server
	ln      net.Listener
}

// New creates a GSI server bound to host:port.
func New(host string, port int, handler Handler) *Server {
	return &Server{
		addr:    net.JoinHostPort(host, fmt.Sprint(port)),
		handler: handler,
	}
}

// Addr returns the configured listen address.
func (s *Server) Addr() string { return s.addr }

// URL returns the URI the GSI config should point at.
func (s *Server) URL() string { return "http://" + s.addr + "/" }

// Start begins listening. It returns once the listener is bound, or an error.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("gsi: listen %s: %w", s.addr, err)
	}
	s.ln = ln
	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("gsi: server stopped: %v", err)
		}
	}()
	return nil
}

// Close shuts the server down.
func (s *Server) Close() error {
	if s.http == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

// debugDump writes the raw GSI body to disk and logs a compact summary when the
// COUNTERDRAFT_GSI_DUMP environment variable is set. It is a diagnostic aid and
// does nothing by default.
func debugDump(body []byte, p Payload) {
	target := os.Getenv("COUNTERDRAFT_GSI_DUMP")
	if target == "" {
		return
	}
	if target == "1" {
		target = filepath.Join(os.TempDir(), "counterdraft-gsi-last.json")
	}
	if err := os.WriteFile(target, body, 0o644); err != nil {
		log.Printf("gsi: dump %s: %v", target, err)
		return
	}
	// Keep one file per game state so short-lived states (e.g. hero selection)
	// are not overwritten by the steady stream of in-progress snapshots.
	state := p.Map.GameState
	if state == "" {
		state = "unknown"
	}
	perState := filepath.Join(filepath.Dir(target), "counterdraft-gsi-"+state+".json")
	_ = os.WriteFile(perState, body, 0o644)

	t2, t3 := p.MinimapHeroes()
	log.Printf("gsi: dump %s state=%q player.team=%d player.team_name=%q hero.id=%d minimap=%d minimapHeroes(2=%d,3=%d) draft.team2(id=%d,class=%d) draft.team3(id=%d,class=%d)",
		target, state, p.Player.Team, p.Player.TeamName, p.Hero.ID,
		len(p.Minimap), len(t2), len(t3),
		len(p.Draft.Team2.Picks()), len(p.Draft.Team2.PickClasses()),
		len(p.Draft.Team3.Picks()), len(p.Draft.Team3.PickClasses()))
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "counterdraft GSI listener OK\n")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	var p Payload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	debugDump(body, p)
	if s.handler != nil {
		s.handler(p)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "OK")
}

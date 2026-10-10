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
		SteamID string `json:"steamid"`
		Name    string `json:"name"`
		Team    int    `json:"team"`
	} `json:"player"`
	Hero struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"hero"`
	Draft   Draft                  `json:"draft"`
	Players map[string]PlayerState `json:"players"`
}

// PlayerState is one entry of the "players" object.
type PlayerState struct {
	SteamID   string `json:"steamid"`
	Name      string `json:"name"`
	Team      int    `json:"team"`
	HeroID    int    `json:"hero_id"`
	PickOrder int    `json:"pick_order"`
}

// Draft holds both teams' picks and bans as reported during the drafting phase.
type Draft struct {
	ActiveTeam int       `json:"activeteam"`
	Pick       bool      `json:"pick"`
	Team2      TeamDraft `json:"team2"`
	Team3      TeamDraft `json:"team3"`
}

// TeamDraft is a team's picks/bans. Team 2 is Radiant, team 3 is Dire.
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
}

// Picks returns the non-empty pick ids for the team.
func (t TeamDraft) Picks() []int {
	return nonZero(t.Pick0ID, t.Pick1ID, t.Pick2ID, t.Pick3ID, t.Pick4ID)
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

// PlayerTeams returns hero ids grouped by team (2 and 3) from the players map.
func (p Payload) PlayerTeams() (team2, team3 []int) {
	for _, ps := range p.Players {
		if ps.HeroID <= 0 {
			continue
		}
		switch ps.Team {
		case 2:
			team2 = append(team2, ps.HeroID)
		case 3:
			team3 = append(team3, ps.HeroID)
		}
	}
	return team2, team3
}

// OwnTeam resolves which team the local player is on, falling back to matching
// the active hero if the player block is empty.
func (p Payload) OwnTeam() int {
	if p.Player.Team == 2 || p.Player.Team == 3 {
		return p.Player.Team
	}
	for _, ps := range p.Players {
		if p.Hero.ID > 0 && ps.HeroID == p.Hero.ID && (ps.Team == 2 || ps.Team == 3) {
			return ps.Team
		}
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
	if s.handler != nil {
		s.handler(p)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "OK")
}

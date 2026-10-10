package gsi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerRoundTrip(t *testing.T) {
	got := make(chan Payload, 1)
	s := New("127.0.0.1", 0, func(p Payload) { got <- p })
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close()

	url := "http://" + s.ln.Addr().String() + "/"
	resp, err := http.Post(url, "application/json", strings.NewReader(`{"player":{"team":2}}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	select {
	case p := <-got:
		if p.Player.Team != 2 {
			t.Errorf("team = %d, want 2", p.Player.Team)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}
}

func post(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handle(rr, req)
	return rr
}

func TestDraftPicksAndOwnTeam(t *testing.T) {
	var got Payload
	s := New("127.0.0.1", 0, func(p Payload) { got = p })

	body := `{
      "player": {"team": 2},
      "hero": {"id": 44},
      "draft": {
        "activeteam": 2,
        "pick": true,
        "team2": {"pick0_id": 1},
        "team3": {"pick0_id": 44, "pick1_id": 32}
      }
    }`
	rr := post(t, s, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if team := got.OwnTeam(); team != 2 {
		t.Errorf("OwnTeam = %d, want 2", team)
	}
	if picks := got.Draft.Team3.Picks(); len(picks) != 2 || picks[0] != 44 || picks[1] != 32 {
		t.Errorf("team3 picks = %v, want [44 32]", picks)
	}
	if picks := got.Draft.Team2.Picks(); len(picks) != 1 || picks[0] != 1 {
		t.Errorf("team2 picks = %v, want [1]", picks)
	}
}

func TestOwnTeamFallsBackToActiveHero(t *testing.T) {
	var got Payload
	s := New("127.0.0.1", 0, func(p Payload) { got = p })

	// No player.team; the local hero is on team 3.
	body := `{
      "hero": {"id": 32},
      "players": {
        "0": {"team": 3, "hero_id": 32},
        "1": {"team": 2, "hero_id": 1}
      }
    }`
	post(t, s, body)
	if team := got.OwnTeam(); team != 3 {
		t.Errorf("OwnTeam = %d, want 3", team)
	}
	t2, t3 := got.PlayerTeams()
	if len(t2) != 1 || t2[0] != 1 || len(t3) != 1 || t3[0] != 32 {
		t.Errorf("player teams = %v / %v, want [1] / [32]", t2, t3)
	}
}

func TestRejectsBadJSON(t *testing.T) {
	s := New("127.0.0.1", 0, func(Payload) {})
	rr := post(t, s, "{nope")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

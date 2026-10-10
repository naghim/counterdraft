package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"counterdraft/internal/config"
	"counterdraft/internal/gsi"
	"counterdraft/internal/heroes"
	"counterdraft/internal/rules"
	"counterdraft/internal/vision"
)

// Valid values for the side argument of the pick-editing methods.
const (
	sideEnemy = "enemy"
	sideOwn   = "own"
)

// HeroRef is a minimal hero reference sent to the frontend.
type HeroRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Snapshot is the complete UI state: the current draft plus recommendations.
type Snapshot struct {
	Status          string                 `json:"status"`
	GameState       string                 `json:"gameState"`
	Own             []HeroRef              `json:"own"`
	Enemy           []HeroRef              `json:"enemy"`
	Recommendations []rules.Recommendation `json:"recommendations"`
	ServerURL       string                 `json:"serverUrl"`
	ConfigPath      string                 `json:"configPath"`
	ConfigInstalled bool                   `json:"configInstalled"`
	Candidates      []string               `json:"candidates"`
	ClickThrough    bool                   `json:"clickThrough"`
	ClickThroughOK  bool                   `json:"clickThroughSupported"`
	EnemyManual     bool                   `json:"enemyManual"`
	OwnManual       bool                   `json:"ownManual"`
	VisionEnabled   bool                   `json:"visionEnabled"`
	VisionAvailable bool                   `json:"visionAvailable"`
	VisionStatus    string                 `json:"visionStatus"`
}

// App is the Wails-bound service. It holds references to the application and
// its overlay window so it can emit events and toggle click-through.
type App struct {
	app    *application.App
	window *application.WebviewWindow

	ix     *heroes.Index
	engine *rules.Engine
	server *gsi.Server

	mu        sync.Mutex
	base      Snapshot
	clickThru bool
	gameState string

	// Detection results as reported by GSI, before any manual correction.
	detectedEnemy []int
	detectedOwn   []int
	// Manual overrides; when the corresponding flag is set these take priority
	// over detection so a mis-read draft can be fixed by hand.
	manualEnemy []int
	manualOwn   []int
	enemyManual bool
	ownManual   bool

	// Anchors the screen reader needs: our own hero and which half of the top
	// bar is ours. Either settles the sides on its own.
	ownHeroID int
	ownSide   string
	// Bans reported by GSI, used to narrow the recogniser's field.
	bannedIDs []int
	matchID   string

	// Vision (screen reading), used when GSI carries no draft data.
	visionEnabled   bool
	visionAvailable bool
	visionStatus    string
	// visionReader is owned by the poll goroutine alone; other goroutines only
	// set the two flags below, which that goroutine drains.
	visionReader   *vision.DraftReader
	visionEnemy    []int
	visionOwn      []int
	visionAdded    map[int]string
	visionMissing  map[int]int
	visionRejected map[int]bool
	visionUnread   int
	visionRebuild  bool
	visionReset    bool
	visionCancel   context.CancelFunc
}

// NewApp creates the service.
func NewApp(app *application.App, window *application.WebviewWindow) *App {
	s := loadSettings()
	return &App{
		app:            app,
		window:         window,
		visionEnabled:  s.Vision,
		visionAdded:    map[int]string{},
		visionMissing:  map[int]int{},
		visionRejected: map[int]bool{},
	}
}

// ServiceStartup loads data and brings up the GSI listener.
func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	ix, err := heroes.Load()
	if err != nil {
		log.Printf("counterdraft: load heroes: %v", err)
		return nil
	}
	a.ix = ix
	a.engine = rules.New(ix)

	_, srv, err := startServer(a.handleGSI)
	if err != nil {
		log.Printf("counterdraft: start gsi server: %v", err)
		a.base.Status = fmt.Sprintf("Could not start GSI listener: %v", err)
		return nil
	}
	a.server = srv

	a.base = Snapshot{
		Status:         "Listening. Waiting for Dota 2...",
		ServerURL:      srv.URL(),
		Candidates:     config.Candidates(),
		ClickThroughOK: true,
		VisionEnabled:  a.visionEnabled,
		VisionStatus:   "Preparing screen reader...",
	}

	if path, err := config.Install(srv.URL(), ""); err != nil {
		a.base.Status = "GSI listener up, but no Dota 2 install found. Set the path manually."
		log.Printf("counterdraft: install config: %v", err)
	} else {
		a.base.ConfigPath = path
		a.base.ConfigInstalled = true
	}

	// Screen reading, in the background: the first run downloads reference art.
	go a.ensurePortraits()
	ctx, cancel := context.WithCancel(context.Background())
	a.visionCancel = cancel
	go a.visionLoop(ctx)
	return nil
}

// ServiceShutdown stops the listener and the screen-reading loop.
func (a *App) ServiceShutdown() error {
	if a.visionCancel != nil {
		a.visionCancel()
		a.visionCancel = nil
	}
	if a.server != nil {
		_ = a.server.Close()
	}
	return nil
}

// GetState returns the current snapshot for the UI to render on load.
func (a *App) GetState() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.base
}

// InstallConfig writes (or rewrites) the GSI cfg using the auto-detected Dota
// directory and returns the written path.
func (a *App) InstallConfig() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server == nil {
		return "", fmt.Errorf("GSI listener is not running")
	}
	path, err := config.Install(a.server.URL(), "")
	if err != nil {
		return "", err
	}
	a.base.ConfigPath = path
	a.base.ConfigInstalled = true
	return path, nil
}

// SetDotaPath installs the GSI cfg into a user-supplied cfg/game directory.
func (a *App) SetDotaPath(path string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server == nil {
		return "", fmt.Errorf("GSI listener is not running")
	}
	written, err := config.Install(a.server.URL(), path)
	if err != nil {
		return "", err
	}
	a.base.ConfigPath = written
	a.base.ConfigInstalled = true
	return written, nil
}

// ConfigCandidates lists auto-detected GSI cfg directories for display.
func (a *App) ConfigCandidates() []string { return config.Candidates() }

// SetClickThrough enables or disables mouse-input passthrough on the overlay.
func (a *App) SetClickThrough(enabled bool) error {
	if a.window == nil {
		return fmt.Errorf("overlay window is not available")
	}
	a.window.SetIgnoreMouseEvents(enabled)
	a.mu.Lock()
	a.clickThru = enabled
	a.base.ClickThrough = enabled
	a.mu.Unlock()
	return nil
}

// Heroes returns every hero for the pick editor, sorted by display name.
func (a *App) Heroes() []HeroRef {
	if a.ix == nil {
		return nil
	}
	all := a.ix.All()
	refs := make([]HeroRef, 0, len(all))
	for _, h := range all {
		refs = append(refs, HeroRef{ID: h.ID, Name: h.LocalizedName})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs
}

// AddPick adds a hero to one side ("enemy" or "own") and switches that side to
// manual picks so later GSI updates no longer overwrite it.
func (a *App) AddPick(side string, heroID int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, ok := a.ix.ByID(heroID); !ok {
		return fmt.Errorf("unknown hero id %d", heroID)
	}
	switch side {
	case sideEnemy:
		a.manualEnemy = addUnique(a.beginEnemyEditLocked(), heroID)
	case sideOwn:
		a.manualOwn = addUnique(a.beginOwnEditLocked(), heroID)
	default:
		return fmt.Errorf("unknown side %q", side)
	}
	a.recomputeLocked()
	return nil
}

// RemovePick removes a hero from one side, switching that side to manual picks
// if it was still tracking detection.
func (a *App) RemovePick(side string, heroID int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch side {
	case sideEnemy:
		a.manualEnemy = removeID(a.beginEnemyEditLocked(), heroID)
	case sideOwn:
		a.manualOwn = removeID(a.beginOwnEditLocked(), heroID)
	default:
		return fmt.Errorf("unknown side %q", side)
	}
	a.recomputeLocked()
	return nil
}

// ResetSide discards manual edits for one side and resumes following detection.
func (a *App) ResetSide(side string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch side {
	case sideEnemy:
		a.enemyManual = false
		a.manualEnemy = nil
	case sideOwn:
		a.ownManual = false
		a.manualOwn = nil
	default:
		return fmt.Errorf("unknown side %q", side)
	}
	a.recomputeLocked()
	return nil
}

// beginEnemyEditLocked snapshots the detected picks into the manual list on the
// first edit, so manual mode starts from what was detected.
func (a *App) beginEnemyEditLocked() []int {
	if !a.enemyManual {
		a.manualEnemy = a.detectedEnemyLocked()
		a.enemyManual = true
	}
	return a.manualEnemy
}

func (a *App) beginOwnEditLocked() []int {
	if !a.ownManual {
		a.manualOwn = a.detectedOwnLocked()
		a.ownManual = true
	}
	return a.manualOwn
}

func addUnique(ids []int, id int) []int {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

func removeID(ids []int, id int) []int {
	out := ids[:0]
	for _, existing := range ids {
		if existing != id {
			out = append(out, existing)
		}
	}
	return out
}

// handleGSI is called for every snapshot the game sends. It refreshes the
// detected picks and recomputes recommendations, honouring manual overrides.
func (a *App) handleGSI(p gsi.Payload) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// A new match id means a new game: drop what vision believed about the
	// previous bar. The account id would survive a reset; vision state must not.
	if p.Map.MatchID != "" && p.Map.MatchID != a.matchID {
		a.matchID = p.Map.MatchID
		a.resetVisionLocked()
	}

	a.gameState = p.Map.GameState
	// Sticky: a payload that happens to omit the hero late in a game must not
	// erase what an earlier payload established.
	if p.Hero.ID > 0 {
		a.ownHeroID = p.Hero.ID
	}
	// Radiant is drawn on the left of the top bar, Dire on the right - a fixed
	// property of the HUD. GSI reports the team from the moment a draft starts,
	// which is what makes screen reading useful in all-pick.
	switch p.OwnTeam() {
	case 2:
		a.ownSide = "left"
	case 3:
		a.ownSide = "right"
	}
	a.bannedIDs = banIDs(p, a.ix)

	ownTeam := p.OwnTeam()
	if ownTeam == 0 {
		a.detectedEnemy = nil
		a.detectedOwn = nil
	} else {
		enemyTeam := 3
		if ownTeam == 3 {
			enemyTeam = 2
		}
		a.detectedOwn = picksForTeam(p, ownTeam, a.ix)
		a.detectedEnemy = picksForTeam(p, enemyTeam, a.ix)
	}
	a.recomputeLocked()
}

// detectedEnemyLocked is what GSI reported unioned with what the screen reader
// read, before any manual correction.
func (a *App) detectedEnemyLocked() []int { return unionIDs(a.detectedEnemy, a.visionEnemy) }

func (a *App) detectedOwnLocked() []int { return unionIDs(a.detectedOwn, a.visionOwn) }

// effectiveLocked returns the picks to use, applying manual overrides.
func (a *App) effectiveLocked() (enemy, own []int) {
	enemy, own = a.detectedEnemyLocked(), a.detectedOwnLocked()
	if a.enemyManual {
		enemy = a.manualEnemy
	}
	if a.ownManual {
		own = a.manualOwn
	}
	return enemy, own
}

// recomputeLocked rebuilds the snapshot and emits it. Callers must hold a.mu.
func (a *App) recomputeLocked() {
	snap := a.base
	snap.GameState = a.gameState

	enemyPicks, ownPicks := a.effectiveLocked()
	snap.Own = a.refs(ownPicks)
	snap.Enemy = a.refs(enemyPicks)
	snap.Recommendations = a.engine.Recommend(enemyPicks, ownPicks, 8)
	snap.EnemyManual = a.enemyManual
	snap.OwnManual = a.ownManual
	snap.VisionEnabled = a.visionEnabled
	snap.VisionAvailable = a.visionAvailable
	snap.VisionStatus = a.visionStatus

	manual := ""
	if a.enemyManual || a.ownManual {
		manual = " (manual picks)"
	}
	switch {
	case len(enemyPicks) == 0 && len(ownPicks) == 0:
		snap.Status = "In match. No heroes visible yet; picks show up as they are made." + manual
	case len(enemyPicks) == 0:
		snap.Status = fmt.Sprintf("%d ally pick(s). No enemy picks yet.%s", len(ownPicks), manual)
	default:
		snap.Status = fmt.Sprintf("%d enemy pick(s), %d ally pick(s).%s",
			len(enemyPicks), len(ownPicks), manual)
	}

	a.base = snap
	a.emit(snap)
}

func (a *App) emit(snap Snapshot) {
	if a.app == nil {
		return
	}
	a.app.Event.Emit("draft:update", snap)
}

func (a *App) refs(ids []int) []HeroRef {
	out := make([]HeroRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, HeroRef{ID: id, Name: a.ix.Name(id)})
	}
	return out
}

// startServer binds the first free port in the 4000-4010 range.
func startServer(handler gsi.Handler) (int, *gsi.Server, error) {
	var lastErr error
	for port := 4000; port <= 4010; port++ {
		srv := gsi.New("127.0.0.1", port, handler)
		if err := srv.Start(); err != nil {
			lastErr = err
			continue
		}
		return port, srv, nil
	}
	return 0, nil, lastErr
}

// picksForTeam unions every source GSI offers for one team's heroes:
//   - the draft block (Captain's Mode and other draft modes), reported as both
//     numeric ids and hero class names;
//   - the local player's own hero, always present via the hero block;
//   - the minimap, the only place GSI exposes other players' heroes in
//     non-draft modes such as All Pick and New Player mode.
func picksForTeam(p gsi.Payload, team int, ix *heroes.Index) []int {
	var d gsi.TeamDraft
	if team == 2 {
		d = p.Draft.Team2
	} else {
		d = p.Draft.Team3
	}
	seen := map[int]bool{}
	var out []int
	add := func(ids ...int) {
		for _, id := range ids {
			if id > 0 && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	add(d.Picks()...)
	for _, name := range d.PickClasses() {
		if h, ok := ix.ByNpcName(name); ok {
			add(h.ID)
		}
	}
	if team == p.OwnTeam() && p.Hero.ID > 0 {
		add(p.Hero.ID)
	}
	m2, m3 := p.MinimapHeroes()
	names := m3
	if team == 2 {
		names = m2
	}
	for _, name := range names {
		if h, ok := ix.ByNpcName(name); ok {
			add(h.ID)
		}
	}
	return out
}

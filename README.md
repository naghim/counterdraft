# counterdraft

A lightweight, always-on-top desktop overlay that helps you draft in Dota 2. It watches the enemy and ally picks and suggests heroes that counter the enemy lineup and synergize with your own team, in real time.

Built with [Wails](https://wails.io) (Go backend + a tiny vanilla-JS frontend), so it ships as a single native binary with no browser or runtime dependency.

## About

This is a helper tool that assists in picking heroes in the popular MOBA called Dota 2. It scans the enemy team picks, offers counterpicks, while also simultaneously monitoring ally picks offering heroes that synergizes well with the already selected heroes.

### Will they ban me for using this?

**No, they shouldn't.** Hero picks are detected using Valve's own Game State Integration system, which violates no Valve policy.
You can also manually enter the heroes, if you want to feel absolutely safe. Then it just runs as any other completely isolated program that won't bother Steam/Dota 2 (think of Google Chrome, or Discord).

When GSI does not report the draft (all-pick modes never send the enemy picks), counterdraft can additionally read the top bar **off your own screen**, exactly as OBS or any recorder does. That path never touches Dota's process, its memory or its files, and it automates no input. It is off by one click in the overlay if you would rather it stayed off.

## Features

- **Live draft detection** via Dota 2's official Game State Integration (GSI) - the game pushes its own state to a local listener.
- **Screen reading fallback** for when GSI carries no draft data (all-pick): the enemy portraits on the top bar are matched against Valve's own hero art, in memory, during hero selection.
- **Counter suggestions** based on the enemy team's aggregate mechanical traits (evasion, invisibility, heavy healing, summons, etc.).
- **Synergy suggestions** that fill gaps in your own lineup (no hard disable, no wave clear, no sustain, greedy late-game comp, ...).
- **Manual pick editing** with a hero picker, plus _reset_ to fall back to detection - fix a mis-read draft, or play fully offline.
- **Explainable recommendations**: every suggestion lists the reasons it scored, and candidates are de-duplicated against already-picked heroes.
- **Transparent, frameless, click-through overlay** that stays on top of the game. Toggle mouse passthrough so clicks reach Dota 2 directly, or drag the window by its header to reposition it.
- **Automatic GSI config install** - detects your Steam/Dota 2 install (including extra Steam libraries) and writes the `.cfg` for you.

## How it works

counterdraft relies on Valve's **Game State Integration**, an official, documented feature. During a match the game POSTs a JSON snapshot of visible state (map, player, team, draft picks and bans) to a local HTTP endpoint. counterdraft runs that endpoint, parses the picks, and recomputes recommendations on every update.

Nothing reads the game's memory, injects code, or hooks the process. The only file counterdraft touches is the GSI config it writes:

```
<Steam>/steamapps/common/dota 2 beta/game/dota/cfg/gamestate_integration/gamestate_integration_counterdraft.cfg
```

When the draft block is empty - as it always is in all-pick - counterdraft falls back to reading the top bar off the screen (`internal/vision`). It captures the primary display, crops the ten portrait slots using layout fractions measured from a real 1920x1080 capture, and matches each crop against reference hero art by normalised cross-correlation. The reference art is Valve's own: 127 defaults downloaded from the CDN, plus alternate portraits (arcanas, immortal sets, personas) decoded out of the installed game's VPK archive. A slot is only accepted when a match clears both a similarity threshold and a clear margin over the runner-up, and when the same hero wins the slot across several frames - a wrong hero silently poisons every recommendation, so the reader refuses rather than guesses.

Recommendations come from a **hand-authored mechanical tag table** (`internal/heroes/heroes.json`), not from win-rate statistics. Each hero is tagged with what its kit does (e.g. `stun`, `invis`, `illusions`, `anti_heal`, `summons`). The engine (`internal/rules`) matches those tags against rules such as _"enemy relies on evasion → recommend magic/pure damage or Break"_, weighting each hit and stopping at the top candidates.

## Getting started

### Prerequisites

- [Go](https://go.dev/dl/) 1.25+
- [Node.js](https://nodejs.org/) (for the frontend build)
- The [Wails v3 CLI](https://v3.wails.io/): `go install github.com/wailsapp/wails/v3/cmd/wails3@latest`
- Linux only: the WebKit2GTK 4.1 development headers (see the Wails docs for your distro)

### Run in development

```sh
wails3 dev
```

This starts the Vite dev server with hot reload and launches the overlay. It binds the first free port in the `4000–4010` range for GSI and installs the config on startup.

### Build a release binary

```sh
wails3 build
```

The binary is written to `bin/` (or to `bin/<os>_<arch>/` when cross-compiling).

## Usage

1. Launch the app. It starts the GSI listener and, if it finds your Dota 2 install, writes the GSI config automatically. The status line and footer show what it detected.
2. If detection fails, click **Re-detect**, or use **Install GSI config**.
3. Start a match. Picks appear as the game reports them and the **Recommendations** list updates live.
4. To correct or force a pick, use **+** next to a team to open the hero picker. Adding or removing a pick switches that team to _manual_ mode; click **reset** to resume following detection.
5. The **lock** button toggles mouse click-through so the overlay ignores input and clicks reach the game. With click-through off, drag the window by its header.

> If you supplied a custom Dota path, the GSI config is written into it; otherwise counterdraft uses the first detected install.

## Project layout

```
.
├── main.go                  # Wails bootstrap: app/window options, asset embedding
├── app.go                   # Bound service (state, picks, snapshot, click-through)
├── internal/
│   ├── config/              # GSI cfg rendering + Steam/Dota install detection
│   ├── gsi/                 # GSI HTTP listener and payload parsing
│   ├── heroes/              # Embedded hero table + tag index
│   ├── rules/               # Tag-based counter/synergy recommendation engine
│   └── vision/              # Screen capture, portrait recognition, VPK/vtex reading
├── frontend/                # Vite + vanilla JS overlay UI
│   └── bindings/            # Generated JS bindings (wails3 generate bindings)
├── build/                   # Wails v3 build assets and per-OS Taskfiles
├── Taskfile.yml             # Wails v3 task entry points (build, dev, package)
└── tools/gen_heroes.py      # Regenerates the embedded hero table
```

## Regenerating hero data

`internal/heroes/heroes.json` is generated from the OpenDota hero list merged with the hand-authored tag table in `tools/gen_heroes.py`:

```sh
python3 tools/gen_heroes.py > internal/heroes/heroes.json
```

Keep the tag vocabulary in sync with the rule definitions in `internal/rules/rules.go`. When adding a hero, give it mechanical tags and merge it into the `TAGS` map.

## Calibrating screen reading

Screen reading is template matching, so a missed slot is almost always one of two things: **geometry** (the portrait boxes do not sit where they should for your resolution, aspect ratio or Dota UI scale) or **thresholds** (a team-tinted or badge-covered portrait scores below the cut). Two tools tell the cases apart:

```sh
# One frame: saves a capture with the ten slot boxes drawn over it, and prints
# what each slot matched, with the similarity and margin that decided it.
# Run while a draft is on screen; the boxes should sit on the portraits.
go run ./tools/visioncheck --own <heroID> --side left

# Continuous labeled collection: runs its own GSI listener and, during hero
# selection and strategy time, writes a screenshot plus a JSON sidecar naming
# the heroes GSI reported (draft picks/bans and the strategy-time minimap).
go run ./tools/visioncollect --port 3000 --install
```

Both write to the user data directory (`vision.CaptureDir()`); the collector prints its target on start. The overlay and the collector cannot both own the GSI config, so close the overlay while collecting, or use `--install` to repoint the config at the collector. Captures are development material and are never sent anywhere.

If `visioncheck` shows the boxes off the portraits, adjust the fractions at the top of `internal/vision/layout.go`. If the boxes are right and slots are still refused, the collected frames hold the real bar art to build better templates from.

## Tests

```sh
go test . ./internal/...
```

(`go test ./...` also walks the platform scaffolding under `build/`, which has build-tag-constrained `main` packages that only compile for their target OS.)

## Platform support

| Feature             | Windows | macOS | Linux |
| ------------------- | :-----: | :---: | :---: |
| GSI draft detection |    ✔    |   ✔   |   ✔   |
| Overlay UI          |    ✔    |   ✔   |   ✔   |
| Mouse click-through |    ✔    |   ✔   |   ✔   |

# counterdraft

A lightweight, always-on-top desktop overlay that helps you draft in Dota 2. It watches the enemy and ally picks and suggests heroes that counter the enemy lineup and synergize with your own team, in real time.

Built with [Wails](https://wails.io) (Go backend + a tiny vanilla-JS frontend), so it ships as a single native binary with no browser or runtime dependency.

## About

This is a helper tool that assists in picking heroes in the popular MOBA called Dota 2. It scans the enemy team picks, offers counterpicks, while also simultaneously monitoring ally picks offering heroes that synergizes well with the already selected heroes.

### Will they ban me for using this?

**No, they shouldn't.** Hero picks are detected using Valve's own Game State Integration system, which violates no Valve policy.
You can also manually enter the heroes, if you want to feel absolutely safe. Then it just runs as any other completely isolated program that won't bother Steam/Dota 2 (think of Google Chrome, or Discord).

## Features

- **Live draft detection** via Dota 2's official Game State Integration (GSI) - the game pushes its own state to a local listener.
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
│   └── rules/               # Tag-based counter/synergy recommendation engine
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

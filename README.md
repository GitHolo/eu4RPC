# eu4-discord-rpc - eu4 discord rpc

this is a little discord rich presence tray app for europa universalis iv (steam & epic games, v1.37.5.0 inca) that reads live game state straight from memory without touching any game files or breaking your ironman/mp checksum. it's written in pure go with zero external dependencies. no electron, no bloat, just a tiny standalone `.exe` that sits in your system tray.

### features
- works on both steam and epic games builds via runtime pattern scanning
- auto-detects your eu4 install folder from the running `eu4.exe` (no manual path setup needed)
- detects main menu, sp lobby, mp lobby (with player count), in-game, and observer mode
- shows your current country, year, and mode (singleplayer, ironman, or multiplayer)
- puts dev, province count, great power rank, current age, war status, and game speed / pause state in the hover tooltips
- pulls country flags from a configurable url and falls back to the eu4 logo
- updates right away when you switch screens or form a new tag, and throttles normal updates to `update_interval_seconds`
- tracks total eu4 session time from when the game started
- system tray menu with live status, force refresh, config reload, log viewer, and start with windows toggle

### tech stack
- go (memory reader, pe pattern scanner, discord ipc, win32 system tray)
- win32 api via syscall (ReadProcessMemory, named pipes, system tray, single-instance lock)
- python + pillow (optional helper script to export icons and flags from eu4's `gfx/` folder)

### setup (easy way)
1. grab `eu4-discord-rpc.exe` and `config.json` from releases and put them in the same folder
2. (optional) tweak `config.json` if you want to change `update_interval_seconds`, `flag_base_url`, or use your own discord app `client_id` (the 8 icons are in `assets/dev_portal/`)
3. run `eu4-discord-rpc.exe` while eu4 and discord are open

### setup (from source)
1. clone the repo
2. build with `go build -ldflags="-H=windowsgui -s -w" -o eu4-discord-rpc.exe ./cmd/eu4-discord-rpc`
3. (optional) build the cli dumper with `go build -ldflags="-s -w" -o eu4-dump.exe ./cmd/eu4-dump` to inspect what it reads from memory
4. (optional) run `python scripts/export_assets.py --flags` to re-export the icons or all 1,027 country flags

### misc
made by me. icons and flags are exported from eu4's own `gfx/` files, and country flags are hosted via jsdelivr.
[![license: mit](https://img.shields.io/badge/license-mit-lightgrey.svg)](https://opensource.org/licenses/MIT)

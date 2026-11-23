# eu4-discord-rpc - eu4 discord rpc

this is a little discord rich presence tray app for europa universalis iv (v1.37.5.0 inca) that reads live game state straight from memory without touching any game files or breaking your ironman/mp checksum. it's written in pure go with zero external dependencies, no electron, no bloat, just a tiny standalone `.exe` that sits in the system tray.

### features
- detects main menu, sp lobby, mp lobby (with player count), in-game, and observer mode
- shows your current country, year, and mode (singleplayer, ironman, or multiplayer)
- puts dev, province count, great power rank, current age, war status, and game speed / pause state in the hover tooltips
- pulls country flags from a configurable url (or dev portal assets) and falls back to the eu4 logo
- updates right away when you switch screens or tags, and throttles normal updates to whatever `update_interval_seconds` is set to
- tracks total eu4 session time from the process start time
- system tray menu with status, force refresh, config reload, log viewer, and start with windows toggle

### tech stack
- go (logic, memory reader, win32 tray, discord ipc)
- win32 api via syscall (ReadProcessMemory, named pipes, system tray)
- python + pillow (optional helper script to export icons and flags from eu4's `gfx/` folder)

### setup (easy way)
1. grab `eu4-discord-rpc.exe` and `config.json` and put them in the same folder
2. put your discord app id in `config.json` under `client_id` (and upload the 8 pngs from `assets/dev_portal/` to your discord dev portal app)
3. tweak `eu4_install_dir`, `update_interval_seconds`, or `flag_base_url` in `config.json` if needed
4. run `eu4-discord-rpc.exe` while eu4 and discord are open

### setup (from source)
1. clone the repo
2. edit `config.json` with your discord app id and eu4 path
3. build with `go build -ldflags="-H=windowsgui -s -w" -o eu4-discord-rpc.exe ./cmd/eu4-discord-rpc`
4. (optional) build the cli dumper with `go build -ldflags="-s -w" -o eu4-dump.exe ./cmd/eu4-dump` to inspect live memory output
5. (optional) run `python scripts/export_assets.py --flags` if you want to re-export the icons or all 1,027 country flags

### misc
made by me. icons and flags are exported from eu4's own `gfx/` files, and country flags are hosted via jsdelivr.
[![license: mit](https://img.shields.io/badge/license-mit-lightgrey.svg)](https://opensource.org/licenses/MIT)

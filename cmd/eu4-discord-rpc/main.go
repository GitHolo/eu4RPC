package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"eu4rpc/internal/config"
	"eu4rpc/internal/discord"
	"eu4rpc/internal/eu4"
	"eu4rpc/internal/loc"
	"eu4rpc/internal/presence"
	"eu4rpc/internal/tray"
)

func main() {
	runtime.LockOSThread()

	cfgPathFlag := flag.String("config", "", "Path to config.json (defaults to config.json next to executable)")
	flag.Parse()

	cfgPath := *cfgPathFlag
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	if abs, err := filepath.Abs(cfgPath); err == nil {
		cfgPath = abs
	}

	logPath := filepath.Join(filepath.Dir(cfgPath), "eu4-discord-rpc.log")
	logFile := openLogFile(logPath)
	if logFile != nil {
		defer logFile.Close()
		log.SetOutput(logFile)
	}
	log.SetFlags(log.LstdFlags)
	log.Printf("Starting EU4 Discord RPC (config=%s)", cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Printf("Failed to load config (%s): %v; using defaults", cfgPath, err)
		cfg = config.Default()
	}

	locStore, err := loc.LoadFromEU4Dir(cfg.EU4InstallDir)
	if err != nil {
		log.Printf("Localization load warning (%s): %v", cfg.EU4InstallDir, err)
	} else {
		log.Printf("Loaded %d country names from %s", len(locStore.Countries), cfg.EU4InstallDir)
	}

	exePath, _ := os.Executable()
	eu4ExeForIcon := filepath.Join(cfg.EU4InstallDir, eu4.ExpectedExeName)

	forceCh := make(chan struct{}, 1)
	reloadCh := make(chan struct{}, 1)
	quitCh := make(chan struct{})
	var quitOnce sync.Once
	triggerQuit := func() {
		quitOnce.Do(func() { close(quitCh) })
	}

	var tr *tray.Tray
	tr = tray.New(eu4ExeForIcon, exePath, tray.Callbacks{
		OnForceRefresh: func() {
			select {
			case forceCh <- struct{}{}:
			default:
			}
		},
		OnReloadConfig: func() {
			select {
			case reloadCh <- struct{}{}:
			default:
			}
		},
		OnOpenConfig: func() {
			tray.OpenPath(cfgPath)
		},
		OnOpenLog: func() {
			tray.OpenPath(logPath)
		},
		OnQuit: func() {
			triggerQuit()
		},
	})

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		runLoop(cfgPath, cfg, locStore, tr, forceCh, reloadCh, quitCh)
	}()

	if err := tr.Run(); err != nil {
		log.Printf("Tray loop error: %v", err)
	}
	triggerQuit()
	<-doneCh
	log.Printf("EU4 Discord RPC exited cleanly")
}

func runLoop(
	cfgPath string,
	cfg *config.Config,
	locStore *loc.Store,
	tr *tray.Tray,
	forceCh <-chan struct{},
	reloadCh <-chan struct{},
	quitCh <-chan struct{},
) {
	reader := eu4.NewReader(locStore)
	defer reader.Close()

	dc := discord.NewClient()
	defer dc.Close()

	ctrl := presence.NewController()

	var (
		lastPID           uint32
		lastConnAttemptAt time.Time
		lastErrLogAt      time.Time
		lastReadErrMsg    string
		pollTicker        = time.NewTicker(time.Duration(cfg.PollIntervalSeconds) * time.Second)
	)
	defer pollTicker.Stop()

	resetPollTicker := func(seconds int) {
		if seconds <= 0 {
			seconds = 2
		}
		pollTicker.Reset(time.Duration(seconds) * time.Second)
	}

	step := func(force bool) {
		if !cfg.HasValidClientID() {
			tr.SetStatus("Client ID missing - edit config.json", "EU4 Discord RPC - Set client_id in config.json")
			return
		}

		st, err := reader.ReadState()
		if err != nil {
			if errors.Is(err, eu4.ErrNotRunning) {
				if lastPID != 0 {
					log.Printf("EU4 exited (pid=%d); clearing Discord activity", lastPID)
					if dc.IsConnected() {
						_ = dc.ClearActivity(int(lastPID))
						_ = dc.Close()
					}
					ctrl.Reset()
					lastPID = 0
				}
				tr.SetStatus("EU4: Not running", "EU4 Discord RPC - Waiting for EU4")
				return
			}
			if errors.Is(err, eu4.ErrNotReady) {
				tr.SetStatus("EU4: Starting up...", "EU4 Discord RPC - Waiting for game state")
				return
			}
			msg := err.Error()
			if msg != lastReadErrMsg || time.Since(lastErrLogAt) > 30*time.Second {
				log.Printf("ReadState error: %v", err)
				lastReadErrMsg = msg
				lastErrLogAt = time.Now()
			}
			tr.SetStatus("EU4: Read error (see log)", "EU4 Discord RPC - Read error")
			return
		}

		lastPID = st.PID
		lastReadErrMsg = ""

		justConnected := false
		if !dc.IsConnected() {
			if !force && time.Since(lastConnAttemptAt) < 5*time.Second {
				tr.SetStatus(formatTrayStatus(st, false), "EU4 Discord RPC - Waiting for Discord")
				return
			}
			lastConnAttemptAt = time.Now()
			if err := dc.Connect(cfg.ClientID); err != nil {
				if time.Since(lastErrLogAt) > 30*time.Second {
					log.Printf("Discord connect warning: %v", err)
					lastErrLogAt = time.Now()
				}
				tr.SetStatus(formatTrayStatus(st, false), "EU4 Discord RPC - Discord not connected")
				return
			}
			log.Printf("Connected to Discord IPC (client_id=%s)", cfg.ClientID)
			justConnected = true
		}

		act := presence.Format(st, cfg)
		now := time.Now()
		updateInterval := time.Duration(cfg.UpdateIntervalSeconds) * time.Second
		push, reason := ctrl.ShouldPush(st, act, now, force || justConnected, updateInterval)
		if push {
			if err := dc.SetActivity(int(st.PID), act); err != nil {
				log.Printf("Discord SetActivity failed (%s): %v", reason, err)
				_ = dc.Close()
				tr.SetStatus(formatTrayStatus(st, false), "EU4 Discord RPC - Reconnecting...")
				return
			}
			ctrl.MarkPushed(st, act, now)
			log.Printf("Updated Discord [%s]: %s | %s", reason, act.Details, act.State)
		}

		tr.SetStatus(formatTrayStatus(st, true), formatTrayTooltip(st))
	}

	step(true)

	for {
		select {
		case <-quitCh:
			if lastPID != 0 && dc.IsConnected() {
				_ = dc.ClearActivity(int(lastPID))
			}
			return
		case <-forceCh:
			step(true)
		case <-reloadCh:
			newCfg, err := config.Load(cfgPath)
			if err != nil {
				log.Printf("Reload config failed: %v", err)
				continue
			}
			if newCfg.ClientID != cfg.ClientID && dc.IsConnected() {
				if lastPID != 0 {
					_ = dc.ClearActivity(int(lastPID))
				}
				_ = dc.Close()
			}
			if newCfg.EU4InstallDir != cfg.EU4InstallDir {
				if newLoc, err := loc.LoadFromEU4Dir(newCfg.EU4InstallDir); err == nil {
					locStore = newLoc
					reader.SetLocStore(locStore)
				}
			}
			cfg = newCfg
			resetPollTicker(cfg.PollIntervalSeconds)
			ctrl.Reset()
			log.Printf("Reloaded config.json (interval=%ds)", cfg.UpdateIntervalSeconds)
			step(true)
		case <-pollTicker.C:
			step(false)
		}
	}
}

func formatTrayStatus(st *eu4.GameState, dcConnected bool) string {
	dcSuffix := "Discord OK"
	if !dcConnected {
		dcSuffix = "No Discord"
	}
	switch st.Mode {
	case eu4.ModeInGame:
		name := st.CountryName
		if name == "" {
			name = st.CountryTag
		}
		return fmt.Sprintf("EU4: %s (%d) • %s", name, st.Year, dcSuffix)
	case eu4.ModeObserving:
		if st.CountryName != "" {
			return fmt.Sprintf("EU4: Observing %s (%d) • %s", st.CountryName, st.Year, dcSuffix)
		}
		return fmt.Sprintf("EU4: Observing (%d) • %s", st.Year, dcSuffix)
	case eu4.ModeSPLobby:
		return "EU4: SP Lobby • " + dcSuffix
	case eu4.ModeMPLobby:
		return "EU4: MP Lobby • " + dcSuffix
	default:
		return "EU4: Main Menu • " + dcSuffix
	}
}

func formatTrayTooltip(st *eu4.GameState) string {
	switch st.Mode {
	case eu4.ModeInGame:
		name := st.CountryName
		if name == "" {
			name = st.CountryTag
		}
		return fmt.Sprintf("EU4 Discord RPC - %s (%d)", name, st.Year)
	case eu4.ModeObserving:
		return fmt.Sprintf("EU4 Discord RPC - Observing (%d)", st.Year)
	case eu4.ModeSPLobby:
		return "EU4 Discord RPC - SP Lobby"
	case eu4.ModeMPLobby:
		return "EU4 Discord RPC - MP Lobby"
	default:
		return "EU4 Discord RPC - Main Menu"
	}
}

func openLogFile(path string) *os.File {
	if info, err := os.Stat(path); err == nil && info.Size() > 1024*1024 {
		_ = os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return f
}

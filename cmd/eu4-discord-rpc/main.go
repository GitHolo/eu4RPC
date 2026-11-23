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
	"syscall"
	"time"
	"unsafe"

	"eu4rpc/internal/config"
	"eu4rpc/internal/discord"
	"eu4rpc/internal/eu4"
	"eu4rpc/internal/loc"
	"eu4rpc/internal/presence"
	"eu4rpc/internal/tray"
)

const (
	singleInstanceMutex = `Local\EU4DiscordRPC`
	maxLogBytes         = 1024 * 1024
	errAlreadyExists    = syscall.Errno(183)
)

var (
	modKernel32      = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = modKernel32.NewProc("CreateMutexW")
	procCloseHandle  = modKernel32.NewProc("CloseHandle")
)

func main() {
	runtime.LockOSThread()

	mutexHandle, ok := acquireSingleInstance(singleInstanceMutex)
	if !ok {
		return
	}
	if mutexHandle != 0 {
		defer procCloseHandle.Call(uintptr(mutexHandle))
	}

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
	logWriter := openRotatingLog(logPath)
	if logWriter != nil {
		defer logWriter.Close()
		log.SetOutput(logWriter)
	}
	log.SetFlags(log.LstdFlags)
	log.Printf("Starting EU4 Discord RPC (config=%s)", cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Printf("Failed to load config (%s): %v; using defaults", cfgPath, err)
		cfg = config.Default()
	}

	var locStore *loc.Store
	if cfg.EU4InstallDir == "" {
		locStore = loc.NewEmpty()
		log.Printf("EU4 install dir not set; will auto-detect from running eu4.exe")
	} else {
		locStore, err = loc.LoadFromEU4Dir(cfg.EU4InstallDir)
		if err != nil {
			log.Printf("Localization load warning (%s): %v", cfg.EU4InstallDir, err)
		} else {
			log.Printf("Loaded %d country names from %s", len(locStore.Countries), cfg.EU4InstallDir)
		}
	}

	exePath, _ := os.Executable()
	var eu4ExeForIcon string
	if cfg.EU4InstallDir != "" {
		eu4ExeForIcon = filepath.Join(cfg.EU4InstallDir, eu4.ExpectedExeName)
	}

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
		notReadySince     time.Time
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
				notReadySince = time.Time{}
				lastReadErrMsg = ""
				tr.SetStatus("EU4: Not running", "EU4 Discord RPC - Waiting for EU4")
				return
			}
			if errors.Is(err, eu4.ErrNotReady) {
				if notReadySince.IsZero() {
					notReadySince = time.Now()
				} else if time.Since(notReadySince) >= 15*time.Second {
					msg := err.Error()
					if msg != lastReadErrMsg || time.Since(lastErrLogAt) > 30*time.Second {
						log.Printf("EU4 still not ready: %v", err)
						lastReadErrMsg = msg
						lastErrLogAt = time.Now()
					}
				}
				tr.SetStatus("EU4: Starting up...", "EU4 Discord RPC - Waiting for game state")
				return
			}
			notReadySince = time.Time{}
			msg := err.Error()
			if msg != lastReadErrMsg || time.Since(lastErrLogAt) > 30*time.Second {
				log.Printf("ReadState error: %v", err)
				lastReadErrMsg = msg
				lastErrLogAt = time.Now()
			}
			tr.SetStatus("EU4: Read error (see log)", "EU4 Discord RPC - Read error")
			return
		}

		notReadySince = time.Time{}
		if st.PID != lastPID {
			rvas := reader.RVAs()
			log.Printf("Attached to eu4.exe (pid=%d, path=%s, gs=0x%x, cdb=0x%x, rp=0x%x)",
				st.PID, reader.ExePath(), rvas.GameState, rvas.CountryDB, rvas.RPMapHead)
			tr.SetIconFromExe(reader.ExePath())
			lastPID = st.PID
		}
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
			reader.ResetFailedScan()
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
				locDir := newCfg.EU4InstallDir
				if locDir == "" && reader.ExePath() != "" {
					locDir = filepath.Dir(reader.ExePath())
				}
				if newLoc, err := loc.LoadFromEU4Dir(locDir); err == nil {
					locStore = newLoc
					reader.SetLocStore(locStore)
				}
			}
			cfg = newCfg
			resetPollTicker(cfg.PollIntervalSeconds)
			reader.ResetFailedScan()
			ctrl.Reset()
			log.Printf("Reloaded config.json (interval=%ds)", cfg.UpdateIntervalSeconds)
			step(true)
		case <-pollTicker.C:
			step(false)
		}
	}
}

func formatModeLabel(st *eu4.GameState) string {
	switch st.Mode {
	case eu4.ModeInGame:
		name := st.CountryName
		if name == "" {
			name = st.CountryTag
		}
		return fmt.Sprintf("%s (%d)", name, st.Year)
	case eu4.ModeObserving:
		if st.CountryName != "" {
			return fmt.Sprintf("Observing %s (%d)", st.CountryName, st.Year)
		}
		return fmt.Sprintf("Observing (%d)", st.Year)
	case eu4.ModeSPLobby:
		return "SP Lobby"
	case eu4.ModeMPLobby:
		return "MP Lobby"
	default:
		return "Main Menu"
	}
}

func formatTrayStatus(st *eu4.GameState, dcConnected bool) string {
	dcSuffix := "Discord OK"
	if !dcConnected {
		dcSuffix = "No Discord"
	}
	return "EU4: " + formatModeLabel(st) + " • " + dcSuffix
}

func formatTrayTooltip(st *eu4.GameState) string {
	if st.Mode == eu4.ModeObserving {
		return fmt.Sprintf("EU4 Discord RPC - Observing (%d)", st.Year)
	}
	return "EU4 Discord RPC - " + formatModeLabel(st)
}

func acquireSingleInstance(name string) (syscall.Handle, bool) {
	p16, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, true
	}
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(p16)))
	if h == 0 {
		return 0, true
	}
	if errors.Is(callErr, errAlreadyExists) {
		_, _, _ = procCloseHandle.Call(h)
		return 0, false
	}
	return syscall.Handle(h), true
}

type rotatingLogWriter struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

func openRotatingLog(path string) *rotatingLogWriter {
	w := &rotatingLogWriter{path: path}
	if info, err := os.Stat(path); err == nil {
		if info.Size() > maxLogBytes {
			_ = os.Remove(path)
		} else {
			w.size = info.Size()
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	w.file = f
	return w
}

func (w *rotatingLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	if w.size+int64(len(p)) > maxLogBytes {
		_ = w.file.Close()
		_ = os.Remove(w.path)
		f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			w.file = nil
			w.size = 0
			return 0, err
		}
		w.file = f
		w.size = 0
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

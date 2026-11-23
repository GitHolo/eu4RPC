package tray

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modAdvapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegisterClassExW       = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW        = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW         = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow          = modUser32.NewProc("DestroyWindow")
	procGetMessageW            = modUser32.NewProc("GetMessageW")
	procTranslateMessage       = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW       = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage        = modUser32.NewProc("PostQuitMessage")
	procPostMessageW           = modUser32.NewProc("PostMessageW")
	procRegisterWindowMessageW = modUser32.NewProc("RegisterWindowMessageW")
	procLoadIconW              = modUser32.NewProc("LoadIconW")
	procDestroyIcon            = modUser32.NewProc("DestroyIcon")
	procCreatePopupMenu        = modUser32.NewProc("CreatePopupMenu")
	procDestroyMenu            = modUser32.NewProc("DestroyMenu")
	procAppendMenuW            = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu         = modUser32.NewProc("TrackPopupMenu")
	procSetForegroundWindow    = modUser32.NewProc("SetForegroundWindow")
	procGetCursorPos           = modUser32.NewProc("GetCursorPos")
	procShellNotifyIconW       = modShell32.NewProc("Shell_NotifyIconW")
	procExtractIconExW         = modShell32.NewProc("ExtractIconExW")
	procShellExecuteW          = modShell32.NewProc("ShellExecuteW")
	procGetModuleHandleW       = modKernel32.NewProc("GetModuleHandleW")
	procRegOpenKeyExW          = modAdvapi32.NewProc("RegOpenKeyExW")
	procRegCreateKeyExW        = modAdvapi32.NewProc("RegCreateKeyExW")
	procRegQueryValueExW       = modAdvapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW         = modAdvapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW        = modAdvapi32.NewProc("RegDeleteValueW")
	procRegCloseKey            = modAdvapi32.NewProc("RegCloseKey")
)

const (
	wmDestroy    = 0x0002
	wmCommand    = 0x0111
	wmNull       = 0x0000
	wmRButtonUp  = 0x0205
	wmLButtonDbl = 0x0203
	wmApp        = 0x8000
	wmTrayIcon   = wmApp + 1
	wmUpdateTray = wmApp + 2

	nimAdd     = 0x00000000
	nimModify  = 0x00000001
	nimDelete  = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString    = 0x00000000
	mfSeparator = 0x00000800
	mfGrayed    = 0x00000001
	mfChecked   = 0x00000008

	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020

	idiApplication = 32512

	cmdStatus        = 1001
	cmdForceRefresh  = 1002
	cmdReloadConfig  = 1003
	cmdOpenConfig    = 1004
	cmdOpenLog       = 1005
	cmdToggleStartup = 1006
	cmdQuit          = 1007

	hkeyCurrentUser = uintptr(0x80000001)
	keyRead         = 0x20019
	keyWrite        = 0x20006
	regSZ           = 1
	runKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName    = "EU4DiscordRPC"
)

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type point struct {
	X int32
	Y int32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type notifyIconDataW struct {
	CbSize           uint32
	Hwnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeoutVersion  uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type Callbacks struct {
	OnForceRefresh func()
	OnReloadConfig func()
	OnOpenConfig   func()
	OnOpenLog      func()
	OnQuit         func()
}

type Tray struct {
	mu             sync.RWMutex
	hwnd           uintptr
	hIcon          uintptr
	ownIcon        bool
	wmTaskbarReset uint32
	statusText     string
	tooltip        string
	startupExePath string
	callbacks      Callbacks
}

var (
	globalTrayMu sync.Mutex
	globalTray   *Tray
)

func New(iconSourceExe, initialExeForStartup string, cb Callbacks) *Tray {
	t := &Tray{
		statusText:     "EU4 Discord RPC: Starting...",
		tooltip:        "EU4 Discord RPC",
		startupExePath: initialExeForStartup,
		callbacks:      cb,
	}
	t.hIcon, t.ownIcon = loadTrayIcon(iconSourceExe)
	return t
}

func (t *Tray) SetStatus(statusLabel, tooltip string) {
	t.mu.Lock()
	changed := t.statusText != statusLabel || t.tooltip != tooltip
	t.statusText = statusLabel
	t.tooltip = tooltip
	hwnd := t.hwnd
	t.mu.Unlock()

	if changed && hwnd != 0 {
		_, _, _ = procPostMessageW.Call(hwnd, wmUpdateTray, 0, 0)
	}
}

func (t *Tray) Run() error {
	globalTrayMu.Lock()
	globalTray = t
	globalTrayMu.Unlock()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("EU4DiscordRPCTrayWindow")
	taskbarMsgName, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	tm, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(taskbarMsgName)))
	t.wmTaskbarReset = uint32(tm)

	wndProc := syscall.NewCallback(trayWndProc)
	wc := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   wndProc,
		HInstance:     hInst,
		LpszClassName: className,
	}
	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return fmt.Errorf("RegisterClassExW failed: %w", err)
	}

	winName, _ := syscall.UTF16PtrFromString("EU4 Discord RPC")
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(winName)),
		0,
		0, 0, 0, 0,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW failed: %w", err)
	}

	t.mu.Lock()
	t.hwnd = hwnd
	t.mu.Unlock()

	t.addIcon()

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	t.removeIcon()
	if t.ownIcon && t.hIcon != 0 {
		_, _, _ = procDestroyIcon.Call(t.hIcon)
	}
	return nil
}

func (t *Tray) SetIconFromExe(exePath string) {
	if exePath == "" {
		return
	}
	t.mu.RLock()
	alreadyOwn := t.ownIcon
	t.mu.RUnlock()
	if alreadyOwn {
		return
	}

	icon := extractSmallIcon(exePath)
	if icon == 0 {
		return
	}

	t.mu.Lock()
	if t.ownIcon {
		t.mu.Unlock()
		_, _, _ = procDestroyIcon.Call(icon)
		return
	}
	t.hIcon = icon
	t.ownIcon = true
	hwnd := t.hwnd
	t.mu.Unlock()

	if hwnd != 0 {
		_, _, _ = procPostMessageW.Call(hwnd, wmUpdateTray, 0, 0)
	}
}

func trayWndProc(hwnd uintptr, uMsg uint32, wParam, lParam uintptr) uintptr {
	globalTrayMu.Lock()
	t := globalTray
	globalTrayMu.Unlock()

	if t != nil && t.wmTaskbarReset != 0 && uMsg == t.wmTaskbarReset {
		t.addIcon()
		return 0
	}

	switch uMsg {
	case wmTrayIcon:
		switch uint32(lParam) {
		case wmRButtonUp:
			if t != nil {
				t.showMenu()
			}
		case wmLButtonDbl:
			if t != nil && t.callbacks.OnForceRefresh != nil {
				t.callbacks.OnForceRefresh()
			}
		}
		return 0
	case wmUpdateTray:
		if t != nil {
			t.modifyIcon()
		}
		return 0
	case wmCommand:
		if t != nil {
			t.handleCommand(uint32(wParam & 0xFFFF))
		}
		return 0
	case wmDestroy:
		if t != nil {
			t.removeIcon()
			t.mu.Lock()
			t.hwnd = 0
			t.mu.Unlock()
		}
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(uMsg), wParam, lParam)
	return ret
}

func (t *Tray) buildNID(flags uint32) notifyIconDataW {
	t.mu.RLock()
	hwnd := t.hwnd
	tip := t.tooltip
	icon := t.hIcon
	t.mu.RUnlock()

	nid := notifyIconDataW{
		CbSize:           uint32(unsafe.Sizeof(notifyIconDataW{})),
		Hwnd:             hwnd,
		UID:              1,
		UFlags:           flags,
		UCallbackMessage: wmTrayIcon,
		HIcon:            icon,
	}
	copyUTF16(nid.SzTip[:], tip)
	return nid
}

func (t *Tray) addIcon() {
	nid := t.buildNID(nifMessage | nifIcon | nifTip)
	_, _, _ = procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
}

func (t *Tray) modifyIcon() {
	nid := t.buildNID(nifIcon | nifTip)
	_, _, _ = procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func (t *Tray) removeIcon() {
	t.mu.RLock()
	hwnd := t.hwnd
	t.mu.RUnlock()
	if hwnd == 0 {
		return
	}
	nid := notifyIconDataW{
		CbSize: uint32(unsafe.Sizeof(notifyIconDataW{})),
		Hwnd:   hwnd,
		UID:    1,
	}
	_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

func (t *Tray) showMenu() {
	t.mu.RLock()
	hwnd := t.hwnd
	status := t.statusText
	t.mu.RUnlock()

	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	appendMenuItem(hMenu, mfString|mfGrayed, cmdStatus, status)
	appendMenuItem(hMenu, mfSeparator, 0, "")
	appendMenuItem(hMenu, mfString, cmdForceRefresh, "Force Refresh")
	appendMenuItem(hMenu, mfString, cmdReloadConfig, "Reload config.json")
	appendMenuItem(hMenu, mfString, cmdOpenConfig, "Open config.json")
	appendMenuItem(hMenu, mfString, cmdOpenLog, "Open Log File")
	appendMenuItem(hMenu, mfSeparator, 0, "")

	startupFlags := uint32(mfString)
	if IsAutoStartEnabled() {
		startupFlags |= mfChecked
	}
	appendMenuItem(hMenu, startupFlags, cmdToggleStartup, "Start with Windows")
	appendMenuItem(hMenu, mfSeparator, 0, "")
	appendMenuItem(hMenu, mfString, cmdQuit, "Quit")

	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	_, _, _ = procSetForegroundWindow.Call(hwnd)
	_, _, _ = procTrackPopupMenu.Call(
		hMenu,
		tpmRightButton|tpmBottomAlign,
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		hwnd,
		0,
	)
	_, _, _ = procPostMessageW.Call(hwnd, wmNull, 0, 0)
}

func (t *Tray) handleCommand(id uint32) {
	switch id {
	case cmdForceRefresh:
		if t.callbacks.OnForceRefresh != nil {
			t.callbacks.OnForceRefresh()
		}
	case cmdReloadConfig:
		if t.callbacks.OnReloadConfig != nil {
			t.callbacks.OnReloadConfig()
		}
	case cmdOpenConfig:
		if t.callbacks.OnOpenConfig != nil {
			t.callbacks.OnOpenConfig()
		}
	case cmdOpenLog:
		if t.callbacks.OnOpenLog != nil {
			t.callbacks.OnOpenLog()
		}
	case cmdToggleStartup:
		_ = SetAutoStartEnabled(!IsAutoStartEnabled(), t.startupExePath)
	case cmdQuit:
		if t.callbacks.OnQuit != nil {
			t.callbacks.OnQuit()
		}
		_, _, _ = procDestroyWindow.Call(t.hwnd)
	}
}

func appendMenuItem(hMenu uintptr, flags, id uint32, text string) {
	if text == "" {
		_, _, _ = procAppendMenuW.Call(hMenu, uintptr(flags), uintptr(id), 0)
		return
	}
	p16, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	// keep p16 directly in .Call() so Go's GC cannot free it early
	_, _, _ = procAppendMenuW.Call(hMenu, uintptr(flags), uintptr(id), uintptr(unsafe.Pointer(p16)))
}

func loadTrayIcon(exePath string) (uintptr, bool) {
	candidates := []string{exePath}
	if self, err := os.Executable(); err == nil {
		candidates = append(candidates, self)
	}
	for _, p := range candidates {
		if icon := extractSmallIcon(p); icon != 0 {
			return icon, true
		}
	}
	hIcon, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	return hIcon, false
}

func extractSmallIcon(p string) uintptr {
	if p == "" {
		return 0
	}
	if _, err := os.Stat(p); err != nil {
		return 0
	}
	p16, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return 0
	}
	var smallIcon uintptr
	ret, _, _ := procExtractIconExW.Call(
		uintptr(unsafe.Pointer(p16)),
		0,
		0,
		uintptr(unsafe.Pointer(&smallIcon)),
		1,
	)
	if ret > 0 && smallIcon != 0 {
		return smallIcon
	}
	return 0
}

func OpenPath(path string) {
	verb, _ := syscall.UTF16PtrFromString("open")
	target, _ := syscall.UTF16PtrFromString(path)
	_, _, _ = procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0,
		0,
		1, // SW_SHOWNORMAL
	)
}

func IsAutoStartEnabled() bool {
	subKey, _ := syscall.UTF16PtrFromString(runKeyPath)
	valName, _ := syscall.UTF16PtrFromString(runValueName)
	var hKey uintptr
	r, _, _ := procRegOpenKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(subKey)),
		0,
		keyRead,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r != 0 {
		return false
	}
	defer procRegCloseKey.Call(hKey)

	var cbData uint32
	r, _, _ = procRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(valName)),
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&cbData)),
	)
	return r == 0 && cbData > 2
}

func SetAutoStartEnabled(enable bool, exePath string) error {
	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return err
		}
	}
	subKey, _ := syscall.UTF16PtrFromString(runKeyPath)
	valName, _ := syscall.UTF16PtrFromString(runValueName)
	var hKey uintptr
	r, _, _ := procRegCreateKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(subKey)),
		0,
		0,
		0,
		keyWrite,
		0,
		uintptr(unsafe.Pointer(&hKey)),
		0,
	)
	if r != 0 {
		return fmt.Errorf("RegCreateKeyExW failed with code %d", r)
	}
	defer procRegCloseKey.Call(hKey)

	if !enable {
		r, _, _ = procRegDeleteValueW.Call(hKey, uintptr(unsafe.Pointer(valName)))
		if r != 0 && r != 2 { // ERROR_FILE_NOT_FOUND
			return fmt.Errorf("RegDeleteValueW failed with code %d", r)
		}
		return nil
	}

	cmd := fmt.Sprintf(`"%s"`, strings.Trim(exePath, `"`))
	u16, err := syscall.UTF16FromString(cmd)
	if err != nil {
		return err
	}
	cbData := uint32(len(u16) * 2)
	r, _, _ = procRegSetValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(valName)),
		0,
		regSZ,
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(cbData),
	)
	if r != 0 {
		return fmt.Errorf("RegSetValueExW failed with code %d", r)
	}
	return nil
}

func copyUTF16(dst []uint16, s string) {
	u16, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	max := len(dst) - 1
	if len(u16)-1 < max {
		max = len(u16) - 1
	}
	copy(dst[:max], u16[:max])
	dst[max] = 0
}

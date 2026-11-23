package eu4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"eu4rpc/internal/loc"
)

var (
	ErrNotRunning = errors.New("eu4.exe is not running")
	ErrNotReady   = errors.New("eu4 game state not yet initialized")

	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modPsapi    = syscall.NewLazyDLL("psapi.dll")

	procCreateToolhelp32Snapshot = modKernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = modKernel32.NewProc("Process32FirstW")
	procProcess32NextW           = modKernel32.NewProc("Process32NextW")
	procOpenProcess              = modKernel32.NewProc("OpenProcess")
	procCloseHandle              = modKernel32.NewProc("CloseHandle")
	procReadProcessMemory        = modKernel32.NewProc("ReadProcessMemory")
	procGetProcessTimes          = modKernel32.NewProc("GetProcessTimes")
	procGetExitCodeProcess       = modKernel32.NewProc("GetExitCodeProcess")
	procQueryFullProcessImageNameW = modKernel32.NewProc("QueryFullProcessImageNameW")
	procEnumProcessModulesEx     = modPsapi.NewProc("EnumProcessModulesEx")
)

const (
	th32csSnapProcess = 0x00000002
	processVMRead     = 0x0010
	processQueryInfo  = 0x0400
	listModules64Bit  = 0x02
	listModulesAll    = 0x03
	stillActive       = 259
	maxPath           = 260
)

type processEntry32W struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [maxPath]uint16
}

type fileTime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

type GameMode string

const (
	ModeMainMenu  GameMode = "MainMenu"
	ModeSPLobby   GameMode = "SPLobby"
	ModeMPLobby   GameMode = "MPLobby"
	ModeInGame    GameMode = "InGame"
	ModeObserving GameMode = "Observing"
)

type GameState struct {
	PID              uint32            `json:"pid"`
	SessionStartUnix int64             `json:"session_start_unix"`
	Mode             GameMode          `json:"mode"`
	Year             int               `json:"year"`
	CountryTag       string            `json:"country_tag,omitempty"`
	CountryName      string            `json:"country_name,omitempty"`
	AgeKey           string            `json:"age_key,omitempty"`
	AgeName          string            `json:"age_name,omitempty"`
	IsMultiplayer    bool              `json:"is_multiplayer"`
	NumPlayers       int               `json:"num_players"`
	IsIronman        bool              `json:"is_ironman"`
	IsPaused         bool              `json:"is_paused"`
	GameSpeed        int               `json:"game_speed"`
	TotalDev         int               `json:"total_dev"`
	NumProvinces     int               `json:"num_provinces"`
	GreatPowerRank   int               `json:"great_power_rank"`
	IsAtWar          bool              `json:"is_at_war"`
	WarCount         int               `json:"war_count"`
	PrimaryEnemy     string            `json:"primary_enemy,omitempty"`
	RawRPMap         map[string]string `json:"raw_rp_map,omitempty"`
}

type Reader struct {
	locStore         *loc.Store
	handle           syscall.Handle
	pid              uint32
	baseAddr         uintptr
	exePath          string
	rvas             ResolvedRVAs
	sessionStartUnix int64
}

func NewReader(locStore *loc.Store) *Reader {
	return &Reader{locStore: locStore}
}

func (r *Reader) SetLocStore(locStore *loc.Store) {
	r.locStore = locStore
}

func (r *Reader) ExePath() string {
	return r.exePath
}

func (r *Reader) RVAs() ResolvedRVAs {
	return r.rvas
}

func (r *Reader) Close() {
	if r.handle != 0 {
		_, _, _ = procCloseHandle.Call(uintptr(r.handle))
		r.handle = 0
	}
	r.pid = 0
	r.baseAddr = 0
	r.exePath = ""
	r.rvas = ResolvedRVAs{}
	r.sessionStartUnix = 0
}

func (r *Reader) ReadState() (*GameState, error) {
	if err := r.ensureAttached(); err != nil {
		return nil, err
	}

	gsPtr, err := r.readU64(r.baseAddr + r.rvas.GameState)
	if err != nil {
		if !r.isProcessAlive() {
			r.Close()
			return nil, ErrNotRunning
		}
		return nil, fmt.Errorf("%w: %v", ErrNotReady, err)
	}
	if gsPtr == 0 {
		return nil, ErrNotReady
	}

	st := &GameState{
		PID:              r.pid,
		SessionStartUnix: r.sessionStartUnix,
		Mode:             ModeMainMenu,
	}

	if yr, err := r.readI32(uintptr(gsPtr) + GSOff_CurrentYear); err == nil && yr >= 1 && yr <= 9999 {
		st.Year = int(yr)
	}
	if np, err := r.readI32(uintptr(gsPtr) + GSOff_NumPlayers); err == nil && np >= 0 && np < 1024 {
		st.NumPlayers = int(np)
	}
	if iron, err := r.readU8(uintptr(gsPtr) + GSOff_IsIronman); err == nil {
		st.IsIronman = iron != 0
	}
	if mp, err := r.readI32(uintptr(gsPtr) + GSOff_IsMultiplayer); err == nil {
		st.IsMultiplayer = mp != 0
	}
	if spd, err := r.readI32(uintptr(gsPtr) + GSOff_GameSpeed); err == nil {
		s := int(spd) + 1
		if s < 1 {
			s = 1
		} else if s > 5 {
			s = 5
		}
		st.GameSpeed = s
	}

	rpMap := r.readCachedRPMap()
	if len(rpMap) > 0 {
		st.RawRPMap = rpMap
	}

	idlerPtr, err := r.readU64(uintptr(gsPtr) + GSOff_CurrentIdler)
	if err != nil || idlerPtr == 0 {
		return nil, ErrNotReady
	}

	isFrontEnd, err := r.checkFrontEnd(uintptr(idlerPtr))
	if err != nil {
		return nil, fmt.Errorf("%w: idler check: %v", ErrNotReady, err)
	}

	playerBytes, _ := r.readBytes(uintptr(gsPtr)+GSOff_PlayerTag, 8)
	observedBytes, _ := r.readBytes(uintptr(gsPtr)+GSOff_ObservedTag, 8)
	isObserving := len(playerBytes) == 8 && playerBytes[6] == 0x07

	if isFrontEnd {
		frontState, _ := r.readI32(uintptr(idlerPtr) + FrontEndOff_State)
		switch frontState {
		case 3:
			if st.IsMultiplayer {
				st.Mode = ModeMPLobby
			} else {
				st.Mode = ModeSPLobby
			}
		case 4:
			st.Mode = ModeMPLobby
		default:
			st.Mode = ModeMainMenu
		}
		switch rpMap["#status"] {
		case "#MpLobby":
			st.Mode = ModeMPLobby
		case "#SpLobby":
			st.Mode = ModeSPLobby
		case "#MainMenu":
			st.Mode = ModeMainMenu
		}
		return st, nil
	}

	if isObserving {
		st.Mode = ModeObserving
	} else {
		st.Mode = ModeInGame
	}

	if pauseMgr, err := r.readU64(uintptr(idlerPtr) + InGameIdlerOff_PauseMgr); err == nil && pauseMgr != 0 {
		directPause, _ := r.readU8(uintptr(pauseMgr) + PauseMgrOff_DirectPause)
		vecBeg, err1 := r.readU64(uintptr(pauseMgr) + PauseMgrOff_VecBegin)
		vecEnd, err2 := r.readU64(uintptr(pauseMgr) + PauseMgrOff_VecEnd)
		vecPause := err1 == nil && err2 == nil && vecEnd > vecBeg && (vecEnd-vecBeg) >= 8 && (vecEnd-vecBeg) <= 8*64
		st.IsPaused = (directPause != 0) || vecPause
	}

	if agePtr, err := r.readU64(uintptr(gsPtr) + GSOff_AgePtr); err == nil && agePtr != 0 {
		if ageKey, err := r.readMSVCString(uintptr(agePtr) + AgeOff_KeyString); err == nil && ageKey != "" {
			st.AgeKey = ageKey
			st.AgeName = r.locStore.LookupAge(ageKey)
		}
	}

	var effectiveBytes []byte
	if isObserving {
		if len(observedBytes) == 8 && observedBytes[7] != 0 {
			effectiveBytes = observedBytes
		}
	} else if len(playerBytes) == 8 {
		effectiveBytes = playerBytes
	}

	tag, countryIdx, tagOK := parseCountryTag(effectiveBytes)
	if tagOK {
		st.CountryTag = tag
		r.populateCountryStats(st, uintptr(gsPtr), tag, countryIdx)
	}

	if cName := strings.TrimSpace(rpMap["country"]); cName != "" && st.CountryTag != "" {
		st.CountryName = cName
	}
	if st.CountryName == "" && st.CountryTag != "" {
		st.CountryName = r.locStore.LookupCountry(st.CountryTag)
	}
	if rpMap["#status"] == "#AtWarWith" {
		st.IsAtWar = true
		if enemy := strings.TrimSpace(rpMap["enemy"]); enemy != "" {
			st.PrimaryEnemy = enemy
		}
		if st.WarCount == 0 {
			st.WarCount = 1
		}
	}

	return st, nil
}

func (r *Reader) populateCountryStats(st *GameState, gsAddr uintptr, tag string, countryIdx int16) {
	if gpMgr, err := r.readU64(gsAddr + GSOff_GreatPowerMgr); err == nil && gpMgr != 0 {
		beg, err1 := r.readU64(uintptr(gpMgr) + GPMgrOff_VecBegin)
		end, err2 := r.readU64(uintptr(gpMgr) + GPMgrOff_VecEnd)
		if err1 == nil && err2 == nil && end >= beg && (end-beg)%uint64(GPEntrySize) == 0 {
			count := int((end - beg) / uint64(GPEntrySize))
			if count > 0 && count <= 64 {
				buf, err := r.readBytes(uintptr(beg), count*GPEntrySize)
				if err == nil {
					for i := 0; i < count; i++ {
						off := i * GPEntrySize
						entryIdx := int16(binary.LittleEndian.Uint16(buf[off+4 : off+6]))
						if entryIdx == countryIdx {
							st.GreatPowerRank = i + 1
							break
						}
					}
				}
			}
		}
	}

	cdbPtr, err := r.readU64(r.baseAddr + r.rvas.CountryDB)
	if err != nil || cdbPtr == 0 {
		return
	}
	arrPtr, err := r.readU64(uintptr(cdbPtr) + CountryDBOff_ArrayPtr)
	if err != nil || arrPtr == 0 {
		return
	}
	if countryIdx <= 0 || countryIdx >= 4096 {
		return
	}
	countryPtr, err := r.readU64(uintptr(arrPtr) + uintptr(countryIdx)*8)
	if err != nil || countryPtr == 0 {
		return
	}

	cTagBytes, err := r.readBytes(uintptr(countryPtr)+CountryOff_Tag, 3)
	if err != nil || string(cTagBytes) != tag {
		return
	}

	if devRaw, err := r.readI32(uintptr(countryPtr) + CountryOff_TotalDevRaw); err == nil && devRaw >= 0 {
		st.TotalDev = int(devRaw / 1000)
	}
	if provs, err := r.readI32(uintptr(countryPtr) + CountryOff_NumProvinces); err == nil && provs >= 0 && provs < 100000 {
		st.NumProvinces = int(provs)
	}
	if custFlag, err := r.readU64(uintptr(countryPtr) + CountryOff_CustomNameFlag); err == nil && custFlag != 0 {
		if custName, err := r.readMSVCString(uintptr(countryPtr) + CountryOff_CustomNameStr); err == nil && custName != "" {
			st.CountryName = custName
		}
	}

	warCount := r.countActiveWars(uintptr(countryPtr))
	if warCount > 0 {
		st.IsAtWar = true
		st.WarCount = warCount
	}
}

func (r *Reader) countActiveWars(countryAddr uintptr) int {
	head, err := r.readU64(countryAddr + CountryOff_WarEnemyHead)
	if err != nil || head == 0 {
		return 0
	}
	relPtr, err := r.readU64(countryAddr + CountryOff_WarRelPtr)
	if err != nil || relPtr == 0 {
		return 0
	}
	relCount, err := r.readI32(countryAddr + CountryOff_WarRelCount)
	if err != nil || relCount <= 0 || relCount > 2048 {
		return 0
	}

	relBuf, err := r.readBytes(uintptr(relPtr), int(relCount)*16)
	if err != nil {
		return 0
	}
	relMap := make(map[int32]uint64, relCount)
	for i := 0; i < int(relCount); i++ {
		off := i * 16
		idx := int32(binary.LittleEndian.Uint32(relBuf[off : off+4]))
		warPtr := binary.LittleEndian.Uint64(relBuf[off+8 : off+16])
		if warPtr != 0 {
			relMap[idx] = warPtr
		}
	}

	seenWars := make(map[uint64]struct{})
	visitedNodes := make(map[uint64]struct{})
	node := head
	for steps := 0; node != 0 && steps < 256; steps++ {
		if _, seen := visitedNodes[node]; seen {
			break
		}
		visitedNodes[node] = struct{}{}

		nb, err := r.readBytes(uintptr(node), 24)
		if err != nil {
			break
		}
		enemyIdx := int32(int16(binary.LittleEndian.Uint16(nb[4:6])))
		if warPtr, ok := relMap[enemyIdx]; ok && warPtr != 0 {
			seenWars[warPtr] = struct{}{}
		}
		node = binary.LittleEndian.Uint64(nb[16:24])
	}
	return len(seenWars)
}

func (r *Reader) checkFrontEnd(idlerAddr uintptr) (bool, error) {
	vtbl, err := r.readU64(idlerAddr)
	if err != nil || vtbl == 0 {
		return false, errors.New("null idler vtable")
	}
	fnPtr, err := r.readU64(uintptr(vtbl) + VtableOff_IsFrontEnd)
	if err != nil || fnPtr == 0 {
		return false, errors.New("bad IsFrontEnd vtable slot")
	}
	code, err := r.readBytes(uintptr(fnPtr), 3)
	if err != nil {
		return false, err
	}
	// CFrontEnd::IsFrontEnd is mov al, 1; ret
	return code[0] == 0xB0 && code[1] == 0x01 && code[2] == 0xC3, nil
}

func (r *Reader) readCachedRPMap() map[string]string {
	if r.rvas.RPMapHead == 0 || r.rvas.RPMapSize == 0 {
		return nil
	}
	head, err := r.readU64(r.baseAddr + r.rvas.RPMapHead)
	if err != nil || head == 0 {
		return nil
	}
	sz, err := r.readU64(r.baseAddr + r.rvas.RPMapSize)
	if err != nil || sz == 0 || sz > 64 {
		return nil
	}
	root, err := r.readU64(uintptr(head) + 8)
	if err != nil || root == 0 {
		return nil
	}

	out := make(map[string]string, sz)
	visited := make(map[uint64]struct{}, sz)
	var walk func(node uint64)
	walk = func(node uint64) {
		if node == 0 || len(visited) >= 64 {
			return
		}
		if _, ok := visited[node]; ok {
			return
		}
		hdr, err := r.readBytes(uintptr(node), 0x20)
		if err != nil || hdr[0x19] != 0 {
			return
		}
		visited[node] = struct{}{}
		left := binary.LittleEndian.Uint64(hdr[0x00:0x08])
		right := binary.LittleEndian.Uint64(hdr[0x10:0x18])
		walk(left)
		k, errK := r.readMSVCString(uintptr(node) + 0x20)
		v, errV := r.readMSVCString(uintptr(node) + 0x40)
		if errK == nil && errV == nil && k != "" {
			out[k] = v
		}
		walk(right)
	}
	walk(root)
	return out
}

func parseCountryTag(b []byte) (string, int16, bool) {
	if len(b) != 8 {
		return "", 0, false
	}
	if b[6] == 0x07 || b[7] == 0 {
		return "", 0, false
	}
	idx := int16(binary.LittleEndian.Uint16(b[4:6]))
	if idx <= 0 {
		return "", 0, false
	}
	tag := string(b[:3])
	if tag == "---" {
		return "", 0, false
	}
	for i := 0; i < 3; i++ {
		c := tag[i]
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) {
			return "", 0, false
		}
	}
	return strings.ToUpper(tag), idx, true
}

func (r *Reader) ensureAttached() error {
	if r.handle != 0 {
		if r.isProcessAlive() {
			return nil
		}
		r.Close()
	}

	pid, err := findEU4PID()
	if err != nil {
		return err
	}

	h, _, callErr := procOpenProcess.Call(processVMRead|processQueryInfo, 0, uintptr(pid))
	if h == 0 {
		return fmt.Errorf("OpenProcess(pid=%d) failed: %w", pid, callErr)
	}
	handle := syscall.Handle(h)

	base, err := getMainModuleBase(handle)
	if err != nil {
		_, _, _ = procCloseHandle.Call(uintptr(handle))
		return err
	}

	rvas, err := resolveRVAs(base, func(addr uintptr, n int) ([]byte, error) {
		return readProcessBytes(handle, addr, n)
	})
	if err != nil {
		_, _, _ = procCloseHandle.Call(uintptr(handle))
		return err
	}

	exePath := getProcessExePath(handle)
	if (r.locStore == nil || len(r.locStore.Countries) == 0) && exePath != "" {
		if autoLoc, err := loc.LoadFromEU4Dir(filepath.Dir(exePath)); err == nil && len(autoLoc.Countries) > 0 {
			r.locStore = autoLoc
		}
	}

	r.handle = handle
	r.pid = pid
	r.baseAddr = base
	r.exePath = exePath
	r.rvas = rvas
	r.sessionStartUnix = getProcessStartUnix(handle)
	return nil
}

func (r *Reader) isProcessAlive() bool {
	if r.handle == 0 {
		return false
	}
	var code uint32
	ret, _, _ := procGetExitCodeProcess.Call(uintptr(r.handle), uintptr(unsafe.Pointer(&code)))
	return ret != 0 && code == stillActive
}

func findEU4PID() (uint32, error) {
	snap, _, err := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if snap == uintptr(syscall.InvalidHandle) || snap == 0 {
		return 0, fmt.Errorf("CreateToolhelp32Snapshot failed: %w", err)
	}
	defer procCloseHandle.Call(snap)

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))

	ret, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	for ret != 0 {
		name := syscall.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, ExpectedExeName) {
			return entry.ProcessID, nil
		}
		ret, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	}
	return 0, ErrNotRunning
}

func getMainModuleBase(h syscall.Handle) (uintptr, error) {
	var hMods [64]uintptr
	var needed uint32
	ret, _, err := procEnumProcessModulesEx.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&hMods[0])),
		uintptr(len(hMods)*int(unsafe.Sizeof(hMods[0]))),
		uintptr(unsafe.Pointer(&needed)),
		listModulesAll,
	)
	if ret == 0 || needed < 8 || hMods[0] == 0 {
		ret, _, err = procEnumProcessModulesEx.Call(
			uintptr(h),
			uintptr(unsafe.Pointer(&hMods[0])),
			uintptr(len(hMods)*int(unsafe.Sizeof(hMods[0]))),
			uintptr(unsafe.Pointer(&needed)),
			listModules64Bit,
		)
		if ret == 0 || needed < 8 || hMods[0] == 0 {
			return 0, fmt.Errorf("EnumProcessModulesEx failed: %w", err)
		}
	}
	return hMods[0], nil
}

func getProcessExePath(h syscall.Handle) string {
	var buf [512]uint16
	size := uint32(len(buf))
	ret, _, _ := procQueryFullProcessImageNameW.Call(
		uintptr(h),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret == 0 || size == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

func getProcessStartUnix(h syscall.Handle) int64 {
	var creation, exit, kernel, user fileTime
	ret, _, _ := procGetProcessTimes.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if ret == 0 {
		return 0
	}
	ft := (uint64(creation.HighDateTime) << 32) | uint64(creation.LowDateTime)
	const windowsToUnixEpoch100ns = 116444736000000000
	if ft <= windowsToUnixEpoch100ns {
		return 0
	}
	return int64((ft - windowsToUnixEpoch100ns) / 10000000)
}

func readProcessBytes(h syscall.Handle, addr uintptr, size int) ([]byte, error) {
	if h == 0 || addr == 0 || size <= 0 {
		return nil, errors.New("invalid read")
	}
	buf := make([]byte, size)
	var read uintptr
	ret, _, err := procReadProcessMemory.Call(
		uintptr(h),
		addr,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(size),
		uintptr(unsafe.Pointer(&read)),
	)
	if ret == 0 || int(read) != size {
		return nil, fmt.Errorf("ReadProcessMemory(0x%x, %d) failed: %w", addr, size, err)
	}
	return buf, nil
}

func (r *Reader) readBytes(addr uintptr, size int) ([]byte, error) {
	return readProcessBytes(r.handle, addr, size)
}

func (r *Reader) readU64(addr uintptr) (uint64, error) {
	b, err := r.readBytes(addr, 8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

func (r *Reader) readI32(addr uintptr) (int32, error) {
	b, err := r.readBytes(addr, 4)
	if err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(b)), nil
}

func (r *Reader) readU8(addr uintptr) (uint8, error) {
	b, err := r.readBytes(addr, 1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *Reader) readMSVCString(addr uintptr) (string, error) {
	b, err := r.readBytes(addr, 32)
	if err != nil {
		return "", err
	}
	size := binary.LittleEndian.Uint64(b[16:24])
	capVal := binary.LittleEndian.Uint64(b[24:32])
	if size == 0 {
		return "", nil
	}
	if size > 512 || capVal < size {
		return "", fmt.Errorf("invalid std::string size=%d cap=%d", size, capVal)
	}
	var raw []byte
	if capVal >= 16 {
		ptr := binary.LittleEndian.Uint64(b[0:8])
		if ptr == 0 {
			return "", errors.New("null std::string heap pointer")
		}
		raw, err = r.readBytes(uintptr(ptr), int(size))
		if err != nil {
			return "", err
		}
	} else {
		raw = b[:size]
	}
	return decodeEU4String(raw), nil
}

// eu4 uses cp1252 in memory
var cp1252Table = [32]rune{
	0x20AC, 0xFFFD, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0xFFFD, 0x017D, 0xFFFD,
	0xFFFD, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0xFFFD, 0x017E, 0x0178,
}

func decodeEU4String(raw []byte) string {
	if utf8.Valid(raw) {
		return string(raw)
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, c := range raw {
		if c < 0x80 {
			b.WriteByte(c)
		} else if c < 0xA0 {
			b.WriteRune(cp1252Table[c-0x80])
		} else {
			b.WriteRune(rune(c))
		}
	}
	return b.String()
}

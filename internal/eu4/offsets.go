package eu4

// eu4 1.37.5.0 (steam & epic win64) struct offsets
const (
	ExpectedExeName = "eu4.exe"

	// idler / pause
	VtableOff_IsFrontEnd    uintptr = 0x380
	FrontEndOff_State       uintptr = 0x900
	InGameIdlerOff_PauseMgr uintptr = 0x418
	PauseMgrOff_VecBegin    uintptr = 0x00
	PauseMgrOff_VecEnd      uintptr = 0x08
	PauseMgrOff_DirectPause uintptr = 0x48

	// CGameState
	GSOff_NumPlayers    uintptr = 0x0A60
	GSOff_CurrentYear   uintptr = 0x1DDC
	GSOff_CurrentIdler  uintptr = 0x1E00
	GSOff_GameSpeed     uintptr = 0x1E08
	GSOff_GreatPowerMgr uintptr = 0x1E58
	GSOff_PlayerTag     uintptr = 0x1E60
	GSOff_ObservedTag   uintptr = 0x1E68
	GSOff_AgePtr        uintptr = 0x2420
	GSOff_IsIronman     uintptr = 0x24D0
	GSOff_IsMultiplayer uintptr = 0x747C

	// great powers
	GPMgrOff_VecBegin uintptr = 0x10
	GPMgrOff_VecEnd   uintptr = 0x18
	GPEntrySize       int     = 12

	// age
	AgeOff_KeyString uintptr = 0x10

	// country db
	CountryDBOff_ArrayPtr uintptr = 0x118

	// CCountry
	CountryOff_Tag            uintptr = 0x20
	CountryOff_TotalDevRaw    uintptr = 0x0E1C
	CountryOff_WarRelPtr      uintptr = 0x1548
	CountryOff_WarRelCount    uintptr = 0x1554
	CountryOff_WarEnemyHead   uintptr = 0x1B88
	CountryOff_NumProvinces   uintptr = 0x1FC8
	CountryOff_CustomNameFlag uintptr = 0x2680
	CountryOff_CustomNameStr  uintptr = 0x2690
)

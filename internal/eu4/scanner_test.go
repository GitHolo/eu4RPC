package eu4

import (
	"encoding/binary"
	"testing"
)

func putRIPDisp(buf []byte, insOff, dispOff, insLen int, chunkRVA, targetRVA uintptr) {
	insAddr := int64(chunkRVA) + int64(insOff)
	disp := int32(int64(targetRVA) - (insAddr + int64(insLen)))
	binary.LittleEndian.PutUint32(buf[insOff+dispOff:insOff+dispOff+4], uint32(disp))
}

func TestScanTextAcrossChunkBoundaryAndOptionalRPMap(t *testing.T) {
	const (
		textVA   = uintptr(0x1000)
		textSize = uintptr(160)
		dataVA   = uintptr(0x200000)
		dataSize = uintptr(0x50000)

		wantGS  = uintptr(0x215000)
		wantCDB = uintptr(0x219000)
	)

	text := make([]byte, textSize)

	// Place GS1 straddling a 72-byte chunk boundary (offset 68..83)
	gsOff := 68
	copy(text[gsOff:], []byte{0x48, 0x8B, 0x05, 0, 0, 0, 0, 0x8B, 0x98, 0x08, 0x1E, 0x00, 0x00, 0xFF, 0xC3})
	putRIPDisp(text, gsOff, 3, 7, textVA, wantGS)

	// Place CDB1 later in .text (no RPMap -> must still succeed for Epic)
	cdbOff := 110
	copy(text[cdbOff:], []byte{0x4C, 0x8B, 0x1D, 0, 0, 0, 0, 0x45, 0x0F, 0xB6, 0xF0, 0x48, 0x8B, 0x01, 0x0F, 0xB6, 0xF2, 0x48, 0xC1, 0xE8, 0x20})
	putRIPDisp(text, cdbOff, 3, 7, textVA, wantCDB)

	rvas, err := scanText(func(rva uintptr, n int) ([]byte, error) {
		off := int(rva - textVA)
		return text[off : off+n], nil
	}, textVA, textSize, dataVA, dataSize, 72)
	if err != nil {
		t.Fatalf("scanText failed: %v", err)
	}
	if rvas.GameState != wantGS {
		t.Fatalf("GameState = 0x%x, want 0x%x", rvas.GameState, wantGS)
	}
	if rvas.CountryDB != wantCDB {
		t.Fatalf("CountryDB = 0x%x, want 0x%x", rvas.CountryDB, wantCDB)
	}
	if rvas.RPMapHead != 0 || rvas.RPMapSize != 0 {
		t.Fatalf("expected optional RPMap to be 0, got %+v", rvas)
	}
}

func TestScanTextWithRPMapAndFallbackCDB(t *testing.T) {
	const (
		textVA   = uintptr(0x1000)
		textSize = uintptr(256)
		dataVA   = uintptr(0x200000)
		dataSize = uintptr(0x50000)

		wantGS  = uintptr(0x220100)
		wantCDB = uintptr(0x220200)
		wantRP  = uintptr(0x230300)
	)

	text := make([]byte, textSize)

	// GS3
	copy(text[10:], []byte{0x48, 0x8B, 0x05, 0, 0, 0, 0, 0x80, 0xB8, 0xD0, 0x24, 0x00, 0x00, 0x00, 0x74, 0x12, 0xE8})
	putRIPDisp(text, 10, 3, 7, textVA, wantGS)

	// Fallback CDB
	copy(text[50:], []byte{0x48, 0x8B, 0x05, 0, 0, 0, 0, 0x48, 0xC1, 0xE9, 0x20, 0x48, 0x0F, 0xBF, 0xD1, 0x48, 0x8B, 0x88, 0x18, 0x01, 0x00, 0x00, 0x48, 0x8B, 0x04, 0xD1})
	putRIPDisp(text, 50, 3, 7, textVA, wantCDB)

	// RPMap (head + size at head+8)
	copy(text[120:], []byte{0x45, 0x33, 0xED, 0x48, 0x8B, 0x1D, 0, 0, 0, 0, 0x48, 0x8B, 0x05, 0, 0, 0, 0, 0x48, 0x39, 0x45, 0xB8})
	putRIPDisp(text, 120, 6, 10, textVA, wantRP)
	putRIPDisp(text, 120, 13, 17, textVA, wantRP+8)

	rvas, err := scanText(func(rva uintptr, n int) ([]byte, error) {
		off := int(rva - textVA)
		return text[off : off+n], nil
	}, textVA, textSize, dataVA, dataSize, 96)
	if err != nil {
		t.Fatalf("scanText failed: %v", err)
	}
	if rvas.GameState != wantGS || rvas.CountryDB != wantCDB || rvas.RPMapHead != wantRP || rvas.RPMapSize != wantRP+8 {
		t.Fatalf("unexpected RVAs: %+v", rvas)
	}
}

package eu4

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestParseCountryTag(t *testing.T) {
	makeTag := func(tag string, idx uint16, kind, valid byte) []byte {
		b := make([]byte, 8)
		copy(b[:3], tag)
		binary.LittleEndian.PutUint16(b[4:6], idx)
		b[6] = kind
		b[7] = valid
		return b
	}

	tag, idx, ok := parseCountryTag(makeTag("rus", 42, 0x06, 0x01))
	if !ok || tag != "RUS" || idx != 42 {
		t.Fatalf("got (%q, %d, %v), want (RUS, 42, true)", tag, idx, ok)
	}

	cases := [][]byte{
		makeTag("---", 1, 0x06, 0x01),
		makeTag("RUS", 1, 0x07, 0x01), // observing marker
		makeTag("RUS", 1, 0x06, 0x00), // invalid flag byte
		makeTag("RUS", 0, 0x06, 0x01), // zero index
		makeTag("R$S", 1, 0x06, 0x01), // non-alphanumeric
		{1, 2, 3},                     // short slice
	}
	for i, tc := range cases {
		if _, _, ok := parseCountryTag(tc); ok {
			t.Fatalf("case %d unexpectedly succeeded", i)
		}
	}
}

func TestDecodeEU4String(t *testing.T) {
	if got := decodeEU4String([]byte("Münster")); got != "Münster" {
		t.Fatalf("UTF-8 passthrough failed: got %q", got)
	}
	// Windows-1252: euro sign (0x80), en-dash (0x96), ü (0xFC), é (0xE9)
	raw := []byte{0x80, ' ', 0x96, ' ', 'M', 0xFC, 'n', 's', 't', 'e', 'r', 0xE9}
	want := "€ \u2013 Münsteré"
	if got := decodeEU4String(raw); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDecodeMSVCString(t *testing.T) {
	// Short string optimization (cap = 15)
	sso := make([]byte, 32)
	copy(sso, "Ottomans")
	binary.LittleEndian.PutUint64(sso[16:24], 8)
	binary.LittleEndian.PutUint64(sso[24:32], 15)
	got, err := decodeMSVCString(sso, func(uintptr, int) ([]byte, error) {
		return nil, errors.New("unexpected heap read")
	})
	if err != nil || got != "Ottomans" {
		t.Fatalf("SSO failed: got %q, err=%v", got, err)
	}

	// Heap-allocated string (cap >= 16)
	heapStr := make([]byte, 32)
	binary.LittleEndian.PutUint64(heapStr[0:8], 0x12340000)
	binary.LittleEndian.PutUint64(heapStr[16:24], 20)
	binary.LittleEndian.PutUint64(heapStr[24:32], 31)
	got, err = decodeMSVCString(heapStr, func(ptr uintptr, size int) ([]byte, error) {
		if ptr != 0x12340000 || size != 20 {
			t.Fatalf("unexpected heap read args ptr=0x%x size=%d", ptr, size)
		}
		return []byte("age_of_reformation!!"), nil
	})
	if err != nil || got != "age_of_reformation!!" {
		t.Fatalf("heap decode failed: got %q, err=%v", got, err)
	}

	// Null heap pointer
	binary.LittleEndian.PutUint64(heapStr[0:8], 0)
	if _, err := decodeMSVCString(heapStr, nil); err == nil {
		t.Fatal("expected error for null heap pointer")
	}

	// Size larger than capacity
	bad := make([]byte, 32)
	binary.LittleEndian.PutUint64(bad[16:24], 20)
	binary.LittleEndian.PutUint64(bad[24:32], 15)
	if _, err := decodeMSVCString(bad, nil); err == nil {
		t.Fatal("expected error for size > cap")
	}
}

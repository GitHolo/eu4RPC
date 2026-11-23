package eu4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type ResolvedRVAs struct {
	GameState uintptr `json:"game_state"`
	CountryDB uintptr `json:"country_db"`
	RPMapHead uintptr `json:"rp_map_head,omitempty"`
	RPMapSize uintptr `json:"rp_map_size,omitempty"`
}

type aobSig struct {
	pattern  []byte
	mask     []bool
	dispOff  int
	insLen   int
	disp2Off int
	ins2Len  int
}

type peSection struct {
	va   uintptr
	size uintptr
}

const (
	scanChunkSize = 2 * 1024 * 1024
	scanOverlap   = 64
)

var (
	gameStateSigs = []aobSig{
		mustParseSig("48 8B 05 ?? ?? ?? ?? 8B 98 08 1E 00 00 FF C3", 3, 7, 0, 0),
		mustParseSig("48 8B 0D ?? ?? ?? ?? 48 8B 91 20 24 00 00 48 83 C2 10", 3, 7, 0, 0),
		mustParseSig("48 8B 05 ?? ?? ?? ?? 80 B8 D0 24 00 00 00 74 12 E8", 3, 7, 0, 0),
	}
	countryDBPrimarySigs = []aobSig{
		mustParseSig("4C 8B 1D ?? ?? ?? ?? 45 0F B6 F0 48 8B 01 0F B6 F2 48 C1 E8 20", 3, 7, 0, 0),
		mustParseSig("48 8B 15 ?? ?? ?? ?? 49 8B C6 48 C1 E8 20 48 0F BF C8 48 8B 82 18 01 00 00 48 8B 3C C8", 3, 7, 0, 0),
	}
	countryDBFallbackSig = mustParseSig("48 8B 05 ?? ?? ?? ?? 48 C1 E9 20 48 0F BF D1 48 8B 88 18 01 00 00 48 8B 04 D1", 3, 7, 0, 0)
	rpMapSig             = mustParseSig("45 33 ED 48 8B 1D ?? ?? ?? ?? 48 8B 05 ?? ?? ?? ?? 48 39 45 B8", 6, 10, 13, 17)
)

func mustParseSig(spec string, dispOff, insLen, disp2Off, ins2Len int) aobSig {
	parts := strings.Fields(spec)
	pat := make([]byte, len(parts))
	mask := make([]bool, len(parts))
	for i, p := range parts {
		if p == "??" || p == "?" {
			continue
		}
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			panic(err)
		}
		pat[i] = byte(v)
		mask[i] = true
	}
	return aobSig{
		pattern:  pat,
		mask:     mask,
		dispOff:  dispOff,
		insLen:   insLen,
		disp2Off: disp2Off,
		ins2Len:  ins2Len,
	}
}

func resolveRVAs(base uintptr, readMem func(addr uintptr, n int) ([]byte, error)) (ResolvedRVAs, error) {
	hdr, err := readMem(base, 0x1000)
	if err != nil {
		return ResolvedRVAs{}, fmt.Errorf("read PE header: %w", err)
	}
	textSec, dataSec, err := parsePESections(hdr, func(n int) ([]byte, error) {
		return readMem(base, n)
	})
	if err != nil {
		return ResolvedRVAs{}, err
	}
	return scanText(func(rva uintptr, n int) ([]byte, error) {
		return readMem(base+rva, n)
	}, textSec.va, textSec.size, dataSec.va, dataSec.size, scanChunkSize)
}

func parsePESections(hdr []byte, readMore func(int) ([]byte, error)) (peSection, peSection, error) {
	if len(hdr) < 0x40 || hdr[0] != 'M' || hdr[1] != 'Z' {
		return peSection{}, peSection{}, errors.New("invalid MZ header")
	}
	peOff := int(binary.LittleEndian.Uint32(hdr[0x3C:0x40]))
	if peOff < 0 || peOff+24 > len(hdr) {
		return peSection{}, peSection{}, errors.New("invalid PE offset")
	}
	if string(hdr[peOff:peOff+4]) != "PE\x00\x00" {
		return peSection{}, peSection{}, errors.New("invalid PE signature")
	}

	numSec := int(binary.LittleEndian.Uint16(hdr[peOff+6 : peOff+8]))
	optSize := int(binary.LittleEndian.Uint16(hdr[peOff+20 : peOff+22]))
	secTableOff := peOff + 24 + optSize
	secTableEnd := secTableOff + numSec*40
	if numSec <= 0 || numSec > 96 {
		return peSection{}, peSection{}, fmt.Errorf("unexpected section count %d", numSec)
	}
	if secTableEnd > len(hdr) && readMore != nil {
		var err error
		hdr, err = readMore(secTableEnd)
		if err != nil {
			return peSection{}, peSection{}, err
		}
	}
	if secTableEnd > len(hdr) {
		return peSection{}, peSection{}, errors.New("section table exceeds header")
	}

	var textSec, dataSec peSection
	for i := 0; i < numSec; i++ {
		o := secTableOff + i*40
		name := strings.TrimRight(string(hdr[o:o+8]), "\x00")
		vsize := uintptr(binary.LittleEndian.Uint32(hdr[o+8 : o+12]))
		vaddr := uintptr(binary.LittleEndian.Uint32(hdr[o+12 : o+16]))
		rsize := uintptr(binary.LittleEndian.Uint32(hdr[o+16 : o+20]))
		switch name {
		case ".text":
			sz := vsize
			if rsize > 0 && (sz == 0 || rsize < sz) {
				sz = rsize
			}
			textSec = peSection{va: vaddr, size: sz}
		case ".data":
			sz := vsize
			if rsize > sz {
				sz = rsize
			}
			dataSec = peSection{va: vaddr, size: sz}
		}
	}

	if textSec.va == 0 || textSec.size == 0 || dataSec.va == 0 || dataSec.size == 0 {
		return peSection{}, peSection{}, errors.New("missing .text or .data section")
	}
	return textSec, dataSec, nil
}

func scanText(
	readChunk func(rva uintptr, n int) ([]byte, error),
	textVA, textSize, dataVA, dataSize, chunkSize uintptr,
) (ResolvedRVAs, error) {
	if chunkSize < scanOverlap {
		chunkSize = scanOverlap
	}
	var out ResolvedRVAs
	var cdbFallback uintptr

	for offset := uintptr(0); offset < textSize; offset += chunkSize {
		if out.GameState != 0 && out.CountryDB != 0 && out.RPMapHead != 0 {
			break
		}

		start := offset
		if start >= scanOverlap {
			start -= scanOverlap
		} else {
			start = 0
		}
		end := offset + chunkSize
		if end > textSize {
			end = textSize
		}

		chunkRVA := textVA + start
		buf, err := readChunk(chunkRVA, int(end-start))
		if err != nil {
			return ResolvedRVAs{}, fmt.Errorf("scan .text at 0x%x: %w", chunkRVA, err)
		}

		if out.GameState == 0 {
			for _, sig := range gameStateSigs {
				if target, ok := findSigTarget(buf, chunkRVA, sig, dataVA, dataSize); ok {
					out.GameState = target
					break
				}
			}
		}
		if out.CountryDB == 0 {
			for _, sig := range countryDBPrimarySigs {
				if target, ok := findSigTarget(buf, chunkRVA, sig, dataVA, dataSize); ok {
					out.CountryDB = target
					break
				}
			}
			if out.CountryDB == 0 && cdbFallback == 0 {
				if target, ok := findSigTarget(buf, chunkRVA, countryDBFallbackSig, dataVA, dataSize); ok {
					cdbFallback = target
				}
			}
		}
		if out.RPMapHead == 0 {
			if target, ok := findSigTarget(buf, chunkRVA, rpMapSig, dataVA, dataSize); ok {
				out.RPMapHead = target
				out.RPMapSize = target + 8
			}
		}
	}

	if out.CountryDB == 0 {
		out.CountryDB = cdbFallback
	}

	if out.GameState == 0 || out.CountryDB == 0 {
		return ResolvedRVAs{}, fmt.Errorf("pattern scan failed (gs=0x%x cdb=0x%x)", out.GameState, out.CountryDB)
	}
	return out, nil
}

func findSigTarget(buf []byte, chunkRVA uintptr, sig aobSig, dataVA, dataSize uintptr) (uintptr, bool) {
	patLen := len(sig.pattern)
	if len(buf) < patLen {
		return 0, false
	}
	limit := len(buf) - patLen
	for i := 0; i <= limit; i++ {
		if !matchSigAt(buf, i, sig) {
			continue
		}
		target, ok := resolveRIP(chunkRVA, i, buf, sig.dispOff, sig.insLen)
		if !ok || !inRange(target, dataVA, dataSize) {
			continue
		}
		if sig.disp2Off > 0 {
			target2, ok2 := resolveRIP(chunkRVA, i, buf, sig.disp2Off, sig.ins2Len)
			if !ok2 || target2 != target+8 || !inRange(target2, dataVA, dataSize) {
				continue
			}
		}
		return target, true
	}
	return 0, false
}

func matchSigAt(buf []byte, idx int, sig aobSig) bool {
	for j, b := range sig.pattern {
		if sig.mask[j] && buf[idx+j] != b {
			return false
		}
	}
	return true
}

func resolveRIP(chunkRVA uintptr, idx int, buf []byte, dispOff, insLen int) (uintptr, bool) {
	if idx+dispOff+4 > len(buf) {
		return 0, false
	}
	disp := int32(binary.LittleEndian.Uint32(buf[idx+dispOff : idx+dispOff+4]))
	addr := int64(chunkRVA) + int64(idx) + int64(insLen) + int64(disp)
	if addr <= 0 {
		return 0, false
	}
	return uintptr(addr), true
}

func inRange(addr, start, size uintptr) bool {
	return addr >= start && addr < start+size
}

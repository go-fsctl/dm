// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import (
	"encoding/binary"
	"testing"
)

// TestEncodeSingleLinear verifies the byte-exact layout of a one-target table
// for a "linear" target, against the dm_target_spec wire format.
func TestEncodeSingleLinear(t *testing.T) {
	tgt := Linear(0, 131072, "/dev/loop0", 0) // 64 MiB / 512 = 131072 sectors
	if tgt.Type != "linear" || tgt.Params != "/dev/loop0 0" {
		t.Fatalf("Linear built %+v", tgt)
	}

	buf, err := encodeTargets([]Target{tgt})
	if err != nil {
		t.Fatalf("encodeTargets: %v", err)
	}

	specSize := int(abiSizeofDMTargetSpec) // 40
	params := "/dev/loop0 0"
	wantLen := align8(specSize + len(params) + 1)
	if len(buf) != wantLen {
		t.Errorf("encoded length = %d, want %d (8-byte aligned)", len(buf), wantLen)
	}
	if len(buf)%8 != 0 {
		t.Errorf("encoded length %d not 8-byte aligned", len(buf))
	}

	sectorStart := binary.NativeEndian.Uint64(buf[0:8])
	length := binary.NativeEndian.Uint64(buf[8:16])
	next := binary.NativeEndian.Uint32(buf[20:24])
	if sectorStart != 0 {
		t.Errorf("sector_start = %d, want 0", sectorStart)
	}
	if length != 131072 {
		t.Errorf("length = %d, want 131072", length)
	}
	if int(next) != wantLen {
		t.Errorf("next = %d, want %d", next, wantLen)
	}

	gotType := cstr(buf[24 : 24+dmMaxTypeName])
	if gotType != "linear" {
		t.Errorf("target_type = %q, want %q", gotType, "linear")
	}
	gotParams := cstr(buf[specSize:])
	if gotParams != params {
		t.Errorf("params = %q, want %q", gotParams, params)
	}
	// The byte immediately after params must be a NUL terminator.
	if buf[specSize+len(params)] != 0 {
		t.Errorf("params not NUL-terminated")
	}
}

// TestEncodeMultiTarget verifies that multiple targets are laid out
// back-to-back, each 8-byte aligned, with correct per-spec Next offsets, and
// that parseTargets round-trips them (parseTargets reads Next as offset from
// the first spec, the DM_TABLE_STATUS convention).
func TestEncodeMultiTarget(t *testing.T) {
	in := []Target{
		Linear(0, 100, "/dev/loop0", 0),
		Linear(100, 200, "/dev/loop1", 50),
	}
	buf, err := encodeTargets(in)
	if err != nil {
		t.Fatalf("encodeTargets: %v", err)
	}

	// First spec Next is the size of the first record.
	rec0 := align8(int(abiSizeofDMTargetSpec) + len("/dev/loop0 0") + 1)
	rec1 := align8(int(abiSizeofDMTargetSpec) + len("/dev/loop1 50") + 1)
	if got := binary.NativeEndian.Uint32(buf[20:24]); int(got) != rec0 {
		t.Errorf("spec0.next = %d, want %d", got, rec0)
	}
	if got := binary.NativeEndian.Uint32(buf[rec0+20 : rec0+24]); int(got) != rec1 {
		t.Errorf("spec1.next = %d, want %d", got, rec1)
	}
	if len(buf) != rec0+rec1 {
		t.Errorf("total len = %d, want %d", len(buf), rec0+rec1)
	}

	// To parse the LOAD-form buffer with parseTargets (STATUS convention,
	// Next from the first spec), rewrite specN.next as cumulative offsets.
	// Build a STATUS-style buffer to exercise parseTargets independently.
	statusBuf := make([]byte, len(buf))
	copy(statusBuf, buf)
	binary.NativeEndian.PutUint32(statusBuf[20:24], uint32(rec0)) // offset to spec1
	binary.NativeEndian.PutUint32(statusBuf[rec0+20:rec0+24], 0)  // last
	out, err := parseTargets(statusBuf, 0, 2)
	if err != nil {
		t.Fatalf("parseTargets: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("parseTargets returned %d targets, want 2", len(out))
	}
	if out[0].SectorStart != 0 || out[0].Length != 100 || out[0].Type != "linear" || out[0].Params != "/dev/loop0 0" {
		t.Errorf("target0 = %+v", out[0])
	}
	if out[1].SectorStart != 100 || out[1].Length != 200 || out[1].Type != "linear" || out[1].Params != "/dev/loop1 50" {
		t.Errorf("target1 = %+v", out[1])
	}
}

// TestEncodeRejectsLongType ensures an over-length target type is rejected.
func TestEncodeRejectsLongType(t *testing.T) {
	_, err := encodeTargets([]Target{{Type: "0123456789abcdef", Params: ""}}) // 16 chars, no room for NUL
	if err == nil {
		t.Fatal("expected error for over-length target type")
	}
}

// TestParseNameList round-trips a synthetic DM_LIST_DEVICES buffer.
func TestParseNameList(t *testing.T) {
	headSize := dmNameListHeadSize // 12
	mk := func(dev uint64, next uint32, name string) []byte {
		raw := headSize + len(name) + 1
		rec := make([]byte, align8(raw))
		binary.NativeEndian.PutUint64(rec[0:8], dev)
		binary.NativeEndian.PutUint32(rec[8:12], next)
		copy(rec[headSize:], name)
		return rec
	}
	r0 := mk(0, 0, "vol-a")
	binary.NativeEndian.PutUint32(r0[8:12], uint32(len(r0))) // next -> r1
	r1 := mk(0, 0, "vol-b")
	buf := append(r0, r1...)

	devs, err := parseNameList(buf, 0)
	if err != nil {
		t.Fatalf("parseNameList: %v", err)
	}
	if len(devs) != 2 || devs[0].Name != "vol-a" || devs[1].Name != "vol-b" {
		t.Fatalf("parseNameList = %+v", devs)
	}
}

// TestTargetString checks the four-column dmsetup-style rendering.
func TestTargetString(t *testing.T) {
	got := Target{SectorStart: 0, Length: 2048, Type: "linear", Params: "/dev/loop0 0"}.String()
	if got != "0 2048 linear /dev/loop0 0" {
		t.Fatalf("Target.String() = %q", got)
	}
}

// TestDevInfoFlags exercises every DevInfo flag accessor against a value with
// all the relevant bits set, and against the zero value.
func TestDevInfoFlags(t *testing.T) {
	all := DevInfo{Flags: DM_SUSPEND_FLAG | DM_READONLY_FLAG | DM_ACTIVE_PRESENT_FLAG | DM_INACTIVE_PRESENT_FLAG}
	if !all.Suspended() || !all.ReadOnly() || !all.ActivePresent() || !all.InactivePresent() {
		t.Fatalf("set: %+v", all)
	}
	var none DevInfo
	if none.Suspended() || none.ReadOnly() || none.ActivePresent() || none.InactivePresent() {
		t.Fatalf("clear: %+v", none)
	}
}

// TestInfoMajorMinor checks the dev_t decoding against a known huge-dev_t.
func TestInfoMajorMinor(t *testing.T) {
	// major 253, minor 0 -> huge dev_t = (253<<8)|0 = 0xfd00 within low 32 bits
	dev := uint64(253<<8 | 0)
	i := DevInfo{Dev: dev}
	if i.Major() != 253 {
		t.Errorf("Major() = %d, want 253", i.Major())
	}
	if i.Minor() != 0 {
		t.Errorf("Minor() = %d, want 0", i.Minor())
	}
}

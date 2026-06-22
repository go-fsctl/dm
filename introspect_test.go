// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import (
	"testing"
	"unsafe"
)

// TestDMTargetVersionsSize pins the on-wire layout of struct dm_target_versions
// against the kernel C layout: {u32 next; u32 version[3]; char name[]}. All
// members are 32-bit, so Go's struct size (16) equals the C offset of name[].
func TestDMTargetVersionsSize(t *testing.T) {
	if got := abiSizeofDMTargetVersions; got != 16 {
		t.Errorf("sizeof(struct dm_target_versions head) = %d, want 16", got)
	}
	if dmTargetVersionsHeadSize != 16 {
		t.Errorf("dmTargetVersionsHeadSize = %d, want 16 (offset of name[])", dmTargetVersionsHeadSize)
	}
}

// TestDMTargetVersionsOffsets pins the field offsets inside the struct so an
// accidental reordering is caught.
func TestDMTargetVersionsOffsets(t *testing.T) {
	var s structDMTargetVersions
	off := func(p unsafe.Pointer) uintptr {
		return uintptr(p) - uintptr(unsafe.Pointer(&s))
	}
	if got := off(unsafe.Pointer(&s.Next)); got != 0 {
		t.Errorf("offsetof(next) = %d, want 0", got)
	}
	if got := off(unsafe.Pointer(&s.Version)); got != 4 {
		t.Errorf("offsetof(version) = %d, want 4", got)
	}
}

// TestListVersionsIoctlNumber pins DM_LIST_VERSIONS specifically (cmd ordinal 13)
// and DM_DEV_WAIT (cmd ordinal 8), the two introspection ioctls.
func TestListVersionsIoctlNumber(t *testing.T) {
	const base = uintptr(0xc138fd00)
	if DM_LIST_VERSIONS != base+13 {
		t.Errorf("DM_LIST_VERSIONS = %#x, want %#x", DM_LIST_VERSIONS, base+13)
	}
	if dmListVersionsCmd != 13 {
		t.Errorf("dmListVersionsCmd = %d, want 13", dmListVersionsCmd)
	}
	if DM_DEV_WAIT != base+8 {
		t.Errorf("DM_DEV_WAIT = %#x, want %#x", DM_DEV_WAIT, base+8)
	}
	if dmDevWaitCmd != 8 {
		t.Errorf("dmDevWaitCmd = %d, want 8", dmDevWaitCmd)
	}
}

// TestParseTargetVersions walks a hand-built two-record dm_target_versions chain
// and checks the names and versions decode, exercising the chain-following and
// the short-tail break.
func TestParseTargetVersions(t *testing.T) {
	headSize := dmTargetVersionsHeadSize
	mk := func(next uint32, ver [3]uint32, name string) []byte {
		rec := make([]byte, align8(headSize+len(name)+1))
		*(*structDMTargetVersions)(unsafe.Pointer(&rec[0])) = structDMTargetVersions{Next: next, Version: ver}
		copy(rec[headSize:], name)
		return rec
	}
	r0 := mk(0, [3]uint32{1, 2, 3}, "linear")
	// patch r0.next to point at r1.
	*(*uint32)(unsafe.Pointer(&r0[0])) = uint32(len(r0))
	r1 := mk(0, [3]uint32{1, 15, 0}, "raid")
	buf := append(r0, r1...)

	tvs, err := parseTargetVersions(buf, 0)
	if err != nil {
		t.Fatalf("parseTargetVersions: %v", err)
	}
	if len(tvs) != 2 {
		t.Fatalf("got %d versions, want 2: %+v", len(tvs), tvs)
	}
	if tvs[0].Name != "linear" || tvs[0].Version.String() != "1.2.3" {
		t.Errorf("tvs[0] = %+v, want linear 1.2.3", tvs[0])
	}
	if tvs[1].Name != "raid" || tvs[1].Version.String() != "1.15.0" {
		t.Errorf("tvs[1] = %+v, want raid 1.15.0", tvs[1])
	}
}

// TestParseTargetVersionsShortTail covers the off+headSize > len(b) break (a
// trailing partial record is ignored) and the empty-name skip.
func TestParseTargetVersionsShortTail(t *testing.T) {
	tvs, err := parseTargetVersions(make([]byte, 4), 0) // shorter than one head
	if err != nil {
		t.Fatalf("parseTargetVersions: %v", err)
	}
	if len(tvs) != 0 {
		t.Fatalf("want no versions, got %+v", tvs)
	}

	// A single zeroed record (empty name, next 0) — the kernel's "empty list".
	empty := make([]byte, align8(dmTargetVersionsHeadSize+1))
	tvs, err = parseTargetVersions(empty, 0)
	if err != nil {
		t.Fatalf("parseTargetVersions empty: %v", err)
	}
	if len(tvs) != 0 {
		t.Fatalf("want no versions for empty record, got %+v", tvs)
	}
}

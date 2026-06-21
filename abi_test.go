// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import (
	"testing"
	"unsafe"
)

// TestDMIoctlNumbers pins the DM_* request numbers against the values produced
// by the dm-ioctl.h _IOWR macros on a generic-ioctl architecture (x86_64,
// arm64, ...). These are computed as _IOWR(0xfd, cmd, sizeof(struct dm_ioctl))
// with sizeof == 312, dir == 3 (read|write).
//
// Expected hex derived from: (3<<30) | (312<<16) | (0xfd<<8) | cmd
//
//	312 == 0x138, so the size field contributes 0x0138_0000
//	0xfd<<8 == 0xfd00
//	dir 3<<30 == 0xC000_0000
//
// => base 0xC138_FD00 + cmd
func TestDMIoctlNumbers(t *testing.T) {
	if dmIOCTLSize != 312 {
		t.Fatalf("sizeof(struct dm_ioctl) = %d, want 312; ioctl numbers below assume 312", dmIOCTLSize)
	}
	const base = 0xc138fd00
	cases := []struct {
		name string
		got  uintptr
		cmd  uintptr
	}{
		{"DM_VERSION", DM_VERSION, 0},
		{"DM_REMOVE_ALL", DM_REMOVE_ALL, 1},
		{"DM_LIST_DEVICES", DM_LIST_DEVICES, 2},
		{"DM_DEV_CREATE", DM_DEV_CREATE, 3},
		{"DM_DEV_REMOVE", DM_DEV_REMOVE, 4},
		{"DM_DEV_RENAME", DM_DEV_RENAME, 5},
		{"DM_DEV_SUSPEND", DM_DEV_SUSPEND, 6},
		{"DM_DEV_STATUS", DM_DEV_STATUS, 7},
		{"DM_DEV_WAIT", DM_DEV_WAIT, 8},
		{"DM_TABLE_LOAD", DM_TABLE_LOAD, 9},
		{"DM_TABLE_CLEAR", DM_TABLE_CLEAR, 10},
		{"DM_TABLE_DEPS", DM_TABLE_DEPS, 11},
		{"DM_TABLE_STATUS", DM_TABLE_STATUS, 12},
		{"DM_LIST_VERSIONS", DM_LIST_VERSIONS, 13},
		{"DM_TARGET_MSG", DM_TARGET_MSG, 14},
		{"DM_DEV_SET_GEOMETRY", DM_DEV_SET_GEOMETRY, 15},
		{"DM_DEV_ARM_POLL", DM_DEV_ARM_POLL, 16},
		{"DM_GET_TARGET_VERSION", DM_GET_TARGET_VERSION, 17},
	}
	for _, c := range cases {
		want := uintptr(base) + c.cmd
		if c.got != want {
			t.Errorf("%s = %#x, want %#x", c.name, c.got, want)
		}
	}
}

// TestDMTargetMsgNumber pins DM_TARGET_MSG specifically: it is the request
// number thin provisioning rides on, so it gets its own assertion in addition
// to the table above. cmd ordinal is 14 (DM_TARGET_MSG_CMD), giving
// base 0xc138fd00 + 14.
func TestDMTargetMsgNumber(t *testing.T) {
	const want = uintptr(0xc138fd00) + 14
	if DM_TARGET_MSG != want {
		t.Errorf("DM_TARGET_MSG = %#x, want %#x", DM_TARGET_MSG, want)
	}
	if dmTargetMsgCmd != 14 {
		t.Errorf("dmTargetMsgCmd = %d, want 14", dmTargetMsgCmd)
	}
}

// TestStructSizes pins the on-wire struct sizes against the kernel C layout.
func TestStructSizes(t *testing.T) {
	if got := abiSizeofDMIoctl; got != 312 {
		t.Errorf("sizeof(struct dm_ioctl) = %d, want 312", got)
	}
	if got := abiSizeofDMTargetSpec; got != 40 {
		t.Errorf("sizeof(struct dm_target_spec) = %d, want 40", got)
	}
	// struct dm_target_msg is {u64 sector; char message[]}; the fixed head is
	// the 8-byte sector, with message[] at offset 8.
	if got := abiSizeofDMTargetMsg; got != 8 {
		t.Errorf("sizeof(struct dm_target_msg head) = %d, want 8", got)
	}
	if dmTargetMsgHeadSize != 8 {
		t.Errorf("dmTargetMsgHeadSize = %d, want 8 (offset of message[])", dmTargetMsgHeadSize)
	}
	// Go pads struct{u64;u32} up to 16, but the kernel places the flexible
	// name[] member at offset 12; parseNameList uses dmNameListHeadSize for
	// that, which is what must be 12.
	if got := abiSizeofDMNameList; got != 16 {
		t.Errorf("Go sizeof(structDMNameList) = %d, want 16 (u64-aligned)", got)
	}
	if dmNameListHeadSize != 12 {
		t.Errorf("dmNameListHeadSize = %d, want 12 (offset of name[])", dmNameListHeadSize)
	}
}

// TestDMIoctlOffsets pins the offset of every field in struct dm_ioctl so an
// accidental reordering or padding change is caught.
func TestDMIoctlOffsets(t *testing.T) {
	var h structDMIoctl
	off := func(p unsafe.Pointer) uintptr {
		return uintptr(p) - uintptr(unsafe.Pointer(&h))
	}
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"version", off(unsafe.Pointer(&h.Version)), 0},
		{"data_size", off(unsafe.Pointer(&h.DataSize)), 12},
		{"data_start", off(unsafe.Pointer(&h.DataStart)), 16},
		{"target_count", off(unsafe.Pointer(&h.TargetCount)), 20},
		{"open_count", off(unsafe.Pointer(&h.OpenCount)), 24},
		{"flags", off(unsafe.Pointer(&h.Flags)), 28},
		{"event_nr", off(unsafe.Pointer(&h.EventNr)), 32},
		{"padding", off(unsafe.Pointer(&h.Padding)), 36},
		{"dev", off(unsafe.Pointer(&h.Dev)), 40},
		{"name", off(unsafe.Pointer(&h.Name)), 48},
		{"uuid", off(unsafe.Pointer(&h.UUID)), 176},
		{"data", off(unsafe.Pointer(&h.Data)), 305},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("offsetof(dm_ioctl.%s) = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestDMTargetSpecOffsets pins the offsets inside struct dm_target_spec.
func TestDMTargetSpecOffsets(t *testing.T) {
	var s structDMTargetSpec
	off := func(p unsafe.Pointer) uintptr {
		return uintptr(p) - uintptr(unsafe.Pointer(&s))
	}
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"sector_start", off(unsafe.Pointer(&s.SectorStart)), 0},
		{"length", off(unsafe.Pointer(&s.Length)), 8},
		{"status", off(unsafe.Pointer(&s.Status)), 16},
		{"next", off(unsafe.Pointer(&s.Next)), 20},
		{"target_type", off(unsafe.Pointer(&s.TargetType)), 24},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("offsetof(dm_target_spec.%s) = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestVersionString checks the human-readable version formatting.
func TestVersionString(t *testing.T) {
	v := DMVersion{Major: 4, Minor: 48, Patch: 0}
	if got := v.String(); got != "4.48.0" {
		t.Errorf("DMVersion.String() = %q, want %q", got, "4.48.0")
	}
}

// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

// Package dm drives the Linux device-mapper directly through the
// /dev/mapper/control character device and the DM_* ioctls. It speaks the
// struct dm_ioctl protocol itself: no cgo, and no shelling out to dmsetup.
//
// dm is the device-mapper member of the go-fsctl family of pure-Go kernel
// control libraries (alongside go-fsctl/zfs, go-fsctl/btrfs and
// go-fsctl/loop). The kernel control path only exists on Linux; on other
// platforms every operation returns ErrUnsupported, while the ABI definitions
// in abi.go remain available for tooling and tests.
//
// A device-mapper device is created in three steps that mirror the kernel's
// active/inactive table model:
//
//	dm.Create("vol", "")                 // empty suspended device
//	dm.LoadTable("vol", []dm.Target{...}) // table into the inactive slot
//	dm.Resume("vol")                     // swap inactive -> active, resume IO
//
// and torn down with dm.Remove("vol").
package dm

import "fmt"

// Target is one row of a device-mapper table: a contiguous run of sectors
// [SectorStart, SectorStart+Length) handled by the target named Type, with a
// type-specific parameter string Params.
//
// For the "linear" target, Params is "<device> <offset-in-sectors>", e.g.
// "/dev/loop0 0" or "253:0 2048". The Target type is intentionally generic so
// callers can drive any kernel target ("striped", "snapshot", "crypt",
// "thin-pool", ...) by supplying the right Type and Params; the library does
// not interpret Params beyond serializing it.
type Target struct {
	SectorStart uint64 // first sector this target covers
	Length      uint64 // number of 512-byte sectors
	Type        string // target type, e.g. "linear" (max 15 chars + NUL)
	Params      string // target-specific parameter string
}

// String renders a Target in the same four-column form dmsetup uses for table
// rows: "<start> <length> <type> <params>".
func (t Target) String() string {
	return fmt.Sprintf("%d %d %s %s", t.SectorStart, t.Length, t.Type, t.Params)
}

// DevInfo is the decoded result of DM_DEV_STATUS for one device. (The accessor
// Info(name) returns this; the type is named DevInfo so it does not collide
// with the function.)
type DevInfo struct {
	Name      string // device name (as under /dev/mapper)
	UUID      string // device uuid, empty if none was set
	Dev       uint64 // dev_t of the mapped block device (see Major/Minor)
	OpenCount int32  // number of openers of the device
	EventNr   uint32 // current event number
	TargetCnt uint32 // number of targets in the active table
	Flags     uint32 // DM_*_FLAG bits reported by the kernel
}

// Suspended reports whether DM_SUSPEND_FLAG is set.
func (i DevInfo) Suspended() bool { return i.Flags&DM_SUSPEND_FLAG != 0 }

// ReadOnly reports whether DM_READONLY_FLAG is set.
func (i DevInfo) ReadOnly() bool { return i.Flags&DM_READONLY_FLAG != 0 }

// ActivePresent reports whether the device has an active table.
func (i DevInfo) ActivePresent() bool { return i.Flags&DM_ACTIVE_PRESENT_FLAG != 0 }

// InactivePresent reports whether the device has an inactive table loaded.
func (i DevInfo) InactivePresent() bool { return i.Flags&DM_INACTIVE_PRESENT_FLAG != 0 }

// Major returns the major number of the device's dev_t, decoded with the
// kernel's "huge" dev_t layout (the same MAJOR() libdevmapper uses).
func (i DevInfo) Major() uint32 {
	return uint32((i.Dev>>8)&0xfff) | uint32((i.Dev>>32)&^uint64(0xfff))
}

// Minor returns the minor number of the device's dev_t.
func (i DevInfo) Minor() uint32 {
	return uint32(i.Dev&0xff) | uint32((i.Dev>>12)&^uint64(0xff))
}

// Device is one entry returned by List: a mapped device's name and dev_t.
type Device struct {
	Name string
	Dev  uint64
}

// Version holds a device-mapper interface version triple {major, minor, patch}.
type DMVersion struct {
	Major uint32
	Minor uint32
	Patch uint32
}

// String formats the version as "major.minor.patch".
func (v DMVersion) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

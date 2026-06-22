// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import (
	"fmt"
	"unsafe"
)

// The device-mapper ioctl interface is defined in the kernel uapi header
// linux/dm-ioctl.h. Every command travels through a single character device,
// /dev/mapper/control, and shares one argument structure, struct dm_ioctl,
// optionally followed by a payload (target specs, a name list, status text,
// ...). golang.org/x/sys/unix exposes essentially nothing for device-mapper,
// so this package mirrors the ABI in pure Go and derives the DM_* request
// numbers from first principles, the way the C macros do.
//
// The request numbers are built with the classic _IOWR encoding:
//
//	#define DM_IOCTL 0xfd
//	#define DM_VERSION _IOWR(DM_IOCTL, DM_VERSION_CMD, struct dm_ioctl)
//	...
//
// _IOWR(type, nr, size) lays out as:
//
//	(dir << 30) | (size << 16) | (type << 8) | nr
//
// with dir = 3 (read|write) for every DM command, type = 0xfd, and size =
// sizeof(struct dm_ioctl). The size field is part of the request number, so it
// must match the kernel's notion of sizeof(struct dm_ioctl) exactly; that is
// pinned by abi_test.go against the documented layout.

// dmIOCTL is the device-mapper ioctl type byte (DM_IOCTL in dm-ioctl.h).
const dmIOCTL = 0xfd

// _IOC direction bits, matching the asm-generic ioctl encoding used on all the
// architectures device-mapper runs on (the generic layout is shared by x86,
// arm64, etc.; mips/parisc/sparc/alpha use a different one, which device-mapper
// userspace also handles via these same generic numbers in practice).
const (
	iocNone  = 0
	iocWrite = 1
	iocRead  = 2

	iocNRBits   = 8
	iocTypeBits = 8
	iocSizeBits = 14
	iocDirBits  = 2

	iocNRShift   = 0
	iocTypeShift = iocNRShift + iocNRBits
	iocSizeShift = iocTypeShift + iocTypeBits
	iocDirShift  = iocSizeShift + iocSizeBits
)

// ioc reproduces the kernel _IOC(dir, type, nr, size) macro.
func ioc(dir, typ, nr, size uintptr) uintptr {
	return (dir << iocDirShift) |
		(typ << iocTypeShift) |
		(nr << iocNRShift) |
		(size << iocSizeShift)
}

// iowr reproduces _IOWR(type, nr, size).
func iowr(typ, nr, size uintptr) uintptr {
	return ioc(iocRead|iocWrite, typ, nr, size)
}

// Command ordinals from the anonymous enum in dm-ioctl.h. The order is
// load-bearing: it is the second argument to _IOWR and must not be reordered.
const (
	dmVersionCmd = iota
	dmRemoveAllCmd
	dmListDevicesCmd

	dmDevCreateCmd
	dmDevRemoveCmd
	dmDevRenameCmd
	dmDevSuspendCmd
	dmDevStatusCmd
	dmDevWaitCmd

	dmTableLoadCmd
	dmTableClearCmd
	dmTableDepsCmd
	dmTableStatusCmd

	dmListVersionsCmd
	dmTargetMsgCmd
	dmDevSetGeometryCmd
	dmDevArmPollCmd
	dmGetTargetVersionCmd
)

// dmIOCTLSize is sizeof(struct dm_ioctl). It is folded into every DM_* request
// number. See structDMIoctl for the layout it is derived from.
const dmIOCTLSize = unsafe.Sizeof(structDMIoctl{})

// DM_* request numbers, derived exactly as the dm-ioctl.h macros do. They are
// computed (not hard-coded) so the derivation is self-documenting and pinned in
// abi_test.go.
var (
	DM_VERSION      = iowr(dmIOCTL, dmVersionCmd, dmIOCTLSize)
	DM_REMOVE_ALL   = iowr(dmIOCTL, dmRemoveAllCmd, dmIOCTLSize)
	DM_LIST_DEVICES = iowr(dmIOCTL, dmListDevicesCmd, dmIOCTLSize)

	DM_DEV_CREATE   = iowr(dmIOCTL, dmDevCreateCmd, dmIOCTLSize)
	DM_DEV_REMOVE   = iowr(dmIOCTL, dmDevRemoveCmd, dmIOCTLSize)
	DM_DEV_RENAME   = iowr(dmIOCTL, dmDevRenameCmd, dmIOCTLSize)
	DM_DEV_SUSPEND  = iowr(dmIOCTL, dmDevSuspendCmd, dmIOCTLSize)
	DM_DEV_STATUS   = iowr(dmIOCTL, dmDevStatusCmd, dmIOCTLSize)
	DM_DEV_WAIT     = iowr(dmIOCTL, dmDevWaitCmd, dmIOCTLSize)
	DM_DEV_ARM_POLL = iowr(dmIOCTL, dmDevArmPollCmd, dmIOCTLSize)

	DM_TABLE_LOAD   = iowr(dmIOCTL, dmTableLoadCmd, dmIOCTLSize)
	DM_TABLE_CLEAR  = iowr(dmIOCTL, dmTableClearCmd, dmIOCTLSize)
	DM_TABLE_DEPS   = iowr(dmIOCTL, dmTableDepsCmd, dmIOCTLSize)
	DM_TABLE_STATUS = iowr(dmIOCTL, dmTableStatusCmd, dmIOCTLSize)

	DM_LIST_VERSIONS      = iowr(dmIOCTL, dmListVersionsCmd, dmIOCTLSize)
	DM_TARGET_MSG         = iowr(dmIOCTL, dmTargetMsgCmd, dmIOCTLSize)
	DM_DEV_SET_GEOMETRY   = iowr(dmIOCTL, dmDevSetGeometryCmd, dmIOCTLSize)
	DM_GET_TARGET_VERSION = iowr(dmIOCTL, dmGetTargetVersionCmd, dmIOCTLSize)
)

// Compiled-in interface version, from DM_VERSION_{MAJOR,MINOR,PATCHLEVEL} in
// dm-ioctl.h. Clients must fill dm_ioctl.version with the version they were
// built against; the kernel negotiates and writes its own back. We send the
// major and a zero minor/patch (the conservative, widely-compatible choice that
// libdevmapper itself uses: {4, 0, 0}); the kernel reports its real version on
// return via Version().
const (
	DM_VERSION_MAJOR      = 4
	DM_VERSION_MINOR      = 0
	DM_VERSION_PATCHLEVEL = 0
)

// Sizes of the fixed-width name/uuid/type fields, from dm-ioctl.h.
const (
	dmNameLen     = 128 // DM_NAME_LEN
	dmUUIDLen     = 129 // DM_UUID_LEN
	dmMaxTypeName = 16  // DM_MAX_TYPE_NAME
)

// dm_ioctl flag bits (dm-ioctl.h). Exported so callers and tests can reference
// them without depending on x/sys.
const (
	DM_READONLY_FLAG             = 1 << 0  // In/Out
	DM_SUSPEND_FLAG              = 1 << 1  // In/Out: set => suspend, clear => resume
	DM_PERSISTENT_DEV_FLAG       = 1 << 3  // In
	DM_STATUS_TABLE_FLAG         = 1 << 4  // In: STATUS returns the table, not status
	DM_ACTIVE_PRESENT_FLAG       = 1 << 5  // Out
	DM_INACTIVE_PRESENT_FLAG     = 1 << 6  // Out
	DM_BUFFER_FULL_FLAG          = 1 << 8  // Out: buffer too small, retry larger
	DM_SKIP_BDGET_FLAG           = 1 << 9  // In (ignored)
	DM_SKIP_LOCKFS_FLAG          = 1 << 10 // In
	DM_NOFLUSH_FLAG              = 1 << 11 // In
	DM_QUERY_INACTIVE_TABLE_FLAG = 1 << 12 // In
	DM_UEVENT_GENERATED_FLAG     = 1 << 13 // Out
	DM_UUID_FLAG                 = 1 << 14 // In
	DM_SECURE_DATA_FLAG          = 1 << 15 // In
	DM_DATA_OUT_FLAG             = 1 << 16 // Out
	DM_DEFERRED_REMOVE           = 1 << 17 // In/Out
	DM_INTERNAL_SUSPEND_FLAG     = 1 << 18 // Out
)

// structDMIoctl mirrors struct dm_ioctl from dm-ioctl.h, field for field. The
// trailing data[7] is the "padding or data" byte array the kernel documents;
// it does not hold the real payload, which we append in our own larger buffer.
//
//	struct dm_ioctl {
//		__u32 version[3];
//		__u32 data_size;
//		__u32 data_start;
//		__u32 target_count;
//		__s32 open_count;
//		__u32 flags;
//		__u32 event_nr;
//		__u32 padding;
//		__u64 dev;
//		char  name[DM_NAME_LEN];   // 128
//		char  uuid[DM_UUID_LEN];   // 129
//		char  data[7];
//	};
type structDMIoctl struct {
	Version     [3]uint32
	DataSize    uint32
	DataStart   uint32
	TargetCount uint32
	OpenCount   int32
	Flags       uint32
	EventNr     uint32
	Padding     uint32
	Dev         uint64
	Name        [dmNameLen]byte
	UUID        [dmUUIDLen]byte
	Data        [7]byte
}

// structDMTargetSpec mirrors struct dm_target_spec from dm-ioctl.h. Each spec
// is immediately followed by its NUL-terminated parameter string, then padding
// to the next 8-byte boundary; Next records the byte offset from the start of
// this spec (LOAD) or from the first spec (STATUS) to the following one.
//
//	struct dm_target_spec {
//		__u64 sector_start;
//		__u64 length;
//		__s32 status;
//		__u32 next;
//		char  target_type[DM_MAX_TYPE_NAME]; // 16
//	};
type structDMTargetSpec struct {
	SectorStart uint64
	Length      uint64
	Status      int32
	Next        uint32
	TargetType  [dmMaxTypeName]byte
}

// structDMTargetMsg mirrors struct dm_target_msg from dm-ioctl.h. It is the
// payload for DM_TARGET_MSG: a sector number identifying which target in the
// table the message is addressed to, followed by a NUL-terminated message
// string. The C struct is
//
//	struct dm_target_msg {
//		__u64 sector;   // device sector the message applies to
//		char  message[0];
//	};
//
// i.e. an 8-byte sector immediately followed by the flexible message[] array.
// Like dm_name_list its Go size (8) happens to equal the C offset of the
// flexible member, but we use dmTargetMsgHeadSize explicitly to make the
// dependence on the C layout (not Go's) clear.
type structDMTargetMsg struct {
	Sector uint64
}

// dmTargetMsgHeadSize is the offset of the flexible message[] member within
// struct dm_target_msg in C: immediately after the u64 sector, at byte 8.
const dmTargetMsgHeadSize = 8

// structDMNameList mirrors the fixed head of struct dm_name_list (dm-ioctl.h).
// The variable-length name[] and optional trailing event_nr/flags/uuid follow
// at runtime and are parsed by hand in the Linux implementation.
//
//	struct dm_name_list {
//		__u64 dev;
//		__u32 next;
//		char  name[];
//	};
type structDMNameList struct {
	Dev  uint64
	Next uint32
}

// dmNameListHeadSize is the offset of the flexible name[] member within
// struct dm_name_list in C: immediately after Next, at byte 12. Note this is
// NOT unsafe.Sizeof(structDMNameList{}), which is 16 in Go because the u64
// forces the struct's size up to an 8-byte multiple; the kernel places name[]
// at offset 12 regardless.
const dmNameListHeadSize = 12

// structDMTargetVersions mirrors the fixed head of struct dm_target_versions
// from dm-ioctl.h, the record DM_LIST_VERSIONS returns once per registered
// target type. The variable-length name[] follows at runtime.
//
//	struct dm_target_versions {
//		__u32 next;        // byte offset from this record to the next, 0 = last
//		__u32 version[3];  // {major, minor, patch} of the target type
//		char  name[];      // NUL-terminated target type name
//	};
//
// Unlike dm_name_list, every member here is 32-bit, so the Go struct's size
// (16) equals the C offset of name[]; we still spell the head size out
// explicitly as dmTargetVersionsHeadSize to keep the dependence on the C layout
// (not Go's) clear.
type structDMTargetVersions struct {
	Next    uint32
	Version [3]uint32
}

// dmTargetVersionsHeadSize is the offset of the flexible name[] member within
// struct dm_target_versions in C: after next + version[3], at byte 16.
const dmTargetVersionsHeadSize = 16

// abiSizeof* are recorded so abi_test.go can pin them against the kernel's C
// sizeof() values on a 64-bit kernel.
var (
	abiSizeofDMIoctl          = unsafe.Sizeof(structDMIoctl{})
	abiSizeofDMTargetSpec     = unsafe.Sizeof(structDMTargetSpec{})
	abiSizeofDMNameList       = unsafe.Sizeof(structDMNameList{})
	abiSizeofDMTargetMsg      = unsafe.Sizeof(structDMTargetMsg{})
	abiSizeofDMTargetVersions = unsafe.Sizeof(structDMTargetVersions{})
)

// align8 rounds n up to the next multiple of 8, matching the alignment
// device-mapper requires between successive dm_target_spec entries.
func align8(n int) int { return (n + 7) &^ 7 }

// encodeTargets serializes targets into the wire payload that follows the
// dm_ioctl header for DM_TABLE_LOAD: a sequence of dm_target_spec records, each
// followed by its NUL-terminated parameter string, each record padded so the
// next begins on an 8-byte boundary. The Next field of each spec is the byte
// distance from the start of that spec to the start of the following one. This
// is pure byte manipulation, so it is platform-independent and unit-tested on
// the host.
func encodeTargets(targets []Target) ([]byte, error) {
	var out []byte
	specSize := int(abiSizeofDMTargetSpec) // 40
	for i, t := range targets {
		if len(t.Type) >= dmMaxTypeName {
			return nil, fmt.Errorf("dm: target %d type %q too long (max %d)", i, t.Type, dmMaxTypeName-1)
		}
		// One record = spec header + params + NUL, padded to 8 bytes.
		recRaw := specSize + len(t.Params) + 1
		recPadded := align8(recRaw)

		spec := structDMTargetSpec{
			SectorStart: t.SectorStart,
			Length:      t.Length,
			Next:        uint32(recPadded),
		}
		copy(spec.TargetType[:], t.Type)

		rec := make([]byte, recPadded)
		*(*structDMTargetSpec)(unsafe.Pointer(&rec[0])) = spec
		copy(rec[specSize:], t.Params) // trailing bytes stay zero => NUL-terminated + pad
		out = append(out, rec...)
	}
	return out, nil
}

// parseTargets walks count dm_target_spec records starting at dataStart in b.
// On DM_TABLE_STATUS the spec.Next of each record is the offset from the first
// spec to the next one, so we advance using base+next.
func parseTargets(b []byte, dataStart, count int) ([]Target, error) {
	specSize := int(abiSizeofDMTargetSpec)
	targets := make([]Target, 0, count)
	off := dataStart
	for i := 0; i < count; i++ {
		if off+specSize > len(b) {
			return nil, fmt.Errorf("dm: truncated target spec %d at offset %d", i, off)
		}
		spec := *(*structDMTargetSpec)(unsafe.Pointer(&b[off]))
		params := cstr(b[off+specSize:])
		targets = append(targets, Target{
			SectorStart: spec.SectorStart,
			Length:      spec.Length,
			Type:        cstr(spec.TargetType[:]),
			Params:      params,
		})
		if spec.Next == 0 {
			break
		}
		// On STATUS, Next is measured from the start of the first spec.
		off = dataStart + int(spec.Next)
	}
	return targets, nil
}

// parseNameList walks the struct dm_name_list chain that DM_LIST_DEVICES
// returns. Each record is {u64 dev; u32 next; char name[]}; next is the byte
// offset from the start of this record to the next, or 0 for the last. An empty
// list is signalled by the first record having an empty name.
func parseNameList(b []byte, dataStart int) ([]Device, error) {
	headSize := dmNameListHeadSize // 12: offset of name[] in the C struct
	var devs []Device
	off := dataStart
	for {
		if off+headSize > len(b) {
			break
		}
		rec := *(*structDMNameList)(unsafe.Pointer(&b[off]))
		name := cstr(b[off+headSize:])
		if name != "" {
			devs = append(devs, Device{Name: name, Dev: rec.Dev})
		}
		if rec.Next == 0 {
			break
		}
		off += int(rec.Next)
	}
	return devs, nil
}

// parseTargetVersions walks the struct dm_target_versions chain that
// DM_LIST_VERSIONS returns. Each record is {u32 next; u32 version[3];
// char name[]}; next is the byte offset from the start of this record to the
// next, or 0 for the last. The list is empty when the first record has an empty
// name (the kernel emits a single zeroed record). This is pure byte walking, so
// it is platform-independent and unit-tested on the host.
func parseTargetVersions(b []byte, dataStart int) ([]TargetVersion, error) {
	headSize := dmTargetVersionsHeadSize // 16: offset of name[] in the C struct
	var out []TargetVersion
	off := dataStart
	for {
		if off+headSize > len(b) {
			break
		}
		rec := *(*structDMTargetVersions)(unsafe.Pointer(&b[off]))
		name := cstr(b[off+headSize:])
		if name != "" {
			out = append(out, TargetVersion{
				Name:    name,
				Version: DMVersion{Major: rec.Version[0], Minor: rec.Version[1], Patch: rec.Version[2]},
			})
		}
		if rec.Next == 0 {
			break
		}
		off += int(rec.Next)
	}
	return out, nil
}

// cstr returns the Go string for the NUL-terminated byte slice b (up to the
// first NUL, or all of b if none).
func cstr(b []byte) string {
	for i := range b {
		if b[i] == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

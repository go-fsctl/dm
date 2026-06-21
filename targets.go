// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// This file holds typed constructors for the device-mapper target types beyond
// "linear" (which lives next to the kernel operations so it is available even
// on the stub build). Every constructor here is pure param-string construction:
// it returns a Target whose Params is exactly the text the kernel parses for
// that target, so they build correctly on any platform and are unit-tested on
// the host. The kernel — not this library — does the actual work (striping,
// copy-on-write, encryption, ...).
//
// The canonical reference for each param format is the Linux kernel
// Documentation/admin-guide/device-mapper/*.rst files; the relevant grammar is
// repeated above each constructor.

// StripeDev names one underlying device of a striped target: a block device
// (path "/dev/loop0" or "major:minor") and the sector offset within it at which
// this stripe begins.
type StripeDev struct {
	Dev    string // backing device: path or "major:minor"
	Offset uint64 // start sector within Dev
}

// Striped builds a "striped" Target that interleaves I/O across len(devs)
// underlying devices in fixed-size chunks (RAID-0).
//
// dm-stripe param format:
//
//	<num_stripes> <chunk_sectors> <dev0> <off0> <dev1> <off1> ...
//
// chunkSectors is the stripe chunk size in 512-byte sectors and must be a power
// of two and at least the page size (kernel requirement; not enforced here).
// length should be a multiple of chunkSectors*len(devs). For example, two
// devices striped in 256-sector (128 KiB) chunks:
//
//	Striped(256, []StripeDev{{"/dev/loop0", 0}, {"/dev/loop1", 0}})
//	  => Params "2 256 /dev/loop0 0 /dev/loop1 0"
func Striped(sectorStart, length, chunkSectors uint64, devs []StripeDev) Target {
	var b strings.Builder
	b.WriteString(strconv.FormatUint(uint64(len(devs)), 10))
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(chunkSectors, 10))
	for _, d := range devs {
		b.WriteByte(' ')
		b.WriteString(d.Dev)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(d.Offset, 10))
	}
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "striped",
		Params:      b.String(),
	}
}

// Zero builds a "zero" Target: reads return zeroed blocks and writes are
// silently discarded (the block-device analogue of /dev/zero). It takes no
// backing device and no parameters, which makes it handy for tests and for
// padding holes in a table.
func Zero(sectorStart, length uint64) Target {
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "zero",
		Params:      "",
	}
}

// Error builds an "error" Target: any I/O to it fails. It takes no parameters
// and is useful for fault injection and for fencing off a region of a table.
func Error(sectorStart, length uint64) Target {
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "error",
		Params:      "",
	}
}

// Snapshot builds a "snapshot" Target: a copy-on-write view of origin whose
// changed chunks are stored on cow.
//
// dm-snapshot param format:
//
//	<origin> <cow> <persistent> <chunk_sectors>
//
// persistent is "P" when the exception store survives a reboot (metadata is
// written to cow) and "N" for a transient store. chunkSectors is the
// copy-on-write granularity in 512-byte sectors (e.g. 8 == 4 KiB). origin and
// cow may be paths or "major:minor".
func Snapshot(sectorStart, length uint64, origin, cow string, persistent bool, chunkSectors uint64) Target {
	p := "N"
	if persistent {
		p = "P"
	}
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "snapshot",
		Params:      origin + " " + cow + " " + p + " " + strconv.FormatUint(chunkSectors, 10),
	}
}

// SnapshotOrigin builds a "snapshot-origin" Target over origin. It is layered
// over the real origin device so that writes to it trigger copy-out to every
// snapshot taken of it. The table for the origin device is just the origin
// device itself.
//
// dm-snapshot-origin param format:
//
//	<origin>
func SnapshotOrigin(sectorStart, length uint64, origin string) Target {
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "snapshot-origin",
		Params:      origin,
	}
}

// SnapStatus is the decoded runtime status of a "snapshot" target, as reported
// by Status (DM_TABLE_STATUS without the table flag).
type SnapStatus struct {
	// Valid is false when the kernel reports "Invalid" (the snapshot is
	// unusable, e.g. its exception store is corrupt). When Valid is false the
	// numeric fields are zero and the raw kernel word is kept in Raw.
	Valid bool
	// UsedSectors is the number of exception-store sectors allocated so far.
	UsedSectors uint64
	// TotalSectors is the size of the exception store in sectors. When it is
	// reached the snapshot fills up and is invalidated.
	TotalSectors uint64
	// MetadataSectors is the part of UsedSectors consumed by metadata rather
	// than copied data. It is the kernel's third status word; it is 0 when the
	// kernel did not report it.
	MetadataSectors uint64
	// Raw is the unparsed status string, preserved for diagnostics and for the
	// special states ("Invalid", "Overflow", "Merge failed").
	Raw string
}

// ParseSnapStatus decodes the status string a "snapshot" target reports through
// DM_TABLE_STATUS.
//
// The kernel emits one of:
//
//	"<used>/<total> <metadata>"   normal: sectors allocated / store size, metadata sectors
//	"Invalid"                     the snapshot is unusable
//	"Overflow"                    the store overflowed
//	"Merge failed"                a snapshot-merge failed
//
// For the normal form, used/total and the metadata word are parsed; for the
// textual states Valid is false and Raw holds the original text.
func ParseSnapStatus(status string) (SnapStatus, error) {
	s := SnapStatus{Raw: status}
	fields := strings.Fields(status)
	if len(fields) == 0 {
		return s, fmt.Errorf("dm: empty snapshot status")
	}
	frac := fields[0]
	slash := strings.IndexByte(frac, '/')
	if slash < 0 {
		// A non-numeric word such as "Invalid" / "Overflow" / "Merge".
		return s, nil
	}
	used, err := strconv.ParseUint(frac[:slash], 10, 64)
	if err != nil {
		return s, fmt.Errorf("dm: snapshot status %q: used: %w", status, err)
	}
	total, err := strconv.ParseUint(frac[slash+1:], 10, 64)
	if err != nil {
		return s, fmt.Errorf("dm: snapshot status %q: total: %w", status, err)
	}
	s.Valid = true
	s.UsedSectors = used
	s.TotalSectors = total
	if len(fields) > 1 {
		if md, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
			s.MetadataSectors = md
		}
	}
	return s, nil
}

// SnapshotStatus reads the runtime status of the named snapshot device and
// decodes its single "snapshot" target with ParseSnapStatus. It errors if the
// device's active table is not a single snapshot target.
func SnapshotStatus(name string) (SnapStatus, error) {
	tgts, err := Status(name)
	if err != nil {
		return SnapStatus{}, err
	}
	if len(tgts) != 1 || tgts[0].Type != "snapshot" {
		return SnapStatus{}, fmt.Errorf("dm: %q is not a single snapshot target (got %d targets)", name, len(tgts))
	}
	return ParseSnapStatus(tgts[0].Params)
}

// Crypt builds a "crypt" Target that transparently encrypts and decrypts I/O to
// a backing device, the way cryptsetup/LUKS does at runtime. This constructor
// only assembles the table line; the kernel's dm-crypt does the crypto.
//
// dm-crypt param format:
//
//	<cipher> <key> <iv_offset> <device> <device_offset> [<#opt> <opt>...]
//
// cipher is the kernel cipher specification, e.g. "aes-xts-plain64" or
// "aes-cbc-essiv:sha256". key is the raw key bytes; dm-crypt expects it
// hex-encoded in the table, so Crypt hex-encodes key for you (an N-byte key
// becomes 2N hex characters — for aes-xts-plain64 a 256-bit cipher uses a
// 64-byte / 512-bit key, i.e. 128 hex chars). ivOffset is the IV offset in
// sectors (usually 0). dev is the backing device (path or "major:minor") and
// devOffset is the start sector on it.
//
// opts are optional dm-crypt option arguments such as "sector_size:4096" or
// "allow_discards"; when present, Crypt prefixes them with their count, as the
// kernel format requires. For example:
//
//	Crypt("aes-xts-plain64", key, 0, "/dev/loop0", 0, nil)
//	  => "aes-xts-plain64 <hexkey> 0 /dev/loop0 0"
//	Crypt("aes-xts-plain64", key, 0, "/dev/loop0", 0, []string{"sector_size:4096"})
//	  => "aes-xts-plain64 <hexkey> 0 /dev/loop0 0 1 sector_size:4096"
//
// Note the key travels through the dm_ioctl payload in clear; callers that care
// can set the device's DM_SECURE_DATA_FLAG via the kernel zeroing path, but the
// hex encoding here is purely the table format dm-crypt mandates.
func Crypt(sectorStart, length uint64, cipher string, key []byte, ivOffset uint64, dev string, devOffset uint64, opts []string) Target {
	var b strings.Builder
	b.WriteString(cipher)
	b.WriteByte(' ')
	b.WriteString(hex.EncodeToString(key))
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(ivOffset, 10))
	b.WriteByte(' ')
	b.WriteString(dev)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(devOffset, 10))
	if len(opts) > 0 {
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(uint64(len(opts)), 10))
		for _, o := range opts {
			b.WriteByte(' ')
			b.WriteString(o)
		}
	}
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "crypt",
		Params:      b.String(),
	}
}

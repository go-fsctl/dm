// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import (
	"fmt"
	"strconv"
	"strings"
)

// This file holds the typed constructors and helpers for device-mapper thin
// provisioning: the "thin-pool" and "thin" targets, the target-message wrappers
// that create and delete thin volumes and snapshots inside a pool, and the
// status parsers. As with the other constructors the param assembly is pure and
// platform-neutral; only the Message-based helpers and the *Status readers
// touch the kernel.
//
// Thin provisioning is built from two targets working together:
//
//   - A "thin-pool" device owns two block devices, a small metadata device and
//     a large data device, and hands out space from the data device in blocks
//     on demand. Thin volumes and snapshots are created inside the pool by
//     sending it target messages (see ThinPoolCreateThin / ThinPoolCreateSnap /
//     ThinPoolDeleteThin); each is identified by a 24-bit numeric device id.
//   - A "thin" device exposes one such volume (selected by its pool + dev id) as
//     a block device that can be formatted and mounted.
//
// The metadata device must be zeroed before the pool is first created (the
// thin-pool target treats all-zero metadata as "format me"); the kernel rejects
// a pool whose metadata is non-zero garbage.
//
// References: Linux Documentation/admin-guide/device-mapper/thin-provisioning.rst.

// ThinPool builds a "thin-pool" Target over a metadata device and a data
// device.
//
// dm-thin-pool param format:
//
//	<metadata_dev> <data_dev> <data_block_sectors> <low_water_mark_blocks> \
//	  [<#feature_args> <feature_arg>...]
//
// metadataDev and dataDev are the backing devices (paths or "major:minor").
// dataBlockSectors is the pool's allocation unit in 512-byte sectors; it must
// be a multiple of 128 (64 KiB) and between 128 and 2097152 (64 KiB..1 GiB)
// (kernel requirement, not enforced here). lowWaterMark is expressed in pool
// blocks (not sectors): when free space drops below it the pool raises a
// dm event so userspace can grow the data device.
//
// opts are optional thin-pool feature arguments, e.g. "skip_block_zeroing",
// "ignore_discard", "no_discard_passdown", "read_only" or
// "error_if_no_space". When present they are prefixed with their count, as the
// kernel format requires. For example, a pool with 128-sector (64 KiB) blocks
// and a 1024-block low-water mark:
//
//	ThinPool(0, length, "/dev/loop0", "/dev/loop1", 128, 1024, nil)
//	  => Params "/dev/loop0 /dev/loop1 128 1024"
//	ThinPool(0, length, "/dev/loop0", "/dev/loop1", 128, 1024,
//		[]string{"skip_block_zeroing"})
//	  => Params "/dev/loop0 /dev/loop1 128 1024 1 skip_block_zeroing"
func ThinPool(sectorStart, length uint64, metadataDev, dataDev string, dataBlockSectors, lowWaterMark uint64, opts []string) Target {
	var b strings.Builder
	b.WriteString(metadataDev)
	b.WriteByte(' ')
	b.WriteString(dataDev)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(dataBlockSectors, 10))
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(lowWaterMark, 10))
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
		Type:        "thin-pool",
		Params:      b.String(),
	}
}

// Thin builds a "thin" Target exposing the thin volume with id devId inside the
// pool device poolDev.
//
// dm-thin param format:
//
//	<pool_dev> <dev_id> [<external_origin_dev>]
//
// poolDev is the thin-pool device (path or "major:minor"). devId is the 24-bit
// numeric id the volume was created under via a "create_thin"/"create_snap"
// pool message. externalOrigin is optional: when non-empty it names a read-only
// device that backs the unprovisioned parts of the thin volume (an external
// snapshot origin); leave it "" for an ordinary thin volume. For example:
//
//	Thin(0, length, "/dev/mapper/pool", 0, "")
//	  => Params "/dev/mapper/pool 0"
//	Thin(0, length, "/dev/mapper/pool", 1, "/dev/loop2")
//	  => Params "/dev/mapper/pool 1 /dev/loop2"
func Thin(sectorStart, length uint64, poolDev string, devId uint64, externalOrigin string) Target {
	params := poolDev + " " + strconv.FormatUint(devId, 10)
	if externalOrigin != "" {
		params += " " + externalOrigin
	}
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "thin",
		Params:      params,
	}
}

// ThinPoolCreateThin creates a new, empty thin volume with id devId inside the
// pool named poolName, by sending it the "create_thin <id>" target message.
// After this returns the volume can be surfaced with a Thin target referencing
// the same devId. devId must not already be in use within the pool.
func ThinPoolCreateThin(poolName string, devId uint64) error {
	_, err := Message(poolName, 0, "create_thin "+strconv.FormatUint(devId, 10))
	return err
}

// ThinPoolCreateSnap creates a thin snapshot with id devId of the existing thin
// volume originId, by sending the pool the "create_snap <id> <origin>" target
// message. The snapshot shares the origin's blocks copy-on-write. The origin
// should be quiesced (e.g. its thin device suspended) for an application-
// consistent snapshot; the pool itself does not require it.
func ThinPoolCreateSnap(poolName string, devId, originId uint64) error {
	_, err := Message(poolName, 0,
		"create_snap "+strconv.FormatUint(devId, 10)+" "+strconv.FormatUint(originId, 10))
	return err
}

// ThinPoolDeleteThin deletes the thin volume (or snapshot) with id devId from
// the pool named poolName, by sending the "delete <id>" target message. The
// corresponding "thin" device, if any, must already have been removed.
func ThinPoolDeleteThin(poolName string, devId uint64) error {
	_, err := Message(poolName, 0, "delete "+strconv.FormatUint(devId, 10))
	return err
}

// ThinPoolStat is the decoded runtime status of a "thin-pool" target, as
// reported by Status (DM_TABLE_STATUS without the table flag).
//
// The kernel status line is:
//
//	<transaction_id> <used_metadata>/<total_metadata> <used_data>/<total_data> \
//	  <held_metadata_root> <discard_passdown> <read_only|rw|out_of_data_space> \
//	  <no_discard_passdown|discard_passdown> [needs_check]
//
// or the single word "Fail" when the pool has failed. The block counts are in
// pool blocks (metadata blocks are a fixed kernel size; data blocks are the
// pool's data_block_sectors).
type ThinPoolStat struct {
	// OK is false when the kernel reports "Fail"; the numeric fields are then 0
	// and Raw holds the original word.
	OK bool
	// TransactionID is the pool's current transaction id (first status word).
	TransactionID uint64
	// UsedMetaBlocks / TotalMetaBlocks are the metadata-device usage.
	UsedMetaBlocks  uint64
	TotalMetaBlocks uint64
	// UsedDataBlocks / TotalDataBlocks are the data-device usage in pool blocks;
	// these are the numbers that grow as thin volumes are written.
	UsedDataBlocks  uint64
	TotalDataBlocks uint64
	// Raw is the unparsed status string.
	Raw string
}

// ParseThinPoolStatus decodes the status string a "thin-pool" target reports
// through DM_TABLE_STATUS. It parses the transaction id and the
// used/total metadata and data block fractions; the trailing mode flags are
// left in Raw. The single-word "Fail" state sets OK=false.
func ParseThinPoolStatus(status string) (ThinPoolStat, error) {
	s := ThinPoolStat{Raw: status}
	fields := strings.Fields(status)
	if len(fields) == 0 {
		return s, fmt.Errorf("dm: empty thin-pool status")
	}
	if fields[0] == "Fail" || fields[0] == "Error" {
		return s, nil
	}
	if len(fields) < 3 {
		return s, fmt.Errorf("dm: thin-pool status %q: too few fields", status)
	}
	txn, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return s, fmt.Errorf("dm: thin-pool status %q: transaction id: %w", status, err)
	}
	usedMeta, totalMeta, err := parseFraction(fields[1])
	if err != nil {
		return s, fmt.Errorf("dm: thin-pool status %q: metadata: %w", status, err)
	}
	usedData, totalData, err := parseFraction(fields[2])
	if err != nil {
		return s, fmt.Errorf("dm: thin-pool status %q: data: %w", status, err)
	}
	s.OK = true
	s.TransactionID = txn
	s.UsedMetaBlocks, s.TotalMetaBlocks = usedMeta, totalMeta
	s.UsedDataBlocks, s.TotalDataBlocks = usedData, totalData
	return s, nil
}

// ThinPoolStatus reads the runtime status of the named thin-pool device and
// decodes its single "thin-pool" target with ParseThinPoolStatus. It errors if
// the device's status is not a single thin-pool target.
func ThinPoolStatus(name string) (ThinPoolStat, error) {
	tgts, err := Status(name)
	if err != nil {
		return ThinPoolStat{}, err
	}
	if len(tgts) != 1 || tgts[0].Type != "thin-pool" {
		return ThinPoolStat{}, fmt.Errorf("dm: %q is not a single thin-pool target (got %d targets)", name, len(tgts))
	}
	return ParseThinPoolStatus(tgts[0].Params)
}

// ThinStat is the decoded runtime status of a "thin" target.
//
// The kernel status line is:
//
//	<nr_mapped_sectors> <highest_mapped_sector>
//
// or the single word "Fail". A freshly created, never-written thin volume
// reports "0 -" (no sectors mapped, no highest sector).
type ThinStat struct {
	// OK is false when the kernel reports "Fail".
	OK bool
	// MappedSectors is the number of sectors actually provisioned (allocated) to
	// this thin volume so far.
	MappedSectors uint64
	// HighestMappedSector is the highest mapped sector, or false in HasHighest
	// when the kernel reported "-" (nothing mapped yet).
	HighestMappedSector uint64
	HasHighest          bool
	// Raw is the unparsed status string.
	Raw string
}

// ParseThinStatus decodes the status string a "thin" target reports through
// DM_TABLE_STATUS: "<nr_mapped_sectors> <highest_mapped_sector>", with the
// highest sector given as "-" when nothing is mapped, or the word "Fail".
func ParseThinStatus(status string) (ThinStat, error) {
	s := ThinStat{Raw: status}
	fields := strings.Fields(status)
	if len(fields) == 0 {
		return s, fmt.Errorf("dm: empty thin status")
	}
	if fields[0] == "Fail" || fields[0] == "Error" {
		return s, nil
	}
	mapped, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return s, fmt.Errorf("dm: thin status %q: mapped sectors: %w", status, err)
	}
	s.OK = true
	s.MappedSectors = mapped
	if len(fields) > 1 && fields[1] != "-" {
		hi, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return s, fmt.Errorf("dm: thin status %q: highest sector: %w", status, err)
		}
		s.HighestMappedSector = hi
		s.HasHighest = true
	}
	return s, nil
}

// ThinStatus reads the runtime status of the named thin device and decodes its
// single "thin" target with ParseThinStatus.
func ThinStatus(name string) (ThinStat, error) {
	tgts, err := Status(name)
	if err != nil {
		return ThinStat{}, err
	}
	if len(tgts) != 1 || tgts[0].Type != "thin" {
		return ThinStat{}, fmt.Errorf("dm: %q is not a single thin target (got %d targets)", name, len(tgts))
	}
	return ParseThinStatus(tgts[0].Params)
}

// parseFraction splits a "used/total" status word into its two unsigned parts.
func parseFraction(frac string) (used, total uint64, err error) {
	slash := strings.IndexByte(frac, '/')
	if slash < 0 {
		return 0, 0, fmt.Errorf("missing '/' in %q", frac)
	}
	used, err = strconv.ParseUint(frac[:slash], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	total, err = strconv.ParseUint(frac[slash+1:], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return used, total, nil
}

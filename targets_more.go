// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import (
	"strconv"
	"strings"
)

// This file holds typed constructors for a further set of device-mapper target
// types: the "delay" and "flakey" test targets, and the "raid", "cache" and
// "integrity" production targets. As with the constructors in targets.go,
// thin.go and verity.go, every function here is pure param-string assembly: it
// returns a Target whose Params is exactly the text the kernel parses for that
// target, so they build on any platform and are unit-tested on the host. The
// kernel — not this library — does the actual work (delaying I/O, RAID parity,
// caching, per-block integrity, ...).
//
// The canonical reference for each grammar is the Linux kernel
// Documentation/admin-guide/device-mapper/*.rst files; the relevant grammar is
// repeated above each constructor.

// DelayLeg names one underlying device of a "delay" target together with the
// sector offset within it and the delay, in milliseconds, applied to the I/O
// directed at that leg.
type DelayLeg struct {
	Dev    string // backing device: path or "major:minor"
	Offset uint64 // start sector within Dev
	Delay  uint64 // delay in milliseconds
}

// Delay builds a "delay" Target: a test target that passes I/O through to a
// backing device after holding it for a fixed delay, useful for exercising I/O
// timing and timeouts. Reads, writes and flushes can be split across separate
// legs, each with its own device, offset and delay.
//
// dm-delay param format (the table must carry 3, 6 or 9 arguments):
//
//	<dev> <offset> <delay> \
//	  [<write_dev> <write_offset> <write_delay> \
//	    [<flush_dev> <flush_offset> <flush_delay>]]
//
// Offsets are in 512-byte sectors; delays are in milliseconds. With only read
// supplied the same device/offset/delay applies to reads, writes and flushes
// (3 args). With write supplied, reads use read and writes+flushes use write
// (6 args). With flush supplied as well, flushes use flush (9 args). write and
// flush may be nil; flush requires write to be non-nil (the 9-arg form is built
// only on top of the 6-arg form). For example:
//
//	Delay(0, n, DelayLeg{"/dev/loop0", 0, 50}, nil, nil)
//	  => "/dev/loop0 0 50"
//	Delay(0, n, DelayLeg{"/dev/loop0", 0, 50}, &DelayLeg{"/dev/loop1", 0, 100}, nil)
//	  => "/dev/loop0 0 50 /dev/loop1 0 100"
func Delay(sectorStart, length uint64, read DelayLeg, write, flush *DelayLeg) Target {
	var b strings.Builder
	writeLeg := func(l DelayLeg) {
		b.WriteString(l.Dev)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(l.Offset, 10))
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(l.Delay, 10))
	}
	writeLeg(read)
	if write != nil {
		b.WriteByte(' ')
		writeLeg(*write)
		// A distinct flush leg is only meaningful as the third group; the kernel
		// requires the write group to be present for the 9-argument form.
		if flush != nil {
			b.WriteByte(' ')
			writeLeg(*flush)
		}
	}
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "delay",
		Params:      b.String(),
	}
}

// Flakey builds a "flakey" Target: a test target for fault injection. It maps
// onto a backing device and works normally for upInterval seconds, then returns
// errors (or applies the configured corruption features) for the next
// downInterval seconds, cycling forever. It is the standard way to test how a
// filesystem or application copes with a flapping device.
//
// dm-flakey param format:
//
//	<dev> <offset> <up_interval> <down_interval> [<#features> <feature>...]
//
// offset is the start sector on dev; the intervals are in SECONDS (unlike
// dm-delay, which uses milliseconds). features are the optional dm-flakey
// feature arguments controlling what happens during the down interval, e.g.
// "error_reads", "drop_writes", "error_writes", or the multi-token
// "corrupt_bio_byte <Nth> <r|w> <value> <flags>"; when present they are
// prefixed with their count, as the kernel format requires. With no features
// the device simply errors all I/O during the down interval. For example:
//
//	Flakey(0, n, "/dev/loop0", 0, 8, 4, nil)
//	  => "/dev/loop0 0 8 4"
//	Flakey(0, n, "/dev/loop0", 0, 8, 4, []string{"drop_writes"})
//	  => "/dev/loop0 0 8 4 1 drop_writes"
func Flakey(sectorStart, length uint64, dev string, offset, upInterval, downInterval uint64, features []string) Target {
	var b strings.Builder
	b.WriteString(dev)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(offset, 10))
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(upInterval, 10))
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(downInterval, 10))
	if len(features) > 0 {
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(uint64(len(features)), 10))
		for _, f := range features {
			b.WriteByte(' ')
			b.WriteString(f)
		}
	}
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "flakey",
		Params:      b.String(),
	}
}

// RaidDev names one device slot of a "raid" target: a metadata device and a
// data device. A slot that is missing or failed at creation time is given as
// "-" for both, which Raid emits verbatim.
type RaidDev struct {
	Meta string // metadata device (path or "major:minor"), or "-" if absent
	Data string // data device (path or "major:minor"), or "-" if absent
}

// Raid builds a "raid" Target driving the kernel's dm-raid personality (the
// same engine behind MD RAID) over a set of metadata+data device pairs.
//
// dm-raid param format:
//
//	<raid_type> <#raid_params> <raid_params...> <#raid_devs> \
//	  <meta0> <data0> [<meta1> <data1> ...]
//
// raidType is one of the kernel's raid personalities, e.g. "raid0", "raid1",
// "raid4", "raid5_ls" (and the other raid5_*/raid6_* layouts) or "raid10".
//
// raidParams is the count-prefixed list of tuning parameters. Its first element
// is the only mandatory one — the chunk (stripe) size in sectors — and the rest
// are optional key/value or flag parameters in any order, such as "sync" or
// "nosync", "rebuild <idx>", "region_size <sectors>", "stripe_cache <sectors>",
// "daemon_sleep <ms>", "min_recovery_rate <kB>", "max_recovery_rate <kB>",
// "write_mostly <idx>", "max_write_behind <sectors>", "raid10_copies <n>",
// "raid10_format <near|far|offset>", "delta_disks <n>", "data_offset <sectors>"
// or "journal_dev <dev>". Raid prefixes raidParams with its element count, as
// the kernel parser requires (so the count reflects every token, e.g.
// "rebuild 0" contributes 2 to the count).
//
// devs is the list of slots; Raid prefixes it with len(devs) (the #raid_devs
// field) and then emits each slot's metadata device followed by its data
// device. A slot with no separate metadata device uses "-" for Meta.
//
// For example, a two-device raid1 with a 64 KiB region size, rebuilding device
// 1, with no separate metadata devices:
//
//	Raid(0, n, "raid1",
//		[]string{"128", "region_size", "1024", "rebuild", "1"},
//		[]RaidDev{{"-", "/dev/loop0"}, {"-", "/dev/loop1"}})
//	  => "raid1 5 128 region_size 1024 rebuild 1 2 - /dev/loop0 - /dev/loop1"
//
// (Here raidParams has 5 tokens — chunk 128, region_size 1024, rebuild 1 — so
// the #raid_params field is 5, and #raid_devs is 2.)
func Raid(sectorStart, length uint64, raidType string, raidParams []string, devs []RaidDev) Target {
	var b strings.Builder
	b.WriteString(raidType)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(uint64(len(raidParams)), 10))
	for _, p := range raidParams {
		b.WriteByte(' ')
		b.WriteString(p)
	}
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(uint64(len(devs)), 10))
	for _, d := range devs {
		b.WriteByte(' ')
		b.WriteString(d.Meta)
		b.WriteByte(' ')
		b.WriteString(d.Data)
	}
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "raid",
		Params:      b.String(),
	}
}

// Cache builds a "cache" Target: the kernel's dm-cache, which front-ends a slow
// origin device with a fast cache device, tracking what is hot in a separate
// metadata device under the control of a pluggable replacement policy.
//
// dm-cache param format:
//
//	<metadata_dev> <cache_dev> <origin_dev> <block_sectors> \
//	  <#feature_args> <feature_arg>... <policy> <#policy_args> <policy_arg>...
//
// metadataDev, cacheDev and originDev are the three backing devices (paths or
// "major:minor"). blockSectors is the cache block size in 512-byte sectors; it
// must be a multiple of 64 (32 KiB) and between 64 and 2097152 (32 KiB..1 GiB)
// (kernel requirement, not enforced here). The metadata device must be zeroed
// before first use (dm-cache treats all-zero metadata as "format me").
//
// features are optional mode/behaviour flags such as "writeback" (the default),
// "writethrough", "passthrough", "metadata2" or "no_discard_passdown"; Cache
// emits their count then the flags. policy is the replacement policy name, e.g.
// "smq" (the modern default) or "default". policyArgs are optional key/value
// tuning pairs for the policy, e.g. "migration_threshold 2048"; Cache emits
// their count then the pairs. For example:
//
//	Cache(0, n, "/dev/loop0", "/dev/loop1", "/dev/loop2", 128,
//		[]string{"writethrough"}, "smq", nil)
//	  => "/dev/loop0 /dev/loop1 /dev/loop2 128 1 writethrough smq 0"
//	Cache(0, n, "253:0", "253:1", "253:2", 64,
//		nil, "smq", []string{"migration_threshold", "2048"})
//	  => "253:0 253:1 253:2 64 0 smq 2 migration_threshold 2048"
func Cache(sectorStart, length uint64, metadataDev, cacheDev, originDev string, blockSectors uint64, features []string, policy string, policyArgs []string) Target {
	var b strings.Builder
	b.WriteString(metadataDev)
	b.WriteByte(' ')
	b.WriteString(cacheDev)
	b.WriteByte(' ')
	b.WriteString(originDev)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(blockSectors, 10))
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(uint64(len(features)), 10))
	for _, f := range features {
		b.WriteByte(' ')
		b.WriteString(f)
	}
	b.WriteByte(' ')
	b.WriteString(policy)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(uint64(len(policyArgs)), 10))
	for _, a := range policyArgs {
		b.WriteByte(' ')
		b.WriteString(a)
	}
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "cache",
		Params:      b.String(),
	}
}

// Integrity builds an "integrity" Target: the kernel's dm-integrity, which adds
// per-block integrity tags (a checksum or keyed MAC) to a backing device,
// detecting — and with a separate dm-crypt layer, helping authenticate —
// silent corruption. The device must be formatted with an integrity superblock
// before first use (e.g. with `integritysetup format`); this constructor only
// assembles the runtime table line.
//
// dm-integrity param format:
//
//	<dev> <reserved_sectors> <tag_size> <mode> [<#opt_args> <opt_arg>...]
//
// dev is the backing device (path or "major:minor"). reservedSectors is the
// number of sectors reserved at the start of the device (the metadata/superblock
// area; commonly 0 when the kernel reads it from the superblock). tagSize is the
// integrity tag size in bytes; "-" may be passed to take it from the
// internal-hash algorithm. mode is a single character: "J" journaled writes,
// "D" direct writes (no journal), "B" bitmap mode, "R" recovery mode, or "I"
// inline mode.
//
// opts are optional dm-integrity arguments, typically "key:value" form such as
// "journal_sectors:N", "interleave_sectors:N", "buffer_sectors:N",
// "block_size:N" or "internal_hash:crc32c"; when present they are prefixed with
// their count, as the kernel format requires. For example:
//
//	Integrity(0, n, "/dev/loop0", 0, "4", "J", nil)
//	  => "/dev/loop0 0 4 J"
//	Integrity(0, n, "/dev/loop0", 0, "-", "J", []string{"internal_hash:crc32c"})
//	  => "/dev/loop0 0 - J 1 internal_hash:crc32c"
func Integrity(sectorStart, length uint64, dev string, reservedSectors uint64, tagSize, mode string, opts []string) Target {
	var b strings.Builder
	b.WriteString(dev)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(reservedSectors, 10))
	b.WriteByte(' ')
	b.WriteString(tagSize)
	b.WriteByte(' ')
	b.WriteString(mode)
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
		Type:        "integrity",
		Params:      b.String(),
	}
}

// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import (
	"fmt"
	"strconv"
	"strings"
)

// This file holds the typed constructor for the device-mapper "verity" target,
// which provides transparent, read-only integrity verification of a block
// device against a precomputed Merkle hash tree (the mechanism behind Android
// dm-verity and verified boot). As with the other constructors this is pure
// param-string assembly: the hash tree and root digest are produced offline
// (e.g. by `veritysetup format`), and the kernel — not this library — verifies
// every block read against them, returning EIO on a mismatch.
//
// References: Linux Documentation/admin-guide/device-mapper/verity.rst.

// VerityParams describes a dm-verity mapping. All sizes are taken verbatim into
// the table line; the kernel validates them against the hash tree at resume.
//
// The dm-verity table grammar is:
//
//	<version> <data_dev> <hash_dev> <data_block_size> <hash_block_size> \
//	  <num_data_blocks> <hash_start_block> <algorithm> <root_digest> <salt> \
//	  [<#opt_params> <opt_param>...]
//
// Fields, in order:
//
//   - Version: hash format version. 1 is the standard format (salt prepended to
//     the data when hashing); 0 is the original Chrome OS format. Use 1.
//   - DataDev / HashDev: the data device being protected and the device holding
//     the hash tree (paths or "major:minor"). They may be the same device when
//     the hash tree is stored past the data (then HashStartBlock is non-zero).
//   - DataBlockSize / HashBlockSize: block sizes in BYTES (not sectors), each a
//     power of two, typically 4096.
//   - NumDataBlocks: the number of data blocks covered, i.e. the protected size
//     is NumDataBlocks*DataBlockSize bytes. Reads past it are not verified.
//   - HashStartBlock: the offset of the hash tree on HashDev, counted in
//     HashBlockSize-byte blocks (0 when the hash device starts with the tree).
//   - Algorithm: the kernel digest name, e.g. "sha256" or "sha512".
//   - RootDigest: the Merkle-tree root hash, hex-encoded.
//   - Salt: the per-device salt, hex-encoded; empty Salt is emitted as "-" as
//     the kernel format requires.
//
// Optional features (any may be left zero/empty):
//
//   - Opts: extra verity option strings such as "ignore_corruption",
//     "restart_on_corruption", "ignore_zero_blocks" or "check_at_most_once".
//   - FEC: forward-error-correction parameters; when FEC.Device is set the four
//     FEC option args are emitted (see VerityFEC).
//
// Opts and the FEC args are concatenated into the optional-arguments list and
// prefixed with their total count, exactly as the kernel parser expects.
type VerityParams struct {
	Version        uint64
	DataDev        string
	HashDev        string
	DataBlockSize  uint64
	HashBlockSize  uint64
	NumDataBlocks  uint64
	HashStartBlock uint64
	Algorithm      string
	RootDigest     string // hex
	Salt           string // hex; empty => emitted as "-"

	Opts []string
	FEC  *VerityFEC
}

// VerityFEC describes optional forward error correction for a verity device: a
// Reed-Solomon code stored on a (possibly separate) device that lets the kernel
// repair a limited number of corrupted blocks instead of failing the read.
//
// It contributes these option arguments to the table:
//
//	use_fec_from_device <fec_dev> fec_roots <n> fec_blocks <n> fec_start <n>
type VerityFEC struct {
	Device string // device holding the FEC data (path or "major:minor")
	Blocks uint64 // total number of blocks covered by FEC (fec_blocks)
	Start  uint64 // offset of the FEC data on Device, in hash blocks (fec_start)
	Roots  uint64 // number of Reed-Solomon roots, 2..24 (fec_roots)
}

// args returns the FEC option arguments in the kernel's expected order.
func (f *VerityFEC) args() []string {
	return []string{
		"use_fec_from_device", f.Device,
		"fec_roots", strconv.FormatUint(f.Roots, 10),
		"fec_blocks", strconv.FormatUint(f.Blocks, 10),
		"fec_start", strconv.FormatUint(f.Start, 10),
	}
}

// Verity builds a "verity" Target from p. sectorStart and length describe the
// mapped device's extent in 512-byte sectors as usual; length normally equals
// NumDataBlocks*DataBlockSize/512. The constructor only assembles the table
// line — the kernel performs the cryptographic verification.
//
// dm-verity devices must be created read-only: a writable load is rejected by
// the kernel ("Device must be readonly"). Use CreateReadOnlyWithTable (or
// Create + LoadTableReadOnly + Resume) to bring a verity device up.
//
// Examples:
//
//	Verity(0, n, VerityParams{
//		Version: 1, DataDev: "/dev/loop0", HashDev: "/dev/loop1",
//		DataBlockSize: 4096, HashBlockSize: 4096,
//		NumDataBlocks: 256, HashStartBlock: 0,
//		Algorithm: "sha256", RootDigest: "ab12...", Salt: "cd34...",
//	})
//	  => "1 /dev/loop0 /dev/loop1 4096 4096 256 0 sha256 ab12... cd34..."
//
// With an empty salt the salt field is "-"; with Opts or FEC the option count
// and arguments are appended:
//
//	... sha256 <root> <salt> 1 ignore_zero_blocks
//	... sha256 <root> <salt> 9 ignore_zero_blocks use_fec_from_device /dev/loop2 \
//	      fec_roots 2 fec_blocks 100 fec_start 100
func Verity(sectorStart, length uint64, p VerityParams) Target {
	var b strings.Builder
	b.WriteString(strconv.FormatUint(p.Version, 10))
	b.WriteByte(' ')
	b.WriteString(p.DataDev)
	b.WriteByte(' ')
	b.WriteString(p.HashDev)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(p.DataBlockSize, 10))
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(p.HashBlockSize, 10))
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(p.NumDataBlocks, 10))
	b.WriteByte(' ')
	b.WriteString(strconv.FormatUint(p.HashStartBlock, 10))
	b.WriteByte(' ')
	b.WriteString(p.Algorithm)
	b.WriteByte(' ')
	b.WriteString(p.RootDigest)
	b.WriteByte(' ')
	salt := p.Salt
	if salt == "" {
		salt = "-" // kernel: "-" denotes the empty salt
	}
	b.WriteString(salt)

	// Collect the optional arguments: caller Opts first, then the FEC group.
	var opts []string
	opts = append(opts, p.Opts...)
	if p.FEC != nil {
		opts = append(opts, p.FEC.args()...)
	}
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
		Type:        "verity",
		Params:      b.String(),
	}
}

// VerityStatus is the decoded runtime status of a "verity" target. The kernel
// reports a single word: "V" (verified / corruption not yet seen) or "C" (a
// corruption was detected). Verified is true for "V".
type VerityStatus struct {
	Verified bool
	Raw      string
}

// ParseVerityStatus decodes the one-word verity status ("V" or "C").
func ParseVerityStatus(status string) (VerityStatus, error) {
	fields := strings.Fields(status)
	if len(fields) == 0 {
		return VerityStatus{}, fmt.Errorf("dm: empty verity status")
	}
	return VerityStatus{Verified: fields[0] == "V", Raw: status}, nil
}

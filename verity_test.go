// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import "testing"

// TestVerityParams pins the dm-verity table grammar:
// "<ver> <data> <hash> <dbs> <hbs> <ndb> <hsb> <algo> <root> <salt>".
func TestVerityParams(t *testing.T) {
	tgt := Verity(0, 2048, VerityParams{
		Version:        1,
		DataDev:        "/dev/loop0",
		HashDev:        "/dev/loop1",
		DataBlockSize:  4096,
		HashBlockSize:  4096,
		NumDataBlocks:  256,
		HashStartBlock: 0,
		Algorithm:      "sha256",
		RootDigest:     "ab12cd34",
		Salt:           "deadbeef",
	})
	if tgt.Type != "verity" {
		t.Errorf("Type = %q, want verity", tgt.Type)
	}
	want := "1 /dev/loop0 /dev/loop1 4096 4096 256 0 sha256 ab12cd34 deadbeef"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestVerityEmptySalt checks an empty salt is emitted as "-".
func TestVerityEmptySalt(t *testing.T) {
	tgt := Verity(0, 2048, VerityParams{
		Version: 1, DataDev: "253:0", HashDev: "253:1",
		DataBlockSize: 4096, HashBlockSize: 4096, NumDataBlocks: 100, HashStartBlock: 1,
		Algorithm: "sha256", RootDigest: "00ff", Salt: "",
	})
	want := "1 253:0 253:1 4096 4096 100 1 sha256 00ff -"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestVerityOpts checks the optional-arg count prefix for plain options.
func TestVerityOpts(t *testing.T) {
	tgt := Verity(0, 2048, VerityParams{
		Version: 1, DataDev: "/dev/loop0", HashDev: "/dev/loop1",
		DataBlockSize: 4096, HashBlockSize: 4096, NumDataBlocks: 256, HashStartBlock: 0,
		Algorithm: "sha256", RootDigest: "aa", Salt: "bb",
		Opts: []string{"ignore_zero_blocks", "check_at_most_once"},
	})
	want := "1 /dev/loop0 /dev/loop1 4096 4096 256 0 sha256 aa bb 2 ignore_zero_blocks check_at_most_once"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestVerityFEC checks FEC arguments are appended in kernel order and folded
// into the option count together with any plain Opts.
func TestVerityFEC(t *testing.T) {
	tgt := Verity(0, 2048, VerityParams{
		Version: 1, DataDev: "/dev/loop0", HashDev: "/dev/loop1",
		DataBlockSize: 4096, HashBlockSize: 4096, NumDataBlocks: 256, HashStartBlock: 0,
		Algorithm: "sha256", RootDigest: "aa", Salt: "bb",
		Opts: []string{"ignore_zero_blocks"},
		FEC:  &VerityFEC{Device: "/dev/loop2", Roots: 2, Blocks: 100, Start: 100},
	})
	// 1 plain opt + 8 FEC tokens = 9 optional args.
	want := "1 /dev/loop0 /dev/loop1 4096 4096 256 0 sha256 aa bb " +
		"9 ignore_zero_blocks use_fec_from_device /dev/loop2 fec_roots 2 fec_blocks 100 fec_start 100"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestParseVerityStatus covers the "V"/"C" verity status words.
func TestParseVerityStatus(t *testing.T) {
	v, err := ParseVerityStatus("V")
	if err != nil || !v.Verified {
		t.Errorf("ParseVerityStatus(V) = %+v, %v", v, err)
	}
	c, err := ParseVerityStatus("C")
	if err != nil || c.Verified {
		t.Errorf("ParseVerityStatus(C) = %+v, %v", c, err)
	}
	if _, err := ParseVerityStatus(""); err == nil {
		t.Error("expected error for empty status")
	}
}

// TestVeritySerialize round-trips a verity target through the table encoder.
func TestVeritySerialize(t *testing.T) {
	tgt := Verity(0, 2048, VerityParams{
		Version: 1, DataDev: "/dev/loop0", HashDev: "/dev/loop1",
		DataBlockSize: 4096, HashBlockSize: 4096, NumDataBlocks: 256, HashStartBlock: 0,
		Algorithm: "sha256", RootDigest: "ab12cd34", Salt: "deadbeef",
	})
	buf, err := encodeTargets([]Target{tgt})
	if err != nil {
		t.Fatalf("encodeTargets: %v", err)
	}
	out, err := parseTargets(buf, 0, 1)
	if err != nil {
		t.Fatalf("parseTargets: %v", err)
	}
	if len(out) != 1 || out[0].Type != "verity" || out[0].Params != tgt.Params {
		t.Errorf("round-trip: got %+v, want %+v", out[0], tgt)
	}
}

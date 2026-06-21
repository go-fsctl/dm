// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import "testing"

// TestThinPoolParams pins the dm-thin-pool table grammar:
// "<meta> <data> <data_block_sectors> <low_water> [<#opt> <opt>...]".
func TestThinPoolParams(t *testing.T) {
	tgt := ThinPool(0, 262144, "/dev/loop0", "/dev/loop1", 128, 1024, nil)
	if tgt.Type != "thin-pool" {
		t.Errorf("Type = %q, want thin-pool", tgt.Type)
	}
	if tgt.SectorStart != 0 || tgt.Length != 262144 {
		t.Errorf("geometry = %d/%d, want 0/262144", tgt.SectorStart, tgt.Length)
	}
	if want := "/dev/loop0 /dev/loop1 128 1024"; tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestThinPoolParamsOpts checks the feature-arg count prefix.
func TestThinPoolParamsOpts(t *testing.T) {
	tgt := ThinPool(0, 1<<20, "253:0", "253:1", 256, 512,
		[]string{"skip_block_zeroing", "ignore_discard"})
	want := "253:0 253:1 256 512 2 skip_block_zeroing ignore_discard"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestThinParams pins the dm-thin table grammar "<pool> <dev_id> [<external>]".
func TestThinParams(t *testing.T) {
	plain := Thin(0, 131072, "/dev/mapper/pool", 0, "")
	if plain.Type != "thin" {
		t.Errorf("Type = %q", plain.Type)
	}
	if want := "/dev/mapper/pool 0"; plain.Params != want {
		t.Errorf("Params = %q, want %q", plain.Params, want)
	}
	ext := Thin(0, 131072, "253:5", 7, "/dev/loop2")
	if want := "253:5 7 /dev/loop2"; ext.Params != want {
		t.Errorf("Params = %q, want %q", ext.Params, want)
	}
}

// TestParseThinPoolStatus covers the normal status line and "Fail".
func TestParseThinPoolStatus(t *testing.T) {
	// transaction_id used_meta/total_meta used_data/total_data ...flags
	raw := "1 24/12800 17/2048 - rw discard_passdown queue_if_no_space"
	s, err := ParseThinPoolStatus(raw)
	if err != nil {
		t.Fatalf("ParseThinPoolStatus: %v", err)
	}
	if !s.OK {
		t.Fatalf("OK = false, want true")
	}
	if s.TransactionID != 1 {
		t.Errorf("TransactionID = %d, want 1", s.TransactionID)
	}
	if s.UsedMetaBlocks != 24 || s.TotalMetaBlocks != 12800 {
		t.Errorf("meta = %d/%d, want 24/12800", s.UsedMetaBlocks, s.TotalMetaBlocks)
	}
	if s.UsedDataBlocks != 17 || s.TotalDataBlocks != 2048 {
		t.Errorf("data = %d/%d, want 17/2048", s.UsedDataBlocks, s.TotalDataBlocks)
	}

	fail, err := ParseThinPoolStatus("Fail")
	if err != nil {
		t.Fatalf("ParseThinPoolStatus(Fail): %v", err)
	}
	if fail.OK {
		t.Errorf("Fail.OK = true, want false")
	}
	if fail.Raw != "Fail" {
		t.Errorf("Raw = %q", fail.Raw)
	}

	if _, err := ParseThinPoolStatus(""); err == nil {
		t.Error("expected error for empty status")
	}
	if _, err := ParseThinPoolStatus("1 24/12800"); err == nil {
		t.Error("expected error for too few fields")
	}
}

// TestParseThinStatus covers "<mapped> <highest>", the "0 -" never-written
// case, and "Fail".
func TestParseThinStatus(t *testing.T) {
	s, err := ParseThinStatus("2048 5119")
	if err != nil {
		t.Fatalf("ParseThinStatus: %v", err)
	}
	if !s.OK || s.MappedSectors != 2048 || !s.HasHighest || s.HighestMappedSector != 5119 {
		t.Errorf("status = %+v, want mapped=2048 highest=5119", s)
	}

	empty, err := ParseThinStatus("0 -")
	if err != nil {
		t.Fatalf("ParseThinStatus(0 -): %v", err)
	}
	if !empty.OK || empty.MappedSectors != 0 || empty.HasHighest {
		t.Errorf("empty status = %+v, want mapped=0 HasHighest=false", empty)
	}

	fail, err := ParseThinStatus("Fail")
	if err != nil {
		t.Fatalf("ParseThinStatus(Fail): %v", err)
	}
	if fail.OK {
		t.Errorf("Fail.OK = true, want false")
	}

	if _, err := ParseThinStatus(""); err == nil {
		t.Error("expected error for empty status")
	}
}

// TestThinTargetsSerialize round-trips the new thin targets through the table
// encoder, the way TestNewTargetsSerialize does for the merged targets.
func TestThinTargetsSerialize(t *testing.T) {
	in := []Target{
		ThinPool(0, 262144, "/dev/loop0", "/dev/loop1", 128, 1024, []string{"skip_block_zeroing"}),
		Thin(0, 131072, "/dev/mapper/pool", 0, ""),
	}
	for i, tgt := range in {
		buf, err := encodeTargets([]Target{tgt})
		if err != nil {
			t.Fatalf("encodeTargets[%d]: %v", i, err)
		}
		out, err := parseTargets(buf, 0, 1)
		if err != nil {
			t.Fatalf("parseTargets[%d]: %v", i, err)
		}
		if len(out) != 1 || out[0].Type != tgt.Type || out[0].Params != tgt.Params {
			t.Errorf("target %d round-trip: got %+v, want %+v", i, out[0], tgt)
		}
	}
}

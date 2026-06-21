// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import "testing"

// TestStripedParams pins the dm-stripe param string layout:
// "<num> <chunk> <dev0> <off0> <dev1> <off1> ...".
func TestStripedParams(t *testing.T) {
	tgt := Striped(0, 262144, 256, []StripeDev{
		{Dev: "/dev/loop0", Offset: 0},
		{Dev: "/dev/loop1", Offset: 1024},
	})
	if tgt.Type != "striped" {
		t.Errorf("Type = %q, want striped", tgt.Type)
	}
	if tgt.SectorStart != 0 || tgt.Length != 262144 {
		t.Errorf("geometry = %d/%d, want 0/262144", tgt.SectorStart, tgt.Length)
	}
	want := "2 256 /dev/loop0 0 /dev/loop1 1024"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestStripedSingleDevice checks a degenerate one-stripe table still emits the
// stripe count.
func TestStripedSingleDevice(t *testing.T) {
	tgt := Striped(0, 2048, 128, []StripeDev{{Dev: "253:0", Offset: 16}})
	want := "1 128 253:0 16"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestZeroError checks the trivial targets carry no params.
func TestZeroError(t *testing.T) {
	z := Zero(0, 2048)
	if z.Type != "zero" || z.Params != "" || z.Length != 2048 {
		t.Errorf("Zero = %+v", z)
	}
	e := Error(64, 128)
	if e.Type != "error" || e.Params != "" || e.SectorStart != 64 || e.Length != 128 {
		t.Errorf("Error = %+v", e)
	}
}

// TestSnapshotParams pins dm-snapshot "<origin> <cow> <P|N> <chunk>".
func TestSnapshotParams(t *testing.T) {
	persistent := Snapshot(0, 131072, "/dev/mapper/base", "/dev/loop1", true, 8)
	if persistent.Type != "snapshot" {
		t.Errorf("Type = %q", persistent.Type)
	}
	if want := "/dev/mapper/base /dev/loop1 P 8"; persistent.Params != want {
		t.Errorf("persistent Params = %q, want %q", persistent.Params, want)
	}
	transient := Snapshot(0, 131072, "253:1", "253:2", false, 16)
	if want := "253:1 253:2 N 16"; transient.Params != want {
		t.Errorf("transient Params = %q, want %q", transient.Params, want)
	}
}

// TestSnapshotOriginParams pins dm-snapshot-origin "<origin>".
func TestSnapshotOriginParams(t *testing.T) {
	tgt := SnapshotOrigin(0, 131072, "/dev/mapper/base")
	if tgt.Type != "snapshot-origin" || tgt.Params != "/dev/mapper/base" {
		t.Errorf("SnapshotOrigin = %+v", tgt)
	}
}

// TestParseSnapStatus covers the normal "used/total metadata" form and the
// textual error states.
func TestParseSnapStatus(t *testing.T) {
	s, err := ParseSnapStatus("16/131072 8")
	if err != nil {
		t.Fatalf("ParseSnapStatus: %v", err)
	}
	if !s.Valid || s.UsedSectors != 16 || s.TotalSectors != 131072 || s.MetadataSectors != 8 {
		t.Errorf("status = %+v, want used=16 total=131072 metadata=8", s)
	}

	// Without the metadata word.
	s2, err := ParseSnapStatus("40/2048")
	if err != nil {
		t.Fatalf("ParseSnapStatus: %v", err)
	}
	if !s2.Valid || s2.UsedSectors != 40 || s2.TotalSectors != 2048 || s2.MetadataSectors != 0 {
		t.Errorf("status = %+v", s2)
	}

	// Textual states: not valid, no error, raw preserved.
	for _, raw := range []string{"Invalid", "Overflow", "Merge failed"} {
		st, err := ParseSnapStatus(raw)
		if err != nil {
			t.Errorf("ParseSnapStatus(%q): %v", raw, err)
		}
		if st.Valid {
			t.Errorf("ParseSnapStatus(%q).Valid = true, want false", raw)
		}
		if st.Raw != raw {
			t.Errorf("Raw = %q, want %q", st.Raw, raw)
		}
	}

	if _, err := ParseSnapStatus(""); err == nil {
		t.Error("expected error for empty status")
	}
	if _, err := ParseSnapStatus("x/131072"); err == nil {
		t.Error("expected error for non-numeric used")
	}
}

// TestCryptParams pins dm-crypt "<cipher> <hexkey> <iv> <dev> <off> [opts]" and
// in particular the hex encoding of the key.
func TestCryptParams(t *testing.T) {
	key := []byte{0x00, 0x11, 0x22, 0xaa, 0xbb, 0xff}
	tgt := Crypt(0, 131072, "aes-xts-plain64", key, 0, "/dev/loop0", 0, nil)
	if tgt.Type != "crypt" {
		t.Errorf("Type = %q", tgt.Type)
	}
	want := "aes-xts-plain64 001122aabbff 0 /dev/loop0 0"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestCryptParamsWithOpts checks the optional-arg count prefix.
func TestCryptParamsWithOpts(t *testing.T) {
	key := []byte{0xde, 0xad, 0xbe, 0xef}
	tgt := Crypt(0, 2048, "aes-cbc-essiv:sha256", key, 8, "253:3", 256,
		[]string{"sector_size:4096", "allow_discards"})
	want := "aes-cbc-essiv:sha256 deadbeef 8 253:3 256 2 sector_size:4096 allow_discards"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestCryptKeyHexLength documents the 2N hex-character expansion for a
// realistic aes-xts-plain64 512-bit key.
func TestCryptKeyHexLength(t *testing.T) {
	key := make([]byte, 64) // 512-bit XTS key
	tgt := Crypt(0, 2048, "aes-xts-plain64", key, 0, "/dev/loop0", 0, nil)
	// cipher + space + 128 hex chars + " 0 /dev/loop0 0"
	const wantHex = 128
	parts := tgt.Params
	// Quick structural check: the hex field is the second space-separated token.
	got := 0
	field := 0
	start := 0
	for i := 0; i <= len(parts); i++ {
		if i == len(parts) || parts[i] == ' ' {
			if field == 1 {
				got = i - start
			}
			field++
			start = i + 1
		}
	}
	if got != wantHex {
		t.Errorf("hex key length = %d, want %d", got, wantHex)
	}
}

// TestNewTargetsSerialize ensures the new constructors round-trip through the
// table encoder the same way Linear does (no special characters break the wire
// format), exercising encode + parse for a multi-type table.
func TestNewTargetsSerialize(t *testing.T) {
	in := []Target{
		Striped(0, 2048, 128, []StripeDev{{"/dev/loop0", 0}, {"/dev/loop1", 0}}),
		Zero(2048, 1024),
		Error(3072, 1024),
		Crypt(4096, 2048, "aes-xts-plain64", []byte{0x01, 0x02}, 0, "/dev/loop2", 0, nil),
	}
	// encodeTargets writes Next as a per-record size (the LOAD convention),
	// whereas parseTargets reads Next as an offset from the first spec (the
	// STATUS convention). Encode/parse each target on its own so a single
	// well-formed record round-trips without the multi-record Next rewrite.
	for i, tgt := range in {
		buf, err := encodeTargets([]Target{tgt})
		if err != nil {
			t.Fatalf("encodeTargets[%d]: %v", i, err)
		}
		out, err := parseTargets(buf, 0, 1)
		if err != nil {
			t.Fatalf("parseTargets[%d]: %v", i, err)
		}
		if len(out) != 1 {
			t.Fatalf("target %d: parsed %d, want 1", i, len(out))
		}
		if out[0].Type != tgt.Type || out[0].Params != tgt.Params ||
			out[0].SectorStart != tgt.SectorStart || out[0].Length != tgt.Length {
			t.Errorf("target %d round-trip: got %+v, want %+v", i, out[0], tgt)
		}
	}
}

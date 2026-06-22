// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import "testing"

// TestDelayParams pins the dm-delay grammar for the 3-, 6- and 9-argument
// forms: "<dev> <off> <delay> [<wdev> <woff> <wdelay> [<fdev> <foff> <fdelay>]]".
func TestDelayParams(t *testing.T) {
	read := DelayLeg{Dev: "/dev/loop0", Offset: 0, Delay: 50}

	one := Delay(0, 2048, read, nil, nil)
	if one.Type != "delay" {
		t.Errorf("Type = %q, want delay", one.Type)
	}
	if one.SectorStart != 0 || one.Length != 2048 {
		t.Errorf("geometry = %d/%d, want 0/2048", one.SectorStart, one.Length)
	}
	if want := "/dev/loop0 0 50"; one.Params != want {
		t.Errorf("3-arg Params = %q, want %q", one.Params, want)
	}

	write := &DelayLeg{Dev: "/dev/loop1", Offset: 64, Delay: 100}
	two := Delay(0, 2048, read, write, nil)
	if want := "/dev/loop0 0 50 /dev/loop1 64 100"; two.Params != want {
		t.Errorf("6-arg Params = %q, want %q", two.Params, want)
	}

	flush := &DelayLeg{Dev: "253:5", Offset: 0, Delay: 0}
	three := Delay(0, 2048, read, write, flush)
	if want := "/dev/loop0 0 50 /dev/loop1 64 100 253:5 0 0"; three.Params != want {
		t.Errorf("9-arg Params = %q, want %q", three.Params, want)
	}
}

// TestDelayFlushWithoutWrite checks that a flush leg is ignored when no write
// leg is given: the kernel only accepts the 9-arg form on top of the 6-arg one,
// so Delay degrades to the 3-arg form.
func TestDelayFlushWithoutWrite(t *testing.T) {
	read := DelayLeg{Dev: "/dev/loop0", Offset: 0, Delay: 10}
	flush := &DelayLeg{Dev: "/dev/loop2", Offset: 0, Delay: 5}
	got := Delay(0, 2048, read, nil, flush)
	if want := "/dev/loop0 0 10"; got.Params != want {
		t.Errorf("Params = %q, want %q (flush ignored without write)", got.Params, want)
	}
}

// TestFlakeyParams pins the dm-flakey grammar
// "<dev> <off> <up> <down> [<#features> <feature>...]".
func TestFlakeyParams(t *testing.T) {
	plain := Flakey(0, 4096, "/dev/loop0", 0, 8, 4, nil)
	if plain.Type != "flakey" {
		t.Errorf("Type = %q, want flakey", plain.Type)
	}
	if plain.Length != 4096 {
		t.Errorf("Length = %d, want 4096", plain.Length)
	}
	if want := "/dev/loop0 0 8 4"; plain.Params != want {
		t.Errorf("Params = %q, want %q", plain.Params, want)
	}

	feat := Flakey(0, 4096, "253:2", 16, 8, 4, []string{"drop_writes"})
	if want := "253:2 16 8 4 1 drop_writes"; feat.Params != want {
		t.Errorf("Params = %q, want %q", feat.Params, want)
	}

	// A multi-token feature (corrupt_bio_byte) contributes every token to the
	// feature count, matching the kernel parser.
	multi := Flakey(0, 4096, "/dev/loop0", 0, 1, 1,
		[]string{"corrupt_bio_byte", "32", "w", "1", "0"})
	if want := "/dev/loop0 0 1 1 5 corrupt_bio_byte 32 w 1 0"; multi.Params != want {
		t.Errorf("Params = %q, want %q", multi.Params, want)
	}
}

// TestRaidParams pins the dm-raid grammar
// "<type> <#params> <params> <#devs> <meta0> <data0> ...".
func TestRaidParams(t *testing.T) {
	tgt := Raid(0, 262144, "raid1",
		[]string{"128", "region_size", "1024", "rebuild", "1"},
		[]RaidDev{{Meta: "-", Data: "/dev/loop0"}, {Meta: "-", Data: "/dev/loop1"}})
	if tgt.Type != "raid" {
		t.Errorf("Type = %q, want raid", tgt.Type)
	}
	want := "raid1 5 128 region_size 1024 rebuild 1 2 - /dev/loop0 - /dev/loop1"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestRaidParamsWithMeta checks separate metadata devices and a raid5 layout,
// and that #raid_params counts only the chunk size when no tuning is given.
func TestRaidParamsWithMeta(t *testing.T) {
	tgt := Raid(0, 1<<20, "raid5_ls",
		[]string{"128"},
		[]RaidDev{
			{Meta: "/dev/loop0", Data: "/dev/loop1"},
			{Meta: "/dev/loop2", Data: "/dev/loop3"},
			{Meta: "/dev/loop4", Data: "/dev/loop5"},
		})
	want := "raid5_ls 1 128 3 /dev/loop0 /dev/loop1 /dev/loop2 /dev/loop3 /dev/loop4 /dev/loop5"
	if tgt.Params != want {
		t.Errorf("Params = %q, want %q", tgt.Params, want)
	}
}

// TestCacheParams pins the dm-cache constructor grammar
// "<meta> <cache> <origin> <block> <#feat> <feat>... <policy> <#pargs> <parg>...".
func TestCacheParams(t *testing.T) {
	withFeat := Cache(0, 1<<20, "/dev/loop0", "/dev/loop1", "/dev/loop2", 128,
		[]string{"writethrough"}, "smq", nil)
	if withFeat.Type != "cache" {
		t.Errorf("Type = %q, want cache", withFeat.Type)
	}
	if want := "/dev/loop0 /dev/loop1 /dev/loop2 128 1 writethrough smq 0"; withFeat.Params != want {
		t.Errorf("Params = %q, want %q", withFeat.Params, want)
	}

	withPolicy := Cache(0, 1<<20, "253:0", "253:1", "253:2", 64,
		nil, "smq", []string{"migration_threshold", "2048"})
	if want := "253:0 253:1 253:2 64 0 smq 2 migration_threshold 2048"; withPolicy.Params != want {
		t.Errorf("Params = %q, want %q", withPolicy.Params, want)
	}
}

// TestIntegrityParams pins the dm-integrity constructor grammar
// "<dev> <reserved> <tag_size> <mode> [<#opt> <opt>...]".
func TestIntegrityParams(t *testing.T) {
	plain := Integrity(0, 2048, "/dev/loop0", 0, "4", "J", nil)
	if plain.Type != "integrity" {
		t.Errorf("Type = %q, want integrity", plain.Type)
	}
	if want := "/dev/loop0 0 4 J"; plain.Params != want {
		t.Errorf("Params = %q, want %q", plain.Params, want)
	}

	// tag size "-" (take from internal hash) plus an option.
	opt := Integrity(0, 2048, "253:7", 8, "-", "J", []string{"internal_hash:crc32c"})
	if want := "253:7 8 - J 1 internal_hash:crc32c"; opt.Params != want {
		t.Errorf("Params = %q, want %q", opt.Params, want)
	}
}

// TestMoreTargetsSerialize round-trips each new target through the table encoder
// the way TestNewTargetsSerialize / TestThinTargetsSerialize do, ensuring the
// param strings survive the wire format intact.
func TestMoreTargetsSerialize(t *testing.T) {
	in := []Target{
		Delay(0, 2048, DelayLeg{"/dev/loop0", 0, 50}, &DelayLeg{"/dev/loop1", 0, 100}, nil),
		Flakey(0, 4096, "/dev/loop0", 0, 8, 4, []string{"drop_writes"}),
		Raid(0, 262144, "raid1", []string{"128"}, []RaidDev{{"-", "/dev/loop0"}, {"-", "/dev/loop1"}}),
		Cache(0, 1<<20, "/dev/loop0", "/dev/loop1", "/dev/loop2", 128, nil, "smq", nil),
		Integrity(0, 2048, "/dev/loop0", 0, "4", "J", nil),
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
		if len(out) != 1 || out[0].Type != tgt.Type || out[0].Params != tgt.Params ||
			out[0].SectorStart != tgt.SectorStart || out[0].Length != tgt.Length {
			t.Errorf("target %d round-trip: got %+v, want %+v", i, out[0], tgt)
		}
	}
}

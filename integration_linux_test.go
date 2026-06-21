// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

package dm

import (
	"os"
	"testing"
)

// These tests touch the real device-mapper subsystem. They are skipped unless
// /dev/mapper/control is present and the process is root. They create and tear
// down a uniquely named dm device backed by nothing fancier than a zero target,
// so they are safe to run on a host without setting up a loop device.
func requireDM(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("skip: /dev/mapper/control not available")
	}
	if os.Geteuid() != 0 {
		t.Skip("skip: device-mapper operations require root")
	}
}

func TestVersionLive(t *testing.T) {
	requireDM(t)
	v, err := Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v.Major != DM_VERSION_MAJOR {
		t.Errorf("kernel DM major = %d, want %d", v.Major, DM_VERSION_MAJOR)
	}
	t.Logf("kernel device-mapper interface version %s", v)
}

func TestZeroDeviceLifecycle(t *testing.T) {
	requireDM(t)
	const name = "gofsctl-itest"

	// Clean any leftover from a previous failed run.
	_ = Remove(name)

	if err := Create(name, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() {
		if err := Remove(name); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	// A "zero" target maps to /dev/zero-like behaviour and needs no backing
	// device, keeping this test self-contained.
	if err := LoadTable(name, []Target{{SectorStart: 0, Length: 2048, Type: "zero", Params: ""}}); err != nil {
		t.Fatalf("LoadTable: %v", err)
	}
	if err := Resume(name); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	info, err := Info(name)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != name {
		t.Errorf("Info.Name = %q, want %q", info.Name, name)
	}
	if !info.ActivePresent() {
		t.Errorf("expected active table present, flags=%#x", info.Flags)
	}

	tbl, err := TableStatus(name)
	if err != nil {
		t.Fatalf("TableStatus: %v", err)
	}
	if len(tbl) != 1 || tbl[0].Type != "zero" || tbl[0].Length != 2048 {
		t.Fatalf("TableStatus = %+v, want one zero target len 2048", tbl)
	}

	devs, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, d := range devs {
		if d.Name == name {
			found = true
		}
	}
	if !found {
		t.Errorf("device %q not found in List() = %+v", name, devs)
	}
}

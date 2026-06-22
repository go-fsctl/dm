// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/go-fsctl/dm"
)

var errBoom = errors.New("boom")

// restore snapshots every seam and returns a deferred restore.
func restore() func() {
	a, b, c, d, e := version, create, loadTable, resume, createWithTable
	f, g, h, i, j := createReadOnlyWithTable, snapshotStatus, thinPoolCreateThin, thinPoolCreateSnap, thinPoolDeleteThin
	k, l, m, n, o := thinPoolStatus, thinStatus, message, status, info
	p, q, r := tableStatus, list, remove
	lv, dw := listVersions, devWait
	x, so, se := osExit, stdout, stderr
	return func() {
		version, create, loadTable, resume, createWithTable = a, b, c, d, e
		createReadOnlyWithTable, snapshotStatus, thinPoolCreateThin, thinPoolCreateSnap, thinPoolDeleteThin = f, g, h, i, j
		thinPoolStatus, thinStatus, message, status, info = k, l, m, n, o
		tableStatus, list, remove = p, q, r
		listVersions, devWait = lv, dw
		osExit, stdout, stderr = x, so, se
	}
}

// happy installs an all-succeeding set of seams; individual tests then break one.
func happy() {
	version = func() (dm.DMVersion, error) { return dm.DMVersion{Major: 4, Minor: 48}, nil }
	create = func(string, string) error { return nil }
	loadTable = func(string, []dm.Target) error { return nil }
	resume = func(string) error { return nil }
	createWithTable = func(string, []dm.Target) error { return nil }
	createReadOnlyWithTable = func(string, []dm.Target) error { return nil }
	snapshotStatus = func(string) (dm.SnapStatus, error) { return dm.SnapStatus{Valid: true}, nil }
	thinPoolCreateThin = func(string, uint64) error { return nil }
	thinPoolCreateSnap = func(string, uint64, uint64) error { return nil }
	thinPoolDeleteThin = func(string, uint64) error { return nil }
	thinPoolStatus = func(string) (dm.ThinPoolStat, error) { return dm.ThinPoolStat{OK: true}, nil }
	thinStatus = func(string) (dm.ThinStat, error) { return dm.ThinStat{OK: true}, nil }
	message = func(string, uint64, string) (string, error) { return "reply", nil }
	status = func(string) ([]dm.Target, error) { return []dm.Target{dm.Linear(0, 10, "/dev/loop0", 0)}, nil }
	info = func(string) (dm.DevInfo, error) { return dm.DevInfo{Name: "vol", Dev: 253<<8 | 1}, nil }
	tableStatus = func(string) ([]dm.Target, error) { return []dm.Target{dm.Zero(0, 2048)}, nil }
	list = func() ([]dm.Device, error) { return []dm.Device{{Name: "vol", Dev: 5}}, nil }
	remove = func(string) error { return nil }
	listVersions = func() ([]dm.TargetVersion, error) {
		return []dm.TargetVersion{{Name: "raid", Version: dm.DMVersion{Major: 1, Minor: 15}}}, nil
	}
	devWait = func(string, uint32) (dm.DevInfo, error) { return dm.DevInfo{Name: "vol", EventNr: 7}, nil }
}

// runWith captures stdout/stderr and runs the command.
func runWith(args ...string) (int, string, string) {
	var out, errBuf bytes.Buffer
	stdout = &out
	stderr = &errBuf
	rc := run(append([]string{"dmtest"}, args...))
	return rc, out.String(), errBuf.String()
}

// okCmds lists every sub-command with a complete, well-formed argument set; each
// must succeed (rc 0) against the happy seams. This drives the success branch of
// every case plus mustUint's success path.
var okCmds = [][]string{
	{"version"},
	{"create", "vol"},
	{"create", "vol", "uuid"},
	{"linear", "vol", "/dev/loop0", "2048"},
	{"striped", "vol", "256", "4096", "/dev/loop0", "0", "/dev/loop1", "0"},
	{"zero", "vol", "2048"},
	{"error", "vol", "2048"},
	{"snapshot", "vol", "/dev/loop0", "/dev/loop1", "P", "8", "2048"},
	{"snaporigin", "vol", "/dev/loop0", "2048"},
	{"snapstatus", "vol"},
	{"crypt", "vol", "aes-xts-plain64", "ab", "0", "/dev/loop0", "0", "2048"},
	{"crypt", "vol", "aes-xts-plain64", "ab", "0", "/dev/loop0", "0", "2048", "allow_discards"},
	{"thinpool", "vol", "/dev/loop0", "/dev/loop1", "128", "1024", "2048"},
	{"thinpool", "vol", "/dev/loop0", "/dev/loop1", "128", "1024", "2048", "skip_block_zeroing"},
	{"createthin", "pool", "0"},
	{"createsnap", "pool", "1", "0"},
	{"deletethin", "pool", "0"},
	{"thin", "vol", "/dev/mapper/pool", "0", "2048"},
	{"thin", "vol", "/dev/mapper/pool", "0", "2048", "/dev/loop2"},
	{"thinpoolstatus", "vol"},
	{"thinstatus", "vol"},
	{"message", "vol", "0", "create_thin", "0"},
	{"verity", "vol", "1", "/dev/loop0", "/dev/loop1", "4096", "4096", "256", "0", "sha256", "abcd", "-", "2048"},
	{"verity", "vol", "1", "/dev/loop0", "/dev/loop1", "4096", "4096", "256", "0", "sha256", "abcd", "-", "2048", "ignore_zero_blocks"},
	{"delay", "vol", "/dev/loop0", "0", "50", "2048"},
	{"delay", "vol", "/dev/loop0", "0", "50", "2048", "/dev/loop1", "0", "100"},
	{"delay", "vol", "/dev/loop0", "0", "50", "2048", "/dev/loop1", "0", "100", "253:5", "0", "0"},
	{"flakey", "vol", "/dev/loop0", "0", "8", "4", "2048"},
	{"flakey", "vol", "/dev/loop0", "0", "8", "4", "2048", "drop_writes"},
	{"raid", "vol", "raid1", "262144", "1", "128", "2", "-", "/dev/loop0", "-", "/dev/loop1"},
	{"cache", "vol", "/dev/loop0", "/dev/loop1", "/dev/loop2", "128", "smq", "1048576"},
	{"cache", "vol", "/dev/loop0", "/dev/loop1", "/dev/loop2", "128", "smq", "1048576", "writethrough"},
	{"integrity", "vol", "/dev/loop0", "0", "4", "J", "2048"},
	{"integrity", "vol", "/dev/loop0", "0", "-", "J", "2048", "internal_hash:crc32c"},
	{"versions"},
	{"devwait", "vol", "0"},
	{"status", "vol"},
	{"info", "vol"},
	{"table", "vol"},
	{"list"},
	{"remove", "vol"},
}

func TestAllCommandsSucceed(t *testing.T) {
	defer restore()()
	for _, cmd := range okCmds {
		happy()
		if rc, _, errOut := runWith(cmd...); rc != exitOK {
			t.Errorf("%v: rc=%d, want 0 (stderr=%q)", cmd, rc, errOut)
		}
	}
}

func TestNoArgs(t *testing.T) {
	defer restore()()
	happy()
	if rc := run([]string{"dmtest"}); rc != exitUsage {
		t.Fatalf("rc=%d, want usage", rc)
	}
}

func TestUnknownCommand(t *testing.T) {
	defer restore()()
	happy()
	if rc, _, _ := runWith("frobnicate"); rc != exitUsage {
		t.Fatalf("rc=%d, want usage", rc)
	}
}

// tooFew lists each sub-command with one fewer argument than required, hitting
// every need()/usage shortfall branch.
var tooFew = [][]string{
	{"create"},
	{"linear", "vol", "/dev/loop0"},
	{"striped", "vol", "256", "4096", "/dev/loop0"},
	{"zero", "vol"},
	{"error", "vol"},
	{"snapshot", "vol", "/dev/loop0", "/dev/loop1", "P", "8"},
	{"snaporigin", "vol", "/dev/loop0"},
	{"snapstatus"},
	{"crypt", "vol", "aes", "ab", "0", "/dev/loop0", "0"},
	{"thinpool", "vol", "/dev/loop0", "/dev/loop1", "128", "1024"},
	{"createthin", "pool"},
	{"createsnap", "pool", "1"},
	{"deletethin", "pool"},
	{"thin", "vol", "/dev/mapper/pool", "0"},
	{"thinpoolstatus"},
	{"thinstatus"},
	{"message", "vol"},
	{"verity", "vol", "1", "/dev/loop0", "/dev/loop1", "4096", "4096", "256", "0", "sha256", "abcd", "-"},
	{"delay", "vol", "/dev/loop0", "0", "50"},
	{"flakey", "vol", "/dev/loop0", "0", "8", "4"},
	{"raid", "vol", "raid1", "262144", "1"},
	{"cache", "vol", "/dev/loop0", "/dev/loop1", "/dev/loop2", "128", "smq"},
	{"integrity", "vol", "/dev/loop0", "0", "4", "J"},
	{"devwait", "vol"},
	{"status"},
	{"info"},
	{"table"},
	{"remove"},
}

func TestTooFewArgs(t *testing.T) {
	defer restore()()
	for _, cmd := range tooFew {
		happy()
		if rc, _, _ := runWith(cmd...); rc != exitUsage {
			t.Errorf("%v: rc=%d, want usage", cmd, rc)
		}
	}
}

// TestStripedOddRest covers the odd dev/offset count -> usage branch.
func TestStripedOddRest(t *testing.T) {
	defer restore()()
	happy()
	if rc, _, _ := runWith("striped", "vol", "256", "4096", "/dev/loop0"); rc != exitUsage {
		t.Fatalf("rc=%d, want usage", rc)
	}
	// odd number in the variadic tail (3 trailing tokens).
	if rc, _, _ := runWith("striped", "vol", "256", "4096", "/dev/loop0", "0", "/dev/loop1"); rc != exitUsage {
		t.Fatalf("rc=%d, want usage", rc)
	}
}

// TestRaidCountShortfalls covers the raid case's two count-derived usage
// branches: a #raid_params that overruns the supplied tokens, and a #raid_devs
// that demands more device tokens than were given.
func TestRaidCountShortfalls(t *testing.T) {
	defer restore()()
	happy()
	// #params = 9 but far fewer param tokens present -> pEnd overruns args, so
	// there is no room for the #devs token either (len(args) < pEnd+1).
	if rc, _, _ := runWith("raid", "vol", "raid1", "262144", "9", "128"); rc != exitUsage {
		t.Fatalf("params overrun: rc=%d, want usage", rc)
	}
	// #devs = 2 but only one (meta,data) pair present.
	if rc, _, _ := runWith("raid", "vol", "raid1", "262144", "1", "128", "2", "-", "/dev/loop0"); rc != exitUsage {
		t.Fatalf("devs overrun: rc=%d, want usage", rc)
	}
}

// TestDelayFlushWithoutWrite covers the dmtest delay path where a flush leg is
// supplied without a write leg; Delay degrades to the 3-arg form and the
// command still succeeds.
func TestDelayFlushWithoutWriteCLI(t *testing.T) {
	defer restore()()
	happy()
	// 6 args triggers neither the >=10 write branch nor the >=13 flush branch.
	if rc, _, errOut := runWith("delay", "vol", "/dev/loop0", "0", "50", "2048"); rc != exitOK {
		t.Fatalf("rc=%d, want ok (stderr=%q)", rc, errOut)
	}
}

// badParse lists invocations whose numeric argument fails to parse, covering
// mustUint's error path in each position (and hex.DecodeString for crypt).
var badParse = [][]string{
	{"linear", "vol", "/dev/loop0", "xx"},
	{"striped", "vol", "xx", "4096", "/dev/loop0", "0", "/dev/loop1", "0"},
	{"striped", "vol", "256", "xx", "/dev/loop0", "0", "/dev/loop1", "0"},
	{"striped", "vol", "256", "4096", "/dev/loop0", "xx", "/dev/loop1", "0"},
	{"zero", "vol", "xx"},
	{"error", "vol", "xx"},
	{"snapshot", "vol", "/dev/loop0", "/dev/loop1", "P", "xx", "2048"},
	{"snapshot", "vol", "/dev/loop0", "/dev/loop1", "P", "8", "xx"},
	{"snaporigin", "vol", "/dev/loop0", "xx"},
	{"crypt", "vol", "aes", "zz", "0", "/dev/loop0", "0", "2048"},         // bad hex key
	{"crypt", "vol", "aes", "ab", "xx", "/dev/loop0", "0", "2048"},        // bad iv
	{"crypt", "vol", "aes", "ab", "0", "/dev/loop0", "xx", "2048"},        // bad off
	{"crypt", "vol", "aes", "ab", "0", "/dev/loop0", "0", "xx"},           // bad sectors
	{"thinpool", "vol", "/dev/loop0", "/dev/loop1", "xx", "1024", "2048"}, // bad dbs
	{"thinpool", "vol", "/dev/loop0", "/dev/loop1", "128", "xx", "2048"},  // bad low
	{"thinpool", "vol", "/dev/loop0", "/dev/loop1", "128", "1024", "xx"},  // bad sectors
	{"createthin", "pool", "xx"},
	{"createsnap", "pool", "xx", "0"},
	{"createsnap", "pool", "1", "xx"},
	{"deletethin", "pool", "xx"},
	{"thin", "vol", "/dev/mapper/pool", "xx", "2048"},
	{"thin", "vol", "/dev/mapper/pool", "0", "xx"},
	{"message", "vol", "xx", "ping"},
	{"verity", "vol", "xx", "/dev/loop0", "/dev/loop1", "4096", "4096", "256", "0", "sha256", "abcd", "-", "2048"}, // bad ver
	{"verity", "vol", "1", "/dev/loop0", "/dev/loop1", "xx", "4096", "256", "0", "sha256", "abcd", "-", "2048"},    // bad dbs
	{"verity", "vol", "1", "/dev/loop0", "/dev/loop1", "4096", "xx", "256", "0", "sha256", "abcd", "-", "2048"},    // bad hbs
	{"verity", "vol", "1", "/dev/loop0", "/dev/loop1", "4096", "4096", "xx", "0", "sha256", "abcd", "-", "2048"},   // bad ndb
	{"verity", "vol", "1", "/dev/loop0", "/dev/loop1", "4096", "4096", "256", "xx", "sha256", "abcd", "-", "2048"}, // bad hsb
	{"verity", "vol", "1", "/dev/loop0", "/dev/loop1", "4096", "4096", "256", "0", "sha256", "abcd", "-", "xx"},    // bad sectors
	{"delay", "vol", "/dev/loop0", "xx", "50", "2048"},                                                             // bad off
	{"delay", "vol", "/dev/loop0", "0", "xx", "2048"},                                                              // bad delay
	{"delay", "vol", "/dev/loop0", "0", "50", "xx"},                                                                // bad sectors
	{"delay", "vol", "/dev/loop0", "0", "50", "2048", "/dev/loop1", "xx", "100"},                                   // bad woff
	{"delay", "vol", "/dev/loop0", "0", "50", "2048", "/dev/loop1", "0", "xx"},                                     // bad wdelay
	{"delay", "vol", "/dev/loop0", "0", "50", "2048", "/dev/loop1", "0", "100", "253:5", "xx", "0"},                // bad foff
	{"delay", "vol", "/dev/loop0", "0", "50", "2048", "/dev/loop1", "0", "100", "253:5", "0", "xx"},                // bad fdelay
	{"flakey", "vol", "/dev/loop0", "xx", "8", "4", "2048"},                                                        // bad off
	{"flakey", "vol", "/dev/loop0", "0", "xx", "4", "2048"},                                                        // bad up
	{"flakey", "vol", "/dev/loop0", "0", "8", "xx", "2048"},                                                        // bad down
	{"flakey", "vol", "/dev/loop0", "0", "8", "4", "xx"},                                                           // bad sectors
	{"raid", "vol", "raid1", "xx", "1", "128", "2", "-", "/dev/loop0", "-", "/dev/loop1"},                          // bad sectors
	{"raid", "vol", "raid1", "262144", "xx", "128", "2", "-", "/dev/loop0", "-", "/dev/loop1"},                     // bad #params
	{"raid", "vol", "raid1", "262144", "1", "128", "xx", "-", "/dev/loop0", "-", "/dev/loop1"},                     // bad #devs
	{"cache", "vol", "/dev/loop0", "/dev/loop1", "/dev/loop2", "xx", "smq", "1048576"},                             // bad block
	{"cache", "vol", "/dev/loop0", "/dev/loop1", "/dev/loop2", "128", "smq", "xx"},                                 // bad sectors
	{"integrity", "vol", "/dev/loop0", "xx", "4", "J", "2048"},                                                     // bad reserved
	{"integrity", "vol", "/dev/loop0", "0", "4", "J", "xx"},                                                        // bad sectors
	{"devwait", "vol", "xx"}, // bad eventnr
}

func TestBadParseArgs(t *testing.T) {
	defer restore()()
	for _, cmd := range badParse {
		happy()
		if rc, _, _ := runWith(cmd...); rc != exitErr {
			t.Errorf("%v: rc=%d, want exitErr (1)", cmd, rc)
		}
	}
}

// errCmds maps each command to a seam-breaker that makes its dm call fail, so
// the check()-driven error-exit branch of every case is covered.
func TestCommandErrors(t *testing.T) {
	defer restore()()
	cases := []struct {
		name   string
		args   []string
		break_ func()
	}{
		{"version", []string{"version"}, func() { version = func() (dm.DMVersion, error) { return dm.DMVersion{}, errBoom } }},
		{"create", []string{"create", "vol"}, func() { create = func(string, string) error { return errBoom } }},
		{"linear-load", []string{"linear", "vol", "/dev/loop0", "2048"}, func() { loadTable = func(string, []dm.Target) error { return errBoom } }},
		{"linear-resume", []string{"linear", "vol", "/dev/loop0", "2048"}, func() { resume = func(string) error { return errBoom } }},
		{"striped", []string{"striped", "vol", "256", "4096", "/dev/loop0", "0", "/dev/loop1", "0"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"zero", []string{"zero", "vol", "2048"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"error", []string{"error", "vol", "2048"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"snapshot", []string{"snapshot", "vol", "/dev/loop0", "/dev/loop1", "P", "8", "2048"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"snaporigin", []string{"snaporigin", "vol", "/dev/loop0", "2048"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"snapstatus", []string{"snapstatus", "vol"}, func() { snapshotStatus = func(string) (dm.SnapStatus, error) { return dm.SnapStatus{}, errBoom } }},
		{"crypt", []string{"crypt", "vol", "aes", "ab", "0", "/dev/loop0", "0", "2048"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"thinpool", []string{"thinpool", "vol", "/dev/loop0", "/dev/loop1", "128", "1024", "2048"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"createthin", []string{"createthin", "pool", "0"}, func() { thinPoolCreateThin = func(string, uint64) error { return errBoom } }},
		{"createsnap", []string{"createsnap", "pool", "1", "0"}, func() { thinPoolCreateSnap = func(string, uint64, uint64) error { return errBoom } }},
		{"deletethin", []string{"deletethin", "pool", "0"}, func() { thinPoolDeleteThin = func(string, uint64) error { return errBoom } }},
		{"thin", []string{"thin", "vol", "/dev/mapper/pool", "0", "2048"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"thinpoolstatus", []string{"thinpoolstatus", "vol"}, func() { thinPoolStatus = func(string) (dm.ThinPoolStat, error) { return dm.ThinPoolStat{}, errBoom } }},
		{"thinstatus", []string{"thinstatus", "vol"}, func() { thinStatus = func(string) (dm.ThinStat, error) { return dm.ThinStat{}, errBoom } }},
		{"message", []string{"message", "vol", "0", "ping"}, func() { message = func(string, uint64, string) (string, error) { return "", errBoom } }},
		{"verity", []string{"verity", "vol", "1", "/dev/loop0", "/dev/loop1", "4096", "4096", "256", "0", "sha256", "abcd", "-", "2048"}, func() { createReadOnlyWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"delay", []string{"delay", "vol", "/dev/loop0", "0", "50", "2048"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"flakey", []string{"flakey", "vol", "/dev/loop0", "0", "8", "4", "2048"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"raid", []string{"raid", "vol", "raid1", "262144", "1", "128", "2", "-", "/dev/loop0", "-", "/dev/loop1"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"cache", []string{"cache", "vol", "/dev/loop0", "/dev/loop1", "/dev/loop2", "128", "smq", "1048576"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"integrity", []string{"integrity", "vol", "/dev/loop0", "0", "4", "J", "2048"}, func() { createWithTable = func(string, []dm.Target) error { return errBoom } }},
		{"versions", []string{"versions"}, func() { listVersions = func() ([]dm.TargetVersion, error) { return nil, errBoom } }},
		{"devwait", []string{"devwait", "vol", "0"}, func() { devWait = func(string, uint32) (dm.DevInfo, error) { return dm.DevInfo{}, errBoom } }},
		{"status", []string{"status", "vol"}, func() { status = func(string) ([]dm.Target, error) { return nil, errBoom } }},
		{"info", []string{"info", "vol"}, func() { info = func(string) (dm.DevInfo, error) { return dm.DevInfo{}, errBoom } }},
		{"table", []string{"table", "vol"}, func() { tableStatus = func(string) ([]dm.Target, error) { return nil, errBoom } }},
		{"list", []string{"list"}, func() { list = func() ([]dm.Device, error) { return nil, errBoom } }},
		{"remove", []string{"remove", "vol"}, func() { remove = func(string) error { return errBoom } }},
	}
	for _, c := range cases {
		happy()
		c.break_()
		rc, _, errOut := runWith(c.args...)
		if rc != exitErr {
			t.Errorf("%s: rc=%d, want exitErr (1)", c.name, rc)
		}
		if !strings.Contains(errOut, "boom") {
			t.Errorf("%s: stderr=%q, want it to mention the error", c.name, errOut)
		}
	}
}

// TestMainInvokesRun drives the thin main() wrapper through the osExit seam.
func TestMainInvokesRun(t *testing.T) {
	defer restore()()
	happy()
	var out, errBuf bytes.Buffer
	stdout = &out
	stderr = &errBuf
	code := -1
	osExit = func(c int) { code = c }
	main() // no args beyond program name -> usage -> exit 2
	if code != exitUsage {
		t.Fatalf("main exit=%d, want %d", code, exitUsage)
	}
}

// TestDefaultSeams confirms the production default seams are the real dm
// functions (not nil), exercising the package-var initializers. It does not call
// them against the kernel; the root integration test does that.
func TestDefaultSeams(t *testing.T) {
	defer restore()()
	if version == nil || create == nil || list == nil || remove == nil ||
		listVersions == nil || devWait == nil {
		t.Fatal("default seams must be wired to the dm package")
	}
}

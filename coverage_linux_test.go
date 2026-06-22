// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

package dm

import (
	"errors"
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// These tests drive every branch of the dm_linux.go kernel paths through the
// indirection seams in seams_linux.go, fault-injecting both success and the
// errno failures without needing root or a real /dev/mapper/control. The
// root-only integration_linux_test.go exercises the genuine DM_* ioctls.

// snapshotSeams captures the production seam values and returns a restore func.
func snapshotSeams() func() {
	o, s := osOpenFile, syscallIoctl
	return func() { osOpenFile, syscallIoctl = o, s }
}

var errInjected = errors.New("injected")

// openOK returns an osOpenFile seam that hands back a fresh real temp file so
// .Fd()/.Close() work, regardless of the requested path.
func openOK(t *testing.T) func(string, int, os.FileMode) (*os.File, error) {
	t.Helper()
	dir := t.TempDir()
	return func(string, int, os.FileMode) (*os.File, error) {
		f, err := os.CreateTemp(dir, "fd")
		if err != nil {
			t.Fatalf("temp: %v", err)
		}
		return f, nil
	}
}

// hdrAt overlays a *structDMIoctl on the dm_ioctl buffer the kernel-side fake
// is handed, letting it read and mutate the header exactly as the real kernel
// would. data is a real []byte, so no uintptr round-trip is involved.
func hdrAt(data []byte) *structDMIoctl {
	return (*structDMIoctl)(unsafe.Pointer(&data[0]))
}

// ioctlOK is a syscallIoctl seam that succeeds and touches nothing.
func ioctlOK(uintptr, uintptr, []byte) unix.Errno { return 0 }

// ioctlErr is a syscallIoctl seam that fails with ENXIO.
func ioctlErr(uintptr, uintptr, []byte) unix.Errno { return unix.ENXIO }

func TestAvailable(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errInjected }
	if Available() {
		t.Fatal("want unavailable on open error")
	}
	osOpenFile = openOK(t)
	if !Available() {
		t.Fatal("want available")
	}
}

func TestIoctlOpenError(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errInjected }
	buf := newBuffer(0)
	if err := buf.ioctl(DM_VERSION); err == nil {
		t.Fatal("want open error")
	}
}

func TestIoctlSyscallError(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	syscallIoctl = ioctlErr
	buf := newBuffer(0)
	if err := buf.ioctl(DM_VERSION); err == nil {
		t.Fatal("want errno")
	}
}

func TestSetNameTooLong(t *testing.T) {
	buf := newBuffer(0)
	long := make([]byte, dmNameLen)
	for i := range long {
		long[i] = 'a'
	}
	if err := buf.setName(string(long)); err == nil {
		t.Fatal("want too-long name error")
	}
	if err := buf.setName("ok"); err != nil {
		t.Fatalf("setName ok: %v", err)
	}
}

func TestSetUUIDTooLong(t *testing.T) {
	buf := newBuffer(0)
	long := make([]byte, dmUUIDLen)
	for i := range long {
		long[i] = 'a'
	}
	if err := buf.setUUID(string(long)); err == nil {
		t.Fatal("want too-long uuid error")
	}
	if err := buf.setUUID("ok"); err != nil {
		t.Fatalf("setUUID ok: %v", err)
	}
}

func TestNewBufferClampsTotal(t *testing.T) {
	// A negative payloadCap (total < hdrSize) clamps total back to hdrSize.
	buf := newBuffer(-1000)
	if len(buf.b) != hdrSize {
		t.Fatalf("len=%d, want hdrSize=%d", len(buf.b), hdrSize)
	}
}

func TestRunNameTooLong(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	syscallIoctl = ioctlOK
	long := make([]byte, dmNameLen)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := run(DM_DEV_REMOVE, string(long), 0); err == nil {
		t.Fatal("want setName error from run")
	}
}

func TestVersion(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	syscallIoctl = ioctlErr
	if _, err := Version(); err == nil {
		t.Fatal("want ioctl error")
	}
	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		hdrAt(data).Version = [3]uint32{4, 48, 0}
		return 0
	}
	v, err := Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v.Major != 4 || v.Minor != 48 {
		t.Fatalf("v=%+v", v)
	}
}

func TestCreate(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	syscallIoctl = ioctlOK

	// setName failure (too long).
	long := make([]byte, dmNameLen)
	for i := range long {
		long[i] = 'a'
	}
	if err := Create(string(long), ""); err == nil {
		t.Fatal("want setName error")
	}

	// setUUID failure (too long) on the uuid != "" branch.
	longU := make([]byte, dmUUIDLen)
	for i := range longU {
		longU[i] = 'b'
	}
	if err := Create("ok", string(longU)); err == nil {
		t.Fatal("want setUUID error")
	}

	// ioctl failure.
	syscallIoctl = ioctlErr
	if err := Create("ok", "uuid"); err == nil {
		t.Fatal("want ioctl error")
	}

	// success with uuid set.
	syscallIoctl = ioctlOK
	if err := Create("ok", "uuid"); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestRemove(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	syscallIoctl = ioctlErr
	if err := Remove("vol"); err == nil {
		t.Fatal("want ioctl error")
	}
	syscallIoctl = ioctlOK
	if err := Remove("vol"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestLoadTable(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	syscallIoctl = ioctlOK

	// empty targets => early error, no kernel call.
	if err := LoadTable("vol", nil); err == nil {
		t.Fatal("want empty-targets error")
	}

	// encodeTargets failure (over-length type).
	bad := []Target{{Type: "0123456789abcdef"}}
	if err := LoadTable("vol", bad); err == nil {
		t.Fatal("want encode error")
	}

	tgts := []Target{Linear(0, 100, "/dev/loop0", 0)}

	// setName failure.
	long := make([]byte, dmNameLen)
	for i := range long {
		long[i] = 'a'
	}
	if err := LoadTable(string(long), tgts); err == nil {
		t.Fatal("want setName error")
	}

	// ioctl failure.
	syscallIoctl = ioctlErr
	if err := LoadTable("vol", tgts); err == nil {
		t.Fatal("want ioctl error")
	}

	// success (plain).
	syscallIoctl = ioctlOK
	if err := LoadTable("vol", tgts); err != nil {
		t.Fatalf("LoadTable: %v", err)
	}

	// read-only variant sets DM_READONLY_FLAG and succeeds.
	if err := LoadTableReadOnly("vol", tgts); err != nil {
		t.Fatalf("LoadTableReadOnly: %v", err)
	}
}

func TestCreateWithTable(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	syscallIoctl = ioctlOK
	tgts := []Target{Zero(0, 2048)}

	// empty targets => early error.
	if err := CreateWithTable("vol", nil); err == nil {
		t.Fatal("want empty-targets error")
	}

	// Create fails.
	syscallIoctl = ioctlErr
	if err := CreateWithTable("vol", tgts); err == nil {
		t.Fatal("want Create error")
	}

	// Create ok, LoadTable fails -> Remove cleanup. The first ioctl (CREATE)
	// succeeds, the second (TABLE_LOAD) fails, the third (REMOVE) is allowed.
	{
		syscallIoctl = func(_, req uintptr, _ []byte) unix.Errno {
			if req == DM_TABLE_LOAD {
				return unix.EINVAL
			}
			return 0
		}
		if err := CreateWithTable("vol", tgts); err == nil {
			t.Fatal("want LoadTable error")
		}
	}

	// Create+Load ok, Resume fails -> Remove cleanup.
	{
		syscallIoctl = func(_, req uintptr, _ []byte) unix.Errno {
			if req == DM_DEV_SUSPEND {
				return unix.EBUSY
			}
			return 0
		}
		if err := CreateWithTable("vol", tgts); err == nil {
			t.Fatal("want Resume error")
		}
	}

	// full success.
	syscallIoctl = ioctlOK
	if err := CreateWithTable("vol", tgts); err != nil {
		t.Fatalf("CreateWithTable: %v", err)
	}

	// read-only one-shot success exercises the readOnly=true branch.
	if err := CreateReadOnlyWithTable("vol", tgts); err != nil {
		t.Fatalf("CreateReadOnlyWithTable: %v", err)
	}
}

func TestSuspendResume(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)

	syscallIoctl = ioctlErr
	if err := Suspend("vol"); err == nil {
		t.Fatal("want Suspend error")
	}
	if err := Resume("vol"); err == nil {
		t.Fatal("want Resume error")
	}

	syscallIoctl = ioctlOK
	if err := Suspend("vol"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if err := Resume("vol"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
}

func TestInfo(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)

	syscallIoctl = ioctlErr
	if _, err := Info("vol"); err == nil {
		t.Fatal("want ioctl error")
	}

	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		h := hdrAt(data)
		copy(h.Name[:], "vol")
		copy(h.UUID[:], "uuid-x")
		h.Dev = uint64(253<<8 | 1)
		h.OpenCount = 2
		h.EventNr = 5
		h.TargetCount = 1
		h.Flags = DM_ACTIVE_PRESENT_FLAG | DM_SUSPEND_FLAG
		return 0
	}
	info, err := Info("vol")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "vol" || info.UUID != "uuid-x" || info.OpenCount != 2 ||
		info.EventNr != 5 || info.TargetCnt != 1 || !info.ActivePresent() || !info.Suspended() {
		t.Fatalf("decoded wrong: %+v", info)
	}
}

func TestTableStatusAndStatus(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)

	long := make([]byte, dmNameLen)
	for i := range long {
		long[i] = 'a'
	}

	// setName failure path inside tableStatus.
	syscallIoctl = ioctlOK
	if _, err := TableStatus(string(long)); err == nil {
		t.Fatal("want setName error")
	}

	// ioctl error.
	syscallIoctl = ioctlErr
	if _, err := TableStatus("vol"); err == nil {
		t.Fatal("want ioctl error")
	}

	// BUFFER_FULL forever => "kept overflowing" after 8 attempts.
	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		hdrAt(data).Flags |= DM_BUFFER_FULL_FLAG
		return 0
	}
	if _, err := TableStatus("vol"); err == nil {
		t.Fatal("want overflow error")
	}

	// One BUFFER_FULL retry then a single encoded "zero" target.
	encoded, err := encodeTargets([]Target{Zero(0, 2048)})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	attempt := 0
	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		h := hdrAt(data)
		if attempt == 0 {
			attempt++
			h.Flags |= DM_BUFFER_FULL_FLAG
			return 0
		}
		// Write the encoded payload right after the header and report 1 target.
		copy(data[hdrSize:], encoded)
		h.DataStart = uint32(hdrSize)
		h.TargetCount = 1
		h.Flags &^= DM_BUFFER_FULL_FLAG
		return 0
	}
	tgts, err := TableStatus("vol")
	if err != nil {
		t.Fatalf("TableStatus: %v", err)
	}
	if len(tgts) != 1 || tgts[0].Type != "zero" {
		t.Fatalf("tgts=%+v", tgts)
	}

	// Status (flags=0) success too, single linear target.
	encoded2, _ := encodeTargets([]Target{Linear(0, 100, "/dev/loop0", 0)})
	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		h := hdrAt(data)
		copy(data[hdrSize:], encoded2)
		h.DataStart = uint32(hdrSize)
		h.TargetCount = 1
		return 0
	}
	if _, err := Status("vol"); err != nil {
		t.Fatalf("Status: %v", err)
	}
}

func TestList(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)

	// ioctl error.
	syscallIoctl = ioctlErr
	if _, err := List(); err == nil {
		t.Fatal("want ioctl error")
	}

	// BUFFER_FULL forever => overflow error.
	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		hdrAt(data).Flags |= DM_BUFFER_FULL_FLAG
		return 0
	}
	if _, err := List(); err == nil {
		t.Fatal("want overflow error")
	}

	// One retry then a two-entry name list.
	headSize := dmNameListHeadSize
	mk := func(dev uint64, next uint32, name string) []byte {
		rec := make([]byte, align8(headSize+len(name)+1))
		*(*structDMNameList)(unsafe.Pointer(&rec[0])) = structDMNameList{Dev: dev, Next: next}
		copy(rec[headSize:], name)
		return rec
	}
	r0 := mk(0, 0, "vol-a")
	*(*uint32)(unsafe.Pointer(&r0[8])) = uint32(len(r0))
	r1 := mk(7, 0, "vol-b")
	list := append(r0, r1...)

	attempt := 0
	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		h := hdrAt(data)
		if attempt == 0 {
			attempt++
			h.Flags |= DM_BUFFER_FULL_FLAG
			return 0
		}
		copy(data[hdrSize:], list)
		h.DataStart = uint32(hdrSize)
		h.Flags &^= DM_BUFFER_FULL_FLAG
		return 0
	}
	devs, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(devs) != 2 || devs[0].Name != "vol-a" || devs[1].Name != "vol-b" {
		t.Fatalf("devs=%+v", devs)
	}
}

func TestMessage(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)

	// setName failure.
	long := make([]byte, dmNameLen)
	for i := range long {
		long[i] = 'a'
	}
	syscallIoctl = ioctlOK
	if _, err := Message(string(long), 0, "ping"); err == nil {
		t.Fatal("want setName error")
	}

	// ioctl error.
	syscallIoctl = ioctlErr
	if _, err := Message("vol", 0, "ping"); err == nil {
		t.Fatal("want ioctl error")
	}

	// success, no reply (DM_DATA_OUT_FLAG clear).
	syscallIoctl = ioctlOK
	reply, err := Message("vol", 0, "create_thin 0")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if reply != "" {
		t.Fatalf("want empty reply, got %q", reply)
	}

	// success with a reply (DM_DATA_OUT_FLAG set, NUL-terminated string at
	// data_start).
	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		h := hdrAt(data)
		h.Flags |= DM_DATA_OUT_FLAG
		copy(data[h.DataStart:], "okreply\x00")
		return 0
	}
	reply, err = Message("vol", 0, "status")
	if err != nil {
		t.Fatalf("Message reply: %v", err)
	}
	if reply != "okreply" {
		t.Fatalf("reply=%q", reply)
	}
}

// TestParseTargetsTruncated covers the truncated-spec error branch in abi.go.
func TestParseTargetsTruncated(t *testing.T) {
	if _, err := parseTargets(make([]byte, 4), 0, 1); err == nil {
		t.Fatal("want truncated spec error")
	}
}

// TestParseNameListShortTail covers the off+headSize > len(b) break in
// parseNameList (a trailing partial record is ignored).
func TestParseNameListShortTail(t *testing.T) {
	devs, err := parseNameList(make([]byte, 4), 0) // shorter than one head
	if err != nil {
		t.Fatalf("parseNameList: %v", err)
	}
	if len(devs) != 0 {
		t.Fatalf("want no devices, got %+v", devs)
	}
}

// TestCstrNoNUL covers cstr's fall-through when there is no NUL terminator.
func TestCstrNoNUL(t *testing.T) {
	if got := cstr([]byte("abcd")); got != "abcd" {
		t.Fatalf("cstr=%q", got)
	}
}

// TestStatusReaderNonSingleTarget covers the SnapshotStatus / ThinPoolStatus /
// ThinStatus guard branches that reject a device whose active table is not the
// expected single target, plus their underlying Status error path.
func TestStatusReadersGuards(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)

	// Status error propagates through each reader.
	syscallIoctl = ioctlErr
	if _, err := SnapshotStatus("vol"); err == nil {
		t.Fatal("SnapshotStatus: want Status error")
	}
	if _, err := ThinPoolStatus("vol"); err == nil {
		t.Fatal("ThinPoolStatus: want Status error")
	}
	if _, err := ThinStatus("vol"); err == nil {
		t.Fatal("ThinStatus: want Status error")
	}

	// A single linear target is the wrong type for all three readers.
	encoded, _ := encodeTargets([]Target{Linear(0, 100, "/dev/loop0", 0)})
	syscallIoctl = statusSeam(encoded)
	if _, err := SnapshotStatus("vol"); err == nil {
		t.Fatal("SnapshotStatus: want wrong-type error")
	}
	if _, err := ThinPoolStatus("vol"); err == nil {
		t.Fatal("ThinPoolStatus: want wrong-type error")
	}
	if _, err := ThinStatus("vol"); err == nil {
		t.Fatal("ThinStatus: want wrong-type error")
	}

	// Correctly typed single targets exercise the success path through each
	// reader and its parser.
	snap, _ := encodeTargets([]Target{{Type: "snapshot", Params: "8/100 4"}})
	syscallIoctl = statusSeam(snap)
	if s, err := SnapshotStatus("vol"); err != nil || !s.Valid || s.UsedSectors != 8 {
		t.Fatalf("SnapshotStatus s=%+v err=%v", s, err)
	}

	pool, _ := encodeTargets([]Target{{Type: "thin-pool", Params: "1 4/128 16/1024 - rw discard_passdown"}})
	syscallIoctl = statusSeam(pool)
	if s, err := ThinPoolStatus("vol"); err != nil || !s.OK || s.UsedDataBlocks != 16 {
		t.Fatalf("ThinPoolStatus s=%+v err=%v", s, err)
	}

	thin, _ := encodeTargets([]Target{{Type: "thin", Params: "2048 4095"}})
	syscallIoctl = statusSeam(thin)
	if s, err := ThinStatus("vol"); err != nil || !s.OK || s.MappedSectors != 2048 || !s.HasHighest {
		t.Fatalf("ThinStatus s=%+v err=%v", s, err)
	}
}

// statusSeam returns a syscallIoctl that writes one encoded target back as the
// device's status.
func statusSeam(encoded []byte) func(uintptr, uintptr, []byte) unix.Errno {
	return func(_, _ uintptr, data []byte) unix.Errno {
		h := hdrAt(data)
		copy(data[hdrSize:], encoded)
		h.DataStart = uint32(hdrSize)
		h.TargetCount = 1
		return 0
	}
}

// TestThinPoolMessageWrappers drives the three thin-pool message wrappers
// through a succeeding ioctl seam.
func TestThinPoolMessageWrappers(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	syscallIoctl = ioctlOK
	if err := ThinPoolCreateThin("pool", 0); err != nil {
		t.Fatalf("ThinPoolCreateThin: %v", err)
	}
	if err := ThinPoolCreateSnap("pool", 1, 0); err != nil {
		t.Fatalf("ThinPoolCreateSnap: %v", err)
	}
	if err := ThinPoolDeleteThin("pool", 0); err != nil {
		t.Fatalf("ThinPoolDeleteThin: %v", err)
	}
}

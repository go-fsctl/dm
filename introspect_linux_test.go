// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

package dm

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TestListVersions drives every branch of ListVersions through the seams: the
// ioctl error, the BUFFER_FULL retry loop bottoming out, one BUFFER_FULL retry
// followed by a real two-record reply, all without root.
func TestListVersions(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)

	// ioctl error.
	syscallIoctl = ioctlErr
	if _, err := ListVersions(); err == nil {
		t.Fatal("want ioctl error")
	}

	// BUFFER_FULL forever => overflow error after 8 attempts.
	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		hdrAt(data).Flags |= DM_BUFFER_FULL_FLAG
		return 0
	}
	if _, err := ListVersions(); err == nil {
		t.Fatal("want overflow error")
	}

	// One BUFFER_FULL retry then a two-record version chain.
	headSize := dmTargetVersionsHeadSize
	mk := func(next uint32, ver [3]uint32, name string) []byte {
		rec := make([]byte, align8(headSize+len(name)+1))
		*(*structDMTargetVersions)(unsafe.Pointer(&rec[0])) = structDMTargetVersions{Next: next, Version: ver}
		copy(rec[headSize:], name)
		return rec
	}
	r0 := mk(0, [3]uint32{1, 0, 0}, "linear")
	*(*uint32)(unsafe.Pointer(&r0[0])) = uint32(len(r0))
	r1 := mk(0, [3]uint32{1, 15, 0}, "raid")
	chain := append(r0, r1...)

	attempt := 0
	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		h := hdrAt(data)
		if attempt == 0 {
			attempt++
			h.Flags |= DM_BUFFER_FULL_FLAG
			return 0
		}
		copy(data[hdrSize:], chain)
		h.DataStart = uint32(hdrSize)
		h.Flags &^= DM_BUFFER_FULL_FLAG
		return 0
	}
	tvs, err := ListVersions()
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(tvs) != 2 || tvs[0].Name != "linear" || tvs[1].Name != "raid" {
		t.Fatalf("tvs = %+v", tvs)
	}
}

// TestDevWait drives DevWait's branches: setName failure, ioctl error, and a
// success path that decodes the returned event counter.
func TestDevWait(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)

	// setName failure (over-length name), no kernel call needed.
	syscallIoctl = ioctlOK
	long := make([]byte, dmNameLen)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := DevWait(string(long), 0); err == nil {
		t.Fatal("want setName error")
	}

	// ioctl error.
	syscallIoctl = ioctlErr
	if _, err := DevWait("vol", 0); err == nil {
		t.Fatal("want ioctl error")
	}

	// success: the kernel writes back the device's woken-at event counter and
	// other status, which DevWait decodes into DevInfo.
	syscallIoctl = func(_, _ uintptr, data []byte) unix.Errno {
		h := hdrAt(data)
		copy(h.Name[:], "vol")
		copy(h.UUID[:], "uuid-y")
		h.Dev = uint64(253<<8 | 2)
		h.OpenCount = 1
		h.EventNr = 7
		h.TargetCount = 1
		h.Flags = DM_ACTIVE_PRESENT_FLAG
		return 0
	}
	info, err := DevWait("vol", 3)
	if err != nil {
		t.Fatalf("DevWait: %v", err)
	}
	if info.Name != "vol" || info.UUID != "uuid-y" || info.EventNr != 7 ||
		info.TargetCnt != 1 || !info.ActivePresent() {
		t.Fatalf("decoded wrong: %+v", info)
	}
}

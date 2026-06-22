// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

package dm

import (
	"os"
	"testing"
)

// TestSyscallIoctlSuccess covers the production syscallIoctl closure's success
// path without root. FIGETBSZ (request 0x2) writes the filesystem block size
// into the buffer and returns errno 0 for a regular file fd, unprivileged —
// exercising the real raw-SYS_IOCTL seam that the fault-injecting tests replace.
// Skipped under -test.short so the emulated (QEMU) CI jobs never issue a real
// ioctl; the native job (no -short) covers it. The genuine DM_* success path is
// additionally exercised by the root-only integration test.
func TestSyscallIoctlSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skip the real FIGETBSZ ioctl under -short (emulated CI)")
	}
	f, err := os.CreateTemp(t.TempDir(), "fd")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	buf := make([]byte, 8)
	if errno := syscallIoctl(f.Fd(), 0x2 /* FIGETBSZ */, buf); errno != 0 {
		t.Fatalf("syscallIoctl FIGETBSZ: errno %v", errno)
	}
}

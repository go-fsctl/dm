// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

package dm

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Indirection seams over the operating-system and ioctl primitives this package
// drives. They exist so the error branches of every kernel call — which only
// trigger on real DM_* ioctl failures that are impractical to provoke against a
// live control node — can be exercised deterministically by fault-injecting
// fakes in tests. Production code uses the real implementations assigned here;
// tests swap a var, run, and restore it. The root-only integration test still
// drives the genuine DM_* ioctls end to end for confidence in the real path.
var (
	osOpenFile = os.OpenFile

	// syscallIoctl issues the raw SYS_IOCTL on fd with request req and the
	// dm_ioctl buffer data as the argument. It returns the kernel errno (0 on
	// success). data is passed as a []byte (not a raw uintptr) so tests can fake
	// the kernel side of buffer.ioctl safely — a fake mutates the slice in place
	// (e.g. to set DM_BUFFER_FULL_FLAG or write a DM_DATA_OUT_FLAG reply) and
	// returns any errno, without ever converting a uintptr back to a pointer.
	// The single unsafe.Pointer conversion lives here in the production default.
	syscallIoctl = func(fd, req uintptr, data []byte) unix.Errno {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(&data[0])))
		return errno
	}
)

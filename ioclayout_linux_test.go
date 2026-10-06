// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TestIocAgainstXSys judges the _IOC encoding against a source that does not
// come from this package: golang.org/x/sys/unix, whose zerrors_linux_<arch>.go
// constants are generated per architecture from the kernel headers by a C
// compiler. x/sys has no DM_* numbers, so the judge works on the helpers, with
// one request of each direction. The pinned DM_* table in abi_test.go is
// written in the asm-generic layout and stays valid on powerpc and mips only
// because every DM command is _IOWR: READ|WRITE is 3<<30 in the asm-generic
// layout and 6<<29 in theirs, the same bits. The other three directions are
// where the layouts part, and where the old asm-generic-only encoding was
// wrong on those architectures.
//
// It is a pure computation, so it runs under -test.short on the emulated lanes.
func TestIocAgainstXSys(t *testing.T) {
	sizeofLong := unsafe.Sizeof(uintptr(0)) // C long and size_t: pointer-sized on every Linux ABI Go has
	for _, c := range []struct {
		name      string
		got, want uintptr
	}{
		{"_IO(0x12, 97) BLKFLSBUF", ioc(iocNone, 0x12, 97, 0), unix.BLKFLSBUF},
		{"_IOR(0x12, 114, size_t) BLKGETSIZE64", ioc(iocRead, 0x12, 114, sizeofLong), unix.BLKGETSIZE64},
		{"_IOW(0x94, 9, int) FICLONE", ioc(iocWrite, 0x94, 9, 4), unix.FICLONE},
		{"_IOW('f', 2, long) FS_IOC_SETFLAGS", ioc(iocWrite, 'f', 2, sizeofLong), unix.FS_IOC_SETFLAGS},
		{"_IOWR(0x94, 54, struct file_dedupe_range) FIDEDUPERANGE", iowr(0x94, 54, 24), unix.FIDEDUPERANGE},
	} {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x (x/sys/unix)", c.name, c.got, c.want)
		}
	}
}

// TestIocLayoutFillsTheWord checks that the layout selected by build tag
// accounts for all 32 bits of the request word: 8 nr + 8 type + size + dir.
func TestIocLayoutFillsTheWord(t *testing.T) {
	if got := iocDirShift + iocDirBits; got != 32 {
		t.Errorf("iocDirShift+iocDirBits = %d, want 32 (size bits %d, dir bits %d)", got, iocSizeBits, iocDirBits)
	}
}

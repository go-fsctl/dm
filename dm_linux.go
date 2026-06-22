// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

package dm

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"unsafe"
)

// controlPath is the device-mapper control node. All DM_* ioctls go through it.
const controlPath = "/dev/mapper/control"

// ErrUnsupported is returned by the non-Linux build; defined here too so the
// symbol exists on every platform.
var ErrUnsupported = errors.New("dm: DM_* ioctls are only supported on Linux")

// Available reports whether the device-mapper control node exists and can be
// opened. It does not require root, but every mutating operation does.
func Available() bool {
	f, err := osOpenFile(controlPath, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// hdrSize is sizeof(struct dm_ioctl); payload is appended after it in the same
// buffer. The kernel reads/writes the header in place and treats everything
// from data_start onward as the variable payload.
const hdrSize = int(dmIOCTLSize)

// buffer is a single contiguous allocation holding a struct dm_ioctl header
// followed by an optional payload, exactly as the kernel expects.
type buffer struct {
	b []byte
}

// newBuffer allocates a buffer with room for the header plus payloadCap bytes
// of payload and initializes the header (version, data_size, data_start).
func newBuffer(payloadCap int) *buffer {
	total := hdrSize + payloadCap
	if total < hdrSize {
		total = hdrSize
	}
	buf := &buffer{b: make([]byte, total)}
	h := buf.hdr()
	h.Version = [3]uint32{DM_VERSION_MAJOR, DM_VERSION_MINOR, DM_VERSION_PATCHLEVEL}
	h.DataSize = uint32(total)
	h.DataStart = uint32(hdrSize)
	return buf
}

// hdr returns a pointer to the dm_ioctl header overlaid on the buffer's front.
func (buf *buffer) hdr() *structDMIoctl {
	return (*structDMIoctl)(unsafe.Pointer(&buf.b[0]))
}

// setName copies name into the header's fixed name[] field (NUL-terminated).
func (buf *buffer) setName(name string) error {
	if len(name) >= dmNameLen {
		return fmt.Errorf("dm: device name %q too long (max %d)", name, dmNameLen-1)
	}
	h := buf.hdr()
	h.Name = [dmNameLen]byte{}
	copy(h.Name[:], name)
	return nil
}

// setUUID copies uuid into the header's fixed uuid[] field (NUL-terminated).
func (buf *buffer) setUUID(uuid string) error {
	if len(uuid) >= dmUUIDLen {
		return fmt.Errorf("dm: uuid %q too long (max %d)", uuid, dmUUIDLen-1)
	}
	h := buf.hdr()
	h.UUID = [dmUUIDLen]byte{}
	copy(h.UUID[:], uuid)
	return nil
}

// ioctl opens the control node and issues req with this buffer. On return the
// header has been updated in place by the kernel. The buffer must not be moved
// by the GC across the syscall; we keep it alive explicitly.
func (buf *buffer) ioctl(req uintptr) error {
	f, err := osOpenFile(controlPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("dm: open %s: %w", controlPath, err)
	}
	defer f.Close()

	errno := syscallIoctl(f.Fd(), req, buf.b)
	runtime.KeepAlive(buf.b)
	if errno != 0 {
		return errno
	}
	return nil
}

// run issues req against a freshly initialized header-only buffer carrying the
// given name and flags, and returns the buffer for the caller to read results
// from. It is the common path for the simple device-level commands.
func run(req uintptr, name string, flags uint32) (*buffer, error) {
	buf := newBuffer(0)
	if name != "" {
		if err := buf.setName(name); err != nil {
			return nil, err
		}
	}
	buf.hdr().Flags = flags
	if err := buf.ioctl(req); err != nil {
		return nil, err
	}
	return buf, nil
}

// Version negotiates the device-mapper interface version (DM_VERSION). It sends
// the version this package was compiled against and returns the version the
// running kernel reports.
func Version() (DMVersion, error) {
	buf, err := run(DM_VERSION, "", 0)
	if err != nil {
		return DMVersion{}, fmt.Errorf("dm: DM_VERSION: %w", err)
	}
	v := buf.hdr().Version
	return DMVersion{Major: v[0], Minor: v[1], Patch: v[2]}, nil
}

// Create creates a new, empty, suspended device named name (DM_DEV_CREATE).
// uuid may be empty; if set it becomes the device's persistent identifier.
func Create(name, uuid string) error {
	buf := newBuffer(0)
	if err := buf.setName(name); err != nil {
		return err
	}
	if uuid != "" {
		if err := buf.setUUID(uuid); err != nil {
			return err
		}
	}
	if err := buf.ioctl(DM_DEV_CREATE); err != nil {
		return fmt.Errorf("dm: DM_DEV_CREATE %q: %w", name, err)
	}
	return nil
}

// Remove removes the device named name and destroys its tables
// (DM_DEV_REMOVE).
func Remove(name string) error {
	if _, err := run(DM_DEV_REMOVE, name, 0); err != nil {
		return fmt.Errorf("dm: DM_DEV_REMOVE %q: %w", name, err)
	}
	return nil
}

// LoadTable loads targets into the device's inactive table slot
// (DM_TABLE_LOAD). The device is not activated until Resume is called.
func LoadTable(name string, targets []Target) error {
	return loadTable(name, targets, 0)
}

// LoadTableReadOnly is LoadTable with the device marked read-only
// (DM_READONLY_FLAG). Some targets require it: dm-verity, for instance, refuses
// to load onto a writable device ("Device must be readonly"). After this the
// device is read-only for its lifetime; a later writable LoadTable would need a
// fresh device.
func LoadTableReadOnly(name string, targets []Target) error {
	return loadTable(name, targets, DM_READONLY_FLAG)
}

func loadTable(name string, targets []Target, flags uint32) error {
	if len(targets) == 0 {
		return errors.New("dm: LoadTable requires at least one target")
	}
	payload, err := encodeTargets(targets)
	if err != nil {
		return err
	}
	buf := newBuffer(len(payload))
	if err := buf.setName(name); err != nil {
		return err
	}
	h := buf.hdr()
	h.TargetCount = uint32(len(targets))
	h.Flags = flags
	copy(buf.b[hdrSize:], payload)
	if err := buf.ioctl(DM_TABLE_LOAD); err != nil {
		return fmt.Errorf("dm: DM_TABLE_LOAD %q: %w", name, err)
	}
	return nil
}

// CreateWithTable is the one-shot convenience that performs the full bring-up
// of a device in a single call: Create (empty suspended device), LoadTable (the
// given targets into the inactive slot) and Resume (promote to active and
// enable IO). On any failure after the device is created it removes the
// half-built device so it does not leak, and returns the underlying error.
//
// It is equivalent to:
//
//	dm.Create(name, "")
//	dm.LoadTable(name, targets)
//	dm.Resume(name)
func CreateWithTable(name string, targets []Target) error {
	return createWithTable(name, targets, false)
}

// CreateReadOnlyWithTable is CreateWithTable for targets that must be loaded
// read-only (notably dm-verity): it does Create + LoadTableReadOnly + Resume,
// cleaning up the half-built device on failure.
func CreateReadOnlyWithTable(name string, targets []Target) error {
	return createWithTable(name, targets, true)
}

func createWithTable(name string, targets []Target, readOnly bool) error {
	if len(targets) == 0 {
		return errors.New("dm: CreateWithTable requires at least one target")
	}
	if err := Create(name, ""); err != nil {
		return err
	}
	load := LoadTable
	if readOnly {
		load = LoadTableReadOnly
	}
	if err := load(name, targets); err != nil {
		_ = Remove(name)
		return err
	}
	if err := Resume(name); err != nil {
		_ = Remove(name)
		return err
	}
	return nil
}

// Suspend suspends IO to the device (DM_DEV_SUSPEND with DM_SUSPEND_FLAG set).
func Suspend(name string) error {
	if _, err := run(DM_DEV_SUSPEND, name, DM_SUSPEND_FLAG); err != nil {
		return fmt.Errorf("dm: DM_DEV_SUSPEND(suspend) %q: %w", name, err)
	}
	return nil
}

// Resume resumes the device (DM_DEV_SUSPEND with the suspend flag clear). If an
// inactive table is loaded it is promoted to active and IO is (re)enabled. This
// is the step that makes a freshly created+loaded device live.
func Resume(name string) error {
	if _, err := run(DM_DEV_SUSPEND, name, 0); err != nil {
		return fmt.Errorf("dm: DM_DEV_SUSPEND(resume) %q: %w", name, err)
	}
	return nil
}

// Info returns the status of the named device (DM_DEV_STATUS): its dev_t, open
// count, event number, target count and flags.
func Info(name string) (DevInfo, error) {
	buf, err := run(DM_DEV_STATUS, name, 0)
	if err != nil {
		return DevInfo{}, fmt.Errorf("dm: DM_DEV_STATUS %q: %w", name, err)
	}
	h := buf.hdr()
	return DevInfo{
		Name:      cstr(h.Name[:]),
		UUID:      cstr(h.UUID[:]),
		Dev:       h.Dev,
		OpenCount: h.OpenCount,
		EventNr:   h.EventNr,
		TargetCnt: h.TargetCount,
		Flags:     h.Flags,
	}, nil
}

// TableStatus returns the device's active table as a slice of Targets
// (DM_TABLE_STATUS with DM_STATUS_TABLE_FLAG: the params come back as the table
// definition rather than runtime status). The buffer is grown and the ioctl
// retried if the kernel signals DM_BUFFER_FULL_FLAG.
func TableStatus(name string) ([]Target, error) {
	return tableStatus(name, DM_STATUS_TABLE_FLAG)
}

// Status returns the device's per-target runtime status (DM_TABLE_STATUS
// without DM_STATUS_TABLE_FLAG). For each target, Params holds the status
// string the target reports (e.g. for "linear" this is empty).
func Status(name string) ([]Target, error) {
	return tableStatus(name, 0)
}

func tableStatus(name string, flags uint32) ([]Target, error) {
	payloadCap := 4096
	for attempt := 0; attempt < 8; attempt++ {
		buf := newBuffer(payloadCap)
		if err := buf.setName(name); err != nil {
			return nil, err
		}
		buf.hdr().Flags = flags
		if err := buf.ioctl(DM_TABLE_STATUS); err != nil {
			return nil, fmt.Errorf("dm: DM_TABLE_STATUS %q: %w", name, err)
		}
		h := buf.hdr()
		if h.Flags&DM_BUFFER_FULL_FLAG != 0 {
			payloadCap *= 2
			continue
		}
		return parseTargets(buf.b, int(h.DataStart), int(h.TargetCount))
	}
	return nil, fmt.Errorf("dm: DM_TABLE_STATUS %q: buffer kept overflowing", name)
}

// List returns all device-mapper devices (DM_LIST_DEVICES), growing and
// retrying the buffer if the kernel reports DM_BUFFER_FULL_FLAG.
func List() ([]Device, error) {
	payloadCap := 4096
	for attempt := 0; attempt < 8; attempt++ {
		buf := newBuffer(payloadCap)
		if err := buf.ioctl(DM_LIST_DEVICES); err != nil {
			return nil, fmt.Errorf("dm: DM_LIST_DEVICES: %w", err)
		}
		h := buf.hdr()
		if h.Flags&DM_BUFFER_FULL_FLAG != 0 {
			payloadCap *= 2
			continue
		}
		return parseNameList(buf.b, int(h.DataStart))
	}
	return nil, errors.New("dm: DM_LIST_DEVICES: buffer kept overflowing")
}

// Message sends a target message to the named device (DM_TARGET_MSG). The
// payload is a struct dm_target_msg — an 8-byte sector that selects which
// target in the table the message is delivered to, immediately followed by the
// NUL-terminated message string. sector is usually 0 to address the single
// target of a one-row table (e.g. a thin-pool).
//
// Target messages are how stateful targets are driven at runtime without
// reloading their table: the thin-pool target, for instance, creates and
// deletes thin volumes and snapshots through messages such as
// "create_thin 0", "create_snap 1 0" and "delete 0". The kernel may write a
// textual reply into the buffer and set DM_DATA_OUT_FLAG; Message returns that
// reply (empty when there is none).
func Message(name string, sector uint64, msg string) (string, error) {
	// Payload: u64 sector + message bytes + NUL, the whole thing padded so the
	// buffer the kernel sees is comfortably sized.
	payloadCap := align8(dmTargetMsgHeadSize + len(msg) + 1)
	buf := newBuffer(payloadCap)
	if err := buf.setName(name); err != nil {
		return "", err
	}
	// dm_target_msg.sector at data_start, message[] right after it.
	hdr := (*structDMTargetMsg)(unsafe.Pointer(&buf.b[hdrSize]))
	hdr.Sector = sector
	copy(buf.b[hdrSize+dmTargetMsgHeadSize:], msg) // trailing byte stays 0 => NUL-terminated
	if err := buf.ioctl(DM_TARGET_MSG); err != nil {
		return "", fmt.Errorf("dm: DM_TARGET_MSG %q %q: %w", name, msg, err)
	}
	// If the target produced a reply the kernel sets DM_DATA_OUT_FLAG and writes
	// a NUL-terminated string starting at data_start.
	h := buf.hdr()
	if h.Flags&DM_DATA_OUT_FLAG != 0 && int(h.DataStart) < len(buf.b) {
		return cstr(buf.b[h.DataStart:]), nil
	}
	return "", nil
}

// Linear is a convenience constructor for a "linear" Target mapping length
// sectors starting at sectorStart onto dev at the given sector offset. dev may
// be a path ("/dev/loop0") or "major:minor".
func Linear(sectorStart, length uint64, dev string, devOffset uint64) Target {
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "linear",
		Params:      dev + " " + strconv.FormatUint(devOffset, 10),
	}
}

// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

package dm

import "fmt"

// This file holds the device-mapper introspection ioctls that do not fit the
// per-device create/load/resume lifecycle: enumerating the target types the
// kernel has registered (DM_LIST_VERSIONS) and blocking until a device's event
// counter advances (DM_DEV_WAIT). Both ride the same struct dm_ioctl protocol
// as the rest of the package.

// ListVersions enumerates the device-mapper target types the running kernel has
// registered, with the version of each (DM_LIST_VERSIONS). It is the
// programmatic equivalent of `dmsetup targets`: a target type appears here only
// once its module is loaded, so it is the way to discover at runtime whether,
// say, "raid", "cache" or "integrity" is available before attempting to load a
// table that uses it.
//
// The kernel returns a chain of struct dm_target_versions records after the
// header; the buffer is grown and the ioctl retried if the kernel signals
// DM_BUFFER_FULL_FLAG.
func ListVersions() ([]TargetVersion, error) {
	payloadCap := 4096
	for attempt := 0; attempt < 8; attempt++ {
		buf := newBuffer(payloadCap)
		if err := buf.ioctl(DM_LIST_VERSIONS); err != nil {
			return nil, fmt.Errorf("dm: DM_LIST_VERSIONS: %w", err)
		}
		h := buf.hdr()
		if h.Flags&DM_BUFFER_FULL_FLAG != 0 {
			payloadCap *= 2
			continue
		}
		return parseTargetVersions(buf.b, int(h.DataStart))
	}
	return nil, fmt.Errorf("dm: DM_LIST_VERSIONS: buffer kept overflowing")
}

// DevWait blocks until the named device's event counter has advanced past
// eventNr, then returns the device's current status (DM_DEV_WAIT). Every
// device-mapper device keeps an event counter that targets bump when something
// noteworthy happens — a RAID array finishing (or needing) a resync, a mirror
// leg failing, a thin-pool crossing its low-water mark. The idiom is to read the
// current counter from Info, do something, then DevWait on that value to block
// until the next event without polling.
//
// If eventNr already lags the device's counter the call returns immediately;
// otherwise it sleeps in the kernel until the next event (or until the device is
// removed, which returns an error). The returned DevInfo carries the counter the
// device woke up at, so a caller can loop, feeding EventNr back in to wait for
// successive events.
func DevWait(name string, eventNr uint32) (DevInfo, error) {
	buf := newBuffer(0)
	if err := buf.setName(name); err != nil {
		return DevInfo{}, err
	}
	buf.hdr().EventNr = eventNr
	if err := buf.ioctl(DM_DEV_WAIT); err != nil {
		return DevInfo{}, fmt.Errorf("dm: DM_DEV_WAIT %q: %w", name, err)
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

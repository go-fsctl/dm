// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build !linux

package dm

import "errors"

// ErrUnsupported is returned by all kernel operations on non-Linux platforms.
// The device-mapper control path (/dev/mapper/control and the DM_* ioctls)
// only exists on Linux. The ABI definitions in abi.go remain available
// everywhere for testing and tooling.
var ErrUnsupported = errors.New("dm: DM_* ioctls are only supported on Linux")

// Available reports false off Linux.
func Available() bool { return false }

// Version is unsupported off Linux.
func Version() (DMVersion, error) { return DMVersion{}, ErrUnsupported }

// Create is unsupported off Linux.
func Create(name, uuid string) error { return ErrUnsupported }

// Remove is unsupported off Linux.
func Remove(name string) error { return ErrUnsupported }

// LoadTable is unsupported off Linux.
func LoadTable(name string, targets []Target) error { return ErrUnsupported }

// LoadTableReadOnly is unsupported off Linux.
func LoadTableReadOnly(name string, targets []Target) error { return ErrUnsupported }

// CreateWithTable is unsupported off Linux.
func CreateWithTable(name string, targets []Target) error { return ErrUnsupported }

// CreateReadOnlyWithTable is unsupported off Linux.
func CreateReadOnlyWithTable(name string, targets []Target) error { return ErrUnsupported }

// Suspend is unsupported off Linux.
func Suspend(name string) error { return ErrUnsupported }

// Resume is unsupported off Linux.
func Resume(name string) error { return ErrUnsupported }

// Info is unsupported off Linux.
func Info(name string) (DevInfo, error) { return DevInfo{}, ErrUnsupported }

// TableStatus is unsupported off Linux.
func TableStatus(name string) ([]Target, error) { return nil, ErrUnsupported }

// Status is unsupported off Linux.
func Status(name string) ([]Target, error) { return nil, ErrUnsupported }

// List is unsupported off Linux.
func List() ([]Device, error) { return nil, ErrUnsupported }

// Message is unsupported off Linux.
func Message(name string, sector uint64, msg string) (string, error) { return "", ErrUnsupported }

// Linear constructs a "linear" Target. It works on every platform since it only
// builds a value; only the kernel operations are Linux-only.
func Linear(sectorStart, length uint64, dev string, devOffset uint64) Target {
	return Target{
		SectorStart: sectorStart,
		Length:      length,
		Type:        "linear",
		Params:      dev + " " + uitoa(devOffset),
	}
}

func uitoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

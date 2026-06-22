// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package dm

import "testing"

// These tests cover the remaining error branches of the pure status parsers:
// the malformed-number returns the happy-path tests in targets_test.go and
// thin_test.go do not reach (a valid first half of a fraction but an invalid
// second, a non-numeric leading field with enough columns, etc.).

func TestParseSnapStatusBadTotal(t *testing.T) {
	// Valid used, invalid total exercises the second ParseUint error branch.
	if _, err := ParseSnapStatus("5/x"); err == nil {
		t.Fatal("expected error for non-numeric total")
	}
}

func TestParseThinPoolStatusFieldErrors(t *testing.T) {
	// Non-numeric transaction id with the full 3+ columns present.
	if _, err := ParseThinPoolStatus("x 1/2 3/4"); err == nil {
		t.Fatal("expected error for non-numeric transaction id")
	}
	// Malformed metadata fraction.
	if _, err := ParseThinPoolStatus("1 a/2 3/4"); err == nil {
		t.Fatal("expected error for malformed metadata fraction")
	}
	// Malformed data fraction.
	if _, err := ParseThinPoolStatus("1 1/2 b/4"); err == nil {
		t.Fatal("expected error for malformed data fraction")
	}
}

func TestParseThinStatusFieldErrors(t *testing.T) {
	// Non-numeric mapped sectors (and not the "Fail"/"Error" sentinel).
	if _, err := ParseThinStatus("xx 10"); err == nil {
		t.Fatal("expected error for non-numeric mapped sectors")
	}
	// Valid mapped count but a non-"-" non-numeric highest sector.
	if _, err := ParseThinStatus("10 zz"); err == nil {
		t.Fatal("expected error for non-numeric highest sector")
	}
}

func TestParseFractionErrors(t *testing.T) {
	// Missing slash.
	if _, _, err := parseFraction("1234"); err == nil {
		t.Fatal("expected error for fraction without '/'")
	}
	// Bad used half.
	if _, _, err := parseFraction("x/2"); err == nil {
		t.Fatal("expected error for non-numeric used")
	}
	// Bad total half.
	if _, _, err := parseFraction("1/y"); err == nil {
		t.Fatal("expected error for non-numeric total")
	}
}

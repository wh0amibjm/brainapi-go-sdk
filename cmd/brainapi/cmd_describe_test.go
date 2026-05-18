package main

import (
	"testing"
)

// TestDescribeKindsExitCodeConsistency locks the cross-reference between
// staticErrorKinds and staticExitCodes: every kind's exit code must appear
// in the exit-code table, AND every kind listed under an exit code must
// exist in staticErrorKinds. Drift either way is a contract bug.
func TestDescribeKindsExitCodeConsistency(t *testing.T) {
	t.Parallel()

	// Every kind's exit code is a known exit code, and is listed in that
	// code's kinds slice.
	codeKinds := make(map[int]map[string]bool, len(staticExitCodes))
	for _, ec := range staticExitCodes {
		codeKinds[ec.Code] = make(map[string]bool, len(ec.Kinds))
		for _, k := range ec.Kinds {
			codeKinds[ec.Code][k] = true
		}
	}
	for _, ek := range staticErrorKinds {
		bucket, ok := codeKinds[ek.ExitCode]
		if !ok {
			t.Errorf("error kind %q claims exit code %d which is not in staticExitCodes", ek.Kind, ek.ExitCode)
			continue
		}
		if !bucket[ek.Kind] {
			t.Errorf("error kind %q has exit code %d but is not listed in staticExitCodes[%d].kinds", ek.Kind, ek.ExitCode, ek.ExitCode)
		}
	}

	// Every kind listed under an exit code exists in staticErrorKinds.
	known := make(map[string]bool, len(staticErrorKinds))
	for _, ek := range staticErrorKinds {
		known[ek.Kind] = true
	}
	for _, ec := range staticExitCodes {
		for _, k := range ec.Kinds {
			if !known[k] {
				t.Errorf("staticExitCodes[%d] lists kind %q but it's missing from staticErrorKinds", ec.Code, k)
			}
		}
	}
}

// TestDescribeWalkCommandsNonEmpty verifies buildDescribeSpec walks the
// cobra tree and emits at least every documented endpoint. A drop to zero
// means the walker stopped recursing.
func TestDescribeWalkCommandsNonEmpty(t *testing.T) {
	t.Parallel()
	spec := buildDescribeSpec(newRootCmd())
	if len(spec.Commands) < 15 {
		t.Fatalf("expected at least 15 commands in describe output, got %d", len(spec.Commands))
	}
	// Spot-check one long-poll command is correctly tagged.
	var found bool
	for _, c := range spec.Commands {
		if len(c.Path) == 2 && c.Path[0] == "alphas" && c.Path[1] == "submit" {
			found = true
			if !c.LongPoll {
				t.Errorf("`alphas submit` should be tagged longPoll=true")
			}
		}
	}
	if !found {
		t.Errorf("`alphas submit` not present in describe output")
	}
}

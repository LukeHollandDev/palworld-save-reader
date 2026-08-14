// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefile"
)

// TestWithTypeHintsMergesUnderCaller covers the merge this package exists to
// perform: every built-in hint reaches gvas, a caller can override one at the
// same path, and the caller's own Options and map come back untouched.
func TestWithTypeHintsMergesUnderCaller(t *testing.T) {
	const builtIn = ".worldSaveData.BaseCampSaveData.Key"
	if typeHints[builtIn] != "Guid" {
		t.Fatalf("test assumes a built-in hint at %s", builtIn)
	}

	caller := map[string]string{
		builtIn:       "StructProperty", // overrides a built-in
		".Custom.Key": "Guid",           // adds a new one
	}
	options := savefile.Options{GVAS: gvas.Options{TypeHints: caller}}

	merged := withTypeHints(options).GVAS.TypeHints
	if got := merged[builtIn]; got != "StructProperty" {
		t.Fatalf("caller hint did not override built-in: %q", got)
	}
	if got := merged[".Custom.Key"]; got != "Guid" {
		t.Fatalf("caller hint missing: %q", got)
	}
	if got := merged[".worldSaveData.GroupSaveDataMap.Key"]; got != "Guid" {
		t.Fatalf("built-in hint lost during merge: %q", got)
	}
	if len(merged) != len(typeHints)+1 {
		t.Fatalf("merged %d hints, want %d built-ins plus one", len(merged), len(typeHints))
	}

	if len(caller) != 2 || caller[builtIn] != "StructProperty" {
		t.Fatalf("withTypeHints mutated the caller's hint map: %#v", caller)
	}
	if options.GVAS.TypeHints[".worldSaveData.GroupSaveDataMap.Key"] != "" {
		t.Fatal("withTypeHints mutated the caller's Options")
	}
}

// TestTypeHintsReturnsACopy keeps TypeHints from handing out the package table.
func TestTypeHintsReturnsACopy(t *testing.T) {
	hints := TypeHints()
	if len(hints) != len(typeHints) {
		t.Fatalf("TypeHints returned %d entries, want %d", len(hints), len(typeHints))
	}
	hints[".worldSaveData.GroupSaveDataMap.Key"] = "tampered"
	if typeHints[".worldSaveData.GroupSaveDataMap.Key"] != "Guid" {
		t.Fatal("TypeHints exposed the package table")
	}
}

// TestTypeHintShape guards against a typo in the table, which is otherwise
// invisible: gvas silently falls back to reading an untagged property list, so a
// misspelled identity decodes a save to the wrong shape instead of failing.
//
// used lists the identities this table needs today rather than everything
// readStructBody accepts. Adding a hint for another struct means widening it
// here, which is the deliberate step the check exists to force.
func TestTypeHintShape(t *testing.T) {
	used := map[string]bool{"StructProperty": true, "Guid": true}
	for path, hint := range typeHints {
		if !used[hint] {
			t.Errorf("hint %s names unexpected struct identity %q", path, hint)
		}
		if path == "" || path[0] != '.' {
			t.Errorf("hint path %q is not a rooted property path", path)
		}
	}
}

// TestRecoverPartyMapHints pins the two untagged GUID identities populated by
// current saves. Without either hint, a GUID is misread as the first property
// name of a generic struct and full traversal fails far from the missing hint.
func TestRecoverPartyMapHints(t *testing.T) {
	for _, path := range []string{
		".worldSaveData.LevelObjectRecoverPartySaveData.Key",
		".worldSaveData.LevelObjectRecoverPartySaveData.Value.PlayerLastUsedTimes.Key",
	} {
		if got := typeHints[path]; got != "Guid" {
			t.Errorf("hint %s = %q, want Guid", path, got)
		}
	}
}

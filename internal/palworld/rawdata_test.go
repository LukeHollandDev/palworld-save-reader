// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"strings"
	"testing"
)

// TestClassifyRawData states the paths by hand rather than reading them back out
// of the table, so a path that changes has to be changed here too.
func TestClassifyRawData(t *testing.T) {
	for path, want := range map[string]RawDataKind{
		".worldSaveData.ItemContainerSaveData.Value.Slots.Slots.RawData":      RawDataItemSlot,
		".worldSaveData.CharacterSaveParameterMap.Value.RawData":              RawDataCharacter,
		".worldSaveData.CharacterContainerSaveData.Value.Slots.Slots.RawData": RawDataCharacterSlot,
		".worldSaveData.GroupSaveDataMap.Value.RawData":                       RawDataGroup,
		".worldSaveData.BaseCampSaveData.Value.RawData":                       RawDataBaseCamp,
		".worldSaveData.BaseCampSaveData.Value.WorkerDirector.RawData":        RawDataWorkerDirector,
		// Neighbouring paths that must not be mistaken for a known layout: a
		// container's own RawData, the property one level up, and the key side of
		// a map whose value side is decoded. The item and character container
		// slots differ only in the container's name, so they are the pair most
		// likely to be confused for one another.
		"": RawDataUnknown,
		".worldSaveData.ItemContainerSaveData.Value.RawData":            RawDataUnknown,
		".worldSaveData.CharacterContainerSaveData.Value.RawData":       RawDataUnknown,
		".worldSaveData.ItemContainerSaveData.Value.Slots.Slots":        RawDataUnknown,
		".worldSaveData.CharacterContainerSaveData.Value.Slots.RawData": RawDataUnknown,
		".worldSaveData.CharacterSaveParameterMap.Key.RawData":          RawDataUnknown,
		// A base camp holds three RawData properties at three depths, and only two
		// of the three are decoded. The work collection is the one that is not: it
		// parses, but its references can only be checked against WorkSaveData's own
		// undecoded RawData, so claiming it would be claiming a join nobody has
		// verified.
		".worldSaveData.BaseCampSaveData.Value.WorkCollection.RawData":  RawDataUnknown,
		".worldSaveData.BaseCampSaveData.Value.ModuleMap.Value.RawData": RawDataUnknown,
		".worldSaveData.GroupSaveDataMap.Key.RawData":                   RawDataUnknown,
	} {
		if got := ClassifyRawData(path); got != want {
			t.Errorf("ClassifyRawData(%q) = %v, want %v", path, got, want)
		}
	}
	for kind, want := range map[RawDataKind]string{
		RawDataItemSlot:       "itemSlot",
		RawDataCharacter:      "character",
		RawDataCharacterSlot:  "characterSlot",
		RawDataGroup:          "group",
		RawDataBaseCamp:       "baseCamp",
		RawDataWorkerDirector: "workerDirector",
		RawDataUnknown:        "unknown",
		RawDataKind(99):       "unknown",
	} {
		if got := kind.String(); got != want {
			t.Errorf("RawDataKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}

// TestRawDataPathsAreRootedAndReturnACopy keeps the table honest: a path that
// is not rooted the way gvas builds them would silently never match.
func TestRawDataPathsAreRootedAndReturnACopy(t *testing.T) {
	paths := RawDataPaths()
	if len(paths) != len(rawDataPaths) {
		t.Fatalf("RawDataPaths returned %d entries, want %d", len(paths), len(rawDataPaths))
	}
	for path, kind := range paths {
		if !strings.HasPrefix(path, ".") {
			t.Errorf("path %q is not a rooted property path", path)
		}
		if !strings.HasSuffix(path, ".RawData") {
			t.Errorf("path %q does not name a RawData property", path)
		}
		if kind == RawDataUnknown {
			t.Errorf("path %q maps to RawDataUnknown, which cannot be decoded", path)
		}
	}
	paths[".injected"] = RawDataItemSlot
	if _, ok := rawDataPaths[".injected"]; ok {
		t.Error("RawDataPaths exposed the package table")
	}
}

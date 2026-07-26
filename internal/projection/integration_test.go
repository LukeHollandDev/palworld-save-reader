// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"path/filepath"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/palworld"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefixtures"
)

// TestBundledPresetsAgainstPrivateFixtures applies every bundled preset to
// every private fixture save of its declared type, in strict mode. Strict is
// the point: Palworld omits properties that still hold their default value, so
// a preset that requests a field only a played-in save carries will pass on one
// fixture and fail on the next. A preset must request fields present in all of
// them.
func TestBundledPresetsAgainstPrivateFixtures(t *testing.T) {
	root := savefixtures.Root(t)
	presets, err := Presets()
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) == 0 {
		t.Fatal("no presets are bundled")
	}
	for _, preset := range presets {
		t.Run(preset.Name, func(t *testing.T) {
			document, err := ResolvePreset(preset.Name)
			if err != nil {
				t.Fatal(err)
			}
			paths := fixturesForSaveType(t, root, preset.SaveType)
			if len(paths) == 0 {
				t.Fatalf("fixture directory has no %s save to exercise", preset.SaveType)
			}
			for _, path := range paths {
				t.Run(filepath.Base(path), func(t *testing.T) {
					save, err := palworld.Load(path)
					if err != nil {
						t.Fatal(err)
					}
					output, diagnostics, err := Apply(save.Properties, document, ApplyOptions{})
					if err != nil {
						t.Fatalf("Apply: %v; diagnostics: %#v", err, diagnostics)
					}
					if output == nil {
						t.Fatal("Apply returned nil output")
					}
				})
			}
		})
	}
}

// fixturesForSaveType maps a preset's declared saveType onto fixture files. A
// preset naming a type with no mapping fails loudly rather than silently going
// unexercised.
func fixturesForSaveType(t *testing.T, root, saveType string) []string {
	t.Helper()
	switch saveType {
	case "player.sav":
		// DPS saves live beside player saves but carry a different root
		// structure, so player presets are not expected to match them.
		return savefixtures.NormalPlayerSaves(t, root)
	case "Level.sav", "LevelMeta.sav":
		return []string{filepath.Join(root, saveType)}
	default:
		t.Fatalf("no fixture mapping for save type %q", saveType)
		return nil
	}
}

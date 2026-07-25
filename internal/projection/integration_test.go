// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/palsav"
)

func TestPlayerDetailsPresetAgainstPrivateFixtures(t *testing.T) {
	root := os.Getenv("PALWORLD_SAVE_FIXTURES")
	if root == "" {
		t.Skip("set PALWORLD_SAVE_FIXTURES to a directory containing external saves")
	}
	paths, err := filepath.Glob(filepath.Join(root, "Players", "*.sav"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := ResolvePreset("player-details", "1.0.1.100619")
	if err != nil {
		t.Fatal(err)
	}
	tested := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_dps.sav") {
			continue
		}
		tested++
		t.Run(filepath.Base(path), func(t *testing.T) {
			save, err := palsav.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			output, diagnostics, err := Apply(save.Properties, document, Options{})
			if err != nil {
				t.Fatalf("Apply: %v; diagnostics: %#v", err, diagnostics)
			}
			if output == nil {
				t.Fatal("Apply returned nil output")
			}
		})
	}
	if tested == 0 {
		t.Fatal("fixture directory contains no non-DPS player saves")
	}
}

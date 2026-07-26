// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// presetFS holds the bundled projection documents so --preset and
// --list-presets resolve without a checkout or a network. The pattern selects
// only .json, keeping the directory's README out of the executable.
//
//go:embed presets/*.json
var presetFS embed.FS

const presetsRoot = "presets"

// Preset describes one bundled projection. GameVersion records the Palworld
// build the document was verified against; it is provenance reported to the
// caller, not part of the preset's identity.
type Preset struct {
	Name        string `json:"name"`
	GameVersion string `json:"gameVersion"`
	SaveType    string `json:"saveType"`
}

var presetCatalog, presetCatalogError = loadPresetCatalog()

// Presets returns the bundled projections in stable name order.
func Presets() ([]Preset, error) {
	if presetCatalogError != nil {
		return nil, presetCatalogError
	}
	output := make([]Preset, 0, len(presetCatalog))
	for _, document := range presetCatalog {
		output = append(output, Preset{
			Name:        document.Name,
			GameVersion: document.GameVersion,
			SaveType:    document.SaveType,
		})
	}
	sort.Slice(output, func(left, right int) bool {
		return output[left].Name < output[right].Name
	})
	return output, nil
}

// ResolvePreset finds a bundled projection by its exact name.
func ResolvePreset(name string) (*Document, error) {
	if presetCatalogError != nil {
		return nil, presetCatalogError
	}
	document, ok := presetCatalog[name]
	if !ok {
		return nil, fmt.Errorf("projection: preset %q is not bundled", name)
	}
	return document, nil
}

func loadPresetCatalog() (map[string]*Document, error) {
	catalog := make(map[string]*Document)
	err := fs.WalkDir(presetFS, presetsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		data, err := presetFS.ReadFile(path)
		if err != nil {
			return err
		}
		document, err := Parse(data)
		if err != nil {
			return fmt.Errorf("embedded preset %s: %w", path, err)
		}
		if _, exists := catalog[document.Name]; exists {
			return fmt.Errorf("projection: duplicate embedded preset %q", document.Name)
		}
		catalog[document.Name] = document
		return nil
	})
	if err != nil {
		return nil, err
	}
	return catalog, nil
}

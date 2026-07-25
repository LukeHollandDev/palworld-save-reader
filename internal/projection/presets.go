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

//go:embed assets
var assets embed.FS

// Preset describes one bundled, versioned projection.
type Preset struct {
	Name        string `json:"name"`
	GameVersion string `json:"gameVersion"`
	SaveType    string `json:"saveType"`
}

type embeddedPreset struct {
	document *Document
}

var presetCatalog, presetCatalogError = loadPresetCatalog()

// Presets returns the bundled projections in stable version/name order.
func Presets() ([]Preset, error) {
	if presetCatalogError != nil {
		return nil, presetCatalogError
	}
	output := make([]Preset, 0, len(presetCatalog))
	for _, item := range presetCatalog {
		output = append(output, Preset{
			Name:        item.document.Name,
			GameVersion: item.document.GameVersion,
			SaveType:    item.document.SaveType,
		})
	}
	sort.Slice(output, func(left, right int) bool {
		if output[left].GameVersion != output[right].GameVersion {
			return output[left].GameVersion < output[right].GameVersion
		}
		return output[left].Name < output[right].Name
	})
	return output, nil
}

// ResolvePreset finds an exact name and game-version pair.
func ResolvePreset(name, gameVersion string) (*Document, error) {
	if presetCatalogError != nil {
		return nil, presetCatalogError
	}
	item, ok := presetCatalog[presetKey(name, gameVersion)]
	if !ok {
		return nil, fmt.Errorf("projection: preset %q is not bundled for Palworld %q", name, gameVersion)
	}
	return item.document, nil
}

// Schema returns the bundled projection format JSON Schema.
func Schema() ([]byte, error) {
	return assets.ReadFile("assets/projection.schema.json")
}

func loadPresetCatalog() (map[string]embeddedPreset, error) {
	catalog := make(map[string]embeddedPreset)
	err := fs.WalkDir(assets, "assets/palworld", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		document, err := Parse(data)
		if err != nil {
			return fmt.Errorf("embedded preset %s: %w", path, err)
		}
		key := presetKey(document.Name, document.GameVersion)
		if _, exists := catalog[key]; exists {
			return fmt.Errorf(
				"projection: duplicate embedded preset %q for Palworld %q",
				document.Name,
				document.GameVersion,
			)
		}
		catalog[key] = embeddedPreset{document: document}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return catalog, nil
}

func presetKey(name, gameVersion string) string {
	return gameVersion + "\x00" + name
}

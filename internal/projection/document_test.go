// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validDocument = `{
  "$schema": "projection-v1.schema.json",
  "projectionVersion": 1,
  "name": "player-fields",
  "gameVersion": "1.0.0",
  "saveType": "Level.sav",
  "shape": {
    "worldSaveData": {
      "Players": [
        {
          "NickName": "",
          "Level": 0,
          "Enabled": false,
          "Identifier": null
        }
      ]
    }
  }
}`

func TestParseProjectionDocument(t *testing.T) {
	document, err := Parse([]byte(validDocument))
	if err != nil {
		t.Fatal(err)
	}
	if document.ProjectionVersion != 1 ||
		document.Name != "player-fields" ||
		document.GameVersion != "1.0.0" ||
		document.SaveType != "Level.sav" {
		t.Fatalf("metadata = %#v", document)
	}
	if document.Shape.Kind != KindObject || document.Shape.Fields[0].Name != "worldSaveData" {
		t.Fatalf("shape = %#v", document.Shape)
	}
}

func TestParseRejectsInvalidDocuments(t *testing.T) {
	tests := map[string]string{
		"duplicate metadata":  `{"projectionVersion":1,"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","shape":{"x":null}}`,
		"duplicate shape key": `{"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","shape":{"x":null,"x":0}}`,
		"unknown metadata":    `{"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","extra":true,"shape":{"x":null}}`,
		"unsupported version": `{"projectionVersion":2,"name":"x","gameVersion":"1","saveType":"x","shape":{"x":null}}`,
		"invalid name":        `{"projectionVersion":1,"name":"Player Fields","gameVersion":"1","saveType":"x","shape":{"x":null}}`,
		"empty array":         `{"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","shape":{"x":[]}}`,
		"two array items":     `{"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","shape":{"x":[null,null]}}`,
		"empty object":        `{"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","shape":{}}`,
		"string literal":      `{"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","shape":{"x":"value"}}`,
		"number literal":      `{"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","shape":{"x":1}}`,
		"boolean literal":     `{"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","shape":{"x":true}}`,
		"primitive root":      `{"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","shape":null}`,
		"trailing JSON":       `{"projectionVersion":1,"name":"x","gameVersion":"1","saveType":"x","shape":{"x":null}} true`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(input))
			if err == nil {
				t.Fatal("Parse accepted invalid document")
			}
			var invalidDocument *InvalidDocumentError
			if !errors.As(err, &invalidDocument) {
				t.Fatalf("error = %T %v, want InvalidDocumentError", err, err)
			}
		})
	}
}

// TestProjectionSchemaIsValidJSON checks the published format contract, which
// lives at the repository root and is deliberately not embedded: nothing in the
// executable reads it, because Parse is the authoritative validator.
func TestProjectionSchemaIsValidJSON(t *testing.T) {
	schema, err := os.ReadFile(filepath.Join("..", "..", "projection-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is invalid JSON: %v", err)
	}
}

func TestProjectionDocumentLimits(t *testing.T) {
	_, err := ParseReader(bytes.NewReader(bytes.Repeat([]byte{' '}, maxDocumentBytes+1)))
	if err == nil || !IsInvalidDocument(err) {
		t.Fatalf("oversized error = %v", err)
	}

	var shape strings.Builder
	for range maxShapeDepth + 1 {
		shape.WriteString(`{"nested":`)
	}
	shape.WriteString(`null`)
	for range maxShapeDepth + 1 {
		shape.WriteByte('}')
	}
	input := `{"projectionVersion":1,"name":"deep","gameVersion":"test","saveType":"test.sav","shape":` + shape.String() + `}`
	if _, err := Parse([]byte(input)); err == nil || !IsInvalidDocument(err) {
		t.Fatalf("deeply nested error = %v", err)
	}
}

func TestEmbeddedPresetCatalog(t *testing.T) {
	presets, err := Presets()
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]struct{})
	for _, preset := range presets {
		if _, exists := seen[preset.Name]; exists {
			t.Fatalf("duplicate preset name %q", preset.Name)
		}
		seen[preset.Name] = struct{}{}
		document, err := ResolvePreset(preset.Name)
		if err != nil {
			t.Fatal(err)
		}
		if document.SaveType != preset.SaveType {
			t.Fatalf("resolved save type = %q, want %q", document.SaveType, preset.SaveType)
		}
		// gameVersion is provenance the catalog reports but never matches on.
		if preset.GameVersion == "" {
			t.Fatalf("preset %q does not record a game version", preset.Name)
		}
	}
	if len(presets) == 0 {
		t.Fatal("no presets are embedded")
	}
}

func TestResolvePresetRejectsUnknownName(t *testing.T) {
	if _, err := ResolvePreset("not-a-bundled-preset"); err == nil {
		t.Fatal("ResolvePreset accepted an unknown preset name")
	}
}

func FuzzParse(fuzz *testing.F) {
	fuzz.Add([]byte(validDocument))
	fuzz.Add([]byte(`{}`))
	fuzz.Add([]byte(`{"shape":{"x":[null]}}`))
	fuzz.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxDocumentBytes+1 {
			t.Skip()
		}
		document, err := Parse(data)
		if err == nil && (document == nil || document.Shape == nil) {
			t.Fatal("successful Parse returned nil document or shape")
		}
		if err != nil && strings.Contains(err.Error(), "\x00") {
			t.Fatal("error contains NUL")
		}
	})
}

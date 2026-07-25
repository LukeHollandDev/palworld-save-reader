// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const validDocument = `{
  "$schema": "projection.schema.json",
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

func TestProjectionSchemaIsValidJSON(t *testing.T) {
	schema, err := Schema()
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
		key := preset.GameVersion + "\x00" + preset.Name
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate preset %#v", preset)
		}
		seen[key] = struct{}{}
		document, err := ResolvePreset(preset.Name, preset.GameVersion)
		if err != nil {
			t.Fatal(err)
		}
		if document.SaveType != preset.SaveType {
			t.Fatalf("resolved save type = %q, want %q", document.SaveType, preset.SaveType)
		}
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

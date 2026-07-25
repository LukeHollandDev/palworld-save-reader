// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LukeHollandDev/palworld-save-reader/internal/palsav"
)

func TestApplyNestedRepeatedProjection(t *testing.T) {
	properties := palsav.Properties{{
		Name: "worldSaveData",
		Value: palsav.StructValue{Type: "World", Value: palsav.Properties{{
			Name: "Players",
			Value: []any{
				palsav.Properties{
					{Name: "NickName", Value: "Ada"},
					{Name: "Level", Value: int32(42)},
					{Name: "Ignored", Value: "not projected"},
				},
				palsav.Properties{
					{Name: "NickName", Value: "Lin"},
					{Name: "Level", Value: uint8(7)},
				},
			},
		}}},
	}}
	document := mustParseDocument(t, `{
		"projectionVersion": 1,
		"name": "players",
		"gameVersion": "test",
		"saveType": "Level.sav",
		"shape": {
			"worldSaveData": {
				"Players": [{"NickName": "", "Level": 0}]
			}
		}
	}`)
	output, diagnostics, err := Apply(properties, document, Options{Explain: true})
	if err != nil {
		t.Fatal(err)
	}
	got := compactJSON(t, output)
	want := `{"worldSaveData":{"Players":[{"NickName":"Ada","Level":42},{"NickName":"Lin","Level":7}]}}`
	if got != want {
		t.Fatalf("output = %s\nwant   = %s", got, want)
	}
	if !hasDiagnostic(diagnostics, DiagnosticRoot) || !hasDiagnostic(diagnostics, DiagnosticSelected) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestApplyStrictAndPartialMissingFields(t *testing.T) {
	properties := palsav.Properties{
		{Name: "NickName", Value: "Ada"},
		{Name: "Level", Value: int32(42)},
	}
	document := mustParseDocument(t, `{
		"projectionVersion": 1,
		"name": "players",
		"gameVersion": "test",
		"saveType": "player.sav",
		"shape": {"NickName": "", "Level": 0, "Missing": false}
	}`)
	if _, diagnostics, err := Apply(properties, document, Options{}); err == nil {
		t.Fatal("strict Apply succeeded with a missing field")
	} else if !hasDiagnostic(diagnostics, DiagnosticMissing) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}

	output, diagnostics, err := Apply(properties, document, Options{AllowPartial: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := compactJSON(t, output), `{"NickName":"Ada","Level":42,"Missing":null}`; got != want {
		t.Fatalf("output = %s, want %s", got, want)
	}
	if !hasDiagnostic(diagnostics, DiagnosticMissing) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestApplyRejectsTypeMismatch(t *testing.T) {
	properties := palsav.Properties{{Name: "Level", Value: "forty-two"}}
	document := mustParseDocument(t, `{
		"projectionVersion": 1,
		"name": "level",
		"gameVersion": "test",
		"saveType": "player.sav",
		"shape": {"Level": 0}
	}`)
	_, diagnostics, err := Apply(properties, document, Options{})
	if err == nil || !hasDiagnostic(diagnostics, DiagnosticIncompatible) {
		t.Fatalf("err = %v, diagnostics = %#v", err, diagnostics)
	}
}

func TestApplyRejectsAmbiguousCandidates(t *testing.T) {
	properties := palsav.Properties{
		{Name: "First", Value: palsav.StructValue{Value: palsav.Properties{{Name: "NickName", Value: "Ada"}}}},
		{Name: "Second", Value: palsav.StructValue{Value: palsav.Properties{{Name: "NickName", Value: "Lin"}}}},
	}
	document := mustParseDocument(t, `{
		"projectionVersion": 1,
		"name": "name",
		"gameVersion": "test",
		"saveType": "player.sav",
		"shape": {"NickName": ""}
	}`)
	_, diagnostics, err := Apply(properties, document, Options{})
	if err == nil || !hasDiagnostic(diagnostics, DiagnosticAmbiguous) {
		t.Fatalf("err = %v, diagnostics = %#v", err, diagnostics)
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyNullCopiesAnyCompatibleValue(t *testing.T) {
	properties := palsav.Properties{{
		Name:  "Position",
		Value: palsav.Vector{X: 1, Y: 2, Z: 3},
	}}
	document := mustParseDocument(t, `{
		"projectionVersion": 1,
		"name": "position",
		"gameVersion": "test",
		"saveType": "player.sav",
		"shape": {"Position": null}
	}`)
	output, _, err := Apply(properties, document, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := compactJSON(t, output), `{"Position":{"X":1,"Y":2,"Z":3}}`; got != want {
		t.Fatalf("output = %s, want %s", got, want)
	}
}

func mustParseDocument(t *testing.T, input string) *Document {
	t.Helper()
	document, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func compactJSON(t *testing.T, value *Value) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func hasDiagnostic(diagnostics []Diagnostic, kind DiagnosticKind) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Kind == kind {
			return true
		}
	}
	return false
}

func FuzzApplySynthetic(fuzz *testing.F) {
	fuzz.Add("Field", "value")
	fuzz.Add("Level", "42")
	fuzz.Fuzz(func(t *testing.T, field, value string) {
		if field == "" || len(field) > 100 || !utf8.ValidString(field) || strings.ContainsAny(field, ".[]") {
			t.Skip()
		}
		shape, err := json.Marshal(map[string]any{field: nil})
		if err != nil {
			t.Fatal(err)
		}
		documentJSON := `{"projectionVersion":1,"name":"fuzz","gameVersion":"test","saveType":"test.sav","shape":` + string(shape) + `}`
		document, err := Parse([]byte(documentJSON))
		if err != nil {
			t.Skip()
		}
		properties := palsav.Properties{{Name: field, Value: value}}
		output, _, err := Apply(properties, document, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := json.Marshal(output); err != nil {
			t.Fatal(err)
		}
	})
}

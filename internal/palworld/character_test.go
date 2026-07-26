// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// streamBuilder writes the Unreal property encoding, enough of it to build a
// character blob by hand. The point of building one rather than embedding a real
// blob is that a fixture blob is private player data; see the package tests that
// take PALWORLD_SAVE_FIXTURES for the checks against a real world.
type streamBuilder struct {
	data []byte
}

func (b *streamBuilder) u32(value uint32) {
	b.data = binary.LittleEndian.AppendUint32(b.data, value)
}

func (b *streamBuilder) fstring(value string) {
	b.u32(uint32(len(value) + 1))
	b.data = append(b.data, value...)
	b.data = append(b.data, 0)
}

// property writes one tagged property. metadata writes the type-specific part of
// the tag, which sits between the array index and the optional-GUID flag.
func (b *streamBuilder) property(name, propertyType string, payload []byte, metadata func(*streamBuilder)) {
	b.fstring(name)
	b.fstring(propertyType)
	b.u32(uint32(len(payload)))
	b.u32(0)
	if metadata != nil {
		metadata(b)
	}
	b.data = append(b.data, 0) // no property GUID
	b.data = append(b.data, payload...)
}

func (b *streamBuilder) none() { b.fstring("None") }

// characterParameters builds the inner PalIndividualCharacterSaveParameter body.
func characterParameters(nickname string, level byte) []byte {
	var inner streamBuilder
	inner.property("Level", "ByteProperty", []byte{level}, func(tag *streamBuilder) {
		tag.fstring("None")
	})
	if nickname != "" {
		var payload streamBuilder
		payload.fstring(nickname)
		inner.property("NickName", "StrProperty", payload.data, nil)
	}
	inner.none()
	return inner.data
}

// The trailer layout, stated here rather than read out of character.go. Building
// the fixture from the same constant the decoder reads would make the offset test
// self-fulfilling: both would move together and the assertion would still pass.
const (
	wantTrailerBytes   = 24
	wantGroupIDOffset  = 4
	wantGroupIDDecoded = "7bf717b9-4a4b-4838-b10a-0a54e4cc168f"
)

// wantGroupIDBytes is that GUID as Palworld writes it: four little-endian words.
var wantGroupIDBytes = []byte{
	0xb9, 0x17, 0xf7, 0x7b,
	0x38, 0x48, 0x4b, 0x4a,
	0x54, 0x0a, 0x0a, 0xb1,
	0x8f, 0x16, 0xcc, 0xe4,
}

// characterBlob builds a whole RawData payload: the SaveParameter stream, the
// terminator, then the framing trailer with a group id inside it.
func characterBlob(nickname string, level byte, group []byte, trailer []byte) []byte {
	var stream streamBuilder
	stream.property("SaveParameter", "StructProperty", characterParameters(nickname, level),
		func(tag *streamBuilder) {
			tag.fstring("PalIndividualCharacterSaveParameter")
			tag.data = append(tag.data, make([]byte, guidBytes)...) // struct GUID
		})
	stream.none()
	if trailer == nil {
		// Four zero bytes, the group id, four more zero bytes -- exactly what
		// every blob in the fixture world carries.
		trailer = make([]byte, wantTrailerBytes)
	}
	if len(trailer) >= wantGroupIDOffset+len(group) {
		copy(trailer[wantGroupIDOffset:], group)
	}
	return append(stream.data, trailer...)
}

func TestDecodeCharacter(t *testing.T) {
	character, err := DecodeCharacter(characterBlob("Luke", 46, wantGroupIDBytes, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := character.GroupID.String(); got != wantGroupIDDecoded {
		t.Errorf("GroupID = %s, want %s", got, wantGroupIDDecoded)
	}
	if len(character.Properties) != 1 || character.Properties[0].Name != "SaveParameter" {
		t.Fatalf("Properties = %+v", character.Properties)
	}
	parameters := character.Parameters()
	if parameters == nil {
		t.Fatal("Parameters returned nil for a well-formed record")
	}
	nickname := parameters.Find("NickName")
	if nickname == nil {
		t.Fatal("no NickName")
	}
	if name, _ := nickname.Value.(string); name != "Luke" {
		t.Errorf("NickName = %v", nickname.Value)
	}
	level := parameters.Find("Level")
	if level == nil {
		t.Fatal("no Level")
	}
	if value, _ := level.Value.(uint8); value != 46 {
		t.Errorf("Level = %v (%T)", level.Value, level.Value)
	}
	if character.PaddingBeyondGroupID() {
		t.Error("a fixture-shaped trailer reported padding beyond the group id")
	}
}

// TestDecodeCharacterRejectsAMalformedStream keeps a bad blob an error rather
// than an empty Character: the whole point of the nested decode is that the
// stream is well-formed, so a failure has to be visible.
func TestDecodeCharacterRejectsAMalformedStream(t *testing.T) {
	valid := characterBlob("Luke", 1, wantGroupIDBytes, nil)
	for _, testCase := range []struct {
		name string
		blob []byte
	}{
		{name: "empty", blob: nil},
		{name: "truncated mid-stream", blob: valid[:len(valid)/2]},
		{name: "no terminator", blob: valid[:len(valid)-wantTrailerBytes-5]},
		{name: "not a property stream at all", blob: []byte("GVAS not a bare stream")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := DecodeCharacter(testCase.blob); err == nil {
				t.Fatal("expected an error")
			} else if !strings.Contains(err.Error(), "character record") {
				t.Errorf("error %q does not name what failed", err)
			}
		})
	}
}

// TestDecodeCharacterToleratesUnexpectedFraming is the other side of that: the
// framing after the terminator is not something this package understands, so an
// unfamiliar width must still yield a readable character rather than an error.
func TestDecodeCharacterToleratesUnexpectedFraming(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		trailer     []byte
		wantGroup   bool
		wantPadding bool
	}{
		{name: "no trailer at all", trailer: []byte{}},
		{name: "too short for a group id", trailer: make([]byte, 12)},
		{name: "short and non-zero", trailer: []byte{0, 0, 1}, wantPadding: true},
		{
			name:      "extended by a future game build",
			trailer:   append(make([]byte, wantTrailerBytes), 0, 0, 0, 0),
			wantGroup: true,
		},
		{
			name:        "something in the padding",
			trailer:     append(make([]byte, wantTrailerBytes-1), 3),
			wantGroup:   true,
			wantPadding: true,
		},
		{
			name:        "something before the group id",
			trailer:     append([]byte{9}, make([]byte, wantTrailerBytes-1)...),
			wantGroup:   true,
			wantPadding: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			character, err := DecodeCharacter(characterBlob("Pal", 1, wantGroupIDBytes, testCase.trailer))
			if err != nil {
				t.Fatal(err)
			}
			if character.Parameters() == nil {
				t.Error("unfamiliar framing lost the parameters")
			}
			if got := len(character.Trailer); got != len(testCase.trailer) {
				t.Errorf("Trailer = %d bytes, want %d", got, len(testCase.trailer))
			}
			if got := character.GroupID.String() == wantGroupIDDecoded; got != testCase.wantGroup {
				t.Errorf("group id found = %v (%s), want %v", got, character.GroupID, testCase.wantGroup)
			}
			if got := character.PaddingBeyondGroupID(); got != testCase.wantPadding {
				t.Errorf("PaddingBeyondGroupID() = %v, want %v", got, testCase.wantPadding)
			}
		})
	}
}

// TestCharacterParametersRejectsAnUnexpectedShape covers the split between a
// broken stream, which DecodeCharacter reports as an error, and a well-formed
// stream holding something else, which Parameters reports as nil.
func TestCharacterParametersRejectsAnUnexpectedShape(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		properties gvas.Properties
	}{
		{name: "nothing at all"},
		{
			name:       "no SaveParameter",
			properties: gvas.Properties{{Name: "SomethingElse", Type: "IntProperty", Value: int32(1)}},
		},
		{
			name:       "SaveParameter is not a struct",
			properties: gvas.Properties{{Name: "SaveParameter", Type: "IntProperty", Value: int32(1)}},
		},
		{
			name: "SaveParameter holds a native struct",
			properties: gvas.Properties{{
				Name:  "SaveParameter",
				Type:  "StructProperty",
				Value: gvas.StructValue{Type: "Vector", Value: gvas.Vector{}},
			}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := (Character{Properties: testCase.properties}).Parameters(); got != nil {
				t.Errorf("Parameters() = %+v, want nil", got)
			}
		})
	}
}

// TestDecodeCharacterAliasesTheTrailer documents that Trailer is a window onto
// the caller's bytes, matching ItemSlot.
func TestDecodeCharacterAliasesTheTrailer(t *testing.T) {
	blob := characterBlob("Luke", 1, wantGroupIDBytes, nil)
	character, err := DecodeCharacter(blob)
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)-1] = 7
	if character.Trailer[len(character.Trailer)-1] != 7 {
		t.Error("Trailer does not alias the input")
	}
}

// TestDecodeCharacterWithOptionsCarriesLimitsAndHints checks that a nested
// stream is not a hole in the bounds a caller set, and that the caller's own
// type hints reach it. The hint is keyed under the character blob's path, which
// is the arrangement that lets one table serve both the archive and the streams
// inside it.
func TestDecodeCharacterWithOptionsCarriesLimitsAndHints(t *testing.T) {
	blob := characterBlob("Luke", 1, wantGroupIDBytes, nil)

	if _, err := DecodeCharacterWithOptions(blob, gvas.Options{
		MaxArchiveBytes: int64(len(blob) - 1),
	}); err == nil {
		t.Error("a blob over MaxArchiveBytes decoded anyway")
	}
	if _, err := DecodeCharacterWithOptions(blob, gvas.Options{MaxDepth: 1}); err == nil {
		t.Error("a nested struct decoded with MaxDepth 1")
	}

	// A hint that changes the decoding observably: the value is three float64s
	// with no property framing, readable only as a native Vector.
	var body streamBuilder
	body.u32(0) // removed keys
	body.u32(1) // entries
	body.u32(11)
	body.data = append(body.data, make([]byte, 24)...)
	var inner streamBuilder
	inner.property("Records", "MapProperty", body.data, func(tag *streamBuilder) {
		tag.fstring("IntProperty")
		tag.fstring("StructProperty")
	})
	inner.none()
	var stream streamBuilder
	stream.property("SaveParameter", "StructProperty", inner.data, func(tag *streamBuilder) {
		tag.fstring("PalIndividualCharacterSaveParameter")
		tag.data = append(tag.data, make([]byte, guidBytes)...)
	})
	stream.none()
	stream.data = append(stream.data, make([]byte, wantTrailerBytes)...)

	hinted := gvas.Options{TypeHints: map[string]string{
		characterRawDataPath + ".SaveParameter.Records.Value": "Vector",
	}}
	character, err := DecodeCharacterWithOptions(stream.data, hinted)
	if err != nil {
		t.Fatal(err)
	}
	records := character.Parameters().Find("Records")
	if records == nil {
		t.Fatal("no Records property")
	}
	mapValue, ok := records.Value.(*gvas.MapValue)
	if !ok {
		t.Fatalf("Records is %T", records.Value)
	}
	entries, err := mapValue.Entries()
	if err != nil {
		t.Fatalf("hinted map value: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	if _, ok := entries[0].Value.(gvas.Vector); !ok {
		t.Errorf("hinted value decoded to %T, want gvas.Vector", entries[0].Value)
	}

	// Without the hint the same bytes are unreadable, which is what proves the
	// hint reached the nested parse rather than the value being framed anyway.
	character, err = DecodeCharacter(stream.data)
	if err != nil {
		t.Fatal(err)
	}
	unhinted, ok := character.Parameters().Find("Records").Value.(*gvas.MapValue)
	if !ok {
		t.Fatal("Records is not a map without the hint")
	}
	if _, err := unhinted.Entries(); err == nil {
		t.Error("an untagged map value decoded without a hint")
	}
}

// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package gvas

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// characterLikeStream builds the shape Palworld writes into a character RawData:
// one struct property holding a property list, then the terminator, then framing
// bytes the property encoding knows nothing about.
func characterLikeStream(trailer []byte) []byte {
	var inner testArchive
	inner.property("Level", "ByteProperty", testFStringBytes("None"), 0,
		func(tag *testArchive) { tag.fstring("None") }, nil)
	inner.property("NickName", "StrProperty", testFStringBytes("Luke"), 0, nil, nil)
	inner.fstring("None")

	var stream testArchive
	stream.property("SaveParameter", "StructProperty", inner.bytes(), 0,
		func(tag *testArchive) {
			tag.fstring("PalIndividualCharacterSaveParameter")
			tag.guid(GUID{})
		}, nil)
	stream.fstring("None")
	stream.raw(trailer)
	return stream.bytes()
}

func TestParsePropertiesReadsAStreamWithNoHeader(t *testing.T) {
	trailer := []byte{
		0, 0, 0, 0,
		0xb9, 0x17, 0xf7, 0x7b, 0x38, 0x48, 0x4b, 0x4a,
		0x54, 0x0a, 0x0a, 0xb1, 0x8f, 0x16, 0xcc, 0xe4,
		0, 0, 0, 0,
	}
	properties, rest, err := ParseProperties(characterLikeStream(trailer), ".RawData", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(properties) != 1 || properties[0].Name != "SaveParameter" {
		t.Fatalf("properties = %+v", properties)
	}
	structValue := propertyValue[StructValue](t, properties, "SaveParameter")
	if structValue.Type != "PalIndividualCharacterSaveParameter" {
		t.Errorf("struct type = %q", structValue.Type)
	}
	inner, ok := structValue.Value.(Properties)
	if !ok {
		t.Fatalf("struct holds %T", structValue.Value)
	}
	if name := propertyValue[string](t, inner, "NickName"); name != "Luke" {
		t.Errorf("NickName = %q", name)
	}
	// The bytes after the terminator come back untouched. Only a caller who
	// knows the enclosing format can interpret them.
	if !bytes.Equal(rest, trailer) {
		t.Errorf("trailer = % x, want % x", rest, trailer)
	}
}

// FuzzParseProperties covers the entry point that takes bytes out of the middle
// of a file. A RawData blob is as untrusted as the archive around it, and it
// reaches the property reader without a header having been validated first.
func FuzzParseProperties(f *testing.F) {
	f.Add(characterLikeStream(make([]byte, 24)))
	f.Add(testFStringBytes("None"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = ParseProperties(data, ".worldSaveData.CharacterSaveParameterMap.Value.RawData", Options{
			MaxArchiveBytes:       1 << 20,
			MaxStringBytes:        1 << 16,
			MaxCollectionElements: 1 << 16,
			MaxProperties:         1 << 16,
			MaxDepth:              32,
		})
	})
}

// TestParsePropertiesRejectsAGVASHeader is the distinction between the two entry
// points. A whole archive handed to ParseProperties is not silently accepted:
// "GVAS" reads as a four-byte FString length, which is nonsense.
func TestParsePropertiesRejectsAGVASHeader(t *testing.T) {
	archive := testGVAS(characterLikeStream(nil), nil)
	if _, _, err := ParseProperties(archive, "", Options{}); err == nil {
		t.Fatal("a full archive parsed as a bare property stream")
	}
}

func TestParsePropertiesRequiresATerminator(t *testing.T) {
	var stream testArchive
	stream.property("NickName", "StrProperty", testFStringBytes("Luke"), 0, nil, nil)
	// No "None": the list runs off the end of the data.
	if _, _, err := ParseProperties(stream.bytes(), "", Options{}); err == nil {
		t.Fatal("an unterminated property list decoded without error")
	}
}

func TestParsePropertiesEnforcesLimits(t *testing.T) {
	stream := characterLikeStream(nil)

	var limitErr *LimitError
	_, _, err := ParseProperties(stream, "", Options{MaxArchiveBytes: int64(len(stream) - 1)})
	if !errors.As(err, &limitErr) {
		t.Fatalf("oversized stream error = %v, want a *LimitError", err)
	}
	if !strings.Contains(limitErr.Kind, "property stream") {
		t.Errorf("limit kind = %q, want it to name the property stream", limitErr.Kind)
	}

	// An invalid Options is rejected here exactly as Parse rejects it, rather
	// than being quietly normalised.
	if _, _, err := ParseProperties(stream, "", Options{MaxDepth: -1}); err == nil {
		t.Fatal("negative MaxDepth accepted")
	}
}

// TestParsePropertiesAppliesTypeHintsUnderThePath is why ParseProperties takes a
// path at all: a nested stream's untagged structs have to be resolvable from the
// same table as the archive that contained them, which only works if the paths
// continue from the byte array rather than restarting at the root.
func TestParsePropertiesAppliesTypeHintsUnderThePath(t *testing.T) {
	var body testArchive
	// A map with one untagged struct value: no struct type in the tag, so only
	// a hint can name it. The payload is a removed-key count, then the entry
	// count, then the entries. The value is a native Vector, three float64s
	// with no property framing at all, so it is only readable with the hint --
	// without one, gvas falls back to reading a property list and fails.
	body.u32(0)
	body.u32(1)
	body.i32(11)
	body.f64(1)
	body.f64(2)
	body.f64(3)

	var stream testArchive
	stream.property("Records", "MapProperty", body.bytes(), 0,
		func(tag *testArchive) {
			tag.fstring("IntProperty")
			tag.fstring("StructProperty")
		}, nil)
	stream.fstring("None")

	const path = ".worldSaveData.CharacterSaveParameterMap.Value.RawData"
	options := Options{TypeHints: map[string]string{path + ".Records.Value": "Vector"}}

	properties, _, err := ParseProperties(stream.bytes(), path, options)
	if err != nil {
		t.Fatal(err)
	}
	mapValue := propertyValue[*MapValue](t, properties, "Records")
	entries, err := mapValue.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	if vector, ok := entries[0].Value.(Vector); !ok || vector != (Vector{X: 1, Y: 2, Z: 3}) {
		t.Fatalf("hinted map value decoded to %#v, want a Vector{1,2,3}", entries[0].Value)
	}

	// The same stream parsed at the root cannot find that hint, which is the
	// check that the path is really doing the work. The map itself still comes
	// back -- it is lazy, so the untagged value only fails when it is read.
	properties, _, err = ParseProperties(stream.bytes(), "", options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := propertyValue[*MapValue](t, properties, "Records").Entries(); err == nil {
		t.Error("an untagged map value decoded without a hint for its path")
	}
}

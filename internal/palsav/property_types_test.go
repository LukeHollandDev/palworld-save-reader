// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palsav

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"testing"
)

// The Unreal property-type strings are switched on independently in five
// places: supportedPlainPropertyType, readPropertyPayload, readTypedArray,
// readBareValue, and minimumBareSize. Nothing in the compiler ties those
// switches together, so a type added to one and missed in another only shows
// up when a real save fails to decode.
//
// propertyTypes is the single authoritative list. TestPropertyTypeSwitches
// checks each switch against it, and TestEveryPropertyTypeIsClassified fails
// if properties.go mentions a type string this table does not, which is what
// catches the drift a lookup table would not have caught either: a map miss
// is still a runtime failure, not a compile error.
var propertyTypes = []propertyTypeCase{
	// Plain scalar tags, decodable everywhere.
	{name: "Int8Property", plainTag: true, minimum: 1, arrayable: true,
		encode: func(a *testArchive) { a.i8(-5) }, want: int8(-5)},
	{name: "Int16Property", plainTag: true, minimum: 2, arrayable: true,
		encode: func(a *testArchive) { a.i16(-300) }, want: int16(-300)},
	{name: "IntProperty", plainTag: true, minimum: 4, arrayable: true,
		encode: func(a *testArchive) { a.i32(-70000) }, want: int32(-70000)},
	{name: "Int64Property", plainTag: true, minimum: 8, arrayable: true,
		encode: func(a *testArchive) { a.i64(-5e12) }, want: int64(-5e12)},
	{name: "UInt8Property", plainTag: true, minimum: 1, arrayable: true,
		encode: func(a *testArchive) { a.u8(200) }, want: uint8(200)},
	{name: "UInt16Property", plainTag: true, minimum: 2, arrayable: true,
		encode: func(a *testArchive) { a.u16(60000) }, want: uint16(60000)},
	{name: "UInt32Property", plainTag: true, minimum: 4, arrayable: true,
		encode: func(a *testArchive) { a.u32(4000000000) }, want: uint32(4000000000)},
	{name: "UInt64Property", plainTag: true, minimum: 8, arrayable: true,
		encode: func(a *testArchive) { a.u64(1 << 63) }, want: uint64(1 << 63)},
	{name: "FixedPoint64Property", plainTag: true, minimum: 4, arrayable: true,
		encode: func(a *testArchive) { a.i32(123) }, want: int32(123)},
	{name: "FloatProperty", plainTag: true, minimum: 4, arrayable: true,
		encode: func(a *testArchive) { a.f32(-1.5) }, want: float32(-1.5)},
	{name: "DoubleProperty", plainTag: true, minimum: 8, arrayable: true,
		encode: func(a *testArchive) { a.f64(2.25) }, want: 2.25},
	{name: "NameProperty", plainTag: true, minimum: 4, arrayable: true,
		encode: func(a *testArchive) { a.fstring("n") }, want: "n"},
	{name: "StrProperty", plainTag: true, minimum: 4, arrayable: true,
		encode: func(a *testArchive) { a.fstring("s") }, want: "s"},
	{name: "ObjectProperty", plainTag: true, minimum: 4, arrayable: true,
		encode: func(a *testArchive) { a.fstring("/o") }, want: "/o"},

	// Tags the reader recognises but deliberately does not decode. These
	// always fall back to UndecodedValue, which preserves the exact bytes.
	{name: "TextProperty", plainTag: true, minimum: 1, taggedRaw: true},
	{name: "SoftObjectProperty", plainTag: true, minimum: 1, taggedRaw: true},
	{name: "WeakObjectProperty", plainTag: true, minimum: 1, taggedRaw: true},
	{name: "LazyObjectProperty", plainTag: true, minimum: 1, taggedRaw: true},
	{name: "InterfaceProperty", plainTag: true, minimum: 1, taggedRaw: true},
	{name: "DelegateProperty", plainTag: true, minimum: 1, taggedRaw: true},
	{name: "MulticastDelegateProperty", plainTag: true, minimum: 1, taggedRaw: true},

	// Tags carrying extra metadata, so not "plain", but still bare-decodable.
	{name: "BoolProperty", minimum: 1, arrayable: true,
		encode: func(a *testArchive) { a.u8(1) }, want: true},
	{name: "ByteProperty", minimum: 1, arrayable: true,
		encode: func(a *testArchive) { a.u8(9) }, want: uint8(9)},
	{name: "EnumProperty", minimum: 4, arrayable: true,
		encode: func(a *testArchive) { a.fstring("E::V") }, want: "E::V"},

	// StructProperty decodes bare as a nested tagged property list, but an
	// array of structs goes through readArrayValue's StructArray path instead
	// of readTypedArray.
	{name: "StructProperty", minimum: 9,
		encode: func(a *testArchive) { a.fstring("None") }, want: Properties(nil)},

	// Guid is never a tag type; it only names a bare map key, set element, or
	// array element resolved from a path hint.
	{name: "Guid", minimum: 16, arrayable: true,
		encode: func(a *testArchive) { a.guid(GUID{A: 1, B: 2, C: 3, D: 4}) },
		want:   GUID{A: 1, B: 2, C: 3, D: 4}},

	// Container tags. Their payloads are read by dedicated functions, never
	// bare and never as a primitive array element.
	{name: "ArrayProperty", minimum: 1},
	{name: "MapProperty", minimum: 1},
	{name: "SetProperty", minimum: 1},
}

type propertyTypeCase struct {
	name string
	// plainTag is the expected supportedPlainPropertyType result.
	plainTag bool
	// minimum is the expected minimumBareSize with no struct hint.
	minimum int
	// encode writes one minimal bare value, or is nil when the type has no
	// bare encoding.
	encode func(*testArchive)
	// want is the value readBareValue must produce from encode's bytes.
	want any
	// arrayable records whether readTypedArray handles the type.
	arrayable bool
	// taggedRaw marks a tag the reader accepts but does not decode.
	taggedRaw bool
}

func TestPropertyTypeSwitches(t *testing.T) {
	for _, testCase := range propertyTypes {
		t.Run(testCase.name, func(t *testing.T) {
			if got := supportedPlainPropertyType(testCase.name); got != testCase.plainTag {
				t.Errorf("supportedPlainPropertyType = %v, want %v", got, testCase.plainTag)
			}
			if got := minimumBareSize(testCase.name, ""); got != testCase.minimum {
				t.Errorf("minimumBareSize = %d, want %d", got, testCase.minimum)
			}

			if testCase.encode == nil {
				if _, err := readBareValue(newTestReader(t, nil), testCase.name, "", ""); err == nil {
					t.Errorf("readBareValue accepted %s, which has no bare encoding", testCase.name)
				}
				if _, err := readTypedArray(newTestReader(t, nil), testCase.name, 0, ""); err == nil {
					t.Errorf("readTypedArray accepted %s as an element type", testCase.name)
				}
				return
			}

			encoded := goldenPayload(testCase.encode)
			reader := newTestReader(t, encoded)
			bare, err := readBareValue(reader, testCase.name, "", "")
			if err != nil {
				t.Fatalf("readBareValue: %v", err)
			}
			if !reflect.DeepEqual(bare, testCase.want) {
				t.Errorf("readBareValue = %#v, want %#v", bare, testCase.want)
			}
			// minimumBareSize is used to reject counts that cannot fit in the
			// remaining bytes, so it must never overstate the real size.
			if used := len(encoded) - reader.remaining(); used < testCase.minimum {
				t.Errorf("minimumBareSize %d exceeds the %d bytes actually read", testCase.minimum, used)
			}

			if !testCase.arrayable {
				if _, err := readTypedArray(newTestReader(t, encoded), testCase.name, 1, ""); err == nil {
					t.Errorf("readTypedArray accepted %s as an element type", testCase.name)
				}
				return
			}
			values, err := readTypedArray(newTestReader(t, encoded), testCase.name, 1, "")
			if err != nil {
				t.Fatalf("readTypedArray: %v", err)
			}
			slice := reflect.ValueOf(values)
			if slice.Kind() != reflect.Slice || slice.Len() != 1 {
				t.Fatalf("readTypedArray = %#v, want a one-element slice", values)
			}
			// The same bytes must mean the same thing bare and in an array.
			if element := slice.Index(0).Interface(); !reflect.DeepEqual(element, testCase.want) {
				t.Errorf("array element = %#v, want %#v", element, testCase.want)
			}
		})
	}
}

// TestTaggedPayloadDecodability checks the split between tags whose payload the
// reader decodes and tags it accepts but leaves as UndecodedValue.
func TestTaggedPayloadDecodability(t *testing.T) {
	for _, testCase := range propertyTypes {
		if !testCase.plainTag {
			continue
		}
		t.Run(testCase.name, func(t *testing.T) {
			payload := []byte{0, 0, 0, 0}
			if testCase.encode != nil {
				payload = goldenPayload(testCase.encode)
			}
			var archive testArchive
			archive.property("Value", testCase.name, payload, 0, nil, nil)
			archive.fstring("None")

			save, err := ParseGVAS(testGVAS(archive.bytes(), nil))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			property := save.Properties.Find("Value")
			if property == nil {
				t.Fatal("property missing")
			}
			_, raw := property.Value.(UndecodedValue)
			if raw != testCase.taggedRaw {
				t.Errorf("UndecodedValue fallback = %v, want %v (value %#v)", raw, testCase.taggedRaw, property.Value)
			}
		})
	}
}

// TestEveryPropertyTypeIsClassified fails when properties.go mentions a type
// string that propertyTypes does not describe. Without it, adding a case to
// one switch and forgetting the others stays invisible until a save fails.
func TestEveryPropertyTypeIsClassified(t *testing.T) {
	source, err := os.ReadFile("properties.go")
	if err != nil {
		t.Fatal(err)
	}
	classified := make(map[string]bool, len(propertyTypes))
	for _, testCase := range propertyTypes {
		classified[testCase.name] = true
	}

	// Type strings are either "<Something>Property" or the bare "Guid" hint.
	pattern := regexp.MustCompile(`"([A-Za-z0-9_]+Property|Guid)"`)
	var missing []string
	seen := make(map[string]bool)
	for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
		name := match[1]
		if classified[name] || seen[name] {
			continue
		}
		seen[name] = true
		missing = append(missing, name)
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		t.Errorf("properties.go handles type(s) %v with no entry in propertyTypes; "+
			"add them there so every switch is checked", missing)
	}
}

func newTestReader(t *testing.T, data []byte) *archiveReader {
	t.Helper()
	cfg, err := Options{}.normalized()
	if err != nil {
		t.Fatal(err)
	}
	return newArchiveReader(data, cfg)
}

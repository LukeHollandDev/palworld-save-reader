// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package gvas

import "testing"

// TestNormalizedCopiesHintsWithoutMutatingOptions covers the contract Options
// makes with a caller: defaults fill in, the hint map is copied rather than
// retained, and the caller's own Options and map come back untouched.
func TestNormalizedCopiesHintsWithoutMutatingOptions(t *testing.T) {
	caller := map[string]string{".Custom.Key": "Guid"}
	options := Options{TypeHints: caller}

	cfg, err := options.normalized()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.TypeHints[".Custom.Key"]; got != "Guid" {
		t.Fatalf("caller hint missing: %q", got)
	}
	if cfg.MaxDepth != defaultMaxDepth || cfg.MaxArchiveBytes != defaultMaxArchiveBytes {
		t.Fatalf("defaults not applied: depth=%d archive=%d", cfg.MaxDepth, cfg.MaxArchiveBytes)
	}

	// The copy has to be private, or a caller mutating its map afterwards would
	// change what an in-flight parse resolves.
	caller[".Custom.Key"] = "StructProperty"
	if got := cfg.TypeHints[".Custom.Key"]; got != "Guid" {
		t.Fatalf("config aliases the caller's hint map: %q", got)
	}
	if options.MaxDepth != 0 {
		t.Fatalf("normalized mutated the caller's Options: %#v", options)
	}
}

// TestNoBuiltInTypeHints pins the layering this package exists to enforce.
// Untagged StructProperty identities are game knowledge, so they live in
// internal/palworld; a parse with no caller hints must resolve none.
func TestNoBuiltInTypeHints(t *testing.T) {
	cfg, err := Options{}.normalized()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TypeHints) != 0 {
		t.Fatalf("gvas ships %d built-in type hints, want 0: %#v", len(cfg.TypeHints), cfg.TypeHints)
	}
}

func TestNormalizedRejectsEmptyHints(t *testing.T) {
	if _, err := (Options{TypeHints: map[string]string{"": "Guid"}}).normalized(); err == nil {
		t.Fatal("accepted an empty hint path")
	}
	if _, err := (Options{TypeHints: map[string]string{".A": ""}}).normalized(); err == nil {
		t.Fatal("accepted an empty hint value")
	}
	if _, err := (Options{MaxDepth: -1}).normalized(); err == nil {
		t.Fatal("accepted a negative MaxDepth")
	}
}

// TestTruncatedPayloadsDegradeSafely feeds every struct body and array element
// encoding at every length shorter than the correct one. Each case must either
// fail the parse outright or fall back to UndecodedValue preserving the exact bytes.
// Returning a partially decoded value would silently corrupt a save, and these
// are the error branches that the fixed-size struct readers and the generic
// slice reader replace.
func TestTruncatedPayloadsDegradeSafely(t *testing.T) {
	for _, testCase := range payloadCases() {
		t.Run(testCase.name, func(t *testing.T) {
			// The untruncated payload must decode, otherwise the case is wrong.
			property := decodeSingleProperty(t, testCase, testCase.payload)
			if _, raw := property.Value.(UndecodedValue); raw {
				t.Fatalf("well-formed payload decoded to UndecodedValue: %v", property.Value)
			}

			for length := 0; length < len(testCase.payload); length++ {
				truncated := testCase.payload[:length]
				save, err := Parse(singlePropertyArchive(testCase, truncated))
				if err != nil {
					continue // A hard parse failure is an acceptable outcome.
				}
				found := save.Properties.Find(testCase.name)
				if found == nil {
					t.Fatalf("length %d: property missing from decode", length)
				}
				rawValue, ok := found.Value.(UndecodedValue)
				if !ok {
					t.Fatalf("length %d: decoded %#v from a truncated payload", length, found.Value)
				}
				if string(rawValue.Data) != string(truncated) {
					t.Fatalf("length %d: UndecodedValue kept %d bytes, want %d", length, len(rawValue.Data), length)
				}
			}
		})
	}
}

type payloadCase struct {
	name         string
	propertyType string
	meta         func(*testArchive)
	payload      []byte
}

func singlePropertyArchive(testCase payloadCase, payload []byte) []byte {
	var archive testArchive
	archive.property(testCase.name, testCase.propertyType, payload, 0, testCase.meta, nil)
	archive.fstring("None")
	return testGVAS(archive.bytes(), nil)
}

func decodeSingleProperty(t *testing.T, testCase payloadCase, payload []byte) *Property {
	t.Helper()
	save, err := Parse(singlePropertyArchive(testCase, payload))
	if err != nil {
		t.Fatalf("decode %s: %v", testCase.name, err)
	}
	property := save.Properties.Find(testCase.name)
	if property == nil {
		t.Fatalf("property %s missing", testCase.name)
	}
	return property
}

func payloadCases() []payloadCase {
	structCase := func(structType string, build func(*testArchive)) payloadCase {
		return payloadCase{
			name:         "Struct" + structType,
			propertyType: "StructProperty",
			meta:         goldenStructMeta(structType),
			payload:      goldenPayload(build),
		}
	}
	arrayCase := func(innerType string, count uint32, build func(*testArchive)) payloadCase {
		return payloadCase{
			name:         "Array" + innerType,
			propertyType: "ArrayProperty",
			meta:         goldenInnerMeta(innerType),
			payload: goldenPayload(func(a *testArchive) {
				a.u32(count)
				build(a)
			}),
		}
	}

	return []payloadCase{
		structCase("Guid", func(a *testArchive) { a.guid(GUID{A: 1, B: 2, C: 3, D: 4}) }),
		structCase("DateTime", func(a *testArchive) { a.i64(1) }),
		structCase("Timespan", func(a *testArchive) { a.i64(1) }),
		structCase("Vector", func(a *testArchive) { a.f64(1); a.f64(2); a.f64(3) }),
		structCase("Vector2D", func(a *testArchive) { a.f64(1); a.f64(2) }),
		structCase("Quat", func(a *testArchive) { a.f64(1); a.f64(2); a.f64(3); a.f64(4) }),
		structCase("Rotator", func(a *testArchive) { a.f64(1); a.f64(2); a.f64(3) }),
		structCase("LinearColor", func(a *testArchive) { a.f32(1); a.f32(2); a.f32(3); a.f32(4) }),
		structCase("Color", func(a *testArchive) { a.u8(1); a.u8(2); a.u8(3); a.u8(4) }),
		structCase("IntPoint", func(a *testArchive) { a.i32(1); a.i32(2) }),
		structCase("IntVector", func(a *testArchive) { a.i32(1); a.i32(2); a.i32(3) }),

		arrayCase("Int8Property", 2, func(a *testArchive) { a.i8(1); a.i8(2) }),
		arrayCase("BoolProperty", 2, func(a *testArchive) { a.u8(1); a.u8(0) }),
		arrayCase("Int16Property", 2, func(a *testArchive) { a.i16(1); a.i16(2) }),
		arrayCase("UInt16Property", 2, func(a *testArchive) { a.u16(1); a.u16(2) }),
		arrayCase("IntProperty", 2, func(a *testArchive) { a.i32(1); a.i32(2) }),
		arrayCase("UInt32Property", 2, func(a *testArchive) { a.u32(1); a.u32(2) }),
		arrayCase("Int64Property", 2, func(a *testArchive) { a.i64(1); a.i64(2) }),
		arrayCase("UInt64Property", 2, func(a *testArchive) { a.u64(1); a.u64(2) }),
		arrayCase("FloatProperty", 2, func(a *testArchive) { a.f32(1); a.f32(2) }),
		arrayCase("DoubleProperty", 2, func(a *testArchive) { a.f64(1); a.f64(2) }),
		arrayCase("NameProperty", 2, func(a *testArchive) { a.fstring("a"); a.fstring("b") }),
		arrayCase("StrProperty", 2, func(a *testArchive) { a.fstring("a"); a.fstring("b") }),
		arrayCase("ObjectProperty", 1, func(a *testArchive) { a.fstring("/a") }),
		arrayCase("EnumProperty", 2, func(a *testArchive) { a.fstring("E::A"); a.fstring("E::B") }),
		arrayCase("Guid", 2, func(a *testArchive) {
			a.guid(GUID{A: 1})
			a.guid(GUID{A: 2})
		}),
	}
}

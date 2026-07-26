// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package gvas

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the testdata golden files")

// TestDecodeGolden pins the decoded shape of every property, struct, array,
// map, and set encoding the reader supports. The synthetic archive built by
// goldenArchive covers each branch of readPropertyPayload, readStructBody,
// readTypedArray, and readBareValue, none of which the rest of the suite
// reaches without an external save fixture.
//
// The rendered tree records the concrete Go type of every leaf and of every
// typed array slice, so a refactor that silently boxes []int16 into []any
// fails here rather than in a consumer.
func TestDecodeGolden(t *testing.T) {
	save, err := ParseWithOptions(goldenArchive(), Options{TypeHints: goldenTypeHints})
	if err != nil {
		t.Fatalf("decode synthetic archive: %v", err)
	}

	rendered, err := json.MarshalIndent(map[string]any{
		"header":     save.Header,
		"properties": materialise(save.Properties),
		"trailer":    base64.StdEncoding.EncodeToString(save.Trailer),
	}, "", "  ")
	if err != nil {
		t.Fatalf("render decoded tree: %v", err)
	}
	rendered = append(rendered, '\n')

	path := filepath.Join("testdata", "decode_golden.json")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, rendered, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d bytes)", path, len(rendered))
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (regenerate with -update-golden): %v", err)
	}
	if string(rendered) != string(want) {
		t.Errorf("decoded tree differs from %s\n%s", path, firstDiff(string(want), string(rendered)))
	}
}

// firstDiff reports the first differing line so a failure names the property
// that changed instead of dumping the whole tree.
func firstDiff(want, got string) string {
	wantLines, gotLines := splitLines(want), splitLines(got)
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		var wantLine, gotLine string
		if i < len(wantLines) {
			wantLine = wantLines[i]
		}
		if i < len(gotLines) {
			gotLine = gotLines[i]
		}
		if wantLine != gotLine {
			return fmt.Sprintf("line %d:\n  want: %s\n   got: %s", i+1, wantLine, gotLine)
		}
	}
	return "files differ only in trailing bytes"
}

func splitLines(text string) []string {
	var lines []string
	start := 0
	for i := range text {
		if text[i] == '\n' {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// materialise converts a decoded value into a stable JSON-able tree, driving
// every lazy iterator. Iteration errors are recorded as data so a regression
// shows up as a golden diff rather than a panic.
func materialise(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case Properties:
		out := make([]any, 0, len(typed))
		for index := range typed {
			property := &typed[index]
			node := map[string]any{
				"name":  property.Name,
				"type":  property.Type,
				"size":  property.Size,
				"value": materialise(property.Value),
			}
			if property.ArrayIndex != 0 {
				node["arrayIndex"] = property.ArrayIndex
			}
			if property.PropertyGUID != nil {
				node["propertyGUID"] = property.PropertyGUID.String()
			}
			out = append(out, node)
		}
		return out
	case StructValue:
		return map[string]any{
			"kind":       "struct",
			"structType": typed.Type,
			"value":      materialise(typed.Value),
		}
	case EnumValue:
		return map[string]any{"kind": "enum", "enumType": typed.Type, "value": typed.Value}
	case UndecodedValue:
		return map[string]any{
			"kind":   "raw",
			"data":   base64.StdEncoding.EncodeToString(typed.Data),
			"reason": typed.Reason,
		}
	case ArrayValue:
		node := map[string]any{"kind": "array", "innerType": typed.InnerType}
		if typed.Structs != nil {
			node["structs"] = materialise(typed.Structs)
			return node
		}
		// goType is the point of this entry: it pins []int16 as []int16.
		node["goType"] = fmt.Sprintf("%T", typed.Values)
		node["values"] = typed.Values
		return node
	case *StructArray:
		node := map[string]any{
			"kind":       "structArray",
			"name":       typed.Name,
			"structType": typed.StructType,
			"count":      typed.Count,
		}
		values, err := typed.Values()
		if err != nil {
			node["error"] = err.Error()
			return node
		}
		node["values"] = materialiseSlice(values)
		return node
	case *MapValue:
		node := map[string]any{
			"kind":            "map",
			"keyType":         typed.KeyType,
			"valueType":       typed.ValueType,
			"keyStructType":   typed.KeyStructType,
			"valueStructType": typed.ValueStructType,
			"count":           typed.Count,
			"removed":         materialiseSlice(typed.Removed),
		}
		entries, err := typed.Entries()
		if err != nil {
			node["error"] = err.Error()
			return node
		}
		items := make([]any, 0, len(entries))
		for _, entry := range entries {
			items = append(items, map[string]any{
				"key":   materialise(entry.Key),
				"value": materialise(entry.Value),
			})
		}
		node["entries"] = items
		return node
	case *SetValue:
		node := map[string]any{
			"kind":              "set",
			"elementType":       typed.ElementType,
			"elementStructType": typed.ElementStructType,
			"count":             typed.Count,
			"removed":           materialiseSlice(typed.Removed),
		}
		values, err := typed.Values()
		if err != nil {
			node["error"] = err.Error()
			return node
		}
		node["values"] = materialiseSlice(values)
		return node
	case []any:
		return materialiseSlice(typed)
	default:
		return map[string]any{"goType": fmt.Sprintf("%T", value), "value": value}
	}
}

func materialiseSlice(input []any) []any {
	out := make([]any, 0, len(input))
	for _, element := range input {
		out = append(out, materialise(element))
	}
	return out
}

func goldenPayload(build func(*testArchive)) []byte {
	var archive testArchive
	build(&archive)
	return archive.bytes()
}

func goldenStructMeta(structType string) func(*testArchive) {
	return func(tag *testArchive) {
		tag.fstring(structType)
		tag.guid(GUID{})
	}
}

func goldenInnerMeta(innerType string) func(*testArchive) {
	return func(tag *testArchive) { tag.fstring(innerType) }
}

// goldenArchive builds one synthetic GVAS archive exercising every supported
// encoding. Floating-point values are exact binary fractions so the rendered
// golden is stable across platforms.
func goldenArchive() []byte {
	var p testArchive

	goldenScalars(&p)
	goldenStructs(&p)
	goldenArrays(&p)
	goldenCollections(&p)
	goldenFallbacks(&p)
	goldenHintedNesting(&p)

	p.fstring("None")
	return testGVAS(p.bytes(), []byte{0xDE, 0xAD, 0xBE, 0xEF})
}

func goldenScalars(p *testArchive) {
	p.property("Int8", "Int8Property", goldenPayload(func(a *testArchive) { a.i8(-128) }), 0, nil, nil)
	p.property("Int16", "Int16Property", goldenPayload(func(a *testArchive) { a.i16(-32768) }), 0, nil, nil)
	p.property("Int32", "IntProperty", goldenPayload(func(a *testArchive) { a.i32(-2147483648) }), 0, nil, nil)
	p.property("Int64", "Int64Property", goldenPayload(func(a *testArchive) { a.i64(-9007199254740993) }), 0, nil, nil)
	p.property("FixedPoint64", "FixedPoint64Property", goldenPayload(func(a *testArchive) { a.i32(1234567) }), 0, nil, nil)
	p.property("UInt8", "UInt8Property", goldenPayload(func(a *testArchive) { a.u8(255) }), 0, nil, nil)
	p.property("UInt16", "UInt16Property", goldenPayload(func(a *testArchive) { a.u16(65535) }), 0, nil, nil)
	p.property("UInt32", "UInt32Property", goldenPayload(func(a *testArchive) { a.u32(4294967295) }), 0, nil, nil)
	p.property("UInt64", "UInt64Property", goldenPayload(func(a *testArchive) { a.u64(18446744073709551615) }), 0, nil, nil)
	p.property("Float", "FloatProperty", goldenPayload(func(a *testArchive) { a.f32(-0.5) }), 0, nil, nil)
	p.property("Double", "DoubleProperty", goldenPayload(func(a *testArchive) { a.f64(3.25) }), 0, nil, nil)
	p.property("Name", "NameProperty", testFStringBytes("PalName"), 0, nil, nil)
	p.property("Str", "StrProperty", testFStringBytes("hello"), 0, nil, nil)
	p.property("StrUTF16", "StrProperty", testUTF16String("Pal 世界"), 0, nil, nil)
	p.property("Object", "ObjectProperty", testFStringBytes("/Game/Pal/Blueprint"), 0, nil, nil)

	p.property("BoolTrue", "BoolProperty", nil, 0, func(tag *testArchive) { tag.u8(1) }, nil)
	p.property("BoolFalse", "BoolProperty", nil, 0, func(tag *testArchive) { tag.u8(0) }, nil)

	p.property("ByteRaw", "ByteProperty", goldenPayload(func(a *testArchive) { a.u8(7) }), 0, goldenInnerMeta("None"), nil)
	p.property("ByteEnum", "ByteProperty", testFStringBytes("EPalWorkType::Cooking"), 0, goldenInnerMeta("EPalWorkType"), nil)
	p.property("Enum", "EnumProperty", testFStringBytes("EPalGender::Female"), 0, goldenInnerMeta("EPalGender"), nil)
}

func goldenStructs(p *testArchive) {
	structGUID := GUID{A: 0x11223344, B: 0x55667788, C: 0x99aabbcc, D: 0xddeeff00}

	p.property("StructGuid", "StructProperty",
		goldenPayload(func(a *testArchive) { a.guid(structGUID) }), 0, goldenStructMeta("Guid"), nil)
	p.property("StructDateTime", "StructProperty",
		goldenPayload(func(a *testArchive) { a.i64(133000000000000000) }), 0, goldenStructMeta("DateTime"), nil)
	p.property("StructTimespan", "StructProperty",
		goldenPayload(func(a *testArchive) { a.i64(864000000000) }), 0, goldenStructMeta("Timespan"), nil)
	p.property("StructVector", "StructProperty",
		goldenPayload(func(a *testArchive) { a.f64(1.5); a.f64(-2.25); a.f64(3.75) }), 0, goldenStructMeta("Vector"), nil)
	p.property("StructVector2D", "StructProperty",
		goldenPayload(func(a *testArchive) { a.f64(0.5); a.f64(-0.25) }), 0, goldenStructMeta("Vector2D"), nil)
	p.property("StructQuat", "StructProperty",
		goldenPayload(func(a *testArchive) { a.f64(0.5); a.f64(0.25); a.f64(-0.125); a.f64(1) }), 0, goldenStructMeta("Quat"), nil)
	p.property("StructRotator", "StructProperty",
		goldenPayload(func(a *testArchive) { a.f64(90); a.f64(-45.5); a.f64(0.25) }), 0, goldenStructMeta("Rotator"), nil)
	p.property("StructLinearColor", "StructProperty",
		goldenPayload(func(a *testArchive) { a.f32(0.25); a.f32(0.5); a.f32(0.75); a.f32(1) }), 0, goldenStructMeta("LinearColor"), nil)
	p.property("StructColor", "StructProperty",
		goldenPayload(func(a *testArchive) { a.u8(10); a.u8(20); a.u8(30); a.u8(40) }), 0, goldenStructMeta("Color"), nil)
	p.property("StructIntPoint", "StructProperty",
		goldenPayload(func(a *testArchive) { a.i32(-7); a.i32(9) }), 0, goldenStructMeta("IntPoint"), nil)
	p.property("StructIntVector", "StructProperty",
		goldenPayload(func(a *testArchive) { a.i32(1); a.i32(-2); a.i32(3) }), 0, goldenStructMeta("IntVector"), nil)

	// Unknown struct types fall through to a nested tagged property list.
	p.property("StructNested", "StructProperty", goldenPayload(func(a *testArchive) {
		a.property("Inner", "IntProperty", goldenPayload(func(b *testArchive) { b.i32(42) }), 0, nil, nil)
		a.property("InnerName", "NameProperty", testFStringBytes("deep"), 0, nil, nil)
		a.fstring("None")
	}), 0, goldenStructMeta("PalNestedStruct"), nil)
}

func goldenArrays(p *testArchive) {
	arrayOf := func(name, innerType string, count uint32, build func(*testArchive)) {
		p.property(name, "ArrayProperty", goldenPayload(func(a *testArchive) {
			a.u32(count)
			build(a)
		}), 0, goldenInnerMeta(innerType), nil)
	}

	arrayOf("ArrByte", "ByteProperty", 4, func(a *testArchive) { a.raw([]byte{1, 2, 3, 250}) })
	arrayOf("ArrUInt8", "UInt8Property", 3, func(a *testArchive) { a.raw([]byte{0, 128, 255}) })
	arrayOf("ArrInt8", "Int8Property", 3, func(a *testArchive) { a.i8(-128); a.i8(0); a.i8(127) })
	arrayOf("ArrBool", "BoolProperty", 3, func(a *testArchive) { a.u8(1); a.u8(0); a.u8(2) })
	arrayOf("ArrInt16", "Int16Property", 2, func(a *testArchive) { a.i16(-32768); a.i16(32767) })
	arrayOf("ArrUInt16", "UInt16Property", 2, func(a *testArchive) { a.u16(0); a.u16(65535) })
	arrayOf("ArrInt32", "IntProperty", 2, func(a *testArchive) { a.i32(-2147483648); a.i32(2147483647) })
	arrayOf("ArrFixedPoint64", "FixedPoint64Property", 2, func(a *testArchive) { a.i32(11); a.i32(-22) })
	arrayOf("ArrUInt32", "UInt32Property", 2, func(a *testArchive) { a.u32(0); a.u32(4294967295) })
	arrayOf("ArrInt64", "Int64Property", 2, func(a *testArchive) { a.i64(-9007199254740993); a.i64(9007199254740993) })
	arrayOf("ArrUInt64", "UInt64Property", 2, func(a *testArchive) { a.u64(0); a.u64(18446744073709551615) })
	arrayOf("ArrFloat", "FloatProperty", 3, func(a *testArchive) { a.f32(-0.5); a.f32(0); a.f32(2.75) })
	arrayOf("ArrDouble", "DoubleProperty", 3, func(a *testArchive) { a.f64(-0.5); a.f64(0); a.f64(1024.125) })
	arrayOf("ArrName", "NameProperty", 2, func(a *testArchive) { a.fstring("alpha"); a.fstring("beta") })
	arrayOf("ArrStr", "StrProperty", 2, func(a *testArchive) { a.fstring("one"); a.fstring("two") })
	arrayOf("ArrObject", "ObjectProperty", 1, func(a *testArchive) { a.fstring("/Game/Pal/Obj") })
	arrayOf("ArrEnum", "EnumProperty", 2, func(a *testArchive) { a.fstring("EPalA::X"); a.fstring("EPalA::Y") })
	arrayOf("ArrGuid", "Guid", 2, func(a *testArchive) {
		a.guid(GUID{A: 1, B: 2, C: 3, D: 4})
		a.guid(GUID{A: 5, B: 6, C: 7, D: 8})
	})
	arrayOf("ArrEmpty", "IntProperty", 0, func(a *testArchive) {})

	// ArrayProperty<StructProperty> with a native element type.
	bodies := goldenPayload(func(a *testArchive) {
		a.f64(1)
		a.f64(2)
		a.f64(3)
		a.f64(4)
		a.f64(5)
		a.f64(6)
	})
	p.property("ArrStructVector", "ArrayProperty", goldenPayload(func(a *testArchive) {
		a.u32(2)
		a.property("Positions", "StructProperty", bodies, 0, goldenStructMeta("Vector"), nil)
	}), 0, goldenInnerMeta("StructProperty"), nil)

	// ArrayProperty<StructProperty> whose elements are tagged property lists.
	listBodies := goldenPayload(func(a *testArchive) {
		a.property("Id", "IntProperty", goldenPayload(func(b *testArchive) { b.i32(1) }), 0, nil, nil)
		a.fstring("None")
		a.property("Id", "IntProperty", goldenPayload(func(b *testArchive) { b.i32(2) }), 0, nil, nil)
		a.fstring("None")
	})
	p.property("ArrStructList", "ArrayProperty", goldenPayload(func(a *testArchive) {
		a.u32(2)
		a.property("Items", "StructProperty", listBodies, 0, goldenStructMeta("PalItemStruct"), nil)
	}), 0, goldenInnerMeta("StructProperty"), nil)
}

func goldenCollections(p *testArchive) {
	p.property("MapIntStr", "MapProperty", goldenPayload(func(a *testArchive) {
		a.u32(1)
		a.i32(9)
		a.u32(2)
		a.i32(1)
		a.fstring("one")
		a.i32(2)
		a.fstring("two")
	}), 0, func(tag *testArchive) {
		tag.fstring("IntProperty")
		tag.fstring("StrProperty")
	}, nil)

	p.property("MapNameDouble", "MapProperty", goldenPayload(func(a *testArchive) {
		a.u32(0)
		a.u32(2)
		a.fstring("first")
		a.f64(0.5)
		a.fstring("second")
		a.f64(-8.25)
	}), 0, func(tag *testArchive) {
		tag.fstring("NameProperty")
		tag.fstring("DoubleProperty")
	}, nil)

	p.property("SetUInt32", "SetProperty", goldenPayload(func(a *testArchive) {
		a.u32(1)
		a.u32(7)
		a.u32(2)
		a.u32(8)
		a.u32(9)
	}), 0, goldenInnerMeta("UInt32Property"), nil)

	p.property("SetName", "SetProperty", goldenPayload(func(a *testArchive) {
		a.u32(0)
		a.u32(2)
		a.fstring("red")
		a.fstring("blue")
	}), 0, goldenInnerMeta("NameProperty"), nil)

	// The remaining bare scalar encodings only appear as map or set elements.
	p.property("MapInt8Int16", "MapProperty", goldenPayload(func(a *testArchive) {
		a.u32(0)
		a.u32(2)
		a.i8(-1)
		a.i16(-300)
		a.i8(2)
		a.i16(300)
	}), 0, func(tag *testArchive) {
		tag.fstring("Int8Property")
		tag.fstring("Int16Property")
	}, nil)

	p.property("MapUInt16Int64", "MapProperty", goldenPayload(func(a *testArchive) {
		a.u32(0)
		a.u32(1)
		a.u16(65535)
		a.i64(-9007199254740993)
	}), 0, func(tag *testArchive) {
		tag.fstring("UInt16Property")
		tag.fstring("Int64Property")
	}, nil)

	p.property("MapUInt64Float", "MapProperty", goldenPayload(func(a *testArchive) {
		a.u32(0)
		a.u32(1)
		a.u64(18446744073709551615)
		a.f32(-2.5)
	}), 0, func(tag *testArchive) {
		tag.fstring("UInt64Property")
		tag.fstring("FloatProperty")
	}, nil)

	p.property("SetBool", "SetProperty", goldenPayload(func(a *testArchive) {
		a.u32(0)
		a.u32(2)
		a.u8(1)
		a.u8(0)
	}), 0, goldenInnerMeta("BoolProperty"), nil)

	p.property("SetByte", "SetProperty", goldenPayload(func(a *testArchive) {
		a.u32(0)
		a.u32(2)
		a.u8(3)
		a.u8(200)
	}), 0, goldenInnerMeta("ByteProperty"), nil)
}

func goldenFallbacks(p *testArchive) {
	// A short payload leaves the tag intact but forces the UndecodedValue fallback.
	p.property("TruncatedInt", "IntProperty", []byte{1, 2}, 0, nil, nil)
	// Trailing payload bytes are also a decode failure, not silent truncation.
	p.property("OverlongInt", "IntProperty", []byte{1, 2, 3, 4, 5}, 0, nil, nil)
	// A duplicated name at a non-zero array index must survive decoding.
	guid := GUID{A: 0x40d2fba7, B: 0x4b484ce5, C: 0xb0385a75, D: 0x884e499e}
	p.property("Indexed", "IntProperty", goldenPayload(func(a *testArchive) { a.i32(41) }), 7, nil, &guid)
	p.property("Indexed", "IntProperty", goldenPayload(func(a *testArchive) { a.i32(42) }), 8, nil, nil)
}

// goldenTypeHints is the one hint the golden archive needs. It is stated here
// rather than borrowed from internal/palworld so this golden pins the hint
// mechanism, not any particular game's schema.
var goldenTypeHints = map[string]string{
	".worldSaveData.BaseCampSaveData.Key": "Guid",
}

// goldenHintedNesting reproduces the nesting that a type hint resolves, where a
// MapProperty declares StructProperty keys whose real identity is only known
// from the property path.
func goldenHintedNesting(p *testArchive) {
	entries := goldenPayload(func(a *testArchive) {
		a.u32(0) // no removed keys
		a.u32(1)
		a.guid(GUID{A: 0xaabbccdd, B: 0x11223344, C: 0x55667788, D: 0x99aabbcc})
		a.property("Id", "IntProperty", goldenPayload(func(b *testArchive) { b.i32(77) }), 0, nil, nil)
		a.fstring("None")
	})
	inner := goldenPayload(func(a *testArchive) {
		a.property("BaseCampSaveData", "MapProperty", entries, 0, func(tag *testArchive) {
			tag.fstring("StructProperty")
			tag.fstring("StructProperty")
		}, nil)
		a.fstring("None")
	})
	p.property("worldSaveData", "StructProperty", inner, 0, goldenStructMeta("PalWorldSaveData"), nil)
}

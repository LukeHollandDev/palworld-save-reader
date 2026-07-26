// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/palworld"
)

// The paths --decode-raw acts on. Stating them here rather than reaching into
// palworld's table means a change to that table has to be reflected
// deliberately, and TestExpanderPathMatchesPalworldTable holds the two together.
const (
	itemSlotPath       = ".worldSaveData.ItemContainerSaveData.Value.Slots.Slots.RawData"
	characterPath      = ".worldSaveData.CharacterSaveParameterMap.Value.RawData"
	characterSlotPath  = ".worldSaveData.CharacterContainerSaveData.Value.Slots.Slots.RawData"
	groupPath          = ".worldSaveData.GroupSaveDataMap.Value.RawData"
	baseCampPath       = ".worldSaveData.BaseCampSaveData.Value.RawData"
	workerDirectorPath = ".worldSaveData.BaseCampSaveData.Value.WorkerDirector.RawData"
)

func TestExpanderPathMatchesPalworldTable(t *testing.T) {
	for path, want := range map[string]palworld.RawDataKind{
		itemSlotPath:       palworld.RawDataItemSlot,
		characterPath:      palworld.RawDataCharacter,
		characterSlotPath:  palworld.RawDataCharacterSlot,
		groupPath:          palworld.RawDataGroup,
		baseCampPath:       palworld.RawDataBaseCamp,
		workerDirectorPath: palworld.RawDataWorkerDirector,
	} {
		if got := palworld.ClassifyRawData(path); got != want {
			t.Errorf("palworld classifies %s as %v, want %v", path, got, want)
		}
	}
}

// byteArrayAt renders one primitive byte array as --full would, at the given
// path, so a test can exercise the decode hook without a whole save file.
func byteArrayAt(t *testing.T, decodeRaw bool, path string, data []byte) map[string]any {
	t.Helper()
	e := &expander{decodeRaw: decodeRaw}
	// Split the path so the walk arrives with the full path assembled, which is
	// what a real expansion does.
	segments := strings.Split(strings.TrimPrefix(path, "."), ".")
	value := any(gvas.ArrayValue{InnerType: "ByteProperty", Values: data})
	properties := gvas.Properties{{Name: segments[len(segments)-1], Type: "ArrayProperty", Value: value}}
	for i := len(segments) - 2; i >= 0; i-- {
		properties = gvas.Properties{{
			Name:  segments[i],
			Type:  "StructProperty",
			Value: gvas.StructValue{Type: "Wrapper", Value: properties},
		}}
	}
	// Walk down to the leaf node the expander produced.
	node := any(e.properties(properties, ""))
	for {
		list, ok := node.([]any)
		if !ok || len(list) != 1 {
			t.Fatalf("unexpected expansion shape %T", node)
		}
		entry, ok := list[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected entry %T", list[0])
		}
		inner, ok := entry["value"]
		if !ok {
			t.Fatalf("entry %v has no value", entry)
		}
		if leaf, ok := inner.(map[string]any); ok {
			if _, isArray := leaf["innerType"]; isArray {
				return leaf
			}
			node = leaf["value"]
			continue
		}
		t.Fatalf("unexpected inner %T", inner)
	}
}

func slotPayload(index, count uint32, itemID string, trailer []byte) []byte {
	blob := make([]byte, 12)
	binary.LittleEndian.PutUint32(blob[0:4], index)
	binary.LittleEndian.PutUint32(blob[4:8], count)
	binary.LittleEndian.PutUint32(blob[8:12], uint32(len(itemID)+1))
	blob = append(blob, itemID...)
	blob = append(blob, 0)
	return append(blob, trailer...)
}

func TestDecodeRawAddsSlotDetailWithoutRemovingBase64(t *testing.T) {
	payload := slotPayload(1, 2180, "PalSphere", make([]byte, 52))

	plain := byteArrayAt(t, false, itemSlotPath, payload)
	if _, ok := plain["decoded"]; ok {
		t.Error("--full without --decode-raw produced a decoded block")
	}
	if plain["values"] == nil {
		t.Error("--full dropped the raw byte array")
	}

	decoded := byteArrayAt(t, true, itemSlotPath, payload)
	// The base64 must survive: --decode-raw adds an interpretation, it does not
	// replace the bytes it was derived from.
	if decoded["values"] == nil {
		t.Error("--decode-raw dropped the raw byte array")
	}
	block, ok := decoded["decoded"].(map[string]any)
	if !ok {
		t.Fatalf("decoded block is %T", decoded["decoded"])
	}
	if block["kind"] != "itemSlot" {
		t.Errorf("kind = %v", block["kind"])
	}
	if block["itemId"] != "PalSphere" {
		t.Errorf("itemId = %v", block["itemId"])
	}
	if block["count"] != uint32(2180) {
		t.Errorf("count = %v (%T)", block["count"], block["count"])
	}
	if block["slotIndex"] != uint32(1) {
		t.Errorf("slotIndex = %v", block["slotIndex"])
	}
	// An all-zero trailer and a zero GUID carry nothing, so neither key should
	// appear -- 27,000 slots' worth of zeroes is noise, not data.
	if _, ok := block["trailer"]; ok {
		t.Error("an all-zero trailer was reported")
	}
	if _, ok := block["dynamicItemId"]; ok {
		t.Error("a zero dynamicItemId was reported")
	}
}

func TestDecodeRawReportsDynamicItemIDAndTrailer(t *testing.T) {
	trailer := make([]byte, 52)
	copy(trailer[16:32], []byte{
		0x85, 0x59, 0xf6, 0x4c, 0xc1, 0x45, 0xe4, 0x51,
		0x50, 0x52, 0x27, 0x9f, 0xcd, 0x7f, 0x5c, 0xed,
	})
	trailer[40] = 0x7 // something in the part that is not decoded yet

	block, ok := byteArrayAt(t, true, itemSlotPath, slotPayload(0, 1, "FurArmorCold", trailer))["decoded"].(map[string]any)
	if !ok {
		t.Fatal("no decoded block")
	}
	if block["dynamicItemId"] != "4cf65985-51e4-45c1-9f27-5250ed5c7fcd" {
		t.Errorf("dynamicItemId = %v", block["dynamicItemId"])
	}
	encoded, ok := block["trailer"].(string)
	if !ok {
		t.Fatalf("trailer is %T, want a base64 string", block["trailer"])
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	// The reported trailer is everything after the GUID, preserved whole rather
	// than trimmed, so an offset can still be worked out from it later.
	if len(raw) != 20 || raw[8] != 0x7 {
		t.Errorf("trailer = % x", raw)
	}
}

// TestDecodeRawIgnoresUnknownPaths is the layering check: a byte array that is
// not at a classified path stays base64 even with the flag on.
func TestDecodeRawIgnoresUnknownPaths(t *testing.T) {
	payload := slotPayload(1, 2, "PalSphere", make([]byte, 52))
	for _, path := range []string{
		".worldSaveData.ItemContainerSaveData.Value.Slots.Slots.CustomVersionData",
		".worldSaveData.ItemContainerSaveData.Value.RawData",
		".worldSaveData.CharacterContainerSaveData.Value.RawData",
		".worldSaveData.CharacterContainerSaveData.Value.Slots.RawData",
	} {
		if _, ok := byteArrayAt(t, true, path, payload)["decoded"]; ok {
			t.Errorf("%s produced a decoded block", path)
		}
	}
}

// characterPayload builds the nested property stream Palworld writes into a
// character RawData, with the trailer the fixture world always carries.
func characterPayload(nickname string, trailer []byte) []byte {
	var name syntheticArchive
	name.fstring(nickname)

	var inner syntheticArchive
	inner.property("NickName", "StrProperty", name.data)
	inner.fstring("None")

	// A StructProperty tag carries its struct type and GUID between the array
	// index and the optional-GUID flag, which syntheticArchive.property does not
	// write, so the tag goes out by hand here.
	var stream syntheticArchive
	stream.fstring("SaveParameter")
	stream.fstring("StructProperty")
	stream.u32(uint32(len(inner.data)))
	stream.u32(0)
	stream.fstring("PalIndividualCharacterSaveParameter")
	stream.data = append(stream.data, make([]byte, 16)...)
	stream.u8(0)
	stream.data = append(stream.data, inner.data...)
	stream.fstring("None")
	return append(stream.data, trailer...)
}

// characterTrailer is four zero bytes, a group id, four more zero bytes.
func characterTrailer() []byte {
	trailer := make([]byte, 24)
	copy(trailer[4:], []byte{
		0xb9, 0x17, 0xf7, 0x7b, 0x38, 0x48, 0x4b, 0x4a,
		0x54, 0x0a, 0x0a, 0xb1, 0x8f, 0x16, 0xcc, 0xe4,
	})
	return trailer
}

// TestDecodeRawExpandsACharacterStream is the phase 2 rendering: a nested stream
// comes out as the same property list every other part of the dump uses, so a
// consumer needs no special case for it, and the base64 still survives.
func TestDecodeRawExpandsACharacterStream(t *testing.T) {
	payload := characterPayload("Luke", characterTrailer())

	plain := byteArrayAt(t, false, characterPath, payload)
	if _, ok := plain["decoded"]; ok {
		t.Error("--full without --decode-raw decoded a character stream")
	}

	node := byteArrayAt(t, true, characterPath, payload)
	if node["values"] == nil {
		t.Error("--decode-raw dropped the raw byte array")
	}
	block, ok := node["decoded"].(map[string]any)
	if !ok {
		t.Fatalf("decoded block is %T", node["decoded"])
	}
	if block["kind"] != "character" {
		t.Errorf("kind = %v", block["kind"])
	}
	if block["groupId"] != "7bf717b9-4a4b-4838-b10a-0a54e4cc168f" {
		t.Errorf("groupId = %v", block["groupId"])
	}
	// The framing is zero bytes around the group id, so there is nothing left to
	// report and the key should be absent.
	if _, ok := block["trailer"]; ok {
		t.Error("a bare character trailer was reported")
	}

	// The nested properties must be rendered by the same expander, which is what
	// makes a nickname reachable by walking the dump rather than by decoding
	// base64 out of it.
	properties, ok := block["properties"].([]any)
	if !ok || len(properties) != 1 {
		t.Fatalf("properties = %#v", block["properties"])
	}
	saveParameter, ok := properties[0].(map[string]any)
	if !ok || saveParameter["name"] != "SaveParameter" {
		t.Fatalf("first property = %#v", properties[0])
	}
	structNode, ok := saveParameter["value"].(map[string]any)
	if !ok {
		t.Fatalf("SaveParameter value is %T", saveParameter["value"])
	}
	if structNode["structType"] != "PalIndividualCharacterSaveParameter" {
		t.Errorf("structType = %v", structNode["structType"])
	}
	inner, ok := structNode["value"].([]any)
	if !ok || len(inner) != 1 {
		t.Fatalf("inner properties = %#v", structNode["value"])
	}
	nickname, ok := inner[0].(map[string]any)
	if !ok || nickname["name"] != "NickName" || nickname["value"] != "Luke" {
		t.Errorf("nickname property = %#v", inner[0])
	}
}

// TestDecodeRawReportsCharacterFramingItCannotName covers the other trailer case:
// a byte the layout does not account for is surfaced rather than silently
// dropped, because that byte is the evidence the framing has changed.
func TestDecodeRawReportsCharacterFramingItCannotName(t *testing.T) {
	trailer := characterTrailer()
	trailer[len(trailer)-1] = 9

	block, ok := byteArrayAt(t, true, characterPath, characterPayload("Luke", trailer))["decoded"].(map[string]any)
	if !ok {
		t.Fatal("no decoded block")
	}
	encoded, ok := block["trailer"].(string)
	if !ok {
		t.Fatalf("trailer is %T, want a base64 string", block["trailer"])
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != len(trailer) || raw[len(raw)-1] != 9 {
		t.Errorf("trailer = % x", raw)
	}
}

// TestDecodeRawReportsABadCharacterBlobInline matches the item-slot behaviour: a
// blob that will not decode is reported in place, and the dump still emits every
// byte that was in the file.
func TestDecodeRawReportsABadCharacterBlobInline(t *testing.T) {
	node := byteArrayAt(t, true, characterPath, []byte{1, 2, 3})
	message, ok := node["decodeError"].(string)
	if !ok {
		t.Fatalf("decodeError is %T", node["decodeError"])
	}
	if !strings.Contains(message, "character record") {
		t.Errorf("decodeError = %q", message)
	}
	if node["values"] == nil {
		t.Error("a failed decode dropped the raw byte array")
	}
	if _, ok := node["decoded"]; ok {
		t.Error("a failed decode also produced a decoded block")
	}
}

// TestDecodeRawRendersACharacterSlotReference is the third layout: a slot that
// names a character record rather than holding one. The instance id is reported
// even when it is zero, because "this slot is empty" is information here.
func TestDecodeRawRendersACharacterSlotReference(t *testing.T) {
	payload := make([]byte, 38)
	copy(payload[16:], []byte{
		0x24, 0x07, 0x15, 0x6b, 0xef, 0x4c, 0x18, 0x21,
		0x78, 0xa0, 0x5a, 0x81, 0x07, 0xbf, 0x95, 0xfb,
	})

	if _, ok := byteArrayAt(t, false, characterSlotPath, payload)["decoded"]; ok {
		t.Error("--full without --decode-raw decoded a character slot")
	}

	node := byteArrayAt(t, true, characterSlotPath, payload)
	if node["values"] == nil {
		t.Error("--decode-raw dropped the raw byte array")
	}
	block, ok := node["decoded"].(map[string]any)
	if !ok {
		t.Fatalf("decoded block is %T", node["decoded"])
	}
	if block["kind"] != "characterSlot" {
		t.Errorf("kind = %v", block["kind"])
	}
	if block["instanceId"] != "6b150724-2118-4cef-815a-a078fb95bf07" {
		t.Errorf("instanceId = %v", block["instanceId"])
	}
	if block["empty"] != false {
		t.Errorf("empty = %v", block["empty"])
	}
	// A zero PlayerUID and a zero trailer are what every fixture slot carries, so
	// neither should add a key -- 2,250 slots' worth of zeroes is noise.
	if _, ok := block["playerUId"]; ok {
		t.Error("a zero playerUId was reported")
	}
	if _, ok := block["trailer"]; ok {
		t.Error("an all-zero trailer was reported")
	}

	empty, ok := byteArrayAt(t, true, characterSlotPath, make([]byte, 38))["decoded"].(map[string]any)
	if !ok {
		t.Fatal("no decoded block for an empty slot")
	}
	if empty["empty"] != true {
		t.Errorf("an empty slot reported empty = %v", empty["empty"])
	}
	if empty["instanceId"] != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("an empty slot reported instanceId = %v", empty["instanceId"])
	}
}

// TestDecodeRawReportsABadCharacterSlotInline matches the other two layouts: a
// blob too short to hold a reference is reported in place rather than failing the
// dump.
func TestDecodeRawReportsABadCharacterSlotInline(t *testing.T) {
	node := byteArrayAt(t, true, characterSlotPath, []byte{1, 2, 3})
	message, ok := node["decodeError"].(string)
	if !ok {
		t.Fatalf("decodeError is %T", node["decodeError"])
	}
	if !strings.Contains(message, "character slot") {
		t.Errorf("decodeError = %q", message)
	}
	if node["values"] == nil {
		t.Error("a failed decode dropped the raw byte array")
	}
}

// TestDecodeRawReportsABadBlobInline keeps a malformed record visible without
// failing the whole dump: --full's job is to show what is in the file.
func TestDecodeRawReportsABadBlobInline(t *testing.T) {
	node := byteArrayAt(t, true, itemSlotPath, []byte{1, 2, 3})
	message, ok := node["decodeError"].(string)
	if !ok {
		t.Fatalf("decodeError is %T", node["decodeError"])
	}
	if !strings.Contains(message, "needs 12 bytes") {
		t.Errorf("decodeError = %q", message)
	}
	if node["values"] == nil {
		t.Error("a failed decode dropped the raw byte array")
	}
	if _, ok := node["decoded"]; ok {
		t.Error("a failed decode also produced a decoded block")
	}
}

// TestDecodeRawOutputIsJSONEncodable guards the uint32 fields: encoding/json
// handles them, but a future change to any/interface juggling could produce a
// value it cannot marshal, and --full's only job is to emit JSON.
func TestDecodeRawOutputIsJSONEncodable(t *testing.T) {
	node := byteArrayAt(t, true, itemSlotPath, slotPayload(3, 9, "Wood", make([]byte, 52)))
	encoded, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"itemId":"Wood"`) {
		t.Errorf("encoded = %s", encoded)
	}
}

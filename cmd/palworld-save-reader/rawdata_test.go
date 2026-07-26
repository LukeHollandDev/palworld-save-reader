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

// itemSlotPath is the one path --decode-raw acts on today. Stating it here
// rather than reaching into palworld's table means a change to that table has
// to be reflected deliberately, and TestExpanderPathMatchesPalworldTable holds
// the two together.
const itemSlotPath = ".worldSaveData.ItemContainerSaveData.Value.Slots.Slots.RawData"

func TestExpanderPathMatchesPalworldTable(t *testing.T) {
	if got := palworld.ClassifyRawData(itemSlotPath); got != palworld.RawDataItemSlot {
		t.Fatalf("palworld no longer classifies %s as an item slot (got %v)", itemSlotPath, got)
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
		".worldSaveData.CharacterSaveParameterMap.Value.RawData",
		".worldSaveData.ItemContainerSaveData.Value.Slots.Slots.CustomVersionData",
	} {
		if _, ok := byteArrayAt(t, true, path, payload)["decoded"]; ok {
			t.Errorf("%s produced a decoded block", path)
		}
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

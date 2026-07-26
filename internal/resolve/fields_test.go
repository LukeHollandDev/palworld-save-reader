// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// sample is a decoded property tree with one of each shape the accessors have to
// see through: a struct wrapping a struct, a native struct value, an enum, a byte
// array, a struct array, and a string array.
func sample() gvas.Properties {
	return gvas.Properties{
		{Name: "Version", Type: "IntProperty", Value: int32(100)},
		{Name: "SaveData", Type: "StructProperty", Value: gvas.StructValue{
			Type: "PalWorldPlayerSaveData",
			Value: gvas.Properties{
				{Name: "PlayerUId", Type: "StructProperty", Value: gvas.StructValue{
					Type: "Guid", Value: id(7),
				}},
				{Name: "LastTransform", Type: "StructProperty", Value: gvas.StructValue{
					Type: "Transform",
					Value: gvas.Properties{
						{Name: "Translation", Type: "StructProperty", Value: gvas.StructValue{
							Type: "Vector", Value: gvas.Vector{X: 1, Y: 2, Z: 3},
						}},
					},
				}},
				{Name: "PlayerPlatform", Type: "EnumProperty", Value: gvas.EnumValue{
					Type: "EPalPlayerPlatform", Value: "EPalPlayerPlatform::PS5",
				}},
				{Name: "Unprefixed", Type: "EnumProperty", Value: gvas.EnumValue{
					Type: "EPalPlayerPlatform", Value: "Steam",
				}},
				{Name: "Empty", Type: "EnumProperty", Value: gvas.EnumValue{Type: "E", Value: ""}},
				{Name: "LastOnlineDateTime", Type: "StructProperty", Value: gvas.StructValue{
					Type: "DateTime", Value: int64(639204910905580000),
				}},
				{Name: "RawData", Type: "ArrayProperty", Value: gvas.ArrayValue{
					InnerType: "ByteProperty", Values: []byte{1, 2, 3},
				}},
				{Name: "Names", Type: "ArrayProperty", Value: gvas.ArrayValue{
					InnerType: "NameProperty", Values: []string{"a", "b"},
				}},
				{Name: "Slots", Type: "ArrayProperty", Value: gvas.ArrayValue{
					InnerType: "StructProperty", Structs: &gvas.StructArray{Count: 2, Name: "Slots"},
				}},
			},
		}},
	}
}

func TestFieldWalksThroughStructWrappers(t *testing.T) {
	properties := sample()

	if got, ok := value[int32](properties, "Version"); !ok || got != 100 {
		t.Errorf("Version = %v, %v", got, ok)
	}
	// A struct contributes no path segment of its own, so the Vector inside the
	// Transform inside the property is reached by naming only the properties.
	if got, ok := value[gvas.Vector](properties, "SaveData.LastTransform.Translation"); !ok ||
		got != (gvas.Vector{X: 1, Y: 2, Z: 3}) {
		t.Errorf("Translation = %v, %v", got, ok)
	}
	if got, ok := guid(properties, "SaveData.PlayerUId"); !ok || got != id(7) {
		t.Errorf("PlayerUId = %v, %v", got, ok)
	}
	if got, ok := stamp(properties, "SaveData.LastOnlineDateTime"); !ok || got.UTC != "2026-07-24T11:58:10Z" {
		t.Errorf("LastOnlineDateTime = %+v, %v", got, ok)
	}
}

func TestFieldReportsWhatIsNotThere(t *testing.T) {
	properties := sample()
	for _, path := range []string{
		"Missing",
		"SaveData.Missing",
		"Version.Missing",              // walking into a value that is not a list
		"SaveData.PlayerUId.Something", // walking into a native struct
		"",
	} {
		if got := field(properties, path); got != nil {
			t.Errorf("field(%q) = %v, want nil", path, got)
		}
	}
	// A path that exists but decoded to another type is a miss, not a zero value:
	// a caller must be able to tell "absent" from "present and 0".
	if got, ok := value[string](properties, "Version"); ok {
		t.Errorf("Version read as a string gave %q", got)
	}
	if _, ok := guid(properties, "SaveData.LastTransform"); ok {
		t.Error("a property list read as a GUID succeeded")
	}
	if _, ok := stamp(properties, "Version"); ok {
		t.Error("an IntProperty read as a DateTime succeeded")
	}
}

func TestEnumNameStripsTheTypePrefix(t *testing.T) {
	properties := sample()
	if got, ok := enumName(properties, "SaveData.PlayerPlatform"); !ok || got != "PS5" {
		t.Errorf("PlayerPlatform = %q, %v", got, ok)
	}
	// A value with no prefix is left alone rather than emptied.
	if got, ok := enumName(properties, "SaveData.Unprefixed"); !ok || got != "Steam" {
		t.Errorf("Unprefixed = %q, %v", got, ok)
	}
	if _, ok := enumName(properties, "SaveData.Empty"); ok {
		t.Error("an empty enum value reported as present")
	}
	if _, ok := enumName(properties, "Version"); ok {
		t.Error("an IntProperty read as an enum succeeded")
	}
}

func TestArrayAccessors(t *testing.T) {
	properties := sample()

	data, ok := blob(properties, "SaveData.RawData")
	if !ok || len(data) != 3 || data[2] != 3 {
		t.Errorf("RawData = %v, %v", data, ok)
	}
	if _, ok := blob(properties, "SaveData.Names"); ok {
		t.Error("a string array read as a byte array succeeded")
	}

	if got := names(properties, "SaveData.Names"); len(got) != 2 || got[0] != "a" {
		t.Errorf("Names = %v", got)
	}
	if got := names(properties, "SaveData.RawData"); got != nil {
		t.Errorf("a byte array read as strings gave %v", got)
	}
	if got := names(properties, "Missing"); got != nil {
		t.Errorf("an absent array gave %v", got)
	}

	array, ok := structArray(properties, "SaveData.Slots")
	if !ok || array.Count != 2 {
		t.Errorf("Slots = %v, %v", array, ok)
	}
	// A byte array is still an ArrayValue, but it has no struct elements, so it
	// must not be mistaken for one.
	if _, ok := structArray(properties, "SaveData.RawData"); ok {
		t.Error("a byte array read as a struct array succeeded")
	}
}

// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefixtures"
)

func TestLoadSuppliedLevelMeta(t *testing.T) {
	root := savefixtures.Root(t)
	save, err := Load(filepath.Join(root, "LevelMeta.sav"))
	if err != nil {
		t.Fatal(err)
	}
	if save.Header.SaveGameVersion != 3 ||
		save.Header.PackageUE4 != 522 ||
		save.Header.PackageUE5 != 1008 {
		t.Fatalf("unexpected package versions: %+v", save.Header)
	}
	if save.Header.Engine != (gvas.EngineVersion{
		Major: 5, Minor: 1, Patch: 1, Branch: "++UE5+Release-5.1",
	}) {
		t.Fatalf("engine = %+v", save.Header.Engine)
	}
	if len(save.Header.CustomVersions) != 85 {
		t.Fatalf("custom versions = %d, want 85", len(save.Header.CustomVersions))
	}
	if save.Header.ClassName != "/Script/Pal.PalWorldBaseInfoSaveGame" {
		t.Fatalf("class = %q", save.Header.ClassName)
	}
	if save.Container.Magic != "PlM" || save.Container.SaveType != 0x31 {
		t.Fatalf("container = %q/%#x", save.Container.Magic, save.Container.SaveType)
	}
	if got := propertyValue[int32](t, save.Properties, "Version"); got != 100 {
		t.Fatalf("Version = %d", got)
	}
	_ = propertyStruct[int64](t, save.Properties, "Timestamp")
	saveData := propertyStruct[gvas.Properties](t, save.Properties, "SaveData")
	_ = propertyValue[string](t, saveData, "WorldName")
	_ = propertyValue[int32](t, saveData, "InGameDay")
	if !bytes.Equal(save.Trailer, make([]byte, 4)) {
		t.Fatalf("trailer = %x", save.Trailer)
	}
}

func TestLoadSuppliedGVASRoots(t *testing.T) {
	root := savefixtures.Root(t)
	for index, path := range savefixtures.AllSaves(t, root) {
		t.Run(fmt.Sprintf("save-%02d", index+1), func(t *testing.T) {
			save, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(save.Properties) == 0 {
				t.Fatal("no root properties")
			}
			if !bytes.Equal(save.Trailer, make([]byte, 4)) {
				t.Fatalf("trailer = %x", save.Trailer)
			}
			for _, property := range save.Properties {
				if len(property.Raw) != int(property.Size) {
					t.Fatalf("%s raw size = %d, want %d", property.Name, len(property.Raw), property.Size)
				}
			}
		})
	}
}

// TestSuppliedLazyCollections walks the world save's largest collections through
// their iterators. It is the one test that proves the type hints in hints.go
// resolve against a real Level.sav: without them a CharacterSaveParameterMap key
// decodes as an untagged property list rather than the struct it is.
func TestSuppliedLazyCollections(t *testing.T) {
	root := savefixtures.Root(t)

	level, err := Load(filepath.Join(root, "Level.sav"))
	if err != nil {
		t.Fatal(err)
	}
	world := propertyStruct[gvas.Properties](t, level.Properties, "worldSaveData")
	if len(world) == 0 {
		t.Fatal("worldSaveData has no properties")
	}
	characters := propertyValue[*gvas.MapValue](t, world, "CharacterSaveParameterMap")
	if characters.Count == 0 {
		t.Fatal("character map is empty")
	}
	characterIterator := characters.Iterator()
	if !characterIterator.Next() {
		t.Fatalf("first character: %v", characterIterator.Err())
	}
	firstCharacter := characterIterator.Entry()
	key, ok := firstCharacter.Key.(gvas.Properties)
	if !ok {
		t.Fatalf("first character key = %T", firstCharacter.Key)
	}
	_ = propertyStruct[gvas.GUID](t, key, "PlayerUId")

	locker := propertyValue[*gvas.SetValue](t, world, "InLockerCharacterInstanceIDArray")
	lockerIterator := locker.Iterator()
	for lockerIterator.Next() {
	}
	if err := lockerIterator.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestWalkEverySuppliedSaveValue(t *testing.T) {
	root := savefixtures.Root(t)
	for index, path := range savefixtures.AllSaves(t, root) {
		t.Run(fmt.Sprintf("save-%02d", index+1), func(t *testing.T) {
			save, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			rawValues := make(map[string]int)
			if err := walkDecodedValue(save.Properties, rawValues); err != nil {
				t.Fatal(err)
			}
			if len(rawValues) != 0 {
				t.Fatalf("opaque values by reason: %#v", rawValues)
			}
			if len(save.Properties) == 1 && save.Properties[0].Name == "SaveParameterArray" {
				array := propertyValue[gvas.ArrayValue](t, save.Properties, "SaveParameterArray").Structs
				if array == nil || array.Count != 9600 {
					t.Fatalf("DPS struct array = %+v", array)
				}
			}
		})
	}
}

func walkDecodedValue(value any, rawValues map[string]int) error {
	switch value := value.(type) {
	case gvas.Properties:
		for _, property := range value {
			if err := walkDecodedValue(property.Value, rawValues); err != nil {
				return fmt.Errorf("%s: %w", property.Name, err)
			}
		}
	case gvas.StructValue:
		return walkDecodedValue(value.Value, rawValues)
	case gvas.ArrayValue:
		if value.Structs == nil {
			return walkDecodedValue(value.Values, rawValues)
		}
		iterator := value.Structs.Iterator()
		for iterator.Next() {
			if err := walkDecodedValue(iterator.Value(), rawValues); err != nil {
				return err
			}
		}
		return iterator.Err()
	case *gvas.MapValue:
		iterator := value.Iterator()
		for iterator.Next() {
			entry := iterator.Entry()
			if err := walkDecodedValue(entry.Key, rawValues); err != nil {
				return err
			}
			if err := walkDecodedValue(entry.Value, rawValues); err != nil {
				return err
			}
		}
		return iterator.Err()
	case *gvas.SetValue:
		iterator := value.Iterator()
		for iterator.Next() {
			if err := walkDecodedValue(iterator.Value(), rawValues); err != nil {
				return err
			}
		}
		return iterator.Err()
	case []any:
		for _, item := range value {
			if err := walkDecodedValue(item, rawValues); err != nil {
				return err
			}
		}
	case gvas.UndecodedValue:
		rawValues[value.Reason]++
	}
	return nil
}

func propertyValue[T any](t *testing.T, properties gvas.Properties, name string) T {
	t.Helper()
	property := properties.Find(name)
	if property == nil {
		t.Fatalf("missing property %q", name)
	}
	value, ok := property.Value.(T)
	if !ok {
		t.Fatalf("%s value type = %T", name, property.Value)
	}
	return value
}

func propertyStruct[T any](t *testing.T, properties gvas.Properties, name string) T {
	t.Helper()
	property := properties.Find(name)
	if property == nil {
		t.Fatalf("missing property %q", name)
	}
	value, ok := property.Value.(gvas.StructValue)
	if !ok {
		t.Fatalf("%s value type = %T", name, property.Value)
	}
	body, ok := value.Value.(T)
	if !ok {
		t.Fatalf("%s struct body type = %T", name, value.Value)
	}
	return body
}

// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefixtures"
)

// itemContainers walks to worldSaveData.ItemContainerSaveData in a world save.
// It fails rather than skips when the path is missing, because a Level.sav
// without item containers means the schema moved and the tests below would
// otherwise pass by vacuously examining nothing.
func itemContainers(t *testing.T, save *gvas.Archive) *gvas.MapValue {
	t.Helper()
	world := save.Properties.Find("worldSaveData")
	if world == nil {
		t.Fatal("Level.sav has no worldSaveData")
	}
	structValue, ok := world.Value.(gvas.StructValue)
	if !ok {
		t.Fatalf("worldSaveData is %T, want gvas.StructValue", world.Value)
	}
	properties, ok := structValue.Value.(gvas.Properties)
	if !ok {
		t.Fatalf("worldSaveData holds %T, want gvas.Properties", structValue.Value)
	}
	containers := properties.Find("ItemContainerSaveData")
	if containers == nil {
		t.Fatal("worldSaveData has no ItemContainerSaveData")
	}
	value, ok := containers.Value.(*gvas.MapValue)
	if !ok {
		t.Fatalf("ItemContainerSaveData is %T, want *gvas.MapValue", containers.Value)
	}
	return value
}

// slotBlobs yields every Slots[].RawData payload in the world save, iterating
// rather than collecting: memory rule R1 forbids Entries() on a Level.sav
// collection, and this test would otherwise be the first thing to break it.
func slotBlobs(t *testing.T, containers *gvas.MapValue, visit func(container int, blob []byte)) int {
	t.Helper()
	index := 0
	iterator := containers.Iterator()
	for iterator.Next() {
		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			t.Fatalf("container %d value is %T, want gvas.Properties", index, iterator.Entry().Value)
		}
		slots := properties.Find("Slots")
		if slots == nil {
			t.Fatalf("container %d has no Slots", index)
		}
		array, ok := slots.Value.(gvas.ArrayValue)
		if !ok || array.Structs == nil {
			t.Fatalf("container %d Slots is %T without struct elements", index, slots.Value)
		}
		elements := array.Structs.Iterator()
		for elements.Next() {
			slotProperties, ok := elements.Value().(gvas.Properties)
			if !ok {
				t.Fatalf("container %d slot is %T", index, elements.Value())
			}
			rawData := slotProperties.Find("RawData")
			if rawData == nil {
				t.Fatalf("container %d slot has no RawData", index)
			}
			payload, ok := rawData.Value.(gvas.ArrayValue)
			if !ok {
				t.Fatalf("container %d slot RawData is %T", index, rawData.Value)
			}
			blob, ok := payload.Values.([]byte)
			if !ok {
				t.Fatalf("container %d slot RawData holds %T, want []byte", index, payload.Values)
			}
			visit(index, blob)
		}
		if err := elements.Err(); err != nil {
			t.Fatalf("container %d slot iteration: %v", index, err)
		}
		index++
	}
	if err := iterator.Err(); err != nil {
		t.Fatalf("container iteration: %v", err)
	}
	return index
}

// TestDecodeItemSlotAgainstWorldSave is the test that makes ItemSlot's layout a
// claim about Palworld rather than about a hand-written blob. Every slot in the
// fixture world must decode; one failure means the layout is wrong.
func TestDecodeItemSlotAgainstWorldSave(t *testing.T) {
	root := savefixtures.Root(t)
	save, err := Load(filepath.Join(root, "Level.sav"))
	if err != nil {
		t.Fatal(err)
	}
	containers := itemContainers(t, save.Archive)

	var slots, occupied, withDynamic int
	items := map[string]int{}
	containerCount := slotBlobs(t, containers, func(container int, blob []byte) {
		slots++
		slot, err := DecodeItemSlot(blob)
		if err != nil {
			t.Fatalf("container %d slot %d (%d bytes): %v", container, slots, len(blob), err)
		}
		if slot.Empty() {
			if slot.Count != 0 {
				t.Errorf("container %d has an empty slot with count %d", container, slot.Count)
			}
			return
		}
		occupied++
		items[slot.ItemID]++
		if !slot.DynamicItemID.IsZero() {
			withDynamic++
		}
	})

	if containerCount == 0 || slots == 0 {
		t.Fatal("fixture world has no item container slots to exercise")
	}
	t.Logf(
		"containers=%d slots=%d occupied=%d distinctItems=%d withDynamicItemID=%d",
		containerCount, slots, occupied, len(items), withDynamic,
	)
	// A world with items must produce recognisable item ids. Without this the
	// test would pass on a decoder that returned empty strings for everything.
	if len(items) < 100 {
		t.Errorf("only %d distinct item ids decoded, expected the fixture world to hold many more", len(items))
	}
	if items["Money"] == 0 {
		t.Error(`no slot decoded to the item id "Money"`)
	}
}

// TestRawDataPathsExistInWorldSave is the guard for the mistake this table
// invites. A path built by reading the convention rather than observing it is
// silently inert: ClassifyRawData returns RawDataUnknown, --decode-raw decodes
// nothing, and every other test still passes. The first version of the item-slot
// entry was wrong in exactly this way -- it omitted the name a struct array
// repeats for its elements -- so this walks a real save and requires every
// declared path to be somewhere in it.
func TestRawDataPathsExistInWorldSave(t *testing.T) {
	root := savefixtures.Root(t)
	save, err := Load(filepath.Join(root, "Level.sav"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	collectBytePaths(save.Properties, "", seen)

	for path, kind := range RawDataPaths() {
		count, ok := seen[path]
		if !ok {
			t.Errorf("path %q (%v) matches no byte array in the fixture world save", path, kind)
			continue
		}
		t.Logf("%v at %s: %d blobs", kind, path, count)
	}
}

// collectBytePaths records the path of every primitive byte array in the tree,
// building paths the way gvas does. It is deliberately a separate walk from
// cmd's expander: if the two disagree, the assertion above fails rather than
// both being wrong in the same way.
func collectBytePaths(value any, path string, seen map[string]int) {
	switch typed := value.(type) {
	case gvas.Properties:
		for i := range typed {
			collectBytePaths(typed[i].Value, path+"."+typed[i].Name, seen)
		}
	case gvas.StructValue:
		collectBytePaths(typed.Value, path, seen)
	case *gvas.MapValue:
		iterator := typed.Iterator()
		for iterator.Next() {
			collectBytePaths(iterator.Entry().Key, path+".Key", seen)
			collectBytePaths(iterator.Entry().Value, path+".Value", seen)
		}
	case *gvas.SetValue:
		iterator := typed.Iterator()
		for iterator.Next() {
			collectBytePaths(iterator.Value(), path+"."+typed.ElementType, seen)
		}
	case gvas.ArrayValue:
		if typed.Structs != nil {
			collectBytePaths(typed.Structs, path, seen)
			return
		}
		if _, ok := typed.Values.([]byte); ok {
			seen[path]++
		}
	case *gvas.StructArray:
		iterator := typed.Iterator()
		for iterator.Next() {
			collectBytePaths(iterator.Value(), path+"."+typed.Name, seen)
		}
	case []any:
		for _, element := range typed {
			collectBytePaths(element, path, seen)
		}
	}
}

// TestSlotDynamicItemIDsResolve checks the field ItemSlot documents as a join
// key actually joins. A DynamicItemID that matched nothing would be a decoding
// artefact -- 16 bytes read at the wrong offset can look like a plausible GUID.
func TestSlotDynamicItemIDsResolve(t *testing.T) {
	root := savefixtures.Root(t)
	save, err := Load(filepath.Join(root, "Level.sav"))
	if err != nil {
		t.Fatal(err)
	}
	world, ok := save.Properties.Find("worldSaveData").Value.(gvas.StructValue)
	if !ok {
		t.Fatal("worldSaveData is not a struct")
	}
	properties := world.Value.(gvas.Properties)

	dynamic := properties.Find("DynamicItemSaveData")
	if dynamic == nil {
		t.Skip("fixture world has no DynamicItemSaveData")
	}
	array, ok := dynamic.Value.(gvas.ArrayValue)
	if !ok || array.Structs == nil {
		t.Fatalf("DynamicItemSaveData is %T without struct elements", dynamic.Value)
	}
	var records [][]byte
	elements := array.Structs.Iterator()
	for elements.Next() {
		recordProperties, ok := elements.Value().(gvas.Properties)
		if !ok {
			continue
		}
		rawData := recordProperties.Find("RawData")
		if rawData == nil {
			continue
		}
		if payload, ok := rawData.Value.(gvas.ArrayValue); ok {
			if blob, ok := payload.Values.([]byte); ok {
				records = append(records, blob)
			}
		}
	}
	if err := elements.Err(); err != nil {
		t.Fatalf("DynamicItemSaveData iteration: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("DynamicItemSaveData holds no RawData records")
	}

	var checked, missing int
	slotBlobs(t, itemContainers(t, save.Archive), func(container int, blob []byte) {
		slot, err := DecodeItemSlot(blob)
		if err != nil || slot.DynamicItemID.IsZero() {
			return
		}
		checked++
		// The record's own id sits inside its RawData, which is a layout this
		// package does not decode yet, so search for the raw 16 bytes rather
		// than pretending to parse the record.
		needle := slot.DynamicItemID
		wanted := []byte{
			byte(needle.A), byte(needle.A >> 8), byte(needle.A >> 16), byte(needle.A >> 24),
			byte(needle.B), byte(needle.B >> 8), byte(needle.B >> 16), byte(needle.B >> 24),
			byte(needle.C), byte(needle.C >> 8), byte(needle.C >> 16), byte(needle.C >> 24),
			byte(needle.D), byte(needle.D >> 8), byte(needle.D >> 16), byte(needle.D >> 24),
		}
		for _, record := range records {
			if bytes.Contains(record, wanted) {
				return
			}
		}
		missing++
		if missing <= 3 {
			t.Errorf("slot item %q has DynamicItemID %s, which matches no DynamicItemSaveData record",
				slot.ItemID, slot.DynamicItemID)
		}
	})
	if checked == 0 {
		t.Skip("fixture world has no slot carrying a DynamicItemID")
	}
	t.Logf("dynamic item ids checked=%d unresolved=%d records=%d", checked, missing, len(records))
	if missing != 0 {
		t.Errorf("%d of %d DynamicItemID values resolved to nothing", missing, checked)
	}
}

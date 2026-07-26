// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"encoding/binary"
	"fmt"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// This file decodes the RawData byte arrays Palworld writes inside a world
// save. gvas surfaces them as plain []byte because Unreal's property tag says
// only "array of ByteProperty" -- the layout inside is game knowledge, which is
// why it lives here.
//
// Nothing decodes on load. gvas already hands back the blob without touching
// it, so a caller that never asks pays nothing; that is the whole of the
// laziness this needs. Level.sav holds 111,579 of these blobs, so eager
// decoding is not an option.
//
// One layout is decoded so far, the fixed item-slot record. See RawDataKind.

// RawDataKind names a RawData layout this package can decode. A path that names
// no known layout returns RawDataUnknown, which is the honest answer for the
// blobs still awaiting reverse engineering.
type RawDataKind int

const (
	// RawDataUnknown means the layout at this path is not decoded yet.
	RawDataUnknown RawDataKind = iota
	// RawDataItemSlot is one slot of an item container: a fixed record ending
	// in a mostly-zero trailer. See ItemSlot.
	RawDataItemSlot
)

func (kind RawDataKind) String() string {
	switch kind {
	case RawDataItemSlot:
		return "itemSlot"
	default:
		return "unknown"
	}
}

// rawDataPaths maps a decoded property path to the layout of the RawData found
// there. Paths use the same convention as typeHints: ".Value" is a map value,
// and a struct array repeats its own name for its elements -- which is why
// "Slots" appears twice below, once for the property and once for the element.
// That repetition is easy to get wrong from reading alone, so
// TestRawDataPathsExistInWorldSave checks each path against a real save.
//
// Keeping this a table rather than a chain of string comparisons means a new
// layout is one line plus a decoder, and makes the set of decoded locations
// something a test can enumerate.
var rawDataPaths = map[string]RawDataKind{
	".worldSaveData.ItemContainerSaveData.Value.Slots.Slots.RawData": RawDataItemSlot,
}

// ClassifyRawData reports which RawData layout sits at a decoded property path.
func ClassifyRawData(path string) RawDataKind { return rawDataPaths[path] }

// RawDataPaths returns the property paths with a known RawData layout. The
// result is a copy.
func RawDataPaths() map[string]RawDataKind {
	out := make(map[string]RawDataKind, len(rawDataPaths))
	for path, kind := range rawDataPaths {
		out[path] = kind
	}
	return out
}

// ItemSlot is one slot of an item container.
//
// The record is a fixed layout rather than a nested property stream:
//
//	uint32   SlotIndex
//	uint32   Count
//	FString  ItemID          -- length includes the NUL terminator
//	16 bytes                 -- zero in every slot of the fixture world
//	16 bytes DynamicItemID   -- zero unless the item has a per-instance record
//	trailer                  -- usually zero; see Trailer
//
// Verified against every one of the 27,094 slots in the 8,723 containers of a
// 1.0.1.100619 world save: all 27,094 decode, and all 422 non-zero
// DynamicItemID values match a DynamicItemSaveData record.
type ItemSlot struct {
	// SlotIndex is the slot's position within its container.
	SlotIndex uint32
	// Count is the stack size. An occupied slot always has a non-empty ItemID,
	// so Count is meaningful only alongside it.
	Count uint32
	// ItemID is the item's static identifier, such as "Money" or "PalSphere".
	// It is empty for an empty slot.
	ItemID string
	// DynamicItemID keys this stack's entry in
	// worldSaveData.DynamicItemSaveData, which holds the per-instance state of
	// items that have any: armour and weapon durability, for example. It is the
	// zero GUID for the great majority of slots, which hold plain stackables.
	DynamicItemID gvas.GUID
	// Trailer is the remainder after DynamicItemID, uninterpreted and aliasing
	// the caller's slice. It is all zero in 26,572 of 27,094 fixture slots; the
	// rest carry a small structure that is not decoded yet, so it is preserved
	// rather than dropped.
	Trailer []byte
}

// Empty reports whether the slot holds no item.
func (slot ItemSlot) Empty() bool { return slot.ItemID == "" }

const (
	// itemSlotHeaderBytes covers SlotIndex, Count, and the ItemID length.
	itemSlotHeaderBytes = 12
	// itemSlotDynamicIDOffset is where DynamicItemID starts, measured from the
	// end of ItemID. The 16 bytes before it are zero in every fixture slot;
	// naming the offset rather than reading a field keeps this honest about not
	// knowing what they are.
	itemSlotDynamicIDOffset = 16
	// itemSlotGUIDBytes is the width of DynamicItemID.
	itemSlotGUIDBytes = 16
)

// DecodeItemSlot decodes one Slots[].RawData payload. The returned ItemSlot
// aliases data through Trailer, so data must not be modified afterwards.
//
// A blob too short to hold the fixed part is an error rather than a partial
// result: a truncated slot means the layout assumption is wrong, and silently
// returning a plausible ItemSlot would hide that.
func DecodeItemSlot(data []byte) (ItemSlot, error) {
	if len(data) < itemSlotHeaderBytes {
		return ItemSlot{}, fmt.Errorf(
			"palworld: item slot needs %d bytes for its header, have %d",
			itemSlotHeaderBytes,
			len(data),
		)
	}
	slot := ItemSlot{
		SlotIndex: binary.LittleEndian.Uint32(data[0:4]),
		Count:     binary.LittleEndian.Uint32(data[4:8]),
	}
	nameBytes := binary.LittleEndian.Uint32(data[8:12])
	rest := data[itemSlotHeaderBytes:]
	if uint64(nameBytes) > uint64(len(rest)) {
		return ItemSlot{}, fmt.Errorf(
			"palworld: item slot declares a %d-byte item id with %d bytes left",
			nameBytes,
			len(rest),
		)
	}
	if nameBytes > 0 {
		encoded := rest[:nameBytes]
		if encoded[len(encoded)-1] != 0 {
			return ItemSlot{}, fmt.Errorf(
				"palworld: item slot id of %d bytes is not NUL-terminated",
				nameBytes,
			)
		}
		slot.ItemID = string(encoded[:len(encoded)-1])
		rest = rest[nameBytes:]
	}

	// Everything past the id is optional. A slot record can stop early, and an
	// absent DynamicItemID simply means the item has no per-instance record.
	if len(rest) >= itemSlotDynamicIDOffset+itemSlotGUIDBytes {
		guid := rest[itemSlotDynamicIDOffset : itemSlotDynamicIDOffset+itemSlotGUIDBytes]
		slot.DynamicItemID = gvas.GUID{
			A: binary.LittleEndian.Uint32(guid[0:4]),
			B: binary.LittleEndian.Uint32(guid[4:8]),
			C: binary.LittleEndian.Uint32(guid[8:12]),
			D: binary.LittleEndian.Uint32(guid[12:16]),
		}
		slot.Trailer = rest[itemSlotDynamicIDOffset+itemSlotGUIDBytes:]
	} else {
		slot.Trailer = rest
	}
	return slot, nil
}

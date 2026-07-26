// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"encoding/binary"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// This file is the registry of RawData layouts. The decoders themselves live
// beside it, one file per layout: itemslot.go, character.go and characterslot.go.
//
// gvas surfaces a RawData property as plain []byte because Unreal's property tag
// says only "array of ByteProperty" -- the layout inside is game knowledge,
// which is why it lives here.
//
// Nothing decodes on load. gvas already hands back the blob without touching
// it, so a caller that never asks pays nothing; that is the whole of the
// laziness this needs. Level.sav holds 111,579 of these blobs, so eager
// decoding is not an option.

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
	// RawDataCharacter is a player or pal record: a nested Unreal property
	// stream followed by a short framing trailer. See Character.
	RawDataCharacter
	// RawDataCharacterSlot is one slot of a pal party, storage box, or base camp
	// worker list: a reference to a character record rather than a record. See
	// CharacterSlot.
	RawDataCharacterSlot
)

func (kind RawDataKind) String() string {
	switch kind {
	case RawDataItemSlot:
		return "itemSlot"
	case RawDataCharacter:
		return "character"
	case RawDataCharacterSlot:
		return "characterSlot"
	default:
		return "unknown"
	}
}

// The paths of the blobs decoded so far. Each is also the path the decoder
// reports for its own contents, so they are named once here rather than
// repeated at the point of use.
const (
	itemSlotRawDataPath      = ".worldSaveData.ItemContainerSaveData.Value.Slots.Slots.RawData"
	characterRawDataPath     = ".worldSaveData.CharacterSaveParameterMap.Value.RawData"
	characterSlotRawDataPath = ".worldSaveData.CharacterContainerSaveData.Value.Slots.Slots.RawData"
)

// rawDataPaths maps a decoded property path to the layout of the RawData found
// there. Paths use the same convention as typeHints: ".Value" is a map value,
// and a struct array repeats its own name for its elements -- which is why
// "Slots" appears twice above, once for the property and once for the element.
// That repetition is easy to get wrong from reading alone, so
// TestRawDataPathsExistInWorldSave checks each path against a real save.
//
// Keeping this a table rather than a chain of string comparisons means a new
// layout is one line plus a decoder, and makes the set of decoded locations
// something a test can enumerate.
var rawDataPaths = map[string]RawDataKind{
	itemSlotRawDataPath:      RawDataItemSlot,
	characterRawDataPath:     RawDataCharacter,
	characterSlotRawDataPath: RawDataCharacterSlot,
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

// guidBytes is the serialized width of a GUID, which several RawData layouts
// embed at a fixed offset.
const guidBytes = 16

// guidAt reads the GUID starting at data[0], which must hold at least guidBytes.
// Unreal writes the four words little-endian, which is what gvas.GUID stores.
func guidAt(data []byte) gvas.GUID {
	return gvas.GUID{
		A: binary.LittleEndian.Uint32(data[0:4]),
		B: binary.LittleEndian.Uint32(data[4:8]),
		C: binary.LittleEndian.Uint32(data[8:12]),
		D: binary.LittleEndian.Uint32(data[12:16]),
	}
}

// allZero reports whether every byte is zero. Several layouts pad with bytes
// nothing here can name, and a caller rendering them wants to know when it is
// dropping nothing by leaving them out.
func allZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

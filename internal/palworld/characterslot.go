// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"fmt"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// CharacterSlot is one slot of a character container: a pal party, a pal storage
// box, or a base camp's worker list. Where an ItemSlot holds an item, this holds
// only a reference -- the pal itself is a CharacterSaveParameterMap entry, which
// is what makes this record the join between "which pals does this container
// hold" and "what is that pal".
//
// The record is Unreal's FPalInstanceID written flat, with no property framing:
//
//	16 bytes PlayerUId    -- zero in every slot of the fixture world
//	16 bytes InstanceId   -- keys worldSaveData.CharacterSaveParameterMap
//	trailer               -- six zero bytes in every slot of the fixture world
//
// Verified against all 2,250 slots in the 35 character containers of a
// 1.0.1.100619 world save: every slot is 38 bytes, every PlayerUId is zero,
// every trailer is zero, and the 2,250 InstanceId values are distinct and each
// matches a character record. That is also the population of pals in the world,
// counted a second way: a pal sits in exactly one slot.
type CharacterSlot struct {
	// PlayerUID is the account half of Unreal's FPalInstanceID. It is zero in
	// every fixture slot -- a character container holds pals, and a pal's
	// character-map key carries the zero UID too -- so it is read rather than
	// skipped precisely because a non-zero value would mean this assumption is
	// wrong.
	PlayerUID gvas.GUID
	// InstanceID keys worldSaveData.CharacterSaveParameterMap: the pal that
	// occupies this slot. It is the zero GUID for an empty slot.
	InstanceID gvas.GUID
	// Trailer is the remainder after InstanceID, uninterpreted and aliasing the
	// caller's slice. It is six zero bytes in every fixture slot, kept rather
	// than assumed to be padding.
	Trailer []byte
}

// Empty reports whether the slot references no character. A container declares
// more slots than it holds -- a pal storage box declares 960 -- but only the
// occupied ones are serialized, so no fixture slot is empty.
func (slot CharacterSlot) Empty() bool { return slot.InstanceID.IsZero() }

// characterSlotBytes is the fixed part: the two GUIDs of an FPalInstanceID.
const characterSlotBytes = 2 * guidBytes

// DecodeCharacterSlot decodes one CharacterContainerSaveData Slots[].RawData
// payload. The result aliases data through Trailer, so data must not be modified
// afterwards.
//
// A blob too short to hold both GUIDs is an error rather than a partial result,
// for the same reason DecodeItemSlot rejects a truncated record: a plausible
// half-read reference would resolve to nothing and look like missing data rather
// than a wrong layout.
func DecodeCharacterSlot(data []byte) (CharacterSlot, error) {
	if len(data) < characterSlotBytes {
		return CharacterSlot{}, fmt.Errorf(
			"palworld: character slot needs %d bytes for its instance id, have %d",
			characterSlotBytes,
			len(data),
		)
	}
	return CharacterSlot{
		PlayerUID:  guidAt(data[0:guidBytes]),
		InstanceID: guidAt(data[guidBytes:characterSlotBytes]),
		Trailer:    data[characterSlotBytes:],
	}, nil
}

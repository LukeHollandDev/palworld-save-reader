// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"path/filepath"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefixtures"
)

// These tests read real pal references, which are account-scoped identifiers.
// Nothing here logs one -- counts and shapes only.

// characterKeys collects the identity half of every CharacterSaveParameterMap
// entry without touching a single RawData blob. That is the cheap half of the
// map: the key carries the instance id and, for a player, the account id, so a
// test that only needs to know which characters exist pays nothing for the 2,259
// nested property streams it is not reading.
func characterKeys(t *testing.T, characters *gvas.MapValue) map[gvas.GUID]gvas.GUID {
	t.Helper()
	owners := map[gvas.GUID]gvas.GUID{}
	iterator := characters.Iterator()
	for iterator.Next() {
		key, ok := iterator.Entry().Key.(gvas.Properties)
		if !ok {
			t.Fatalf("character key is %T, want gvas.Properties", iterator.Entry().Key)
		}
		owners[keyGUID(t, key, "InstanceId")] = keyGUID(t, key, "PlayerUId")
	}
	if err := iterator.Err(); err != nil {
		t.Fatalf("character iteration: %v", err)
	}
	return owners
}

// TestDecodeCharacterSlotAgainstWorldSave is what makes CharacterSlot a claim
// about Palworld rather than about a hand-built blob, and it is a stronger claim
// than "every blob decodes". The slots are references, so the test of the layout
// is whether they refer to anything: 16 bytes read at the wrong offset decode
// into a perfectly plausible GUID that names nothing.
func TestDecodeCharacterSlotAgainstWorldSave(t *testing.T) {
	root := savefixtures.Root(t)
	save, err := Load(filepath.Join(root, "Level.sav"))
	if err != nil {
		t.Fatal(err)
	}

	owners := characterKeys(t, worldMap(t, save.Archive, "CharacterSaveParameterMap"))
	var pals int
	for _, owner := range owners {
		if owner.IsZero() {
			pals++
		}
	}
	if pals == 0 {
		t.Fatal("fixture world has no pal records for the slots to refer to")
	}

	var (
		slots       int
		empty       int
		unresolved  int
		withPlayer  int
		nonZeroTail int
		referenced  = map[gvas.GUID]int{}
		widths      = map[int]int{}
	)
	containers := slotBlobs(t, worldMap(t, save.Archive, "CharacterContainerSaveData"),
		func(container int, blob []byte) {
			slots++
			widths[len(blob)]++
			slot, err := DecodeCharacterSlot(blob)
			if err != nil {
				t.Fatalf("container %d slot %d (%d bytes): %v", container, slots, len(blob), err)
			}
			if !allZero(slot.Trailer) {
				nonZeroTail++
			}
			// A non-zero PlayerUID would mean a character container can hold a
			// player, which nothing else in this package assumes.
			if !slot.PlayerUID.IsZero() {
				withPlayer++
			}
			if slot.Empty() {
				empty++
				return
			}
			referenced[slot.InstanceID]++
			owner, ok := owners[slot.InstanceID]
			if !ok {
				unresolved++
				if unresolved <= 3 {
					t.Errorf("container %d slot %d refers to no character record", container, slots)
				}
				return
			}
			// Every referenced character must be a pal. A player is in the world
			// through their own save, not through a container slot.
			if !owner.IsZero() {
				t.Errorf("container %d slot %d refers to a character with a PlayerUId", container, slots)
			}
		})

	if containers == 0 || slots == 0 {
		t.Fatal("fixture world has no character container slots to exercise")
	}
	t.Logf("containers=%d slots=%d distinctReferences=%d empty=%d unresolved=%d withPlayerUID=%d nonZeroTrailer=%d widths=%v",
		containers, slots, len(referenced), empty, unresolved, withPlayer, nonZeroTail, widths)

	if unresolved != 0 {
		t.Errorf("%d slot references resolved to nothing", unresolved)
	}
	if withPlayer != 0 {
		t.Errorf("%d slots carry a non-zero PlayerUID", withPlayer)
	}
	// A pal sits in exactly one slot, so a duplicate reference would mean either
	// the slots are being read twice or two containers claim the same pal.
	for _, count := range referenced {
		if count > 1 {
			t.Errorf("%d slots refer to the same character record", count)
			break
		}
	}
	// The cross-check that phase 2 set up: the pal population counted through the
	// character map and counted through container slots must agree. Either count
	// alone can be wrong in a way that looks fine; agreeing is much harder.
	if len(referenced) != pals {
		t.Errorf("%d distinct pals are referenced by a slot, but %d pal records exist", len(referenced), pals)
	}
}

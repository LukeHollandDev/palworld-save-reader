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
// test that only needs to know which characters exist pays nothing for the
// thousands of nested property streams it is not reading.
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

// claimedContainers is every character container something in the save set points
// at: the two a player.sav names, and the one each base camp's worker director
// names.
//
// The distinction turns out to matter. A container nothing points at is not
// reachable from any join this tool performs, and the fixture world has nine of
// them -- leftovers, holding references to characters that are also gone.
func claimedContainers(t *testing.T, root string, save *gvas.Archive) map[gvas.GUID]string {
	t.Helper()
	claimed := map[gvas.GUID]string{}
	for _, path := range savefixtures.NormalPlayerSaves(t, root) {
		player, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		data := player.Properties.Find("SaveData")
		if data == nil {
			t.Fatalf("%s has no SaveData", filepath.Base(path))
		}
		structValue, ok := data.Value.(gvas.StructValue)
		if !ok {
			t.Fatalf("SaveData is %T", data.Value)
		}
		properties, ok := structValue.Value.(gvas.Properties)
		if !ok {
			t.Fatalf("SaveData holds %T", structValue.Value)
		}
		for _, name := range []string{"OtomoCharacterContainerId", "PalStorageContainerId"} {
			property := properties.Find(name)
			if property == nil {
				t.Fatalf("%s has no %s", filepath.Base(path), name)
			}
			inner, ok := property.Value.(gvas.StructValue)
			if !ok {
				t.Fatalf("%s is %T", name, property.Value)
			}
			list, ok := inner.Value.(gvas.Properties)
			if !ok {
				t.Fatalf("%s holds %T", name, inner.Value)
			}
			claimed[keyGUID(t, list, "ID")] = "player"
		}
	}
	guidKeyed(t, worldMap(t, save, "BaseCampSaveData"), func(id gvas.GUID, value gvas.Properties) {
		director, err := DecodeWorkerDirector(valueBlob(t, value, "WorkerDirector"))
		if err != nil {
			t.Fatalf("base camp %s worker director: %v", id, err)
		}
		claimed[director.ContainerID] = "baseCamp"
	})
	return claimed
}

// TestDecodeCharacterSlotAgainstWorldSave is what makes CharacterSlot a claim
// about Palworld rather than about a hand-built blob, and it is a stronger claim
// than "every blob decodes". The slots are references, so the test of the layout
// is whether they refer to anything: 16 bytes read at the wrong offset decode
// into a perfectly plausible GUID that names nothing.
//
// Phase 3 asserted that every reference in the save resolves, which was true of
// the save it was written against. Phase 4 found a newer save of the same world
// where seven do not, all of them in containers nothing points at -- so what is
// asserted now is the invariant that actually holds: every reference a player or
// a base camp can reach resolves, and together they reach every pal.
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
	claimed := claimedContainers(t, root, save.Archive)

	var (
		slots       int
		empty       int
		withPlayer  int
		nonZeroTail int
		reachable   = map[gvas.GUID]int{}
		orphaned    = map[gvas.GUID]int{}
		unresolved  = map[string]int{}
		containers  = map[gvas.GUID]bool{}
		widths      = map[int]int{}
	)
	characterContainers(t, worldMap(t, save.Archive, "CharacterContainerSaveData"),
		func(containerID gvas.GUID, slot CharacterSlot, index int32) {
			slots++
			containers[containerID] = true
			widths[characterSlotBytes+len(slot.Trailer)]++
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
			owner, resolved := owners[slot.InstanceID]
			kind, isClaimed := claimed[containerID]
			if !isClaimed {
				kind = "unreferenced"
				orphaned[slot.InstanceID]++
			} else {
				reachable[slot.InstanceID]++
			}
			if !resolved {
				unresolved[kind]++
				return
			}
			// Every referenced character must be a pal. A player is in the world
			// through their own save, not through a container slot.
			if !owner.IsZero() {
				t.Errorf("a slot of a %s container refers to a character with a PlayerUId", kind)
			}
		})

	if len(containers) == 0 || slots == 0 {
		t.Fatal("fixture world has no character container slots to exercise")
	}
	// containers counts only those with at least one slot, which is why it can be
	// smaller than the number of containers in the save.
	t.Logf("containersWithSlots=%d claimed=%d slots=%d reachable=%d inUnreferencedContainers=%d empty=%d withPlayerUID=%d nonZeroTrailer=%d widths=%v",
		len(containers), len(claimed), slots, len(reachable), len(orphaned), empty, withPlayer, nonZeroTail, widths)
	t.Logf("references that resolve to no record, by container kind: %v", unresolved)

	if unresolved["player"] != 0 || unresolved["baseCamp"] != 0 {
		t.Errorf("%d references from containers a player or a base camp names resolve to nothing", unresolved["player"]+unresolved["baseCamp"])
	}
	if withPlayer != 0 {
		t.Errorf("%d slots carry a non-zero PlayerUID", withPlayer)
	}
	// A pal sits in exactly one slot, so a duplicate reference would mean either
	// the slots are being read twice or two containers claim the same pal.
	for _, count := range reachable {
		if count > 1 {
			t.Errorf("%d slots refer to the same character record", count)
			break
		}
	}
	// The cross-check phase 2 set up and phase 4 sharpened: the pal population
	// counted through the character map and counted through the slots a player or
	// a base camp can reach must agree. Either count alone can be wrong in a way
	// that looks fine; agreeing is much harder.
	if len(reachable) != pals {
		t.Errorf("%d distinct pals sit in a container something names, but %d pal records exist",
			len(reachable), pals)
	}
}

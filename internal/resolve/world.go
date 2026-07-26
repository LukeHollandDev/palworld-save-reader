// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"fmt"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// World resolves the save set's metadata and entity counts.
//
// It is the cheap resolve: every count but the player/pal split is a collection's
// own declared size, and that split needs only the character map's keys, so no
// RawData blob is decoded at all.
func (r *Resolver) World() (*World, error) {
	world := &World{
		Counts:   Counts{PlayerSaves: len(r.set.Players)},
		Warnings: r.warnings,
	}

	if version, ok := value[int32](r.level.Properties, "Version"); ok {
		world.SaveVersion = &version
	}
	if revision, ok := value[int32](r.level.Properties, "Revision"); ok {
		world.Revision = &revision
	}
	if savedAt, ok := stamp(r.level.Properties, "Timestamp"); ok {
		world.SavedAt = &savedAt
	}
	if r.meta != nil {
		world.Name, _ = value[string](r.meta, "WorldName")
		if day, ok := value[int32](r.meta, "InGameDay"); ok {
			world.InGameDay = &day
		}
	}
	// Both GameTimeSaveData fields are elapsed times rather than dates. Reading
	// them as dates puts them in the year 1, which is how they were identified;
	// the game figure then agreed with LevelMeta's InGameDay, which is the check
	// that the reading is right.
	if ticks, ok := value[int64](r.world, "GameTimeSaveData.GameDateTimeTicks"); ok {
		gameTime := elapsed(ticks)
		world.GameTime = &gameTime
	}
	if ticks, ok := value[int64](r.world, "GameTimeSaveData.RealDateTimeTicks"); ok {
		realTime := elapsed(ticks)
		world.RealTime = &realTime
	}

	world.Counts.ItemContainers = r.mapCount("ItemContainerSaveData")
	world.Counts.CharacterContainers = r.mapCount("CharacterContainerSaveData")
	world.Counts.BaseCamps = r.mapCount("BaseCampSaveData")
	world.Counts.Groups = r.mapCount("GroupSaveDataMap")
	world.Counts.FoliageGrids = r.mapCount("FoliageGridSaveDataMap")
	world.Counts.DynamicItems = r.arrayCount("DynamicItemSaveData")
	world.Counts.MapObjects = r.arrayCount("MapObjectSaveData")

	characters, players, err := r.countCharacters()
	if err != nil {
		return nil, err
	}
	world.Counts.Characters = characters
	world.Counts.Players = players
	world.Counts.Pals = characters - players
	if players != len(r.set.Players) {
		world.Warnings = append(world.Warnings, fmt.Sprintf(
			"the world save holds %d player records but the directory has %d player saves",
			players, len(r.set.Players),
		))
	}
	return world, nil
}

// countCharacters splits the character map into players and everything else,
// reading only the keys. A player's key carries their account id and a pal's
// carries the zero GUID, so the split costs no RawData decoding -- which matters,
// because decoding all 2,259 records to count them would be the most expensive
// possible way to answer "how many are there".
func (r *Resolver) countCharacters() (characters, players int, err error) {
	collection, err := r.worldMap("CharacterSaveParameterMap")
	if err != nil {
		return 0, 0, err
	}
	iterator := collection.Iterator()
	for iterator.Next() {
		characters++
		key, ok := iterator.Entry().Key.(gvas.Properties)
		if !ok {
			continue
		}
		if uid, ok := guid(key, "PlayerUId"); ok && !uid.IsZero() {
			players++
		}
	}
	if err := iterator.Err(); err != nil {
		return 0, 0, fmt.Errorf("resolve: character records: %w", err)
	}
	return characters, players, nil
}

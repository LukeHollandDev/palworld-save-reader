// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"time"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// Version is the version of the documents this package emits.
//
// Unlike --full and --preset, these documents are a shape this tool invents:
// they name things "pals" and "inventory", which is an interpretation of the
// save rather than a view of it. A consumer therefore needs to know when the
// shape changes, and that is what this number is for. The save's own version and
// revision are separate, and reported in World.
const Version = 1

// Player is one resolved player: their own save joined to the world save.
//
// The split matters when reading a result, so the fields are grouped by where
// they came from. PlayerUID down to TechnologyPoints are read from the player's
// own player.sav. Character, Guild and Pals come from the world save, and are
// absent when the join found nothing -- a player.sav on its own cannot answer
// them. Nickname in particular is only in the world save; see Character.
type Player struct {
	PlayerUID        gvas.GUID  `json:"playerUId"`
	InstanceID       *gvas.GUID `json:"instanceId,omitempty"`
	Platform         string     `json:"platform,omitempty"`
	LastOnline       *Timestamp `json:"lastOnline,omitempty"`
	Position         *Position  `json:"position,omitempty"`
	TechnologyPoints *int32     `json:"technologyPoints,omitempty"`

	Character *Character `json:"character,omitempty"`
	Guild     *Guild     `json:"guild,omitempty"`
	Inventory Inventory  `json:"inventory"`
	Pals      []Pal      `json:"pals"`

	// Warnings names each join that found nothing, in the order it was
	// attempted. An unresolved reference is reported rather than dropped: "this
	// player has no pals" and "this player's pal container is missing from the
	// world save" look identical in the output otherwise.
	Warnings []string `json:"warnings,omitempty"`
}

// Character is the part of a player that lives in the world save's
// CharacterSaveParameterMap rather than in their own save.
//
// Nickname is the reason this section exists. A player.sav contains no name at
// all -- every string in it is a GUID, an enum, or an asset name -- so the only
// way to reach one is through this record.
type Character struct {
	Nickname string `json:"nickname,omitempty"`
	Level    uint8  `json:"level"`
	Exp      int64  `json:"exp"`
	// HP is Unreal's FixedPoint64 value exactly as stored. The scale is a game
	// constant this tool does not know, so it is reported unconverted rather
	// than divided by a guess.
	HP          int64   `json:"hp"`
	FullStomach float32 `json:"fullStomach,omitempty"`
}

// Guild is the group a player belongs to, identified by the group id carried in
// their character record's framing.
//
// Only the id is available: a guild's name and member list live in
// GroupSaveDataMap, whose RawData is a bespoke binary layout that is not decoded
// yet. Reporting the id alone is the honest half -- it is a verified join key,
// and every one in the fixture world names a real group.
type Guild struct {
	ID gvas.GUID `json:"id"`
}

// Pal is one pal a player owns, joined from three places: the container slot
// that places it, the character record that describes it, and the player save
// that named the container.
type Pal struct {
	InstanceID gvas.GUID `json:"instanceId"`
	// Species is Palworld's CharacterID, such as "DreamDemon". It is the
	// internal name, not the displayed one; mapping the two needs game data
	// this tool does not ship.
	Species  string `json:"species,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	Gender   string `json:"gender,omitempty"`
	Level    uint8  `json:"level"`
	Exp      int64  `json:"exp"`
	// HP is FixedPoint64 as stored, like Character.HP.
	HP int64 `json:"hp"`
	// Location is PalInParty or PalInStorage: which of the player's two
	// character containers holds this pal.
	Location      string   `json:"location"`
	Slot          int32    `json:"slot"`
	Talents       *Talents `json:"talents,omitempty"`
	PassiveSkills []string `json:"passiveSkills,omitempty"`
}

// Where a pal sits. A player has exactly two character containers: the party
// that follows them and the storage box behind them.
const (
	PalInParty   = "party"
	PalInStorage = "storage"
)

// Talents are a pal's individual values, 0-100.
type Talents struct {
	HP      uint8 `json:"hp"`
	Shot    uint8 `json:"shot"`
	Defense uint8 `json:"defense"`
}

// Position is a world-space location, in Unreal units.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Inventory is a player's six item containers, named for what the game puts in
// them. Every field is present even when empty, so a consumer can index them
// without checking.
type Inventory struct {
	Common    []ItemStack `json:"common"`
	DropSlot  []ItemStack `json:"dropSlot"`
	Essential []ItemStack `json:"essential"`
	Weapons   []ItemStack `json:"weapons"`
	Armor     []ItemStack `json:"armor"`
	Food      []ItemStack `json:"food"`
}

// ItemStack is one occupied slot of an item container. Empty slots are not
// emitted, because Palworld does not serialize them.
type ItemStack struct {
	Slot   uint32 `json:"slot"`
	ItemID string `json:"itemId"`
	Count  uint32 `json:"count"`
	// DynamicItemID keys DynamicItemSaveData, where the per-instance state of
	// items that have any -- durability, for example -- is stored. It is absent
	// for the great majority of stacks, which are plain stackables.
	DynamicItemID *gvas.GUID `json:"dynamicItemId,omitempty"`
}

// World is the save set as a whole: what it is, when it was written, and how
// much is in it.
type World struct {
	Name string `json:"name,omitempty"`
	// SaveVersion and Revision are the save's own version numbers, not this
	// package's. Revision matches the Palworld build that wrote the save --
	// 100619 for 1.0.1.100619 -- which makes it the value to quote when a
	// decode goes wrong.
	SaveVersion *int32     `json:"saveVersion,omitempty"`
	Revision    *int32     `json:"revision,omitempty"`
	SavedAt     *Timestamp `json:"savedAt,omitempty"`
	InGameDay   *int32     `json:"inGameDay,omitempty"`
	// GameTime and RealTime are elapsed times rather than dates; see Elapsed.
	GameTime *Elapsed `json:"gameTime,omitempty"`
	RealTime *Elapsed `json:"realTime,omitempty"`
	Counts   Counts   `json:"counts"`
	Warnings []string `json:"warnings,omitempty"`
}

// Counts is how much of each thing the save set holds. Every figure but Players,
// Pals and PlayerSaves is a collection's own declared count, so this costs one
// cheap pass over the character map's keys and nothing else.
type Counts struct {
	// PlayerSaves is the number of player.sav files found, which can differ
	// from Players: a player record stays in the world save after their file is
	// deleted.
	PlayerSaves int `json:"playerSaves"`
	Characters  int `json:"characters"`
	Players     int `json:"players"`
	Pals        int `json:"pals"`

	ItemContainers      int `json:"itemContainers"`
	CharacterContainers int `json:"characterContainers"`
	BaseCamps           int `json:"baseCamps"`
	Groups              int `json:"groups"`
	DynamicItems        int `json:"dynamicItems"`
	MapObjects          int `json:"mapObjects"`
	FoliageGrids        int `json:"foliageGrids"`
}

// Timestamp is one of Unreal's DateTime values used as a date: 100-nanosecond
// ticks since 0001-01-01. Both forms are reported because the ticks are what the
// save holds and the string is what a human reads.
type Timestamp struct {
	Ticks int64 `json:"ticks"`
	// UTC is the RFC 3339 rendering, absent when the ticks fall outside the
	// years a four-digit date can express. That is not a hypothetical: the same
	// DateTime type is also used for elapsed times, which land in year 1.
	UTC string `json:"utc,omitempty"`
}

// Elapsed is one of Unreal's DateTime values used as a duration rather than a
// date, which is how GameTimeSaveData stores both of its fields. Reading them as
// dates puts them in the year 1, which is how they were identified.
//
// The interpretation is checked rather than assumed: the fixture world's
// GameDateTimeTicks is 601.045 days and its LevelMeta InGameDay is 601.
type Elapsed struct {
	Ticks   int64 `json:"ticks"`
	Seconds int64 `json:"seconds"`
	Days    int64 `json:"days"`
}

const (
	// ticksPerSecond is Unreal's DateTime resolution: 100-nanosecond intervals.
	ticksPerSecond = 10_000_000
	// unrealEpochOffset is the number of seconds from 0001-01-01, where Unreal
	// counts from, to 1970-01-01, where Unix does.
	unrealEpochOffset = 62_135_596_800
	// maxRFC3339Seconds is the end of year 9999, the last a four-digit date can
	// express, in Unix seconds.
	maxRFC3339Seconds = 253_402_300_799
)

// timestamp converts Unreal DateTime ticks to a Timestamp, leaving UTC empty when
// the value is not a date a four-digit year can express.
//
// A negative tick count is rejected rather than clamped. It is before Unreal's own
// epoch, so it is not a date at all, and truncating it to 0001-01-01 would present
// nonsense as a real time.
func timestamp(ticks int64) Timestamp {
	stamp := Timestamp{Ticks: ticks}
	seconds := ticks/ticksPerSecond - unrealEpochOffset
	if ticks >= 0 && seconds <= maxRFC3339Seconds {
		stamp.UTC = time.Unix(seconds, 0).UTC().Format(time.RFC3339)
	}
	return stamp
}

// elapsed converts Unreal DateTime ticks to a duration.
func elapsed(ticks int64) Elapsed {
	seconds := ticks / ticksPerSecond
	return Elapsed{Ticks: ticks, Seconds: seconds, Days: seconds / 86_400}
}

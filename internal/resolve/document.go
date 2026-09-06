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
//
// Version 2 added the guild document and gave a player's guild a name and a
// member count, which version 1 could not supply because GroupSaveDataMap was
// not decoded. Version 3 adds the compact roster progress counters and arena
// rank points. Version 4 adds exact private progress keys to a single-player
// document. These changes only add fields, but a consumer that branches on
// their presence needs a number to branch on.
const Version = 4

// Roster is the compact player identity document used by integrations that do
// not need inventories or owned Pals. It deliberately shares the stable player
// and guild shapes with Player while avoiding the expensive collection passes
// required to populate a complete Player document.
type Roster struct {
	PlayerUID          gvas.GUID  `json:"playerUId"`
	Character          *Character `json:"character,omitempty"`
	Guild              *GuildRef  `json:"guild,omitempty"`
	FastTravelUnlocked *int       `json:"fastTravelUnlocked,omitempty"`
	AreasDiscovered    *int       `json:"areasDiscovered,omitempty"`
	BossDefeats        *int       `json:"bossDefeats,omitempty"`
	TowerDefeats       *int       `json:"towerDefeats,omitempty"`
	Warnings           []string   `json:"warnings,omitempty"`
}

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
	Progress         Progress   `json:"progress"`

	Character *Character `json:"character,omitempty"`
	Guild     *GuildRef  `json:"guild,omitempty"`
	Inventory Inventory  `json:"inventory"`
	Pals      []Pal      `json:"pals"`

	// Warnings names each join that found nothing, in the order it was
	// attempted. An unresolved reference is reported rather than dropped: "this
	// player has no pals" and "this player's pal container is missing from the
	// world save" look identical in the output otherwise.
	Warnings []string `json:"warnings,omitempty"`
}

// Progress contains exact save keys for self-only completion matching. A
// non-nil empty slice means the map decoded successfully and has no set flags;
// nil means that domain could not be decoded and must be treated as unknown.
// Consumers must never publish these keys as another player's public data.
type Progress struct {
	FastTravel   []string `json:"fastTravel"`
	Areas        []string `json:"areas"`
	Notes        []string `json:"notes"`
	Relics       []string `json:"relics"`
	ItemPickups  []string `json:"itemPickups"`
	NormalBosses []string `json:"normalBosses"`
	TowerBosses  []string `json:"towerBosses"`
}

// Character is the part of a player that lives in the world save's
// CharacterSaveParameterMap rather than in their own save.
//
// Nickname is the reason this section exists. A player.sav contains no name at
// all -- every string in it is a GUID, an enum, or an asset name -- so the only
// way to reach one is through this record.
type Character struct {
	Nickname        string `json:"nickname,omitempty"`
	Level           uint8  `json:"level"`
	Exp             int64  `json:"exp"`
	ArenaRankPoints *int32 `json:"arenaRankPoints,omitempty"`
	// HP is Unreal's FixedPoint64 value exactly as stored. The scale is a game
	// constant this tool does not know, so it is reported unconverted rather
	// than divided by a guess.
	HP          int64   `json:"hp"`
	FullStomach float32 `json:"fullStomach,omitempty"`
}

// GuildRef is the guild a player belongs to, as much of it as a player document
// needs: the group id from their character record's framing, and the name and
// size the group record adds.
//
// The name costs one extra pass over GroupSaveDataMap, which holds 15 entries in
// the fixture world against the character map's 3,349 -- cheap enough that
// answering "which guild" with an opaque id would be a worse trade. Ask for the
// guild itself to get its bases and members.
type GuildRef struct {
	ID gvas.GUID `json:"id"`
	// Name is the guild's display name, absent when the group record could not be
	// read. "Unnamed Guild" is a real value: it is what Palworld stores until a
	// guild is named.
	Name string `json:"name,omitempty"`
	// MemberCount is how many accounts the guild lists.
	MemberCount int `json:"memberCount,omitempty"`
}

// Guild is one resolved guild: who is in it, where its bases are, and which pals
// work at them.
//
// Everything here comes from the world save alone -- no player.sav is read, and
// none is needed. A guild record carries its members' names, which is the second
// place in a save where a player's name can be found and the only one that gives
// you the names of players whose saves you are not reading.
type Guild struct {
	GroupID gvas.GUID `json:"groupId"`
	Name    string    `json:"name,omitempty"`
	// Admin is the account that runs the guild. Palworld usually also stores this
	// id as the group's internal name, but current saves can leave that field empty.
	Admin *gvas.GUID `json:"admin,omitempty"`
	// BaseCampLevel is the guild's base camp level, which is guild-wide rather
	// than per base.
	BaseCampLevel int32         `json:"baseCampLevel"`
	Members       []GuildMember `json:"members"`
	Bases         []Base        `json:"bases"`
	Counts        GuildCounts   `json:"counts"`
	// Warnings names each join that found nothing, in the order it was attempted.
	Warnings []string `json:"warnings,omitempty"`
}

// GuildMember is one account in a guild, as the guild's own record describes it.
type GuildMember struct {
	PlayerUID gvas.GUID `json:"playerUId"`
	Name      string    `json:"name,omitempty"`
	// LastOnline is elapsed real time since the world began, not a date: it is
	// measured in the same clock as World.RealTime, and a member who was online
	// when the save was written carries exactly that value.
	LastOnline *Elapsed `json:"lastOnline,omitempty"`
	// Role is 1 for the admin of every fixture guild and 3 for the only
	// non-admin member. The values are reported unmapped because two samples are
	// not a decoding.
	Role uint8 `json:"role"`
}

// Base is one of a guild's base camps.
type Base struct {
	ID   gvas.GUID `json:"id"`
	Name string    `json:"name,omitempty"`
	// Location is the camp's position, on the same scale as a player's.
	Location *Position `json:"location,omitempty"`
	// AreaRange is the camp's radius in world units.
	AreaRange float32 `json:"areaRange,omitempty"`
	// OwnerMapObjectID is the map object the camp is built around, which the
	// guild record also lists among its base points. The object's own record is
	// in MapObjectSaveData, which is not decoded, so nothing here says what the
	// object is.
	OwnerMapObjectID *gvas.GUID `json:"ownerMapObjectId,omitempty"`
	// Workers are the pals assigned to the camp. They are the pals no player
	// owns: in the fixture world 205 pals sit in these containers and 3,135 sit
	// in a player's party or box, which together are every one of the save's
	// 3,340 pals.
	Workers []Pal `json:"workers"`
}

// GuildCounts is the guild's population, counted from the character handles the
// group record lists rather than from anything this tool joined. Players plus
// Pals is Characters by construction; the value of the split is that Players
// matches the member count and Pals matches what the containers hold.
type GuildCounts struct {
	Characters int `json:"characters"`
	Players    int `json:"players"`
	Pals       int `json:"pals"`
	Bases      int `json:"bases"`
	// Workers is how many of the guild's pals were found in its bases' worker
	// containers.
	Workers int `json:"workers"`
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
	// Location is PalInParty, PalInStorage or PalAtBase: which character
	// container holds this pal.
	Location      string   `json:"location"`
	Slot          int32    `json:"slot"`
	Talents       *Talents `json:"talents,omitempty"`
	PassiveSkills []string `json:"passiveSkills,omitempty"`
}

// Where a pal sits. A player has exactly two character containers, the party
// that follows them and the storage box behind them; a guild's base camp has one
// more, holding the pals that work there and belong to no player.
const (
	PalInParty   = "party"
	PalInStorage = "storage"
	PalAtBase    = "base"
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

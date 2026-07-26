// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// The synthetic world the tests below share. The ids are deliberately arranged so
// that every branch of the join is reachable: a container the world save does not
// have, a container the player save does not name, a pal nobody owns, a record
// with no Level, and a character record for a player whose save file is gone.
const (
	// Player one, complete and consistent.
	oneUID       = 1
	oneInstance  = 101
	oneParty     = 20
	oneStorage   = 21
	oneCommon    = 10
	oneDropSlot  = 11
	oneEssential = 12
	oneWeapons   = 13
	oneArmor     = 14
	oneFood      = 15
	// Player two, missing things on purpose.
	twoUID           = 2
	twoInstance      = 102
	twoParty         = 30
	twoStorageGone   = 31
	twoCommon        = 16
	twoEssentialGone = 35
	// A character container belonging to a base camp rather than a player.
	campContainer = 40
	// A character record whose player save is not in the directory.
	ghostUID      = 3
	ghostInstance = 103
)

func testWorld() worldFixture {
	one := playerFixture{
		uid:        id(oneUID),
		instance:   id(oneInstance),
		stem:       "alpha",
		platform:   "PS5",
		technology: 48,
		position:   gvas.Vector{X: 1.5, Y: -2.5, Z: 3.5},
		// 2026-07-24T11:58:10Z, so the conversion is checked against a real date.
		lastOnline: 639204910905580000,
		nickname:   "Alpha",
		level:      46,
		exp:        1641238,
		hp:         1500000,
		stomach:    57.5,
		group:      id(90),
		common:     id(oneCommon),
		dropSlot:   id(oneDropSlot),
		essential:  id(oneEssential),
		weapons:    id(oneWeapons),
		armor:      id(oneArmor),
		food:       id(oneFood),
		party:      id(oneParty),
		storage:    id(oneStorage),
		isPlayer:   true,
	}
	two := playerFixture{
		uid:      id(twoUID),
		instance: id(twoInstance),
		stem:     "beta",
		nickname: "Beta",
		level:    3,
		hp:       500000,
		group:    id(91),
		common:   id(twoCommon),
		// No dropSlot, weapons, armor or food container named at all, and an
		// essential container the world save does not have.
		essential: id(twoEssentialGone),
		party:     id(twoParty),
		// A storage container the world save does not have.
		storage: id(twoStorageGone),
		// No IsPlayer flag, which must be reported rather than assumed.
		isPlayer: false,
	}
	ghost := playerFixture{
		uid:      id(ghostUID),
		instance: id(ghostInstance),
		nickname: "Ghost",
		level:    9,
		isPlayer: true,
	}

	world := worldFixture{
		revision:   100619,
		savedAt:    639204910905580000,
		gameTicks:  519302980000000, // 601 days
		realTicks:  11444635700000,  // 13 days
		players:    []playerFixture{one, two},
		mapObjects: 4,
		pals: []palFixture{
			// Out of slot order, so the sort has something to do.
			{instance: id(201), species: "Suzaku", gender: "Female", level: 42, exp: 695203,
				hp: 3419000, talents: []byte{25, 15, 76}, passives: []string{"PAL_sadist"},
				owner: id(oneParty), slot: 1},
			{instance: id(202), species: "DreamDemon", gender: "Male", level: 8, exp: 539,
				hp: 877000, owner: id(oneParty), slot: 0},
			// No Level and no Exp: a freshly caught level-one pal, which Palworld
			// writes by omitting both properties.
			{instance: id(203), species: "Lamball", owner: id(oneStorage), slot: 5},
			{instance: id(204), species: "Foxparks", nickname: "Sparky", level: 12,
				owner: id(oneStorage), slot: 2},
		},
		unowned: []palFixture{
			{instance: id(205), species: "Workerbee", level: 20, owner: id(campContainer), slot: 0},
		},
		characters: []gvas.GUID{id(oneParty), id(oneStorage), id(twoParty), id(campContainer)},
		itemOrder: []gvas.GUID{
			id(oneCommon), id(oneDropSlot), id(oneEssential),
			id(oneWeapons), id(oneArmor), id(oneFood), id(twoCommon),
		},
		items: map[gvas.GUID][]itemFixture{
			id(oneCommon): {
				// Out of slot order, so the sort has something to do.
				{slot: 2, itemID: "PalSphere_Mega", count: 7},
				{slot: 0, itemID: "Money", count: 70434},
				{slot: 1, itemID: "SphereLauncher_Once", count: 1, dynamic: id(70)},
			},
			id(oneWeapons): {{slot: 0, itemID: "AssaultRifle_Advanced", count: 1, dynamic: id(71)}},
			id(twoCommon):  {{slot: 0, itemID: "Wood", count: 12}},
		},
	}
	// The ghost has a record in the world save but no save file, since it has no
	// stem, so it appears in the character map with nothing in the directory to
	// account for it.
	world.players = append(world.players, ghost)
	return world
}

func openTestResolver(t *testing.T) *Resolver {
	t.Helper()
	directory := writeSaveSet(t, testWorld(), metaSave("Synthetic", 601))
	set, err := Discover(directory)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := Open(set, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func resolveAll(t *testing.T, resolver *Resolver) []*Player {
	t.Helper()
	var players []*Player
	if err := resolver.Players(func(player *Player) error {
		players = append(players, player)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return players
}

// TestResolvePlayersJoinsBothHalves is the whole point of the package, stated
// against a world small enough to write down: the player save half, the world save
// half, and the pals that only exist as the intersection of the two.
func TestResolvePlayersJoinsBothHalves(t *testing.T) {
	players := resolveAll(t, openTestResolver(t))
	if len(players) != 2 {
		t.Fatalf("resolved %d players, want 2", len(players))
	}
	// File order, which is the stems "alpha" then "beta" -- deliberately not the
	// account ids, so a resolve that read the id off the file name would fail.
	one, two := players[0], players[1]
	if one.PlayerUID != id(oneUID) || two.PlayerUID != id(twoUID) {
		t.Fatalf("resolved %s then %s", one.PlayerUID, two.PlayerUID)
	}

	if one.InstanceID == nil || *one.InstanceID != id(oneInstance) {
		t.Errorf("instanceId = %v", one.InstanceID)
	}
	if one.Platform != "PS5" {
		t.Errorf("platform = %q, want the enum prefix stripped", one.Platform)
	}
	if one.TechnologyPoints == nil || *one.TechnologyPoints != 48 {
		t.Errorf("technologyPoints = %v", one.TechnologyPoints)
	}
	if one.Position == nil || *one.Position != (Position{X: 1.5, Y: -2.5, Z: 3.5}) {
		t.Errorf("position = %v", one.Position)
	}
	if one.LastOnline == nil || one.LastOnline.UTC != "2026-07-24T11:58:10Z" {
		t.Errorf("lastOnline = %v", one.LastOnline)
	}

	// The world-save half. A name is the thing a player.sav cannot answer.
	if one.Character == nil {
		t.Fatal("player one has no character record")
	}
	if one.Character.Nickname != "Alpha" || one.Character.Level != 46 || one.Character.Exp != 1641238 {
		t.Errorf("character = %+v", *one.Character)
	}
	if one.Character.HP != 1500000 {
		t.Errorf("hp = %d, want the FixedPoint64 value unconverted", one.Character.HP)
	}
	if one.Guild == nil || one.Guild.ID != id(90) {
		t.Errorf("guild = %v", one.Guild)
	}
	if len(one.Warnings) != 0 {
		t.Errorf("a complete player produced warnings: %q", one.Warnings)
	}

	// The inventory, sorted by slot rather than left in serialized order.
	want := []ItemStack{
		{Slot: 0, ItemID: "Money", Count: 70434},
		{Slot: 1, ItemID: "SphereLauncher_Once", Count: 1},
		{Slot: 2, ItemID: "PalSphere_Mega", Count: 7},
	}
	if len(one.Inventory.Common) != len(want) {
		t.Fatalf("common inventory = %+v", one.Inventory.Common)
	}
	for index, stack := range one.Inventory.Common {
		if stack.Slot != want[index].Slot || stack.ItemID != want[index].ItemID || stack.Count != want[index].Count {
			t.Errorf("common[%d] = %+v, want %+v", index, stack, want[index])
		}
	}
	if one.Inventory.Common[1].DynamicItemID == nil || *one.Inventory.Common[1].DynamicItemID != id(70) {
		t.Errorf("the stack with a per-instance record reported dynamicItemId %v",
			one.Inventory.Common[1].DynamicItemID)
	}
	if one.Inventory.Common[0].DynamicItemID != nil {
		t.Error("a plain stackable reported a dynamicItemId")
	}
	if len(one.Inventory.Weapons) != 1 || one.Inventory.Weapons[0].ItemID != "AssaultRifle_Advanced" {
		t.Errorf("weapons = %+v", one.Inventory.Weapons)
	}
	for name, stacks := range map[string][]ItemStack{
		"dropSlot":  one.Inventory.DropSlot,
		"essential": one.Inventory.Essential,
		"armor":     one.Inventory.Armor,
		"food":      one.Inventory.Food,
	} {
		if len(stacks) != 0 {
			t.Errorf("%s = %+v, want empty", name, stacks)
		}
	}

	// The pals, party first and then by slot.
	if len(one.Pals) != 4 {
		t.Fatalf("pals = %+v", one.Pals)
	}
	wantPals := []Pal{
		{InstanceID: id(202), Species: "DreamDemon", Gender: "Male", Level: 8, Exp: 539, HP: 877000, Location: PalInParty, Slot: 0},
		{InstanceID: id(201), Species: "Suzaku", Gender: "Female", Level: 42, Exp: 695203, HP: 3419000, Location: PalInParty, Slot: 1},
		{InstanceID: id(204), Species: "Foxparks", Nickname: "Sparky", Level: 12, Location: PalInStorage, Slot: 2},
		// No Level and no Exp in the record: a level-one pal.
		{InstanceID: id(203), Species: "Lamball", Level: 1, Location: PalInStorage, Slot: 5},
	}
	for index, pal := range one.Pals {
		expected := wantPals[index]
		if pal.InstanceID != expected.InstanceID || pal.Species != expected.Species ||
			pal.Nickname != expected.Nickname || pal.Gender != expected.Gender ||
			pal.Level != expected.Level || pal.Exp != expected.Exp || pal.HP != expected.HP ||
			pal.Location != expected.Location || pal.Slot != expected.Slot {
			t.Errorf("pals[%d] = %+v, want %+v", index, pal, expected)
		}
	}
	if one.Pals[1].Talents == nil || *one.Pals[1].Talents != (Talents{HP: 25, Shot: 15, Defense: 76}) {
		t.Errorf("talents = %v", one.Pals[1].Talents)
	}
	if one.Pals[0].Talents != nil {
		t.Errorf("a record with no Talent_ properties reported talents %v", one.Pals[0].Talents)
	}
	if len(one.Pals[1].PassiveSkills) != 1 || one.Pals[1].PassiveSkills[0] != "PAL_sadist" {
		t.Errorf("passiveSkills = %v", one.Pals[1].PassiveSkills)
	}

	// The pal in the base camp's container belongs to nobody and must not have
	// been handed to either player.
	for _, player := range players {
		for _, pal := range player.Pals {
			if pal.InstanceID == id(205) {
				t.Errorf("the unowned pal was resolved to player %s", player.PlayerUID)
			}
		}
	}
	if len(two.Pals) != 0 {
		t.Errorf("player two's empty party resolved to %+v", two.Pals)
	}
}

// TestResolveReportsUnresolvedJoins is the other half of the contract: a join that
// found nothing is named rather than left as an absent field, because "no pals"
// and "the pal container is missing" are otherwise indistinguishable.
func TestResolveReportsUnresolvedJoins(t *testing.T) {
	players := resolveAll(t, openTestResolver(t))
	two := players[1]
	for _, want := range []string{
		"essential inventory container", // named by the save, absent from the world
		"storage pal container",         // named by the save, absent from the world
		"names no dropSlot inventory",   // not named by the save at all
		"names no weapons inventory",
		"names no armor inventory",
		"names no food inventory",
		"not flagged as a player", // the record has no IsPlayer
	} {
		if !containsSubstring(two.Warnings, want) {
			t.Errorf("warnings %q do not mention %q", two.Warnings, want)
		}
	}
	// The record was still read, so the name is still available: an unresolved
	// join degrades the document rather than emptying it.
	if two.Character == nil || two.Character.Nickname != "Beta" {
		t.Errorf("character = %+v", two.Character)
	}
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

// TestPlayerSelectsOneByID checks the single-player path resolves the same thing
// the whole-set path does, and that an id nobody has is an error rather than an
// empty document.
func TestPlayerSelectsOneByID(t *testing.T) {
	resolver := openTestResolver(t)
	player, err := resolver.Player(id(twoUID))
	if err != nil {
		t.Fatal(err)
	}
	if player.PlayerUID != id(twoUID) || player.Character == nil || player.Character.Nickname != "Beta" {
		t.Fatalf("resolved %+v", player)
	}

	missing := id(999)
	if _, err := resolver.Player(missing); err == nil {
		t.Error("an id no player save has resolved without error")
	} else if !strings.Contains(err.Error(), missing.String()) {
		t.Errorf("error %q does not name the id", err)
	}
	// The ghost has a character record but no save file, so it is not resolvable
	// either: a player is a save file joined to a record, not a record alone.
	if _, err := resolver.Player(id(ghostUID)); err == nil {
		t.Error("a player with a record but no save file resolved without error")
	}
	if _, err := resolver.Player(gvas.GUID{}); err == nil {
		t.Error("the zero id resolved without error")
	}
}

// TestPlayersPropagatesAVisitError matters for the streaming caller: a write
// failure part way through the array has to stop the resolve rather than be
// swallowed.
func TestPlayersPropagatesAVisitError(t *testing.T) {
	sentinel := errors.New("stop")
	visited := 0
	err := openTestResolver(t).Players(func(*Player) error {
		visited++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want the visit error", err)
	}
	if visited != 1 {
		t.Errorf("visited %d players after an error, want 1", visited)
	}
}

// TestWorldCountsWhatIsThere covers the cheap resolve, including the two figures
// that are counted rather than declared and the mismatch between them.
func TestWorldCountsWhatIsThere(t *testing.T) {
	world, err := openTestResolver(t).World()
	if err != nil {
		t.Fatal(err)
	}
	if world.Name != "Synthetic" {
		t.Errorf("name = %q", world.Name)
	}
	if world.InGameDay == nil || *world.InGameDay != 601 {
		t.Errorf("inGameDay = %v", world.InGameDay)
	}
	if world.Revision == nil || *world.Revision != 100619 {
		t.Errorf("revision = %v", world.Revision)
	}
	if world.SavedAt == nil || world.SavedAt.UTC != "2026-07-24T11:58:10Z" {
		t.Errorf("savedAt = %v", world.SavedAt)
	}
	// The reason GameTime is an Elapsed and not a Timestamp: it agrees with the
	// in-game day, which a date reading of the same ticks would not.
	if world.GameTime == nil || world.GameTime.Days != 601 {
		t.Errorf("gameTime = %v, want 601 days to match inGameDay", world.GameTime)
	}
	if world.RealTime == nil || world.RealTime.Days != 13 {
		t.Errorf("realTime = %v", world.RealTime)
	}

	want := Counts{
		PlayerSaves:         2,
		Characters:          8, // 3 player records + 4 pals + 1 unowned pal
		Players:             3, // including the ghost, whose save file is gone
		Pals:                5,
		ItemContainers:      7,
		CharacterContainers: 4,
		MapObjects:          4,
	}
	if world.Counts != want {
		t.Errorf("counts = %+v, want %+v", world.Counts, want)
	}
	// The world save holds a player record the directory cannot account for, which
	// is a discrepancy worth reporting rather than resolving silently.
	if !containsSubstring(world.Warnings, "3 player records") {
		t.Errorf("warnings %q do not mention the record without a save file", world.Warnings)
	}
}

// TestWorldWithoutLevelMeta covers the file that is allowed to be missing.
func TestWorldWithoutLevelMeta(t *testing.T) {
	directory := writeSaveSet(t, testWorld(), nil)
	set, err := Discover(directory)
	if err != nil {
		t.Fatal(err)
	}
	if set.LevelMeta != "" {
		t.Fatalf("LevelMeta = %q, want empty", set.LevelMeta)
	}
	resolver, err := Open(set, Options{})
	if err != nil {
		t.Fatalf("a save set without %s failed to open: %v", levelMetaName, err)
	}
	world, err := resolver.World()
	if err != nil {
		t.Fatal(err)
	}
	if world.Name != "" || world.InGameDay != nil {
		t.Errorf("a world with no metadata save reported name %q and day %v", world.Name, world.InGameDay)
	}
	if !containsSubstring(world.Warnings, levelMetaName) {
		t.Errorf("warnings %q do not mention the missing metadata save", world.Warnings)
	}
	// Everything that comes from the world save is still there.
	if world.Counts.Characters == 0 {
		t.Error("a world with no metadata save counted no characters")
	}
}

// TestResolveWithoutPlayerSaves covers a world nobody has joined: an empty result
// rather than an error, since that is a true answer.
func TestResolveWithoutPlayerSaves(t *testing.T) {
	world := testWorld()
	world.players = nil
	directory := writeSaveSet(t, world, metaSave("Empty", 1))
	set, err := Discover(directory)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := Open(set, Options{})
	if err != nil {
		t.Fatal(err)
	}
	visited := 0
	if err := resolver.Players(func(*Player) error {
		visited++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if visited != 0 {
		t.Errorf("visited %d players in a world with no player saves", visited)
	}
}

// TestOpenRejectsANonWorldSave keeps the failure legible: pointing --saves at a
// directory whose Level.sav is something else should say so rather than resolve
// nothing.
func TestOpenRejectsANonWorldSave(t *testing.T) {
	world := testWorld()
	directory := writeSaveSet(t, world, nil)
	// Overwrite Level.sav with a player save, which parses but has no
	// worldSaveData.
	writeFile(t, directory, levelName, world.players[0].save())

	set, err := Discover(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(set, Options{}); err == nil {
		t.Fatal("a save with no worldSaveData opened as a world")
	} else if !strings.Contains(err.Error(), "worldSaveData") {
		t.Errorf("error %q does not name what was missing", err)
	}
}

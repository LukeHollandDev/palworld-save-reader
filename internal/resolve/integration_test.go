// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"reflect"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/palworld"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefixtures"
)

// These tests resolve real players, which is private data: names, account
// identifiers, coordinates and progression. Nothing here logs any of it -- counts
// and shapes only. Failure messages are allowed to name an identifier, because a
// failure has to be diagnosable, and they are not committed anywhere.

// fixtureResolver opens the private save set, skipping when there is none.
func fixtureResolver(t *testing.T) *Resolver {
	t.Helper()
	set, err := Discover(savefixtures.Root(t))
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := Open(set, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

// TestResolveEveryPlayerInTheFixtureSet is the claim the phase was built for: for
// every player with a save file, the world save answers the questions their own
// save cannot. It is stated as "no warnings at all", which is stronger than any
// individual field check -- every unresolved join reports itself.
func TestResolveEveryPlayerInTheFixtureSet(t *testing.T) {
	var (
		players  int
		named    int
		guilds   = map[gvas.GUID]int{}
		pals     int
		stacks   int
		warnings int
	)
	err := fixtureResolver(t).Players(func(player *Player) error {
		players++
		if player.Character != nil && player.Character.Nickname != "" {
			named++
		}
		if player.Guild != nil {
			guilds[player.Guild.ID]++
		}
		pals += len(player.Pals)
		for _, group := range [][]ItemStack{
			player.Inventory.Common, player.Inventory.DropSlot, player.Inventory.Essential,
			player.Inventory.Weapons, player.Inventory.Armor, player.Inventory.Food,
		} {
			stacks += len(group)
		}
		for _, warning := range player.Warnings {
			warnings++
			t.Errorf("player %s: %s", player.PlayerUID, warning)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("players=%d named=%d guilds=%d pals=%d stacks=%d warnings=%d",
		players, named, len(guilds), pals, stacks, warnings)

	if players == 0 {
		t.Fatal("the fixture save set resolved no players")
	}
	// A name is what a player.sav cannot answer, so every resolved player having
	// one is the phase 2 join working through this package.
	if named != players {
		t.Errorf("%d of %d players resolved to a name", named, players)
	}
	if pals == 0 {
		t.Error("no player resolved to any pals")
	}
	if stacks == 0 {
		t.Error("no player resolved to any inventory")
	}
	if len(guilds) == 0 {
		t.Error("no player resolved to a guild")
	}
}

// TestResolvedPalsAgreeWithTheirRecords is the cross-check that the placement is
// right rather than merely consistent. This package decides a pal is a player's
// because it sits in a container the player's save names; the record itself
// separately carries an OwnerPlayerUId. Those are two independent statements of
// ownership, and they have to agree.
//
// It decodes every character record, which is exactly what the resolver refuses to
// do. That is the point: the expensive reading is what makes the cheap reading
// checkable.
func TestResolvedPalsAgreeWithTheirRecords(t *testing.T) {
	resolver := fixtureResolver(t)

	// Every pal record's own idea of who owns it.
	owners := map[gvas.GUID]gvas.GUID{}
	characters, err := resolver.worldMap("CharacterSaveParameterMap")
	if err != nil {
		t.Fatal(err)
	}
	var owned int
	iterator := characters.Iterator()
	for iterator.Next() {
		key, ok := iterator.Entry().Key.(gvas.Properties)
		if !ok {
			continue
		}
		instance, _ := guid(key, "InstanceId")
		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			continue
		}
		data, ok := blob(properties, "RawData")
		if !ok {
			continue
		}
		character, err := palworld.DecodeCharacter(data)
		if err != nil {
			t.Fatalf("character record %s: %v", instance, err)
		}
		parameters := character.Parameters()
		if parameters == nil {
			continue
		}
		if owner, ok := guid(parameters, "OwnerPlayerUId"); ok && !owner.IsZero() {
			owners[instance] = owner
			owned++
		}
	}
	if err := iterator.Err(); err != nil {
		t.Fatal(err)
	}
	if owned == 0 {
		t.Fatal("no character record carries an OwnerPlayerUId, so there is nothing to cross-check")
	}

	var resolved, mismatched, unowned int
	err = resolver.Players(func(player *Player) error {
		for _, pal := range player.Pals {
			resolved++
			owner, ok := owners[pal.InstanceID]
			if !ok {
				unowned++
				if unowned <= 3 {
					t.Errorf("pal %s is in player %s's %s but its record names no owner",
						pal.InstanceID, player.PlayerUID, pal.Location)
				}
				continue
			}
			if owner != player.PlayerUID {
				mismatched++
				if mismatched <= 3 {
					t.Errorf("pal %s is in player %s's %s but its record names owner %s",
						pal.InstanceID, player.PlayerUID, pal.Location, owner)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("palsWithAnOwnerRecord=%d resolvedToAPlayer=%d mismatched=%d withoutAnOwner=%d",
		owned, resolved, mismatched, unowned)
	if resolved == 0 {
		t.Fatal("no pals were resolved, so the cross-check proved nothing")
	}
	// The two counts are arrived at completely differently -- one by walking
	// containers the player saves name, the other by reading a field inside each
	// record -- so agreement is a strong statement that neither is wrong.
	if resolved != owned {
		t.Errorf("%d pals resolved to a player, but %d records carry an owner", resolved, owned)
	}
}

// TestResolvedGuildIDsAreRealGroups repeats for guilds the standard phase 1 and 2
// held their join keys to: an id that names nothing is a decoding artefact.
func TestResolvedGuildIDsAreRealGroups(t *testing.T) {
	resolver := fixtureResolver(t)

	groups := map[gvas.GUID]bool{}
	collection, err := resolver.worldMap("GroupSaveDataMap")
	if err != nil {
		t.Fatal(err)
	}
	iterator := collection.Iterator()
	for iterator.Next() {
		if key, ok := iterator.Entry().Key.(gvas.GUID); ok {
			groups[key] = true
		}
	}
	if err := iterator.Err(); err != nil {
		t.Fatal(err)
	}
	if len(groups) == 0 {
		t.Fatal("the fixture world has no groups to resolve against")
	}

	distinct := map[gvas.GUID]bool{}
	var missing, without int
	err = resolver.Players(func(player *Player) error {
		if player.Guild == nil {
			without++
			return nil
		}
		distinct[player.Guild.ID] = true
		if !groups[player.Guild.ID] {
			missing++
			t.Errorf("player %s has guild %s, which matches no group", player.PlayerUID, player.Guild.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("distinctGuilds=%d groupMapKeys=%d unresolved=%d withoutAGuild=%d",
		len(distinct), len(groups), missing, without)
	if len(distinct) == 0 {
		t.Fatal("no player carried a guild id")
	}
}

// TestPlayerMatchesTheWholeSetResolve checks the two entry points agree. Resolving
// one player takes a different path through the same scan -- a smaller wanted set
// -- and a difference between them would mean the filtering changes the answer
// rather than just the work.
func TestPlayerMatchesTheWholeSetResolve(t *testing.T) {
	resolver := fixtureResolver(t)

	var all []*Player
	if err := resolver.Players(func(player *Player) error {
		all = append(all, player)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("the fixture save set resolved no players")
	}

	for _, want := range all {
		got, err := resolver.Player(want.PlayerUID)
		if err != nil {
			t.Fatalf("player %s resolved in the set but not alone: %v", want.PlayerUID, err)
		}
		if len(got.Pals) != len(want.Pals) {
			t.Errorf("player %s has %d pals alone and %d in the set",
				want.PlayerUID, len(got.Pals), len(want.Pals))
			continue
		}
		for index, pal := range got.Pals {
			if !reflect.DeepEqual(pal, want.Pals[index]) {
				t.Errorf("player %s pal %d differs between the two paths", want.PlayerUID, index)
				break
			}
		}
		if (got.Character == nil) != (want.Character == nil) {
			t.Errorf("player %s has a character record on only one path", want.PlayerUID)
			continue
		}
		if got.Character != nil && *got.Character != *want.Character {
			t.Errorf("player %s has a different character record on each path", want.PlayerUID)
		}
		if len(got.Inventory.Common) != len(want.Inventory.Common) {
			t.Errorf("player %s has %d common stacks alone and %d in the set",
				want.PlayerUID, len(got.Inventory.Common), len(want.Inventory.Common))
		}
	}
}

// TestWorldAgreesWithItself checks the counts against each other rather than
// against numbers written down here. The fixture save set belongs to whoever runs
// the tests and can change; the relationships between its counts cannot.
func TestWorldAgreesWithItself(t *testing.T) {
	resolver := fixtureResolver(t)
	world, err := resolver.World()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("counts=%+v warnings=%d", world.Counts, len(world.Warnings))

	counts := world.Counts
	if counts.Characters == 0 || counts.ItemContainers == 0 || counts.CharacterContainers == 0 {
		t.Fatalf("a real world save counted nothing: %+v", counts)
	}
	if counts.Players+counts.Pals != counts.Characters {
		t.Errorf("%d players plus %d pals is not %d characters", counts.Players, counts.Pals, counts.Characters)
	}
	// Every player save should have a record, and the reverse: an imbalance is
	// reported as a warning rather than silently.
	if counts.Players != counts.PlayerSaves && len(world.Warnings) == 0 {
		t.Errorf("%d player records against %d saves, with no warning", counts.Players, counts.PlayerSaves)
	}
	if world.Name == "" {
		t.Error("no world name resolved from the metadata save")
	}
	// The reading of GameDateTimeTicks as elapsed time, checked against a figure
	// the game stores separately. They are the same quantity by two routes.
	if world.GameTime == nil || world.InGameDay == nil {
		t.Fatal("the world has no game time or in-game day")
	}
	if difference := world.GameTime.Days - int64(*world.InGameDay); difference < -1 || difference > 1 {
		t.Errorf("game time is %d days but the in-game day is %d", world.GameTime.Days, *world.InGameDay)
	}
}

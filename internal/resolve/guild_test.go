// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// guildWorld builds a synthetic world with two guilds and one organization.
//
// Every name and id here is invented. Guild names and player names are the
// players' own, so a real one does not belong in this repository even as a test
// fixture.
//
// The first guild is the healthy case: two members, two bases, workers in both.
// The second exists for the failures a real save does not contain -- a base camp
// the guild names that is not there, a camp that names a different guild, and a
// camp naming no worker container.
func guildWorld() worldFixture {
	firstGuild, secondGuild, faction := id(90), id(91), id(92)
	admin, mate, loner := id(1), id(2), id(3)
	workerBox, secondBox := id(70), id(71)

	healthy := guildFixture{
		group: firstGuild,
		name:  "Kestrel Company",
		admin: admin,
		level: 14,
		members: []guildMemberFixture{
			{uid: admin, name: "Ada", lastOnline: 12_000_000_000_000, role: 1},
			{uid: mate, name: "Grace", lastOnline: 6_000_000_000_000, role: 3},
		},
		players: []gvas.GUID{admin, mate},
		handles: []gvas.GUID{id(10), id(11), id(40), id(41)},
		bases: []baseFixture{
			{
				id:        id(60),
				name:      "拠点1",
				location:  gvas.Vector{X: -122500.5, Y: 48250.25, Z: 2600.75},
				areaRange: 3500,
				workers:   workerBox,
				point:     id(80),
			},
			{
				id:        id(61),
				name:      "拠点2",
				location:  gvas.Vector{X: 1, Y: 2, Z: 3},
				areaRange: 3500,
				workers:   secondBox,
				point:     id(81),
			},
		},
	}
	broken := guildFixture{
		group:   secondGuild,
		name:    "Unnamed Guild",
		admin:   loner,
		level:   3,
		members: []guildMemberFixture{{uid: loner, name: "Kay", role: 1}},
		players: []gvas.GUID{loner},
		bases: []baseFixture{
			{id: id(62), name: "gone", absent: true, point: id(82)},
			{id: id(63), name: "misfiled", otherGroup: firstGuild, workers: id(72), point: id(83)},
			{id: id(64), name: "no workers", point: id(84)},
		},
	}
	organization := guildFixture{
		group:        faction,
		organization: true,
		handles:      []gvas.GUID{id(10)},
	}

	return worldFixture{
		revision:  100619,
		gameTicks: 1,
		realTicks: 12_000_000_000_000,
		guilds:    []guildFixture{healthy, broken, organization},
		players: []playerFixture{{
			uid:      admin,
			instance: playerCharacter(admin),
			stem:     "0000005A00000000000000000000000",
			nickname: "Ada",
			level:    57,
			isPlayer: true,
			group:    firstGuild,
			party:    id(50),
			storage:  id(51),
		}},
		// The workers: two in the first camp's container, one in the second's, and
		// one in a container no camp names, which must not appear anywhere.
		pals: []palFixture{
			{instance: id(10), species: "Lamball", owner: workerBox, slot: 1, level: 5, talents: []byte{7, 8, 9}},
			{instance: id(11), species: "Cattiva", owner: workerBox, slot: 0},
			{instance: id(40), species: "Chikipi", owner: secondBox, slot: 3, nickname: "Sous-chef"},
			{instance: id(41), species: "Depresso", owner: id(99), slot: 0},
		},
		characters: []gvas.GUID{workerBox, secondBox, id(99), id(50), id(51)},
	}
}

func openGuildWorld(t *testing.T) *Resolver {
	t.Helper()
	directory := writeSaveSet(t, guildWorld(), nil)
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

// TestGuildResolvesItsMembersAndBases is the healthy path: the whole document, in
// one place, so a field that stops being filled in shows up here.
func TestGuildResolvesItsMembersAndBases(t *testing.T) {
	guild, err := openGuildWorld(t).Guild(id(90))
	if err != nil {
		t.Fatal(err)
	}
	if guild.GroupID != id(90) || guild.Name != "Kestrel Company" || guild.BaseCampLevel != 14 {
		t.Errorf("guild = %+v", guild)
	}
	if guild.Admin == nil || *guild.Admin != id(1) {
		t.Errorf("Admin = %v", guild.Admin)
	}
	if len(guild.Warnings) != 0 {
		t.Errorf("warnings = %v", guild.Warnings)
	}

	// Members come in the guild's own order, with the role and the elapsed clock
	// the record carries.
	if len(guild.Members) != 2 {
		t.Fatalf("members = %d, want 2", len(guild.Members))
	}
	if guild.Members[0].Name != "Ada" || guild.Members[0].Role != 1 {
		t.Errorf("members[0] = %+v", guild.Members[0])
	}
	if guild.Members[1].Name != "Grace" || guild.Members[1].Role != 3 {
		t.Errorf("members[1] = %+v", guild.Members[1])
	}
	if online := guild.Members[0].LastOnline; online == nil || online.Days != 13 {
		t.Errorf("members[0].LastOnline = %+v, want 13 days", online)
	}

	// Counts come from the group's own handle list rather than from the join, so
	// they are the independent check on it.
	want := GuildCounts{Characters: 6, Players: 2, Pals: 4, Bases: 2, Workers: 3}
	if guild.Counts != want {
		t.Errorf("counts = %+v, want %+v", guild.Counts, want)
	}

	if len(guild.Bases) != 2 {
		t.Fatalf("bases = %d, want 2", len(guild.Bases))
	}
	base := guild.Bases[0]
	if base.ID != id(60) || base.Name != "拠点1" || base.AreaRange != 3500 {
		t.Errorf("bases[0] = %+v", base)
	}
	if base.Location == nil || base.Location.X != -122500.5 {
		t.Errorf("bases[0].Location = %+v", base.Location)
	}
	if base.OwnerMapObjectID == nil || *base.OwnerMapObjectID != id(80) {
		t.Errorf("bases[0].OwnerMapObjectID = %v", base.OwnerMapObjectID)
	}
	// Workers arrive in slot order, not in the order the character map lists them,
	// and they are described from their own records.
	if len(base.Workers) != 2 {
		t.Fatalf("bases[0].Workers = %d, want 2", len(base.Workers))
	}
	if base.Workers[0].Species != "Cattiva" || base.Workers[0].Slot != 0 {
		t.Errorf("workers[0] = %+v", base.Workers[0])
	}
	if base.Workers[1].Species != "Lamball" || base.Workers[1].Level != 5 {
		t.Errorf("workers[1] = %+v", base.Workers[1])
	}
	if base.Workers[1].Location != PalAtBase {
		t.Errorf("a base camp worker has location %q, want %q", base.Workers[1].Location, PalAtBase)
	}
	// The pal in a container no camp names must not have been picked up by
	// either base.
	for _, base := range guild.Bases {
		for _, worker := range base.Workers {
			if worker.InstanceID == id(41) {
				t.Error("a pal in an unreferenced container was reported as a worker")
			}
		}
	}
	if guild.Bases[1].Workers[0].Nickname != "Sous-chef" {
		t.Errorf("bases[1].Workers[0] = %+v", guild.Bases[1].Workers[0])
	}
}

// TestGuildReportsEveryBrokenJoin covers the cases the fixture world does not
// contain. Each is a warning rather than an error: a guild with a missing base is
// still worth reporting, and saying nothing would make it look intact.
func TestGuildReportsEveryBrokenJoin(t *testing.T) {
	guild, err := openGuildWorld(t).Guild(id(91))
	if err != nil {
		t.Fatal(err)
	}
	if guild.Name != "Unnamed Guild" {
		t.Errorf("Name = %q", guild.Name)
	}
	for _, want := range []string{
		"is not in the world save",     // the absent camp
		"says it belongs to the group", // the camp naming another guild
		"names no worker container",    // the camp with no container
	} {
		found := false
		for _, warning := range guild.Warnings {
			if strings.Contains(warning, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("warnings %v do not mention %q", guild.Warnings, want)
		}
	}
	// All three bases are still reported, in the guild's own order: a base that
	// could not be described is not a base that does not exist.
	if len(guild.Bases) != 3 {
		t.Fatalf("bases = %d, want 3", len(guild.Bases))
	}
	if guild.Bases[0].Name != "" || guild.Bases[0].Location != nil {
		t.Errorf("the absent camp was described: %+v", guild.Bases[0])
	}
	if guild.Counts.Workers != 0 {
		t.Errorf("workers = %d, want 0", guild.Counts.Workers)
	}
}

// TestGuildsVisitsEveryGuildAndNoOrganization is the array form. An organization
// shares the first half of the record, so leaving it out is a decision this test
// pins rather than an accident of the loop.
func TestGuildsVisitsEveryGuildAndNoOrganization(t *testing.T) {
	var seen []gvas.GUID
	if err := openGuildWorld(t).Guilds(func(guild *Guild) error {
		seen = append(seen, guild.GroupID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != id(90) || seen[1] != id(91) {
		t.Errorf("guilds = %v, want the two guilds in save order", seen)
	}
}

// TestGuildRejectsAnOrganizationAndAnUnknownID is the difference between "not a
// guild" and "no such guild". Both are errors rather than empty documents,
// because the id came from somewhere.
func TestGuildRejectsAnOrganizationAndAnUnknownID(t *testing.T) {
	resolver := openGuildWorld(t)
	if _, err := resolver.Guild(id(92)); err == nil {
		t.Error("resolving an organization succeeded")
	} else if !strings.Contains(err.Error(), "not a guild") {
		t.Errorf("error = %v", err)
	}
	missing := id(9999)
	if _, err := resolver.Guild(missing); err == nil {
		t.Error("resolving an unknown group succeeded")
	} else if !strings.Contains(err.Error(), missing.String()) {
		t.Errorf("error %v does not name the id", err)
	}
	if _, err := resolver.Guild(gvas.GUID{}); err == nil {
		t.Error("resolving the zero id succeeded")
	}
}

// TestPlayerGuildCarriesTheGuildName is the phase-4 addition to a player
// document: version 1 could only report the group id.
func TestPlayerGuildCarriesTheGuildName(t *testing.T) {
	player, err := openGuildWorld(t).Player(id(1))
	if err != nil {
		t.Fatal(err)
	}
	if player.Guild == nil {
		t.Fatal("the player has no guild")
	}
	if player.Guild.ID != id(90) || player.Guild.Name != "Kestrel Company" || player.Guild.MemberCount != 2 {
		t.Errorf("guild = %+v", player.Guild)
	}
}

// TestGuildNeedsNoPlayerSaves pins the claim that makes guilds useful on a server
// whose player files you do not have: the members' names come out of the world
// save's group record, not out of anybody's player.sav.
func TestGuildNeedsNoPlayerSaves(t *testing.T) {
	world := guildWorld()
	for i := range world.players {
		// An empty stem writes the world-save record without the save file.
		world.players[i].stem = ""
	}
	set, err := Discover(writeSaveSet(t, world, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Players) != 0 {
		t.Fatalf("the save set has %d player saves, want none", len(set.Players))
	}
	resolver, err := Open(set, Options{})
	if err != nil {
		t.Fatal(err)
	}
	guild, err := resolver.Guild(id(90))
	if err != nil {
		t.Fatal(err)
	}
	if len(guild.Members) != 2 || guild.Members[0].Name != "Ada" {
		t.Errorf("members = %+v", guild.Members)
	}
	if guild.Counts.Workers != 3 || len(guild.Warnings) != 0 {
		t.Errorf("counts = %+v warnings = %v", guild.Counts, guild.Warnings)
	}
}

// TestGuildStopsOnAVisitorError keeps the callback contract: a consumer that
// fails part way through stops the resolve rather than having its error swallowed.
func TestGuildStopsOnAVisitorError(t *testing.T) {
	sentinel := errSentinel("stop")
	count := 0
	err := openGuildWorld(t).Guilds(func(*Guild) error {
		count++
		return sentinel
	})
	if err != sentinel {
		t.Errorf("error = %v, want the visitor's", err)
	}
	if count != 1 {
		t.Errorf("the visitor was called %d times after failing", count)
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

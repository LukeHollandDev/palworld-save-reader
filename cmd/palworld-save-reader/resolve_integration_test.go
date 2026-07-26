// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/resolve"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefixtures"
)

// This test runs the real binary path over a real save set, so its output is
// private: player names, account identifiers, coordinates. Nothing here logs any of
// it -- the assertions are on the envelope and on counts.

// TestRunResolvesAgainstTheFixtureSaveSet is the wiring check. The joins themselves
// are tested in internal/resolve; this is here because a flag parsed into the wrong
// variable, or an envelope that is not valid JSON, would pass every test in that
// package.
func TestRunResolvesAgainstTheFixtureSaveSet(t *testing.T) {
	root := savefixtures.Root(t)

	var stdout, stderr bytes.Buffer
	if status := run([]string{"--resolve", "world", "--saves", root}, &stdout, &stderr); status != 0 {
		t.Fatalf("--resolve world status = %d, stderr = %q", status, stderr.String())
	}
	var world struct {
		ResolveVersion int `json:"resolveVersion"`
		Kind           string
		World          struct {
			Counts struct {
				Players     int
				Pals        int
				PlayerSaves int
			}
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &world); err != nil {
		t.Fatalf("--resolve world output is not JSON: %v", err)
	}
	if world.ResolveVersion != resolve.Version || world.Kind != "world" {
		t.Errorf("envelope = version %d kind %q", world.ResolveVersion, world.Kind)
	}
	if world.World.Counts.Players == 0 || world.World.Counts.Pals == 0 {
		t.Fatalf("counts = %+v", world.World.Counts)
	}
	t.Logf("world counts: players=%d pals=%d playerSaves=%d",
		world.World.Counts.Players, world.World.Counts.Pals, world.World.Counts.PlayerSaves)

	stdout.Reset()
	stderr.Reset()
	if status := run([]string{"--resolve", "players", "--saves", root}, &stdout, &stderr); status != 0 {
		t.Fatalf("--resolve players status = %d, stderr = %q", status, stderr.String())
	}
	var players struct {
		Kind    string
		Players []resolve.Player
	}
	if err := json.Unmarshal(stdout.Bytes(), &players); err != nil {
		t.Fatalf("--resolve players output is not JSON: %v", err)
	}
	if players.Kind != "players" || len(players.Players) == 0 {
		t.Fatalf("kind = %q with %d players", players.Kind, len(players.Players))
	}
	t.Logf("resolved %d players from %d bytes of JSON", len(players.Players), stdout.Len())

	// The same player, fetched alone through the id the array reported. This is
	// also the check that --id accepts what the tool itself prints: the documents
	// carry the dashed form, and the file names carry the bare one.
	first := players.Players[0]
	for _, id := range []string{
		first.PlayerUID.String(),
		strings.ToUpper(strings.ReplaceAll(first.PlayerUID.String(), "-", "")),
	} {
		stdout.Reset()
		stderr.Reset()
		if status := run([]string{"--resolve", "player", "--id", id, "--saves", root}, &stdout, &stderr); status != 0 {
			t.Fatalf("--resolve player --id %s status = %d, stderr = %q", id, status, stderr.String())
		}
		var single struct {
			Kind   string
			Player resolve.Player
		}
		if err := json.Unmarshal(stdout.Bytes(), &single); err != nil {
			t.Fatalf("--resolve player output is not JSON: %v", err)
		}
		if single.Kind != "player" || single.Player.PlayerUID != first.PlayerUID {
			t.Errorf("resolving %s gave kind %q and a different player", id, single.Kind)
		}
		if len(single.Player.Pals) != len(first.Pals) {
			t.Errorf("resolving %s gave %d pals, but the array had %d",
				id, len(single.Player.Pals), len(first.Pals))
		}
	}

	// The guild kinds, and the id a player document reports as its guild: the two
	// modes have to agree on what an id means, or --resolve guild is unreachable
	// from --resolve player.
	stdout.Reset()
	stderr.Reset()
	if status := run([]string{"--resolve", "guilds", "--saves", root}, &stdout, &stderr); status != 0 {
		t.Fatalf("--resolve guilds status = %d, stderr = %q", status, stderr.String())
	}
	var guilds struct {
		Kind   string
		Guilds []resolve.Guild
	}
	if err := json.Unmarshal(stdout.Bytes(), &guilds); err != nil {
		t.Fatalf("--resolve guilds output is not JSON: %v", err)
	}
	if guilds.Kind != "guilds" || len(guilds.Guilds) == 0 {
		t.Fatalf("kind = %q with %d guilds", guilds.Kind, len(guilds.Guilds))
	}
	t.Logf("resolved %d guilds from %d bytes of JSON", len(guilds.Guilds), stdout.Len())

	if first.Guild == nil {
		t.Fatal("the first player has no guild, so the guild id cannot be followed")
	}
	stdout.Reset()
	stderr.Reset()
	if status := run([]string{
		"--resolve", "guild", "--id", first.Guild.ID.String(), "--saves", root,
	}, &stdout, &stderr); status != 0 {
		t.Fatalf("--resolve guild status = %d, stderr = %q", status, stderr.String())
	}
	var single struct {
		Kind  string
		Guild resolve.Guild
	}
	if err := json.Unmarshal(stdout.Bytes(), &single); err != nil {
		t.Fatalf("--resolve guild output is not JSON: %v", err)
	}
	if single.Kind != "guild" || single.Guild.GroupID != first.Guild.ID {
		t.Errorf("resolving %s gave kind %q and group %s",
			first.Guild.ID, single.Kind, single.Guild.GroupID)
	}
	if single.Guild.Name != first.Guild.Name {
		t.Errorf("the guild is %q in the player document and %q in its own",
			first.Guild.Name, single.Guild.Name)
	}
	if len(single.Guild.Members) != first.Guild.MemberCount {
		t.Errorf("the player reports %d members and the guild lists %d",
			first.Guild.MemberCount, len(single.Guild.Members))
	}

	// An id nobody has fails at runtime rather than emitting an empty document.
	stdout.Reset()
	stderr.Reset()
	status := run([]string{
		"--resolve", "player",
		"--id", "ffffffff-ffff-ffff-ffff-ffffffffffff",
		"--saves", root,
	}, &stdout, &stderr)
	if status != 1 {
		t.Errorf("an unknown id gave status %d, want 1", status)
	}
	if stdout.Len() != 0 {
		t.Errorf("a failed resolve wrote %d bytes to stdout", stdout.Len())
	}
}

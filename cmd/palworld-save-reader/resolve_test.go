// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/resolve"
)

// fakeDocuments turns a list of documents into the callback shape
// writeResolvedArray consumes, so the envelope and its framing can be tested
// without a save file.
func fakeDocuments[T any](documents ...T) func(func(any) error) error {
	return func(emit func(any) error) error {
		for _, document := range documents {
			if err := emit(document); err != nil {
				return err
			}
		}
		return nil
	}
}

func testPlayer(t *testing.T, uid string) *resolve.Player {
	t.Helper()
	parsed, err := gvas.ParseGUID(uid)
	if err != nil {
		t.Fatal(err)
	}
	return &resolve.Player{
		PlayerUID: parsed,
		Inventory: resolve.Inventory{Common: []resolve.ItemStack{{Slot: 0, ItemID: "Money", Count: 7}}},
		Pals:      []resolve.Pal{{Species: "Lamball", Level: 1, Location: resolve.PalInStorage}},
	}
}

// TestResolvedPlayersStreamIntoOneEnvelope covers the hand-written framing. It is
// the one place in this program that assembles JSON without encoding/json, which it
// does so the array can be streamed, so the result has to be checked as JSON rather
// than as text.
func TestResolvedPlayersStreamIntoOneEnvelope(t *testing.T) {
	var out bytes.Buffer
	first, second := testPlayer(t, "00000001-0000-0000-0000-000000000000"), testPlayer(t, "00000002-0000-0000-0000-000000000000")
	if err := writeResolvedArray(&out, resolvePlayers, fakeDocuments(first, second)); err != nil {
		t.Fatal(err)
	}

	var envelope struct {
		ResolveVersion int              `json:"resolveVersion"`
		Kind           string           `json:"kind"`
		Players        []resolve.Player `json:"players"`
		Player         *resolve.Player  `json:"player"`
		World          *json.RawMessage `json:"world"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if envelope.ResolveVersion != resolve.Version {
		t.Errorf("resolveVersion = %d, want %d", envelope.ResolveVersion, resolve.Version)
	}
	if envelope.Kind != "players" {
		t.Errorf("kind = %q", envelope.Kind)
	}
	if envelope.Player != nil || envelope.World != nil {
		t.Error("the players envelope also carried a single-document field")
	}
	if len(envelope.Players) != 2 {
		t.Fatalf("players = %d, want 2", len(envelope.Players))
	}
	// Order is the resolver's, not the encoder's.
	if envelope.Players[0].PlayerUID != first.PlayerUID || envelope.Players[1].PlayerUID != second.PlayerUID {
		t.Errorf("players came out as %s then %s", envelope.Players[0].PlayerUID, envelope.Players[1].PlayerUID)
	}
	if len(envelope.Players[0].Pals) != 1 || envelope.Players[0].Pals[0].Species != "Lamball" {
		t.Errorf("the streamed element lost its pals: %+v", envelope.Players[0].Pals)
	}
	// The streamed elements must be indented like anything encoding/json produced
	// in place, since the whole document is read by humans as often as by programs.
	if !strings.Contains(out.String(), "\n    {\n      \"playerUId\"") {
		t.Errorf("streamed elements are not indented inside the array:\n%s", out.String())
	}
}

// TestResolvedPlayersEmitsAnEmptyArray covers a world nobody has joined: a true
// answer, and one a consumer can iterate without a special case.
func TestResolvedPlayersEmitsAnEmptyArray(t *testing.T) {
	var out bytes.Buffer
	if err := writeResolvedArray(&out, resolvePlayers, fakeDocuments[*resolve.Player]()); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Kind    string           `json:"kind"`
		Players []resolve.Player `json:"players"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if envelope.Kind != "players" || envelope.Players == nil || len(envelope.Players) != 0 {
		t.Errorf("output = %s", out.String())
	}
}

// TestResolvedPlayersStopsOnAResolveError is why nothing is written until the first
// document exists: a failure must not leave half an envelope behind for a consumer
// to misread.
func TestResolvedPlayersStopsOnAResolveError(t *testing.T) {
	sentinel := errors.New("world save is unreadable")
	var out bytes.Buffer
	err := writeResolvedArray(&out, resolvePlayers, func(func(any) error) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want the resolve error", err)
	}
	if out.Len() != 0 {
		t.Errorf("a failed resolve wrote %q", out.String())
	}
}

// TestRunValidatesResolveFlags keeps the mode's argument rules in one place. Each of
// these is a usage error rather than a runtime one, so none of them touches a file.
func TestRunValidatesResolveFlags(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		arguments []string
		want      string
	}{
		{
			name:      "unknown kind",
			arguments: []string{"--resolve", "base", "--saves", "dir"},
			want:      "guild|guilds|player|players|roster|world",
		},
		{
			name:      "no directory",
			arguments: []string{"--resolve", "world"},
			want:      "--saves",
		},
		{
			name:      "a file as well",
			arguments: []string{"--resolve", "world", "--saves", "dir", "Level.sav"},
			want:      "not a file",
		},
		{
			name:      "player without an id",
			arguments: []string{"--resolve", "player", "--saves", "dir"},
			want:      "requires --id",
		},
		{
			name:      "an id where none is meaningful",
			arguments: []string{"--resolve", "players", "--id", "abc", "--saves", "dir"},
			want:      "does not accept --id",
		},
		{
			name:      "guild without an id",
			arguments: []string{"--resolve", "guild", "--saves", "dir"},
			want:      "requires --id",
		},
		{
			name:      "guilds with an id",
			arguments: []string{"--resolve", "guilds", "--id", "abc", "--saves", "dir"},
			want:      "does not accept --id",
		},
		{
			name:      "an id that is not a GUID",
			arguments: []string{"--resolve", "player", "--id", "not-a-guid", "--saves", "dir"},
			want:      "is not a GUID",
		},
		{
			name:      "projection options",
			arguments: []string{"--resolve", "world", "--saves", "dir", "--explain"},
			want:      "projection options",
		},
		{
			name:      "with another mode",
			arguments: []string{"--resolve", "world", "--saves", "dir", "--full", "Level.sav"},
			want:      "exactly one",
		},
		{
			name:      "--saves without --resolve",
			arguments: []string{"--full", "--saves", "dir", "Level.sav"},
			want:      "only valid with --resolve",
		},
		{
			name:      "--id without --resolve",
			arguments: []string{"--list-presets", "--id", "abc"},
			want:      "only valid with --resolve",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if status := run(testCase.arguments, &stdout, &stderr); status != 2 {
				t.Fatalf("status = %d, want 2; stderr = %q", status, stderr.String())
			}
			if !strings.Contains(stderr.String(), testCase.want) {
				t.Errorf("stderr %q does not mention %q", stderr.String(), testCase.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("a usage error wrote %q to stdout", stdout.String())
			}
		})
	}
}

// TestRunResolveReportsAMissingDirectory is the runtime half: an argument that is
// well formed but names nothing exits 1, not 2.
func TestRunResolveReportsAMissingDirectory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := run([]string{"--resolve", "world", "--saves", t.TempDir() + "/absent"}, &stdout, &stderr)
	if status != 1 {
		t.Fatalf("status = %d, want 1; stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "save directory") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

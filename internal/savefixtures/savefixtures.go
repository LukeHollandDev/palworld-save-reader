// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

// Package savefixtures locates the private save files the integration tests run
// against.
//
// Real saves cannot live in this repository: they contain player names, account
// identifiers, coordinates, and progression. Tests therefore read them from a
// directory named by PALWORLD_SAVE_FIXTURES and skip when it is unset, which
// keeps the default `go test ./...` hermetic.
//
// The savefile, palworld, and projection tests all need the same directory
// layout, so discovery and its validation live here once rather than in three
// slightly different copies. Nothing outside a test imports this package.
package savefixtures

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Root returns the fixture directory, skipping the test when
// PALWORLD_SAVE_FIXTURES is unset. A directory that is set but incomplete fails
// instead of skipping: a half-populated fixture set would quietly reduce
// coverage while still reporting success.
func Root(tb testing.TB) string {
	tb.Helper()
	root := os.Getenv("PALWORLD_SAVE_FIXTURES")
	if root == "" {
		tb.Skip("set PALWORLD_SAVE_FIXTURES to a directory containing external saves")
	}
	for _, name := range []string{"Level.sav", "LevelMeta.sav"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			tb.Fatalf("fixture directory does not contain %s", name)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "Players")); err != nil || !info.IsDir() {
		tb.Fatal("fixture directory does not contain Players/")
	}
	if len(playerSaves(tb, root, false)) == 0 || len(playerSaves(tb, root, true)) == 0 {
		tb.Fatal("fixture directory must contain both normal and DPS player saves")
	}
	return root
}

// PlayerSaves returns every save under Players/, DPS files included.
func PlayerSaves(tb testing.TB, root string) []string {
	tb.Helper()
	return append(playerSaves(tb, root, false), playerSaves(tb, root, true)...)
}

// NormalPlayerSaves returns the saves under Players/ that hold a player's own
// state. A *_dps.sav sits beside them but carries a different root structure, so
// anything written against the player schema must exclude it.
func NormalPlayerSaves(tb testing.TB, root string) []string {
	tb.Helper()
	return playerSaves(tb, root, false)
}

// DPSPlayerSaves returns the *_dps.sav damage-log saves under Players/.
func DPSPlayerSaves(tb testing.TB, root string) []string {
	tb.Helper()
	return playerSaves(tb, root, true)
}

// AllSaves returns every fixture save: the two world files and every player
// file, in a stable order.
func AllSaves(tb testing.TB, root string) []string {
	tb.Helper()
	saves := []string{
		filepath.Join(root, "Level.sav"),
		filepath.Join(root, "LevelMeta.sav"),
	}
	return append(saves, PlayerSaves(tb, root)...)
}

func playerSaves(tb testing.TB, root string, dps bool) []string {
	tb.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "Players", "*.sav"))
	if err != nil {
		tb.Fatal(err)
	}
	paths := make([]string, 0, len(matches))
	for _, path := range matches {
		if strings.HasSuffix(path, "_dps.sav") == dps {
			paths = append(paths, path)
		}
	}
	return paths
}

// GoldenHashes returns count SHA-256 digests of the expected decompressed
// payloads, or nil when none are configured. The digests are supplied out of
// band, through PALWORLD_SAVE_GOLDEN_SHA256 or a file named by
// PALWORLD_SAVE_GOLDEN_SHA256_FILE, so a decoder regression can be caught
// against private saves without committing anything derived from them.
func GoldenHashes(tb testing.TB, count int) []string {
	tb.Helper()
	inline := os.Getenv("PALWORLD_SAVE_GOLDEN_SHA256")
	path := os.Getenv("PALWORLD_SAVE_GOLDEN_SHA256_FILE")
	if inline == "" && path == "" {
		return nil
	}
	data := []byte(inline)
	if inline == "" {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			tb.Fatalf("read private golden hash file: %v", err)
		}
	}
	hashes := strings.Fields(string(data))
	if len(hashes) != count {
		tb.Fatalf("private golden hash list has %d entries, want %d", len(hashes), count)
	}
	for _, hash := range hashes {
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != sha256.Size {
			tb.Fatal("private golden hash list contains an invalid SHA-256 value")
		}
	}
	return hashes
}

// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverFindsTheSavesPalworldWrites(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{
		levelName,
		levelMetaName,
		filepath.Join(playersDir, "B.sav"),
		filepath.Join(playersDir, "A.sav"),
		// A damage-log companion, which is a different save type and not a player.
		filepath.Join(playersDir, "A_dps.sav"),
		// Something that is not a save at all.
		filepath.Join(playersDir, "notes.txt"),
	} {
		writeFile(t, directory, name, []byte("not a real save"))
	}
	if err := os.MkdirAll(filepath.Join(directory, playersDir, "Backups"), 0o700); err != nil {
		t.Fatal(err)
	}

	set, err := Discover(directory)
	if err != nil {
		t.Fatal(err)
	}
	if set.Level != filepath.Join(directory, levelName) {
		t.Errorf("Level = %q", set.Level)
	}
	if set.LevelMeta != filepath.Join(directory, levelMetaName) {
		t.Errorf("LevelMeta = %q", set.LevelMeta)
	}
	// Sorted, so resolving the whole set twice produces the same order.
	want := []string{
		filepath.Join(directory, playersDir, "A.sav"),
		filepath.Join(directory, playersDir, "B.sav"),
	}
	if len(set.Players) != len(want) {
		t.Fatalf("Players = %q, want %q", set.Players, want)
	}
	for index, path := range set.Players {
		if path != want[index] {
			t.Errorf("Players[%d] = %q, want %q", index, path, want[index])
		}
	}
}

func TestDiscoverRejectsWhatIsNotASaveDirectory(t *testing.T) {
	empty := t.TempDir()
	file := filepath.Join(t.TempDir(), "Level.sav")
	writeFile(t, filepath.Dir(file), "Level.sav", []byte("x"))

	for _, testCase := range []struct {
		name      string
		directory string
		want      string
	}{
		{name: "nothing given", directory: "", want: "no save directory"},
		{name: "missing", directory: filepath.Join(empty, "absent"), want: "save directory"},
		{name: "a file", directory: file, want: "not a directory"},
		{name: "no world save", directory: empty, want: levelName},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Discover(testCase.directory)
			if err == nil {
				t.Fatalf("Discover(%q) returned no error", testCase.directory)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("error %q does not mention %q", err, testCase.want)
			}
		})
	}
}

// TestDiscoverAllowsAWorldWithNoPlayers covers a fresh save or an unjoined
// server: the optional parts of a save set are genuinely optional.
func TestDiscoverAllowsAWorldWithNoPlayers(t *testing.T) {
	directory := t.TempDir()
	writeFile(t, directory, levelName, []byte("x"))

	set, err := Discover(directory)
	if err != nil {
		t.Fatalf("a directory with only %s failed: %v", levelName, err)
	}
	if set.LevelMeta != "" {
		t.Errorf("LevelMeta = %q, want empty", set.LevelMeta)
	}
	if len(set.Players) != 0 {
		t.Errorf("Players = %q, want none", set.Players)
	}
}

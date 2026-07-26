// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The file names Palworld writes into a save directory.
const (
	levelName     = "Level.sav"
	levelMetaName = "LevelMeta.sav"
	playersDir    = "Players"
	// dpsSuffix marks the damage-log companion of a player save. It has a
	// different root structure and is not a player.
	dpsSuffix = "_dps.sav"
)

// Set is the files of one Palworld save directory.
type Set struct {
	// Directory is the path Discover was given.
	Directory string
	// Level is the world save. It is the only required file: everything a join
	// resolves to is in it.
	Level string
	// LevelMeta is the world's metadata save, empty when the directory has none.
	// It holds the world name and in-game day and nothing a player needs, so a
	// missing one is a warning rather than a failure.
	LevelMeta string
	// Players are the player saves, in sorted order so a resolve of the whole
	// set is reproducible. The *_dps.sav companions are excluded.
	Players []string
}

// Discover finds the saves in a Palworld save directory. A caller points at the
// directory rather than naming files because the join spans all of them, and
// which files those are is a fact about Palworld rather than a choice.
func Discover(directory string) (Set, error) {
	if directory == "" {
		return Set{}, fmt.Errorf("resolve: no save directory given")
	}
	info, err := os.Stat(directory)
	if err != nil {
		return Set{}, fmt.Errorf("resolve: save directory %s: %w", directory, err)
	}
	if !info.IsDir() {
		return Set{}, fmt.Errorf("resolve: %s is not a directory", directory)
	}

	set := Set{Directory: directory, Level: filepath.Join(directory, levelName)}
	if _, err := os.Stat(set.Level); err != nil {
		return Set{}, fmt.Errorf(
			"resolve: %s contains no %s, so it is not a Palworld save directory: %w",
			directory, levelName, err,
		)
	}
	meta := filepath.Join(directory, levelMetaName)
	if _, err := os.Stat(meta); err == nil {
		set.LevelMeta = meta
	}

	// A world with no players is legitimate -- a fresh save, or a dedicated
	// server nobody has joined -- so an absent Players directory is not an error.
	entries, err := os.ReadDir(filepath.Join(directory, playersDir))
	if err != nil && !os.IsNotExist(err) {
		return Set{}, fmt.Errorf("resolve: read %s/%s: %w", directory, playersDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sav") || strings.HasSuffix(name, dpsSuffix) {
			continue
		}
		set.Players = append(set.Players, filepath.Join(directory, playersDir, name))
	}
	sort.Strings(set.Players)
	return set, nil
}

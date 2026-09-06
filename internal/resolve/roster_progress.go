// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// readRosterProgress derives compact, lifetime-style counters from the sparse
// boolean maps in a player save. Unreal omits a property while it still has its
// default value, so an absent map means zero. A present map with an unexpected
// shape is an error rather than a zero: that is a schema change, not a player
// with no progress.
func readRosterProgress(properties gvas.Properties, roster *Roster) {
	metrics := []struct {
		path        string
		destination **int
	}{
		{playerFastTravelPath, &roster.FastTravelUnlocked},
		{playerAreaPath, &roster.AreasDiscovered},
		{playerBossPath, &roster.BossDefeats},
		{playerTowerPath, &roster.TowerDefeats},
	}
	for _, metric := range metrics {
		count, err := countTrueFlags(properties, metric.path)
		if err != nil {
			roster.Warnings = append(roster.Warnings, err.Error())
			continue
		}
		*metric.destination = &count
	}
}

func countTrueFlags(properties gvas.Properties, path string) (int, error) {
	keys, err := trueFlagKeys(properties, path)
	return len(keys), err
}

func trueFlagKeys(properties gvas.Properties, path string) ([]string, error) {
	raw := field(properties, path)
	if raw == nil {
		return []string{}, nil
	}
	flags, ok := raw.(*gvas.MapValue)
	if !ok {
		return nil, fmt.Errorf("%s is not a map", path)
	}
	seen := make(map[string]struct{}, flags.Count)
	keys := make([]string, 0, flags.Count)
	iterator := flags.Iterator()
	for iterator.Next() {
		entry := iterator.Entry()
		key, ok := entry.Key.(string)
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("%s has a non-name or empty key", path)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("%s repeats key %q", path, key)
		}
		seen[key] = struct{}{}
		set, ok := entry.Value.(bool)
		if !ok {
			return nil, fmt.Errorf("%s[%q] is not a boolean", path, key)
		}
		if set {
			keys = append(keys, key)
		}
	}
	if err := iterator.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	sort.Strings(keys)
	return keys, nil
}

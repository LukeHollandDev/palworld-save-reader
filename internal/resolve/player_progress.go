// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import "github.com/LukeHollandDev/palworld-save-reader/internal/gvas"

func readPlayerProgress(properties gvas.Properties, player *Player) {
	domains := []struct {
		path        string
		destination *[]string
	}{
		{playerFastTravelPath, &player.Progress.FastTravel},
		{playerAreaPath, &player.Progress.Areas},
		{playerNotePath, &player.Progress.Notes},
		{playerRelicPath, &player.Progress.Relics},
		{playerItemPickupPath, &player.Progress.ItemPickups},
		{playerBossPath, &player.Progress.NormalBosses},
		{playerTowerPath, &player.Progress.TowerBosses},
	}
	for _, domain := range domains {
		keys, err := trueFlagKeys(properties, domain.path)
		if err != nil {
			player.Warnings = append(player.Warnings, err.Error())
			continue
		}
		*domain.destination = keys
	}
}

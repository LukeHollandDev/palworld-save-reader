// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"fmt"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/palworld"
)

// Roster resolves only the account identity, character name and level, and
// guild membership for every player.sav in the set. Unlike Players, it does not
// read item containers or character containers and never decodes Pal records.
func (r *Resolver) Roster(visit func(*Roster) error) error {
	entries, err := r.readRosterEntries()
	if err != nil {
		return err
	}
	if len(entries.order) == 0 {
		return nil
	}
	if err := entries.readCharacters(r); err != nil {
		return err
	}
	if err := r.rosterGuildNames(entries.guilds()); err != nil {
		return err
	}
	for _, entry := range entries.order {
		if err := visit(entry.document); err != nil {
			return err
		}
	}
	return nil
}

type rosterEntry struct {
	document *Roster
}

func (e *rosterEntry) warn(format string, arguments ...any) {
	e.document.Warnings = append(e.document.Warnings, fmt.Sprintf(format, arguments...))
}

type rosterScan struct {
	order []*rosterEntry
	byUID map[gvas.GUID]*rosterEntry
}

func (r *Resolver) readRosterEntries() (*rosterScan, error) {
	scan := &rosterScan{byUID: make(map[gvas.GUID]*rosterEntry, len(r.set.Players))}
	for _, path := range r.set.Players {
		save, err := palworld.LoadWithOptions(path, r.options.Save)
		if err != nil {
			return nil, fmt.Errorf("resolve: player save %s: %w", path, err)
		}
		uid, ok := guid(save.Properties, playerUIDPath)
		if !ok || uid.IsZero() {
			return nil, fmt.Errorf("resolve: player save %s has no %s", path, playerUIDPath)
		}
		if scan.byUID[uid] != nil {
			return nil, fmt.Errorf("resolve: two player saves in %s claim the id %s", r.set.Directory, uid)
		}
		entry := &rosterEntry{document: &Roster{PlayerUID: uid}}
		scan.byUID[uid] = entry
		scan.order = append(scan.order, entry)
	}
	return scan, nil
}

func (s *rosterScan) readCharacters(r *Resolver) error {
	characters, err := r.worldMap(characterCollection)
	if err != nil {
		return err
	}
	found := make(map[gvas.GUID]bool, len(s.order))
	iterator := characters.Iterator()
	for iterator.Next() {
		key, ok := iterator.Entry().Key.(gvas.Properties)
		if !ok {
			continue
		}
		uid, _ := guid(key, "PlayerUId")
		entry := s.byUID[uid]
		if entry == nil {
			continue
		}
		if found[uid] {
			entry.warn("the world save has more than one character record for this player; the first was used")
			continue
		}
		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			entry.warn("the world save character record is not a property list")
			continue
		}
		data, ok := blob(properties, "RawData")
		if !ok {
			entry.warn("the world save character record has no RawData")
			continue
		}
		character, err := palworld.DecodeCharacterWithOptions(data, r.options.Save.GVAS)
		if err != nil {
			entry.warn("the world save character record did not decode: %v", err)
			continue
		}
		parameters := character.Parameters()
		if parameters == nil {
			entry.warn("the world save character record has no SaveParameter")
			continue
		}
		if flag, ok := value[bool](parameters, recordIsPlayer); !ok || !flag {
			entry.warn("the world save character record is not flagged as a player")
		}
		level := uint8(defaultLevel)
		if value, ok := value[uint8](parameters, recordLevel); ok {
			level = value
		}
		nickname, _ := value[string](parameters, recordNickname)
		entry.document.Character = &Character{Nickname: nickname, Level: level}
		if !character.GroupID.IsZero() {
			entry.document.Guild = &GuildRef{ID: character.GroupID}
		}
		found[uid] = true
	}
	if err := iterator.Err(); err != nil {
		return fmt.Errorf("resolve: character records: %w", err)
	}
	for _, entry := range s.order {
		if !found[entry.document.PlayerUID] {
			entry.warn("the world save has no character record for this player, so no name, level or guild is available")
		}
	}
	return nil
}

func (s *rosterScan) guilds() map[gvas.GUID][]*GuildRef {
	wanted := make(map[gvas.GUID][]*GuildRef)
	for _, entry := range s.order {
		if entry.document.Guild != nil {
			wanted[entry.document.Guild.ID] = append(wanted[entry.document.Guild.ID], entry.document.Guild)
		}
	}
	return wanted
}

func (r *Resolver) rosterGuildNames(wanted map[gvas.GUID][]*GuildRef) error {
	return r.guildNames(wanted)
}

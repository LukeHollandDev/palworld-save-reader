// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

// Package resolve answers questions that span more than one save file.
//
// The layers below it each answer "what is in this one file": savefile unwraps a
// container, gvas decodes Unreal's properties, palworld supplies the game
// knowledge that decoding needs, and projection extracts a declared shape from a
// single save. None of them can answer "what does this player have", because that
// answer is spread across a player.sav, the world save's item containers, its
// character containers, and its character records -- joined by identifiers that
// mean nothing without knowing what they refer to.
//
// This package owns that knowledge, which is a different kind from palworld's.
// palworld knows how to *decode*: which concrete struct hides behind an untagged
// property, and which byte layout a RawData blob holds. resolve knows what the
// decoded values *mean*: that InventoryInfo.CommonContainerId names an entry of
// ItemContainerSaveData, that a pal's presence in a container slot is what makes
// it that player's pal, and that the name on the screen is in the world save
// rather than in the player's own file. Both are game knowledge that a Palworld
// update can invalidate, which is why they are the two packages to look at first
// when it does.
//
// The output shape is this tool's own invention rather than a view of the save,
// so it is versioned; see Version. Projection deliberately stays dumb -- "shape
// in, same shape out, no semantics" -- and everything interpretive lives here.
//
// # Memory
//
// A Level.sav holds 8,723 item containers, 2,259 character records and 111,579
// RawData blobs, so the way this package reads is a correctness property rather
// than a matter of taste:
//
//   - Every collection is iterated, never collected with Entries or Values.
//   - Each collection is read exactly once per call, whatever is being asked
//     for. Resolving nine players does not scan the containers nine times.
//   - The scan order follows the dependencies rather than the file: character
//     containers are read before character records, because the containers are
//     what say which of the 2,259 records are wanted. Each collection is lazy
//     and independent, so reading them out of order costs nothing.
//   - Only wanted records are decoded. A character record is ~3.4KB of dense
//     nested properties; deciding from the map key alone whether to decode one
//     is what keeps resolving one player cheap in a world of 2,259.
package resolve

import (
	"fmt"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/palworld"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefile"
)

// Options controls the reads this package performs.
type Options struct {
	// Save is passed to every save file opened, so a caller can lower the byte
	// limits or work around a schema change with extra type hints exactly as
	// they would for a single file.
	Save savefile.Options
}

// Resolver holds the world save for the length of a resolve. Opening it is the
// expensive part -- a Level.sav is hundreds of megabytes of decompressed archive
// -- so a caller opens once and asks several questions if they want to.
//
// A Resolver is not safe for concurrent use: the lazy collections it hands out
// decode on read.
type Resolver struct {
	set     Set
	options Options
	level   *savefile.Save
	// world is worldSaveData's property list, which is where everything a join
	// needs lives.
	world gvas.Properties
	// meta is LevelMeta.sav's SaveData, nil when the file is absent or unreadable.
	meta gvas.Properties
	// warnings records what could not be read while opening, to be reported in
	// the document rather than raised as an error: a missing LevelMeta costs a
	// world name, not a resolve.
	warnings []string
}

// Open reads the world save of a discovered set. It does not read the player
// saves: which of those are needed depends on the question.
func Open(set Set, options Options) (*Resolver, error) {
	level, err := palworld.LoadWithOptions(set.Level, options.Save)
	if err != nil {
		return nil, fmt.Errorf("resolve: world save: %w", err)
	}
	world, ok := value[gvas.Properties](level.Properties, "worldSaveData")
	if !ok {
		return nil, fmt.Errorf(
			"resolve: %s has no worldSaveData, so it is not a Palworld world save",
			set.Level,
		)
	}
	resolver := &Resolver{set: set, options: options, level: level, world: world}

	switch {
	case set.LevelMeta == "":
		resolver.warn("%s is missing, so the world name and in-game day are unavailable", levelMetaName)
	default:
		meta, err := palworld.LoadWithOptions(set.LevelMeta, options.Save)
		if err != nil {
			resolver.warn("%s could not be read: %v", levelMetaName, err)
			break
		}
		if properties, ok := value[gvas.Properties](meta.Properties, "SaveData"); ok {
			resolver.meta = properties
		} else {
			resolver.warn("%s has no SaveData", levelMetaName)
		}
	}
	return resolver, nil
}

// Set reports the files this resolver was opened over.
func (r *Resolver) Set() Set { return r.set }

func (r *Resolver) warn(format string, arguments ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, arguments...))
}

// worldMap returns a named collection of worldSaveData. A missing one is an
// error rather than a warning: every caller of this is about to iterate it, and
// silently resolving nothing would look like an empty world.
func (r *Resolver) worldMap(name string) (*gvas.MapValue, error) {
	collection, ok := value[*gvas.MapValue](r.world, name)
	if !ok {
		return nil, fmt.Errorf("resolve: %s has no worldSaveData.%s", r.set.Level, name)
	}
	return collection, nil
}

// mapCount reports a collection's declared element count without iterating it,
// which is what makes the world counts cheap.
func (r *Resolver) mapCount(name string) int {
	collection, ok := value[*gvas.MapValue](r.world, name)
	if !ok {
		return 0
	}
	return int(collection.Count)
}

// arrayCount is mapCount for a struct array.
func (r *Resolver) arrayCount(name string) int {
	array, ok := structArray(r.world, name)
	if !ok {
		return 0
	}
	return int(array.Count)
}

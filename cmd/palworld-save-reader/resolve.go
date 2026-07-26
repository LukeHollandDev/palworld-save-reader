// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/resolve"
)

// The questions --resolve can answer. Each name is also the JSON field its
// answer appears under, so a consumer can find the payload from the kind alone.
const (
	resolvePlayer  = "player"
	resolvePlayers = "players"
	resolveGuild   = "guild"
	resolveGuilds  = "guilds"
	resolveWorld   = "world"
)

// resolveKinds maps each kind to whether it requires --id.
var resolveKinds = map[string]bool{
	resolvePlayer:  true,
	resolvePlayers: false,
	resolveGuild:   true,
	resolveGuilds:  false,
	resolveWorld:   false,
}

// runResolveMode validates the --resolve arguments and runs the join. It returns a
// process status rather than an error because the two failure kinds are different:
// an argument this tool cannot make sense of is a usage error, and a save it
// cannot read is a runtime one.
func runResolveMode(stdout, stderr io.Writer, kind, id, directory string, positional int, projectionFlags bool) int {
	needsID, known := resolveKinds[kind]
	if !known {
		return usageError(stderr, fmt.Sprintf("--resolve %q is not one of %s", kind, resolveKindNames()))
	}
	if positional != 0 {
		return usageError(stderr, "--resolve reads the save directory named by --saves, not a file")
	}
	if projectionFlags {
		return usageError(stderr, "--resolve does not accept projection options")
	}
	if directory == "" {
		return usageError(stderr, "--resolve requires --saves DIR")
	}
	var uid gvas.GUID
	switch {
	case needsID && id == "":
		return usageError(stderr, fmt.Sprintf("--resolve %s requires --id UID", kind))
	case !needsID && id != "":
		return usageError(stderr, fmt.Sprintf("--resolve %s does not accept --id", kind))
	case needsID:
		parsed, err := gvas.ParseGUID(id)
		if err != nil {
			return usageError(stderr, err.Error())
		}
		uid = parsed
	}
	if err := runResolve(stdout, kind, uid, directory); err != nil {
		return runtimeError(stderr, err)
	}
	return 0
}

// resolveKindNames lists the kinds for a usage message.
func resolveKindNames() string {
	names := make([]string, 0, len(resolveKinds))
	for name := range resolveKinds {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "|")
}

// resolveEnvelope wraps a single resolved document. The version is the shape's,
// not the save's: --resolve output is an interpretation this tool owns, so a
// consumer needs to know when it changes. Only one payload field is ever set,
// and it is named for the kind.
type resolveEnvelope struct {
	ResolveVersion int             `json:"resolveVersion"`
	Kind           string          `json:"kind"`
	World          *resolve.World  `json:"world,omitempty"`
	Player         *resolve.Player `json:"player,omitempty"`
	Guild          *resolve.Guild  `json:"guild,omitempty"`
}

// runResolve answers one question about a save directory.
func runResolve(stdout io.Writer, kind string, id gvas.GUID, directory string) error {
	set, err := resolve.Discover(directory)
	if err != nil {
		return err
	}
	resolver, err := resolve.Open(set, resolve.Options{})
	if err != nil {
		return err
	}
	switch kind {
	case resolveWorld:
		world, err := resolver.World()
		if err != nil {
			return err
		}
		return writeJSON(stdout, resolveEnvelope{
			ResolveVersion: resolve.Version,
			Kind:           kind,
			World:          world,
		})
	case resolvePlayer:
		player, err := resolver.Player(id)
		if err != nil {
			return err
		}
		return writeJSON(stdout, resolveEnvelope{
			ResolveVersion: resolve.Version,
			Kind:           kind,
			Player:         player,
		})
	case resolvePlayers:
		return writeResolvedArray(stdout, resolvePlayers, func(emit func(any) error) error {
			return resolver.Players(func(player *resolve.Player) error { return emit(player) })
		})
	case resolveGuild:
		guild, err := resolver.Guild(id)
		if err != nil {
			return err
		}
		return writeJSON(stdout, resolveEnvelope{
			ResolveVersion: resolve.Version,
			Kind:           kind,
			Guild:          guild,
		})
	case resolveGuilds:
		return writeResolvedArray(stdout, resolveGuilds, func(emit func(any) error) error {
			return resolver.Guilds(func(guild *resolve.Guild) error { return emit(guild) })
		})
	default:
		return fmt.Errorf("resolve: unknown kind %q", kind)
	}
}

// writeResolvedArray streams one of the array kinds: each document is encoded to
// the writer as it arrives rather than assembled into one value and encoded at the
// end, which is memory rule R3.
//
// Nothing is written until the first document exists. A resolve that fails part
// way through must not leave half an envelope on standard output, and a caller
// reading the exit status would have no way to tell the difference.
//
// The documents arrive through a callback rather than a slice so the framing can
// be tested against documents that were never read from a save file, and so both
// array kinds share one implementation: they differ only in the field name and
// the element type.
func writeResolvedArray(stdout io.Writer, kind string, each func(func(any) error) error) error {
	written := 0
	err := each(func(document any) error {
		if written == 0 {
			if err := writeResolvePrefix(stdout, kind); err != nil {
				return err
			}
			if _, err := io.WriteString(stdout, "[\n    "); err != nil {
				return err
			}
		} else if _, err := io.WriteString(stdout, ",\n    "); err != nil {
			return err
		}
		written++
		return writeIndented(stdout, document, "    ")
	})
	if err != nil {
		return err
	}
	if written == 0 {
		// A save with no players, or no guilds, is a legitimate answer rather than
		// an error, so the empty array is emitted rather than nothing at all.
		if err := writeResolvePrefix(stdout, kind); err != nil {
			return err
		}
		_, err := io.WriteString(stdout, "[]\n}\n")
		return err
	}
	_, err = io.WriteString(stdout, "\n  ]\n}\n")
	return err
}

// writeResolvePrefix opens the envelope by hand, which is what lets the array
// inside it be streamed. encoding/json cannot emit a struct field incrementally.
func writeResolvePrefix(stdout io.Writer, kind string) error {
	_, err := fmt.Fprintf(
		stdout,
		"{\n  \"resolveVersion\": %d,\n  \"kind\": %q,\n  %q: ",
		resolve.Version, kind, kind,
	)
	return err
}

// writeIndented encodes one value at a nesting depth, matching writeJSON's
// two-space indent and its HTML escaping, so a streamed element is
// indistinguishable from one encoding/json produced in place.
func writeIndented(stdout io.Writer, value any, prefix string) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetIndent(prefix, "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	_, err := stdout.Write(bytes.TrimRight(buffer.Bytes(), "\n"))
	return err
}

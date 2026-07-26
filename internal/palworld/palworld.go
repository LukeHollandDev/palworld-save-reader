// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

// Package palworld reads Palworld saves by supplying the game knowledge the
// generic layers below it deliberately lack.
//
// savefile knows the container and gvas knows Unreal's property encoding, but
// neither can tell which concrete struct hides behind an untagged
// StructProperty — Unreal's legacy tags omit that for map keys, map values, and
// set elements. This package owns the path-to-struct table that resolves them,
// which makes it the one place where a Palworld version change is expected to
// land.
//
// Load is the entry point for the rest of the program. Callers that want no game
// knowledge at all can still reach savefile and gvas directly.
package palworld

import (
	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefile"
)

// Load reads, decompresses, and parses a Palworld .sav file with the game's type
// hints applied.
func Load(path string) (*savefile.Save, error) {
	return LoadWithOptions(path, savefile.Options{})
}

// LoadWithOptions is Load with explicit limits and additional type hints. A hint
// the caller supplies overrides the built-in hint at the same path, which is how
// a new game build can be worked around without changing this package.
func LoadWithOptions(path string, options savefile.Options) (*savefile.Save, error) {
	return savefile.Open(path, withTypeHints(options))
}

// Read is Load for a save already held in memory.
func Read(data []byte) (*savefile.Save, error) {
	return ReadWithOptions(data, savefile.Options{})
}

// ReadWithOptions is Read with explicit limits and additional type hints.
func ReadWithOptions(data []byte, options savefile.Options) (*savefile.Save, error) {
	return savefile.Read(data, withTypeHints(options))
}

// TypeHints returns Palworld's path-to-struct table. The result is a copy, so a
// caller may adjust it and pass it back through savefile.Options.
func TypeHints() map[string]string {
	return withGVASTypeHints(gvas.Options{}).TypeHints
}

// withTypeHints merges the built-in hints under the caller's, leaving the
// caller's Options and map untouched.
func withTypeHints(options savefile.Options) savefile.Options {
	options.GVAS = withGVASTypeHints(options.GVAS)
	return options
}

// withGVASTypeHints is withTypeHints for a parse with no container around it,
// which is what decoding a nested property stream out of a RawData blob is. Both
// entry points share one merge so a nested stream is hinted exactly like the
// archive it came out of.
func withGVASTypeHints(options gvas.Options) gvas.Options {
	merged := make(map[string]string, len(typeHints)+len(options.TypeHints))
	for path, hint := range typeHints {
		merged[path] = hint
	}
	for path, hint := range options.TypeHints {
		// An invalid entry is copied through rather than dropped so gvas reports
		// it to the caller instead of silently ignoring it.
		merged[path] = hint
	}
	options.TypeHints = merged
	return options
}

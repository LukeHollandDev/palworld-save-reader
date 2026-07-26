// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"fmt"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// Character is one entry of worldSaveData.CharacterSaveParameterMap: a player or
// a pal, decoded from the nested property stream in its RawData.
//
// Unlike an item slot, this blob is not a bespoke record. It is the same
// property encoding gvas already reads, written without a GVAS header in front
// of it, so decoding is a re-entrant call into gvas.ParseProperties rather than
// a new parser. That is what makes species, level, nickname, HP and passive
// skills readable: gvas does the work, and this file supplies only the framing
// and the paths.
//
// Verified against all 2,259 character blobs in a 1.0.1.100619 world save: every
// one decodes, every one holds exactly one "SaveParameter" property, and no
// property anywhere inside falls back to gvas.UndecodedValue -- the type-hint
// table already covers the nested structs.
type Character struct {
	// Properties is the stream as parsed, not reshaped. In all 2,259 fixture
	// blobs it is exactly one "SaveParameter" StructProperty of type
	// PalIndividualCharacterSaveParameter; Parameters unwraps that.
	Properties gvas.Properties
	// GroupID keys worldSaveData.GroupSaveDataMap -- the guild this character
	// belongs to, whether it is a player or one of their pals. All eight
	// distinct values in the fixture world resolve to a group entry, which is
	// what distinguishes a real field from sixteen bytes that merely look like
	// a GUID.
	GroupID gvas.GUID
	// Trailer is the framing after the stream's terminator, aliasing the
	// caller's slice. GroupID is read out of it at characterGroupIDOffset; the
	// eight bytes surrounding that are zero in every fixture blob and are kept
	// because nothing here knows what they are. See PaddingBeyondGroupID.
	Trailer []byte
}

const (
	// characterGroupIDOffset is where GroupID starts within Trailer. The four
	// bytes before it are zero in every fixture blob.
	characterGroupIDOffset = 4
	// characterTrailerBytes is the trailer width observed in every one of the
	// 2,259 fixture blobs: four zero bytes, the group id, four more zero bytes.
	// It is documentation rather than a requirement -- DecodeCharacter accepts
	// any width, because a game update that extends the framing should still
	// yield a readable character.
	characterTrailerBytes = 24
)

// Parameters returns the PalIndividualCharacterSaveParameter properties: the
// character's species, level, nickname, stats and so on.
//
// It returns nil when the stream does not have the single-SaveParameter shape
// every fixture blob has, so a caller must check rather than assume. That is a
// deliberate split from DecodeCharacter, which reports a malformed *stream* as
// an error: a well-formed stream holding something unexpected is a schema change
// to report upward, not a decoding failure.
func (character Character) Parameters() gvas.Properties {
	property := character.Properties.Find("SaveParameter")
	if property == nil {
		return nil
	}
	structValue, ok := property.Value.(gvas.StructValue)
	if !ok {
		return nil
	}
	properties, _ := structValue.Value.(gvas.Properties)
	return properties
}

// PaddingBeyondGroupID reports whether any trailer byte outside GroupID is
// non-zero. Every fixture blob pads the group id with eight zero bytes, so a
// caller rendering the trailer can leave it out when this is false and know it
// is dropping nothing.
func (character Character) PaddingBeyondGroupID() bool {
	if len(character.Trailer) <= characterGroupIDOffset {
		return !allZero(character.Trailer)
	}
	if !allZero(character.Trailer[:characterGroupIDOffset]) {
		return true
	}
	after := characterGroupIDOffset + guidBytes
	if len(character.Trailer) <= after {
		return false
	}
	return !allZero(character.Trailer[after:])
}

// DecodeCharacter decodes one CharacterSaveParameterMap value's RawData. The
// result aliases data, which must not be modified afterwards.
func DecodeCharacter(data []byte) (Character, error) {
	return DecodeCharacterWithOptions(data, gvas.Options{})
}

// DecodeCharacterWithOptions is DecodeCharacter with explicit limits and
// additional type hints, merged over the game's own the same way
// LoadWithOptions does it. The limits matter here: the blob is untrusted input
// and a nested stream is parsed with the same bounds as a whole archive.
func DecodeCharacterWithOptions(data []byte, options gvas.Options) (Character, error) {
	properties, trailer, err := gvas.ParseProperties(
		data,
		characterRawDataPath,
		withGVASTypeHints(options),
	)
	if err != nil {
		return Character{}, fmt.Errorf("palworld: character record: %w", err)
	}
	character := Character{Properties: properties, Trailer: trailer}
	if len(trailer) >= characterGroupIDOffset+guidBytes {
		character.GroupID = guidAt(trailer[characterGroupIDOffset:])
	}
	return character, nil
}

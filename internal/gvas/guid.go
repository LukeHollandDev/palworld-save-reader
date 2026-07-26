// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package gvas

import (
	"fmt"
	"strconv"
	"strings"
)

// GUID is Unreal's four-uint32 FGuid representation.
type GUID struct {
	A uint32
	B uint32
	C uint32
	D uint32
}

// guidTextBytes is the length of the canonical form, dashes included.
const guidTextBytes = 36

// guidHexBytes is the length with the dashes removed: four words of eight hex
// digits.
const guidHexBytes = 32

func (g GUID) String() string {
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%04x%08x",
		g.A,
		g.B>>16,
		g.B&0xffff,
		g.C>>16,
		g.C&0xffff,
		g.D,
	)
}

func (g GUID) IsZero() bool { return g.A|g.B|g.C|g.D == 0 }

func (g GUID) MarshalText() ([]byte, error) { return []byte(g.String()), nil }

func (g *GUID) UnmarshalText(text []byte) error {
	parsed, err := ParseGUID(string(text))
	if err != nil {
		return err
	}
	*g = parsed
	return nil
}

// ParseGUID is the inverse of String. The dashes are optional: Unreal writes the
// canonical 8-4-4-4-12 form, but Palworld names a player's save file after their
// UID with the dashes stripped, so both spellings reach a caller from real data.
// Hex digits may be upper or lower case, for the same reason.
//
// When dashes are present they must sit in the canonical positions. Accepting
// them anywhere would mean two spellings of the same value that neither Unreal
// nor Palworld ever writes, and silently accepting a mangled identifier is worse
// than rejecting it: the result is a GUID that resolves to nothing.
func ParseGUID(text string) (GUID, error) {
	digits := text
	if strings.ContainsRune(text, '-') {
		if len(text) != guidTextBytes ||
			text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' {
			return GUID{}, fmt.Errorf("gvas: %q is not a GUID: dashes are misplaced", text)
		}
		digits = strings.ReplaceAll(text, "-", "")
	}
	if len(digits) != guidHexBytes {
		return GUID{}, fmt.Errorf(
			"gvas: %q is not a GUID: %d hex digits, want %d",
			text,
			len(digits),
			guidHexBytes,
		)
	}
	var words [4]uint32
	for i := range words {
		// ParseUint with an explicit base rejects a sign, an "0x" prefix and
		// underscores, so the only thing that gets through is eight hex digits.
		word, err := strconv.ParseUint(digits[i*8:(i+1)*8], 16, 32)
		if err != nil {
			return GUID{}, fmt.Errorf("gvas: %q is not a GUID: %w", text, err)
		}
		words[i] = uint32(word)
	}
	return GUID{A: words[0], B: words[1], C: words[2], D: words[3]}, nil
}

// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"strings"
	"testing"
)

// The record layout, stated here rather than read out of characterslot.go.
// Building the fixture from the same constants the decoder reads would make the
// offset tests self-fulfilling: both would move together and the assertions
// would still pass.
const (
	wantSlotBytes          = 38
	wantSlotInstanceOffset = 16
)

// The GUID bytes and the string they decode to, so the test pins Unreal's
// four-little-endian-words layout rather than agreeing with guidAt by
// construction.
var (
	wantSlotInstanceBytes = []byte{
		0x24, 0x07, 0x15, 0x6b, 0xef, 0x4c, 0x18, 0x21,
		0x78, 0xa0, 0x5a, 0x81, 0x07, 0xbf, 0x95, 0xfb,
	}
	// A player UID in the shape Palworld writes -- four bytes of account id then
	// zeroes -- but invented rather than observed. A real one identifies an
	// account, so it does not belong in a public repository.
	wantSlotPlayerBytes = []byte{
		0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
	}
)

const (
	wantSlotInstanceDecoded = "6b150724-2118-4cef-815a-a078fb95bf07"
	wantSlotPlayerDecoded   = "deadbeef-0000-0000-0000-000000000000"
)

// characterSlotBlob builds the 38-byte pal reference Palworld writes, so a test
// states the record it means rather than a hex literal.
func characterSlotBlob(playerUID, instanceID []byte) []byte {
	blob := make([]byte, wantSlotBytes)
	copy(blob, playerUID)
	copy(blob[wantSlotInstanceOffset:], instanceID)
	return blob
}

func TestDecodeCharacterSlot(t *testing.T) {
	slot, err := DecodeCharacterSlot(characterSlotBlob(nil, wantSlotInstanceBytes))
	if err != nil {
		t.Fatal(err)
	}
	if got := slot.InstanceID.String(); got != wantSlotInstanceDecoded {
		t.Errorf("InstanceID = %s, want %s", got, wantSlotInstanceDecoded)
	}
	if !slot.PlayerUID.IsZero() {
		t.Errorf("PlayerUID = %s, want zero", slot.PlayerUID)
	}
	if slot.Empty() {
		t.Error("a slot referencing a character reports itself empty")
	}
	// The fixture trailer is six zero bytes. Six is not a requirement, but the
	// decoder must hand back exactly what followed the instance id.
	if want := wantSlotBytes - 2*wantSlotInstanceOffset; len(slot.Trailer) != want {
		t.Errorf("Trailer = %d bytes, want %d", len(slot.Trailer), want)
	}
}

// TestDecodeCharacterSlotReadsBothGUIDs is the check that the two halves are not
// transposed. Every fixture slot has a zero PlayerUID, so a decoder that read the
// instance id from offset 0 would still look right against the fixtures as long
// as it was consistently wrong; a blob with both halves set catches it.
func TestDecodeCharacterSlotReadsBothGUIDs(t *testing.T) {
	slot, err := DecodeCharacterSlot(characterSlotBlob(wantSlotPlayerBytes, wantSlotInstanceBytes))
	if err != nil {
		t.Fatal(err)
	}
	if got := slot.PlayerUID.String(); got != wantSlotPlayerDecoded {
		t.Errorf("PlayerUID = %s, want %s", got, wantSlotPlayerDecoded)
	}
	if got := slot.InstanceID.String(); got != wantSlotInstanceDecoded {
		t.Errorf("InstanceID = %s, want %s", got, wantSlotInstanceDecoded)
	}
}

// TestDecodeCharacterSlotReportsAnEmptySlot covers the case no fixture slot
// exercises: a container only serializes the slots it holds, so Empty is written
// against the layout rather than against observed data.
func TestDecodeCharacterSlotReportsAnEmptySlot(t *testing.T) {
	slot, err := DecodeCharacterSlot(characterSlotBlob(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !slot.Empty() {
		t.Errorf("a slot with a zero instance id reports Empty() = false")
	}
}

// TestDecodeCharacterSlotRejectsATruncatedRecord pins the failure as an error
// rather than a plausible half-read reference, which would resolve to nothing and
// look like missing data instead of a wrong layout.
func TestDecodeCharacterSlotRejectsATruncatedRecord(t *testing.T) {
	for _, testCase := range []struct {
		name string
		blob []byte
	}{
		{name: "empty", blob: nil},
		{name: "one guid only", blob: make([]byte, 16)},
		{name: "one byte short", blob: make([]byte, 31)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := DecodeCharacterSlot(testCase.blob)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), "character slot") {
				t.Errorf("error %q does not name the character slot", err)
			}
		})
	}

	// Exactly the two GUIDs and nothing else is valid: the trailer is optional,
	// which is the boundary the length check sits on.
	slot, err := DecodeCharacterSlot(make([]byte, 32))
	if err != nil {
		t.Fatalf("a record with no trailer: %v", err)
	}
	if len(slot.Trailer) != 0 {
		t.Errorf("Trailer = %v, want empty", slot.Trailer)
	}
}

// TestDecodeCharacterSlotAliasesTrailer documents that Trailer is a window onto
// the caller's bytes, so a caller who reuses a buffer knows what they are doing.
func TestDecodeCharacterSlotAliasesTrailer(t *testing.T) {
	blob := characterSlotBlob(nil, wantSlotInstanceBytes)
	slot, err := DecodeCharacterSlot(blob)
	if err != nil {
		t.Fatal(err)
	}
	if len(slot.Trailer) == 0 {
		t.Fatal("the fixture-shaped blob decoded to an empty trailer")
	}
	blob[len(blob)-1] = 9
	if slot.Trailer[len(slot.Trailer)-1] != 9 {
		t.Error("Trailer does not alias the input")
	}
}

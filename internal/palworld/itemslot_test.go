// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"encoding/binary"
	"strings"
	"testing"
)

// slotBlob builds an item-slot payload the way Palworld writes one, so a test
// can state the record it means rather than a hex literal.
func slotBlob(index, count uint32, itemID string, trailer []byte) []byte {
	blob := make([]byte, itemSlotHeaderBytes)
	binary.LittleEndian.PutUint32(blob[0:4], index)
	binary.LittleEndian.PutUint32(blob[4:8], count)
	if itemID == "" {
		return append(blob, trailer...)
	}
	binary.LittleEndian.PutUint32(blob[8:12], uint32(len(itemID)+1))
	blob = append(blob, itemID...)
	blob = append(blob, 0)
	return append(blob, trailer...)
}

func TestDecodeItemSlot(t *testing.T) {
	// The real fixture shape: 16 zero bytes, then a dynamic-item GUID, then 20
	// zero bytes.
	withGUID := make([]byte, 52)
	for i, b := range []byte{
		0x85, 0x59, 0xf6, 0x4c, 0xc1, 0x45, 0xe4, 0x51,
		0x50, 0x52, 0x27, 0x9f, 0xcd, 0x7f, 0x5c, 0xed,
	} {
		withGUID[16+i] = b
	}

	for _, testCase := range []struct {
		name    string
		blob    []byte
		want    ItemSlot
		wantHex string
	}{
		{
			name: "plain stackable",
			blob: slotBlob(1, 2180, "PalSphere", make([]byte, 52)),
			want: ItemSlot{SlotIndex: 1, Count: 2180, ItemID: "PalSphere"},
		},
		{
			name: "empty slot",
			blob: slotBlob(7, 0, "", make([]byte, 52)),
			want: ItemSlot{SlotIndex: 7},
		},
		{
			name:    "item with a per-instance record",
			blob:    slotBlob(0, 1, "FurArmorCold", withGUID),
			want:    ItemSlot{SlotIndex: 0, Count: 1, ItemID: "FurArmorCold"},
			wantHex: "4cf65985-51e4-45c1-9f27-5250ed5c7fcd",
		},
		{
			name: "record that stops before the guid",
			blob: slotBlob(3, 4, "Wood", nil),
			want: ItemSlot{SlotIndex: 3, Count: 4, ItemID: "Wood"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			slot, err := DecodeItemSlot(testCase.blob)
			if err != nil {
				t.Fatal(err)
			}
			if slot.SlotIndex != testCase.want.SlotIndex {
				t.Errorf("SlotIndex = %d, want %d", slot.SlotIndex, testCase.want.SlotIndex)
			}
			if slot.Count != testCase.want.Count {
				t.Errorf("Count = %d, want %d", slot.Count, testCase.want.Count)
			}
			if slot.ItemID != testCase.want.ItemID {
				t.Errorf("ItemID = %q, want %q", slot.ItemID, testCase.want.ItemID)
			}
			if testCase.wantHex == "" {
				if !slot.DynamicItemID.IsZero() {
					t.Errorf("DynamicItemID = %s, want zero", slot.DynamicItemID)
				}
			} else if got := slot.DynamicItemID.String(); got != testCase.wantHex {
				t.Errorf("DynamicItemID = %s, want %s", got, testCase.wantHex)
			}
			if (slot.ItemID == "") != slot.Empty() {
				t.Errorf("Empty() = %v with ItemID %q", slot.Empty(), slot.ItemID)
			}
		})
	}
}

// TestDecodeItemSlotRejectsMalformed pins the failures as errors rather than
// plausible-looking slots, because a wrong layout assumption must be visible.
func TestDecodeItemSlotRejectsMalformed(t *testing.T) {
	unterminated := slotBlob(0, 1, "Wood", nil)
	unterminated[len(unterminated)-1] = 'X' // clobber the NUL

	overlong := make([]byte, itemSlotHeaderBytes+4)
	binary.LittleEndian.PutUint32(overlong[8:12], 1<<20)

	for _, testCase := range []struct {
		name string
		blob []byte
		want string
	}{
		{name: "empty", blob: nil, want: "needs 12 bytes"},
		{name: "truncated header", blob: make([]byte, 11), want: "needs 12 bytes"},
		{name: "id length past the end", blob: overlong, want: "bytes left"},
		{name: "id not NUL-terminated", blob: unterminated, want: "not NUL-terminated"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := DecodeItemSlot(testCase.blob)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %q does not mention %q", err, testCase.want)
			}
		})
	}
}

// TestDecodeItemSlotAliasesTrailer documents that Trailer is a window onto the
// caller's bytes, so a caller who reuses a buffer knows what they are doing.
func TestDecodeItemSlotAliasesTrailer(t *testing.T) {
	blob := slotBlob(0, 1, "Wood", []byte{1, 2, 3})
	slot, err := DecodeItemSlot(blob)
	if err != nil {
		t.Fatal(err)
	}
	if len(slot.Trailer) != 3 {
		t.Fatalf("Trailer = %v, want 3 bytes", slot.Trailer)
	}
	blob[len(blob)-1] = 9
	if slot.Trailer[2] != 9 {
		t.Error("Trailer does not alias the input")
	}
}

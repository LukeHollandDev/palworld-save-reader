// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// The record layout, stated here rather than read out of group.go, so an offset
// that moves has to move in two places. Every identifier below is invented: a
// real account id names a real account and does not belong in this repository.
const (
	wantHandleBytes         = 32
	wantGuildReservedBytes  = 14
	wantOrganizationPadding = 4
)

var (
	// The four words of each test GUID, and the string they decode to. Writing
	// both pins Unreal's four-little-endian-words layout rather than agreeing
	// with guidAt by construction.
	testGroupID     = [4]uint32{0xdead0001, 0x11112222, 0x33334444, 0x55556666}
	testGroupIDText = "dead0001-1111-2222-3333-444455556666"
	testAdmin       = [4]uint32{0xbeef0002, 0, 0, 0}
	testAdminText   = "beef0002-0000-0000-0000-000000000000"
	testPal         = [4]uint32{0xfeed0003, 0x0a0b0c0d, 0x01020304, 0x05060708}
	testPalText     = "feed0003-0a0b-0c0d-0102-030405060708"
	testBase        = [4]uint32{0xcafe0004, 0, 0, 1}
	testBaseText    = "cafe0004-0000-0000-0000-000000000001"
	testPoint       = [4]uint32{0xcafe0005, 0, 0, 2}
	testPointText   = "cafe0005-0000-0000-0000-000000000002"
	// A distinct value for NamedBy. In every fixture guild it is either zero or
	// equal to the admin, so a test that used the admin here would not notice the
	// two being read in the wrong order.
	testNamer     = [4]uint32{0xbeef0006, 0, 0, 0}
	testNamerText = "beef0006-0000-0000-0000-000000000000"
)

// guildBlob builds the record a guild carries: the shared part, then the guild
// half. The two halves are returned separately because that is how the decoders
// take them, and the split is itself part of the layout.
func guildBlob() []byte {
	shared := new(blobBuilder).
		guid(testGroupID[0], testGroupID[1], testGroupID[2], testGroupID[3]).
		utf8(strings.ToUpper(strings.ReplaceAll(testAdminText, "-", ""))).
		u32(2).                                                       // two handles
		guid(testAdmin[0], testAdmin[1], testAdmin[2], testAdmin[3]). // a player's uid...
		guid(testPal[0], testPal[1], testPal[2], testPal[3]).         // ...and their character
		guid(0, 0, 0, 0).                                             // a pal has no uid
		guid(testPal[0], testPal[1], testPal[2], testPal[3]+1).
		u32(0). // the uint32 nothing has explained
		u8(0).  // a guild's organization type
		u32(1). // one base camp
		guid(testBase[0], testBase[1], testBase[2], testBase[3]).
		done()
	guild := new(blobBuilder).
		u32(0).  // the uint32 before the level
		i32(13). // base camp level
		u32(1).  // one base camp point
		guid(testPoint[0], testPoint[1], testPoint[2], testPoint[3]).
		utf8("Kestrel Company").
		guid(testNamer[0], testNamer[1], testNamer[2], testNamer[3]). // named by
		zeros(wantGuildReservedBytes).
		guid(testAdmin[0], testAdmin[1], testAdmin[2], testAdmin[3]). // admin
		u32(1).                                                       // one member
		guid(testAdmin[0], testAdmin[1], testAdmin[2], testAdmin[3]).
		i64(12_000_000_000_000).
		utf8("Ada").
		u8(1).
		bytes(9, 9, 9). // a trailer that is not zero, so it cannot be mistaken for padding
		done()
	return append(shared, guild...)
}

// organizationBlob builds what one of the world's fixed factions carries: the
// shared part and four zero bytes.
func organizationBlob() []byte {
	return new(blobBuilder).
		guid(testGroupID[0], testGroupID[1], testGroupID[2], testGroupID[3]).
		utf8("").
		u32(0). // no handles
		u32(0).
		u8(5).  // an organization type
		u32(0). // no base camps
		zeros(wantOrganizationPadding).
		done()
}

func TestDecodeGroupReadsTheSharedPart(t *testing.T) {
	group, err := DecodeGroup(guildBlob())
	if err != nil {
		t.Fatal(err)
	}
	if got := group.ID.String(); got != testGroupIDText {
		t.Errorf("ID = %s, want %s", got, testGroupIDText)
	}
	if want := strings.ToUpper(strings.ReplaceAll(testAdminText, "-", "")); group.Name != want {
		t.Errorf("Name = %q, want %q", group.Name, want)
	}
	if group.OrganizationType != 0 {
		t.Errorf("OrganizationType = %d, want 0 for a guild", group.OrganizationType)
	}
	if group.Unknown != 0 {
		t.Errorf("Unknown = %d, want 0", group.Unknown)
	}
	if len(group.Handles) != 2 {
		t.Fatalf("Handles = %d, want 2", len(group.Handles))
	}
	// The player half and the character half of a handle must not be transposed:
	// a pal's handle has a zero uid, so a decoder that read the instance id from
	// the wrong offset would still look right against pals alone.
	if got := group.Handles[0].PlayerUID.String(); got != testAdminText {
		t.Errorf("Handles[0].PlayerUID = %s, want %s", got, testAdminText)
	}
	if got := group.Handles[0].InstanceID.String(); got != testPalText {
		t.Errorf("Handles[0].InstanceID = %s, want %s", got, testPalText)
	}
	if !group.Handles[1].PlayerUID.IsZero() {
		t.Errorf("Handles[1].PlayerUID = %s, want zero", group.Handles[1].PlayerUID)
	}
	if len(group.BaseIDs) != 1 || group.BaseIDs[0].String() != testBaseText {
		t.Errorf("BaseIDs = %v, want [%s]", group.BaseIDs, testBaseText)
	}
	if len(group.Remainder) == 0 {
		t.Error("Remainder is empty, so the guild half was consumed by the shared part")
	}
}

func TestDecodeGuildReadsTheGuildHalf(t *testing.T) {
	group, err := DecodeGroup(guildBlob())
	if err != nil {
		t.Fatal(err)
	}
	guild, err := DecodeGuild(group.Remainder)
	if err != nil {
		t.Fatal(err)
	}
	if guild.Name != "Kestrel Company" {
		t.Errorf("Name = %q", guild.Name)
	}
	if guild.BaseCampLevel != 13 {
		t.Errorf("BaseCampLevel = %d, want 13", guild.BaseCampLevel)
	}
	if len(guild.BasePoints) != 1 || guild.BasePoints[0].String() != testPointText {
		t.Errorf("BasePoints = %v, want [%s]", guild.BasePoints, testPointText)
	}
	if got := guild.Admin.String(); got != testAdminText {
		t.Errorf("Admin = %s, want %s", got, testAdminText)
	}
	if got := guild.NamedBy.String(); got != testNamerText {
		t.Errorf("NamedBy = %s, want %s", got, testNamerText)
	}
	if len(guild.Reserved) != wantGuildReservedBytes {
		t.Errorf("Reserved = %d bytes, want %d", len(guild.Reserved), wantGuildReservedBytes)
	}
	if len(guild.Members) != 1 {
		t.Fatalf("Members = %d, want 1", len(guild.Members))
	}
	member := guild.Members[0]
	if got := member.PlayerUID.String(); got != testAdminText {
		t.Errorf("member PlayerUID = %s, want %s", got, testAdminText)
	}
	if member.Name != "Ada" || member.Role != 1 || member.LastOnlineTicks != 12_000_000_000_000 {
		t.Errorf("member = %+v", member)
	}
	// The trailer is whatever follows the members, and it is handed back rather
	// than assumed to be padding.
	if want := []byte{9, 9, 9}; string(guild.Trailer) != string(want) {
		t.Errorf("Trailer = %v, want %v", guild.Trailer, want)
	}
}

// TestDecodeGroupReadsAnOrganization covers the other half of the split: the
// shared part alone, with four bytes after it.
func TestDecodeGroupReadsAnOrganization(t *testing.T) {
	group, err := DecodeGroup(organizationBlob())
	if err != nil {
		t.Fatal(err)
	}
	if group.Name != "" || len(group.Handles) != 0 || len(group.BaseIDs) != 0 {
		t.Errorf("group = %+v", group)
	}
	if group.OrganizationType != 5 {
		t.Errorf("OrganizationType = %d, want 5", group.OrganizationType)
	}
	if len(group.Remainder) != wantOrganizationPadding {
		t.Errorf("Remainder = %d bytes, want %d", len(group.Remainder), wantOrganizationPadding)
	}
}

// TestDecodeGuildRejectsAnOrganization is the check that makes the two-call split
// safe. A renderer that has no GroupType property to consult attempts the guild
// half and reports it when it decodes, so "an organization does not decode as a
// guild" is load-bearing rather than a nicety.
func TestDecodeGuildRejectsAnOrganization(t *testing.T) {
	group, err := DecodeGroup(organizationBlob())
	if err != nil {
		t.Fatal(err)
	}
	if guild, err := DecodeGuild(group.Remainder); err == nil {
		t.Fatalf("an organization's remainder decoded as the guild %+v", guild)
	}
}

// TestDecodeGroupRejectsTruncationAtEveryLength is the fuzz-shaped test done
// exhaustively: every prefix of a real record must be an error rather than a
// plausible half-read group, and none may panic.
func TestDecodeGroupRejectsTruncationAtEveryLength(t *testing.T) {
	blob := guildBlob()
	// The last three bytes are the guild trailer, which is whatever follows the
	// members: cutting into it is not truncation, it is a shorter trailer. Every
	// prefix shorter than that must fail.
	fixed := len(blob) - 3
	for length := 0; length < fixed; length++ {
		group, groupErr := DecodeGroup(blob[:length])
		if groupErr != nil {
			if !strings.Contains(groupErr.Error(), "group record") {
				t.Errorf("%d bytes: error %q does not name the record", length, groupErr)
			}
			continue
		}
		// The shared part can succeed on a prefix, since Remainder is whatever is
		// left. The guild half must then fail, because the prefix cut into it.
		if _, err := DecodeGuild(group.Remainder); err == nil {
			t.Errorf("%d bytes of %d decoded as a whole guild", length, len(blob))
		}
	}
}

// TestDecodeGroupRejectsAnAbsurdCount is why the array reader checks the count
// against the bytes left before allocating: a blob is untrusted input, and four
// billion handles is 128GB.
func TestDecodeGroupRejectsAnAbsurdCount(t *testing.T) {
	blob := new(blobBuilder).
		guid(1, 2, 3, 4).
		utf8("").
		u32(0xffffffff).
		done()
	if _, err := DecodeGroup(blob); err == nil {
		t.Fatal("a group claiming four billion handles decoded")
	} else if !strings.Contains(err.Error(), "Handles") {
		t.Errorf("error %q does not name the array", err)
	}
	if got := wantHandleBytes; got != characterHandleBytes {
		t.Errorf("a handle is %d bytes, want %d", characterHandleBytes, got)
	}
}

// TestDecodeGuildReadsAUTF16Name covers the encoding Unreal uses for anything
// outside ASCII, which the fixture world's base camp names are written in and a
// guild name can be.
func TestDecodeGuildReadsAUTF16Name(t *testing.T) {
	const name = "ギルド"
	blob := new(blobBuilder).
		u32(0).
		i32(1).
		u32(0).
		utf16(name).
		guid(0, 0, 0, 0).
		zeros(wantGuildReservedBytes).
		guid(testAdmin[0], testAdmin[1], testAdmin[2], testAdmin[3]).
		u32(0).
		done()
	guild, err := DecodeGuild(blob)
	if err != nil {
		t.Fatal(err)
	}
	if guild.Name != name {
		t.Errorf("Name = %q, want %q", guild.Name, name)
	}
	if !guild.NamedBy.IsZero() {
		t.Errorf("NamedBy = %s, want zero", guild.NamedBy)
	}
	if len(guild.Members) != 0 {
		t.Errorf("Members = %v, want none", guild.Members)
	}
	var zero gvas.GUID
	if guild.Admin == zero {
		t.Error("Admin is zero, so the fields after the name are misaligned")
	}
}

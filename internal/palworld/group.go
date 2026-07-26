// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// A group is Palworld's word for a set of characters that belong together. Two
// kinds share the record: a Guild, which is a player's guild, and an
// Organization, which is one of the world's fixed factions. The sibling
// GroupType property says which, and only a guild carries the second half of the
// record -- which is why decoding is two calls rather than one.
//
// Verified against all 15 entries of a 1.0.1.100619 world save: 8 guilds and 7
// organizations, every one consumed to a fixed remainder.

// CharacterHandle is a reference to a character record: Unreal's FPalInstanceID,
// the same 32 bytes a CharacterSlot opens with.
//
// A group lists every character that belongs to it, players and pals alike. In
// the fixture world the eight guilds' handles are exactly the 3,349 records of
// worldSaveData.CharacterSaveParameterMap, each named once -- the whole
// population, partitioned. The 9 handles with a non-zero PlayerUID are exactly
// the 9 players, so counting them counts a guild's members a second way.
//
// An organization's handles are not so tidy: one fixture organization lists 62
// characters the save does not hold. A handle is a reference, and a reference can
// outlive what it names.
type CharacterHandle struct {
	// PlayerUID is the account this handle belongs to, and is the zero GUID for
	// a pal.
	PlayerUID gvas.GUID
	// InstanceID keys worldSaveData.CharacterSaveParameterMap.
	InstanceID gvas.GUID
}

// characterHandleBytes is the serialized width of one handle.
const characterHandleBytes = 2 * guidBytes

// Group is the part of a GroupSaveDataMap record that every group carries.
type Group struct {
	// ID is the group's own id, and equals the map key in all 15 fixture
	// entries. It is read rather than taken from the key so a caller can check
	// the two agree.
	ID gvas.GUID
	// Name is Palworld's internal name for the group. It is empty for all seven
	// fixture organizations, and for a guild it is the admin's account id
	// written as 32 hex digits -- not the guild's display name, which is in the
	// guild half of the record.
	Name string
	// Handles lists every character in the group.
	Handles []CharacterHandle
	// OrganizationType is 0 for all eight fixture guilds and 2 to 8, one value
	// each, for the seven organizations. The values are not otherwise known: an
	// organization is a fixed faction, and seven distinct types across seven
	// factions is what makes this the type field rather than the zero uint32
	// before it.
	OrganizationType uint8
	// BaseIDs keys worldSaveData.BaseCampSaveData: the group's base camps. All
	// 19 references in the fixture world resolve, they are distinct, and they
	// account for every base camp in the save -- and each of those camps names
	// this group back. See BaseCamp.GroupID.
	BaseIDs []gvas.GUID
	// Remainder is what follows the shared part, aliasing the caller's slice.
	// For an organization it is four zero bytes in all seven fixture records;
	// for a guild it is the guild record, which DecodeGuild reads.
	Remainder []byte
	// Unknown is the uint32 between the handles and OrganizationType. It is zero
	// in all 15 fixture records, so nothing can be said about it beyond that it
	// is there -- and it is read rather than skipped because a non-zero value
	// would mean this reading of the record is wrong.
	Unknown uint32
}

// DecodeGroup decodes the shared part of one GroupSaveDataMap value's RawData.
//
// The guild half is a separate call because the record does not say whether it
// is there: the sibling GroupType property does. Deciding it here from the bytes
// left over would be a guess dressed up as a decoder.
//
// The result aliases data through Remainder, so data must not be modified
// afterwards.
func DecodeGroup(data []byte) (Group, error) {
	r := newRecord("group record", data)
	group := Group{
		ID:   r.guid("GroupId"),
		Name: r.text("Name"),
	}
	if handles := r.array("Handles", characterHandleBytes); handles != nil {
		group.Handles = make([]CharacterHandle, len(handles)/characterHandleBytes)
		for i := range group.Handles {
			at := i * characterHandleBytes
			group.Handles[i] = CharacterHandle{
				PlayerUID:  guidAt(handles[at:]),
				InstanceID: guidAt(handles[at+guidBytes:]),
			}
		}
	}
	group.Unknown = r.u32("the uint32 before the organization type")
	group.OrganizationType = r.u8("OrganizationType")
	group.BaseIDs = r.guids("BaseIds")
	group.Remainder = r.rest()
	if r.err != nil {
		return Group{}, r.err
	}
	return group, nil
}

// Guild is the second half of a guild's group record: what the guild is called,
// who runs it, and who is in it.
//
// This is the phase the group blob existed to unlock. A guild's display name is
// nowhere else in the save, and neither is the list of accounts that belong to
// it -- a character record names its group, but only the group names its people.
type Guild struct {
	// BaseCampLevel is the guild's base camp level, 3 to 22 across the fixture
	// world's eight guilds.
	BaseCampLevel int32
	// BasePoints has one entry per BaseIDs entry, and each matches the
	// OwnerMapObjectID of a base camp -- all 19 of them in the fixture world. It
	// is the map object a camp is built around, which in Palworld means the
	// palbox, though nothing here confirms that: MapObjectSaveData's own RawData
	// is not decoded.
	BasePoints []gvas.GUID
	// Name is the guild's display name, "Unnamed Guild" while it has not been
	// set.
	Name string
	// Admin is the guild's admin. It is the account whose id Palworld also uses
	// as Group.Name, and the member carrying Role 1, in all eight fixture
	// guilds.
	Admin gvas.GUID
	// Members lists the guild's players.
	Members []GuildMember
	// NamedBy is an account id written just before Admin. It is zero in exactly
	// the three fixture guilds still called "Unnamed Guild" and equal to Admin
	// in the other five, which is what the name records -- an exact correlation
	// across eight guilds, and still an inference rather than a decoded meaning.
	// Nothing in this program depends on it.
	NamedBy gvas.GUID
	// Reserved is the 14 bytes between NamedBy and Admin, byte-identical in all
	// eight fixture guilds (a zero uint32, the uint32 2, the bytes 2 and 3, and
	// a zero uint32). Kept because dropping bytes nobody has explained would
	// make a change in them invisible.
	Reserved []byte
	// Trailer is what follows the members, aliasing the caller's slice. It is
	// the same 30 bytes in all eight fixture guilds: a count of three, then
	// three byte-keyed lists (2 -> 0,3,4,5,7; 3 -> 4,7; 4 -> nothing), then four
	// zero bytes. That shape is legible but its meaning is not, so it is
	// preserved rather than reported as fields.
	Trailer []byte
}

// GuildMember is one account in a guild.
type GuildMember struct {
	// PlayerUID is the member's account id, which names their player.sav.
	PlayerUID gvas.GUID
	// Name is the member's character name. It is the same name the world save's
	// character record carries, which makes a guild the cheap way to read the
	// names of players whose saves are not being read.
	Name string
	// LastOnlineTicks is when the member was last online, measured in the same
	// clock as worldSaveData.GameTimeSaveData.RealDateTimeTicks: 100ns ticks of
	// real time since the world began, not a date. Two fixture members carry
	// exactly the world's current value, which is what identifies the clock --
	// they were online when the save was written.
	LastOnlineTicks int64
	// Role is 1 on the admin of all eight fixture guilds and 3 on the only
	// non-admin member. The values themselves are not decoded; the guild trailer
	// happens to hold byte-keyed lists for 2, 3 and 4, which is suggestive and
	// not evidence.
	Role uint8
}

// guildReservedBytes is the width of Guild.Reserved.
const guildReservedBytes = 14

// DecodeGuild decodes the guild half of a group record: pass Group.Remainder,
// and only when the sibling GroupType property says the group is a guild.
//
// An organization's four-byte remainder cannot satisfy the fixed fields, so it
// is rejected rather than silently producing an empty guild -- which is what
// makes calling this on the wrong group an error instead of a plausible lie.
//
// The result aliases data through Reserved and Trailer.
func DecodeGuild(data []byte) (Guild, error) {
	r := newRecord("guild record", data)
	// The uint32 before the base camp level is zero in all eight fixture
	// guilds, as is the one an organization's remainder consists of. Reading it
	// unnamed is deliberate: it is the same field either way, and neither value
	// is understood.
	r.u32("the uint32 before the base camp level")
	guild := Guild{
		BaseCampLevel: r.i32("BaseCampLevel"),
		BasePoints:    r.guids("BasePoints"),
		Name:          r.text("GuildName"),
		NamedBy:       r.guid("the account id before the admin"),
	}
	guild.Reserved = r.take(guildReservedBytes, "the bytes before the admin")
	guild.Admin = r.guid("AdminPlayerUid")
	if r.err == nil {
		count := r.u32("Members count")
		// A member is at least a guid, a tick count, a string length and a role
		// byte, so a count larger than the bytes left could ever hold is
		// rejected before anything is allocated.
		const memberMinimumBytes = guidBytes + 8 + 4 + 1
		if uint64(count) > uint64(len(data)-r.at)/memberMinimumBytes {
			r.fail("Members declares %d entries with %d bytes left", count, len(data)-r.at)
		} else {
			guild.Members = make([]GuildMember, 0, count)
			for i := uint32(0); i < count && r.err == nil; i++ {
				guild.Members = append(guild.Members, GuildMember{
					PlayerUID:       r.guid("a member's PlayerUid"),
					LastOnlineTicks: r.i64("a member's last online time"),
					Name:            r.text("a member's name"),
					Role:            r.u8("a member's role"),
				})
			}
		}
	}
	guild.Trailer = r.rest()
	if r.err != nil {
		return Guild{}, r.err
	}
	return guild, nil
}

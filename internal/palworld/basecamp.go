// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// BaseCamp is one entry of worldSaveData.BaseCampSaveData: a guild's base, where
// it is in the world, and which map object is its palbox.
//
// The record is a bespoke layout with no property framing:
//
//	16 bytes  Id
//	FString   Name                     -- UTF-16 in all 19 fixture camps
//	1 byte    State
//	80 bytes  Transform                -- ten float64s, rotation first
//	4 bytes   AreaRange                -- float32, 3500 in all 19
//	16 bytes  GroupID
//	80 bytes  fast-travel transform
//	16 bytes  OwnerMapObjectID
//	trailer                            -- four zero bytes in all 19
//
// Verified against all 19 camps of a 1.0.1.100619 world save. Two independent
// checks say the offsets are right rather than merely plausible: every rotation
// is a unit quaternion, and every GroupID names a guild that lists this camp in
// its own BaseIDs. Sixteen bytes read at the wrong offset can look like a GUID,
// but they cannot look like a GUID that names you back.
type BaseCamp struct {
	// ID is the camp's own id and equals the map key it was read from.
	ID gvas.GUID
	// Name is the camp's name. In the fixture world all 19 are the game's
	// Japanese default, which is also what proves the string reader handles
	// UTF-16 against real data rather than only a test fixture.
	Name string
	// State is a single byte, 1 in every fixture camp.
	State uint8
	// Transform is where the camp is. Its Translation is the camp's position in
	// world units, on the same scale as a player's LastTransform.
	Transform Transform
	// AreaRange is the camp's radius in world units, 3500 in all 19 fixture
	// camps.
	AreaRange float32
	// GroupID keys worldSaveData.GroupSaveDataMap: the guild that owns the camp.
	// All 19 fixture camps name one of the world's 8 guilds.
	GroupID gvas.GUID
	// FastTravel is a second transform, in the camp's local space. It is kept
	// because skipping 80 bytes would make the record's shape a guess, and
	// because a wrong width here would break OwnerMapObjectID -- which resolves,
	// so the width is right.
	FastTravel Transform
	// OwnerMapObjectID is the map object this camp is built around, which matches
	// an entry of the owning guild's BasePoints for all 19 fixture camps. A camp
	// is created by placing a palbox, so that is very likely what the object is --
	// but worldSaveData.MapObjectSaveData's own RawData is not decoded, so nothing
	// here can say so.
	OwnerMapObjectID gvas.GUID
	// Trailer is the remainder, aliasing the caller's slice. It is four zero
	// bytes in all 19 fixture camps.
	Trailer []byte
}

// DecodeBaseCamp decodes one BaseCampSaveData value's RawData. The result aliases
// data through Trailer, so data must not be modified afterwards.
func DecodeBaseCamp(data []byte) (BaseCamp, error) {
	r := newRecord("base camp record", data)
	camp := BaseCamp{
		ID:        r.guid("Id"),
		Name:      r.text("Name"),
		State:     r.u8("State"),
		Transform: r.transform("Transform"),
		AreaRange: r.float32("AreaRange"),
		GroupID:   r.guid("GroupIdBelongTo"),
	}
	camp.FastTravel = r.transform("FastTravelLocalTransform")
	camp.OwnerMapObjectID = r.guid("OwnerMapObjectInstanceId")
	camp.Trailer = r.rest()
	if r.err != nil {
		return BaseCamp{}, r.err
	}
	return camp, nil
}

// WorkerDirector is the WorkerDirector sub-record of a base camp, and the reason
// phase 4 reads base camps at all: it names the character container holding the
// camp's workers.
//
// That container is what closes a gap phase 3 left open. Of the fixture world's
// 3,340 pals, 3,135 sit in a player's party or storage box and 205 sit in
// nothing a player names. All 205 are in these 19 containers -- so every pal in
// the world is now accounted for, and by a second route: the guilds' character
// handles come to the same 3,340.
//
// The record is:
//
//	16 bytes  Id            -- the base camp's own id
//	80 bytes  Transform
//	2 bytes                 -- zero in all 19 fixture records
//	16 bytes  ContainerID
//	trailer                 -- four zero bytes in all 19
type WorkerDirector struct {
	// ID is the base camp this director belongs to, and is that camp's id in all
	// 19 fixture records.
	ID gvas.GUID
	// Transform is a transform whose translation sits inside the camp's area.
	Transform Transform
	// ContainerID keys worldSaveData.CharacterContainerSaveData: the camp's
	// workers. All 19 fixture values resolve to a container, and the 19
	// containers are distinct.
	ContainerID gvas.GUID
	// Reserved is the two bytes before ContainerID, zero in all 19 fixture
	// records.
	Reserved []byte
	// Trailer is the remainder, aliasing the caller's slice.
	Trailer []byte
}

// workerDirectorReservedBytes is the width of WorkerDirector.Reserved.
const workerDirectorReservedBytes = 2

// DecodeWorkerDirector decodes one BaseCampSaveData value's
// WorkerDirector.RawData. The result aliases data.
func DecodeWorkerDirector(data []byte) (WorkerDirector, error) {
	r := newRecord("base camp worker director", data)
	director := WorkerDirector{
		ID:        r.guid("Id"),
		Transform: r.transform("Transform"),
	}
	director.Reserved = r.take(workerDirectorReservedBytes, "the bytes before the container id")
	director.ContainerID = r.guid("ContainerId")
	director.Trailer = r.rest()
	if r.err != nil {
		return WorkerDirector{}, r.err
	}
	return director, nil
}

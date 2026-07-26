// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"path/filepath"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefixtures"
)

// These tests read real guild data: player names, account identifiers, base camp
// coordinates. Nothing here logs any of it -- counts, byte widths and shapes only.

// worldSave loads the fixture Level.sav.
func worldSave(t *testing.T) *gvas.Archive {
	t.Helper()
	save, err := Load(filepath.Join(savefixtures.Root(t), "Level.sav"))
	if err != nil {
		t.Fatal(err)
	}
	return save.Archive
}

// valueBlob reads a RawData payload out of a map value, optionally through one
// named sub-struct, which is how a base camp nests its worker director.
func valueBlob(t *testing.T, properties gvas.Properties, path ...string) []byte {
	t.Helper()
	for _, name := range path {
		property := properties.Find(name)
		if property == nil {
			t.Fatalf("no %s", name)
		}
		structValue, ok := property.Value.(gvas.StructValue)
		if !ok {
			t.Fatalf("%s is %T, want gvas.StructValue", name, property.Value)
		}
		properties, ok = structValue.Value.(gvas.Properties)
		if !ok {
			t.Fatalf("%s holds %T, want gvas.Properties", name, structValue.Value)
		}
	}
	rawData := properties.Find("RawData")
	if rawData == nil {
		t.Fatal("no RawData")
	}
	array, ok := rawData.Value.(gvas.ArrayValue)
	if !ok {
		t.Fatalf("RawData is %T", rawData.Value)
	}
	blob, ok := array.Values.([]byte)
	if !ok {
		t.Fatalf("RawData holds %T, want []byte", array.Values)
	}
	return blob
}

// guidKeyed iterates a map whose keys are plain GUIDs, which is how the world save
// keys its groups and its base camps.
func guidKeyed(t *testing.T, collection *gvas.MapValue, visit func(id gvas.GUID, value gvas.Properties)) int {
	t.Helper()
	count := 0
	iterator := collection.Iterator()
	for iterator.Next() {
		id, ok := iterator.Entry().Key.(gvas.GUID)
		if !ok {
			t.Fatalf("entry %d key is %T, want gvas.GUID", count, iterator.Entry().Key)
		}
		value, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			t.Fatalf("entry %d value is %T, want gvas.Properties", count, iterator.Entry().Value)
		}
		visit(id, value)
		count++
	}
	if err := iterator.Err(); err != nil {
		t.Fatalf("iteration: %v", err)
	}
	return count
}

// groupType reads the sibling property that says whether a group record carries
// the guild half.
func groupType(t *testing.T, value gvas.Properties) string {
	t.Helper()
	property := value.Find("GroupType")
	if property == nil {
		t.Fatal("a group has no GroupType")
	}
	enum, ok := property.Value.(gvas.EnumValue)
	if !ok {
		t.Fatalf("GroupType is %T, want gvas.EnumValue", property.Value)
	}
	return enum.Value
}

// fixtureGuilds is what the group pass found, kept so the base camp test can check
// the join in both directions.
type fixtureGuilds struct {
	// bases maps a base camp id to the guild that claims it.
	bases map[gvas.GUID]gvas.GUID
	// points is every base camp point the guilds list.
	points map[gvas.GUID]bool
	// ids is every guild's group id.
	ids map[gvas.GUID]bool
	// handles maps a character instance id to the guild that lists it.
	handles map[gvas.GUID]gvas.GUID
}

// TestDecodeGroupAgainstWorldSave is what makes Group and Guild claims about
// Palworld rather than about a hand-built blob.
//
// Every check here is a join rather than a shape. A record that merely decodes
// proves little: sixteen bytes read at the wrong offset are a plausible GUID. A
// record whose ids name real characters, real base camps and real accounts -- and
// are named back -- cannot be read at the wrong offset.
func TestDecodeGroupAgainstWorldSave(t *testing.T) {
	save := worldSave(t)
	characters := characterKeys(t, worldMap(t, save, "CharacterSaveParameterMap"))
	players := 0
	for _, uid := range characters {
		if !uid.IsZero() {
			players++
		}
	}

	found := &fixtureGuilds{
		bases:   map[gvas.GUID]gvas.GUID{},
		points:  map[gvas.GUID]bool{},
		ids:     map[gvas.GUID]bool{},
		handles: map[gvas.GUID]gvas.GUID{},
	}
	var (
		guilds, organizations int
		orgTypes              = map[uint8]int{}
		reserved              = map[string]int{}
		trailers              = map[string]int{}
		remainders            = map[string]int{}
		nonZeroUnknown        int
		handleCount           int
		danglingHandles       int
		playerHandles         int
		members               = map[gvas.GUID]int{}
		namedMembers          int
		roles                 = map[uint8]int{}
		defaultNames          int
		defaultNamesNamedBy   int
		levels                = map[int32]int{}
	)
	groups := guidKeyed(t, worldMap(t, save, "GroupSaveDataMap"), func(id gvas.GUID, value gvas.Properties) {
		kind := groupType(t, value)
		blob := valueBlob(t, value)
		group, err := DecodeGroup(blob)
		if err != nil {
			t.Fatalf("group %s (%d bytes): %v", id, len(blob), err)
		}
		// The record states its own id and the map states it too. Two independent
		// statements of the same fact agreeing is the cheapest evidence there is
		// that the first field starts where this thinks it does.
		if group.ID != id {
			t.Errorf("a group record calls itself %s but is keyed as %s", group.ID, id)
		}
		if group.Unknown != 0 {
			nonZeroUnknown++
		}
		for _, handle := range group.Handles {
			handleCount++
			if !handle.PlayerUID.IsZero() {
				playerHandles++
			}
			if _, ok := characters[handle.InstanceID]; !ok {
				danglingHandles++
				continue
			}
			if kind == "EPalGroupType::Guild" {
				if existing, taken := found.handles[handle.InstanceID]; taken {
					t.Errorf("the character in group %s is also in group %s", id, existing)
				}
				found.handles[handle.InstanceID] = id
			}
		}
		orgTypes[group.OrganizationType]++

		if kind != "EPalGroupType::Guild" {
			organizations++
			remainders[string(group.Remainder)]++
			// The two-call split rests on this: an organization's remainder must
			// not decode as a guild, or the CLI renderer that has no GroupType to
			// consult would report one.
			if guild, err := DecodeGuild(group.Remainder); err == nil {
				t.Errorf("the organization %s decoded a guild named %q", id, guild.Name)
			}
			return
		}

		guilds++
		found.ids[id] = true
		guild, err := DecodeGuild(group.Remainder)
		if err != nil {
			t.Fatalf("guild %s (%d bytes of guild record): %v", id, len(group.Remainder), err)
		}
		reserved[string(guild.Reserved)]++
		trailers[string(guild.Trailer)]++
		levels[guild.BaseCampLevel]++
		if len(guild.BasePoints) != len(group.BaseIDs) {
			t.Errorf("guild %s names %d base camps and %d base camp points",
				id, len(group.BaseIDs), len(guild.BasePoints))
		}
		for _, point := range guild.BasePoints {
			found.points[point] = true
		}
		for _, base := range group.BaseIDs {
			if existing, taken := found.bases[base]; taken {
				t.Errorf("the base camp claimed by guild %s is also claimed by %s", id, existing)
			}
			found.bases[base] = id
		}
		if guild.Name == "Unnamed Guild" {
			defaultNames++
			if guild.NamedBy.IsZero() {
				defaultNamesNamedBy++
			}
		} else if guild.NamedBy != guild.Admin {
			t.Errorf("guild %s is named but its NamedBy is not its admin", id)
		}
		adminIsAMember := false
		for _, member := range guild.Members {
			members[member.PlayerUID]++
			roles[member.Role]++
			if member.Name != "" {
				namedMembers++
			}
			if member.PlayerUID == guild.Admin {
				adminIsAMember = true
			}
		}
		// The admin is named twice over: as its own field and as the group's
		// internal name, which is the account id in hex.
		if !guild.Admin.IsZero() {
			if !adminIsAMember {
				t.Errorf("the admin of guild %s is not one of its members", id)
			}
			if want := bareHex(guild.Admin); group.Name != want {
				t.Errorf("guild %s has internal name %q, want the admin's id %q", id, group.Name, want)
			}
		}
	})

	if groups == 0 || guilds == 0 {
		t.Fatal("fixture world has no groups to exercise")
	}
	t.Logf("groups=%d guilds=%d organizations=%d orgTypes=%v levels=%v",
		groups, guilds, organizations, orgTypes, levels)
	t.Logf("handles=%d dangling=%d withPlayerUID=%d distinctGuildHandles=%d characters=%d players=%d",
		handleCount, danglingHandles, playerHandles, len(found.handles), len(characters), players)
	t.Logf("members=%d named=%d roles=%v defaultNames=%d ofThoseWithNoNamedBy=%d",
		len(members), namedMembers, roles, defaultNames, defaultNamesNamedBy)
	t.Logf("distinct reserved blocks=%d trailers=%d organization remainders=%d",
		len(reserved), len(trailers), len(remainders))

	// The guilds' handles are the whole character population, partitioned. Neither
	// count can be made to agree with the other by reading the record wrongly.
	if len(found.handles) != len(characters) {
		t.Errorf("the guilds list %d of the world's %d characters", len(found.handles), len(characters))
	}
	// And the handles that carry an account id are exactly the guilds' members,
	// which is the same population counted a third way.
	if playerHandles != len(members) || len(members) != players {
		t.Errorf("%d handles carry an account id, %d accounts are members, %d character records are players",
			playerHandles, len(members), players)
	}
	for uid, count := range members {
		if count > 1 {
			t.Errorf("an account is a member of %d guilds", count)
			break
		}
		_ = uid
	}
	if namedMembers != len(members) {
		t.Errorf("%d of %d members have no name", len(members)-namedMembers, len(members))
	}
	if nonZeroUnknown != 0 {
		t.Errorf("%d group records have a non-zero value where all 15 fixture records have zero", nonZeroUnknown)
	}
	// NamedBy is zero in exactly the guilds still carrying the default name. That
	// is what the field's documentation claims, and it is the whole basis for the
	// claim, so it is asserted rather than described.
	if defaultNames != defaultNamesNamedBy {
		t.Errorf("%d guilds have the default name but only %d of those have no NamedBy",
			defaultNames, defaultNamesNamedBy)
	}
	// The undecoded runs are identical across every fixture guild. If a save ever
	// varies them, the assumption that they can be preserved as opaque bytes needs
	// revisiting, and this is what says so.
	if len(reserved) != 1 || len(trailers) != 1 {
		t.Errorf("the reserved block has %d distinct values and the trailer %d, want 1 each",
			len(reserved), len(trailers))
	}
	if len(remainders) != 1 {
		t.Errorf("organizations have %d distinct remainders, want 1", len(remainders))
	}

	testBaseCampsAgainstWorldSave(t, save, found, characters)
}

// bareHex renders a GUID the way Palworld writes an account id into a group's
// internal name: 32 upper-case hex digits, no dashes.
func bareHex(id gvas.GUID) string {
	const digits = "0123456789ABCDEF"
	out := make([]byte, 0, 32)
	for _, word := range []uint32{id.A, id.B, id.C, id.D} {
		for shift := 28; shift >= 0; shift -= 4 {
			out = append(out, digits[(word>>uint(shift))&0xf])
		}
	}
	return string(out)
}

// testBaseCampsAgainstWorldSave checks the base camps against the guilds that
// claimed them. It runs from the group test rather than on its own because the
// interesting assertions are the ones that need both sides.
func testBaseCampsAgainstWorldSave(t *testing.T, save *gvas.Archive, guilds *fixtureGuilds, characters map[gvas.GUID]gvas.GUID) {
	t.Helper()
	var (
		unclaimed     int
		disagreeing   int
		unmatched     int
		nonUnitQuat   int
		states        = map[uint8]int{}
		ranges        = map[float32]int{}
		nonASCII      int
		tails         = map[string]int{}
		directorTails = map[string]int{}
		containers    = map[gvas.GUID]gvas.GUID{}
		reservedBytes = map[string]int{}
	)
	camps := guidKeyed(t, worldMap(t, save, "BaseCampSaveData"), func(id gvas.GUID, value gvas.Properties) {
		blob := valueBlob(t, value)
		camp, err := DecodeBaseCamp(blob)
		if err != nil {
			t.Fatalf("base camp %s (%d bytes): %v", id, len(blob), err)
		}
		if camp.ID != id {
			t.Errorf("a base camp calls itself %s but is keyed as %s", camp.ID, id)
		}
		// The join in both directions: the guild named this camp, and the camp
		// names the guild.
		owner, claimed := guilds.bases[id]
		switch {
		case !claimed:
			unclaimed++
		case owner != camp.GroupID:
			disagreeing++
		}
		if !guilds.points[camp.OwnerMapObjectID] {
			unmatched++
		}
		states[camp.State]++
		ranges[camp.AreaRange]++
		tails[string(camp.Trailer)]++
		for _, r := range camp.Name {
			if r > 127 {
				nonASCII++
				break
			}
		}
		// A transform read at the wrong offset would not produce a unit
		// quaternion. This is the check that the 80 bytes are what this thinks
		// they are, and it applies to both of a camp's transforms.
		for _, transform := range []Transform{camp.Transform, camp.FastTravel} {
			q := transform.Rotation
			if length := q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W; length < 0.999 || length > 1.001 {
				nonUnitQuat++
			}
		}

		directorBlob := valueBlob(t, value, "WorkerDirector")
		director, err := DecodeWorkerDirector(directorBlob)
		if err != nil {
			t.Fatalf("base camp %s worker director (%d bytes): %v", id, len(directorBlob), err)
		}
		if director.ID != id {
			t.Errorf("the worker director of base camp %s calls itself %s", id, director.ID)
		}
		directorTails[string(director.Trailer)]++
		reservedBytes[string(director.Reserved)]++
		if existing, taken := containers[director.ContainerID]; taken {
			t.Errorf("base camps %s and %s name the same worker container", id, existing)
		}
		containers[director.ContainerID] = id
	})

	if camps == 0 {
		t.Fatal("fixture world has no base camps to exercise")
	}
	t.Logf("camps=%d claimedByAGuild=%d disagreeing=%d ownerObjectsUnmatched=%d nonUnitQuaternions=%d",
		camps, camps-unclaimed, disagreeing, unmatched, nonUnitQuat)
	t.Logf("states=%v areaRanges=%v nonASCIINames=%d campTails=%d directorTails=%d directorReserved=%d",
		states, ranges, nonASCII, len(tails), len(directorTails), len(reservedBytes))

	if unclaimed != 0 {
		t.Errorf("%d base camps are not claimed by any guild", unclaimed)
	}
	if disagreeing != 0 {
		t.Errorf("%d base camps name a different guild than the one claiming them", disagreeing)
	}
	if len(guilds.bases) != camps {
		t.Errorf("the guilds name %d base camps but the world save holds %d", len(guilds.bases), camps)
	}
	// A camp's owner object is also listed by its guild as a base camp point, so
	// two records agree on a third id that neither of them owns.
	if unmatched != 0 {
		t.Errorf("%d base camps have an owner object no guild lists as a base point", unmatched)
	}
	if nonUnitQuat != 0 {
		t.Errorf("%d transforms have a rotation that is not a unit quaternion", nonUnitQuat)
	}
	if len(containers) != camps {
		t.Errorf("%d worker containers for %d camps", len(containers), camps)
	}

	// The workers themselves: every base camp's container must hold characters the
	// save actually has, and every one of them must be a pal.
	workers, unresolved, withPlayer := 0, 0, 0
	characterContainers(t, worldMap(t, save, "CharacterContainerSaveData"),
		func(containerID gvas.GUID, slot CharacterSlot, index int32) {
			if _, isWorkerBox := containers[containerID]; !isWorkerBox {
				return
			}
			workers++
			owner, ok := characters[slot.InstanceID]
			if !ok {
				unresolved++
				return
			}
			if !owner.IsZero() {
				withPlayer++
			}
		})
	t.Logf("baseCampWorkers=%d unresolved=%d withAnAccountId=%d", workers, unresolved, withPlayer)
	if workers == 0 {
		t.Error("no base camp holds a worker, so the container join proves nothing")
	}
	if unresolved != 0 {
		t.Errorf("%d base camp workers have no character record", unresolved)
	}
	if withPlayer != 0 {
		t.Errorf("%d base camp workers are players rather than pals", withPlayer)
	}
}

// characterContainers iterates every slot of every character container, handing
// the container's own id to the visitor so a caller can tell whose slots these
// are.
func characterContainers(t *testing.T, collection *gvas.MapValue, visit func(id gvas.GUID, slot CharacterSlot, index int32)) {
	t.Helper()
	iterator := collection.Iterator()
	for iterator.Next() {
		key, ok := iterator.Entry().Key.(gvas.Properties)
		if !ok {
			t.Fatalf("container key is %T, want gvas.Properties", iterator.Entry().Key)
		}
		id := keyGUID(t, key, "ID")
		value, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			t.Fatalf("container value is %T, want gvas.Properties", iterator.Entry().Value)
		}
		slots := value.Find("Slots")
		if slots == nil {
			t.Fatal("a character container has no Slots")
		}
		array, ok := slots.Value.(gvas.ArrayValue)
		if !ok || array.Structs == nil {
			t.Fatalf("Slots is %T without struct elements", slots.Value)
		}
		elements := array.Structs.Iterator()
		for elements.Next() {
			properties, ok := elements.Value().(gvas.Properties)
			if !ok {
				t.Fatalf("a slot is %T", elements.Value())
			}
			slot, err := DecodeCharacterSlot(valueBlob(t, properties))
			if err != nil {
				t.Fatalf("a slot of container %s: %v", id, err)
			}
			index := int32(0)
			if property := properties.Find("SlotIndex"); property != nil {
				index, _ = property.Value.(int32)
			}
			visit(id, slot, index)
		}
		if err := elements.Err(); err != nil {
			t.Fatalf("slot iteration: %v", err)
		}
	}
	if err := iterator.Err(); err != nil {
		t.Fatalf("container iteration: %v", err)
	}
}

// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"fmt"
	"sort"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/palworld"
)

// The world-save collections a guild resolve reads, in the order it reads them.
// That order is a dependency chain and it is close to the reverse of the file's:
// the character records come first in the save and last here, because nothing
// knows which of them matter until the groups, the base camps and the worker
// containers have all been read.
const (
	groupCollection     = "GroupSaveDataMap"
	baseCampCollection  = "BaseCampSaveData"
	containerCollection = "CharacterContainerSaveData"
	characterCollection = "CharacterSaveParameterMap"
)

// guildGroupType is the GroupType value a guild carries. The other value in the
// fixture world is EPalGroupType::Organization, which is one of the world's fixed
// factions rather than anything a player belongs to.
const guildGroupType = "Guild"

// Guild resolves one guild by its group id.
//
// The id is the one a player document reports as its guild, and the one the
// character records carry in their framing. Nothing else identifies a guild: a
// name is a display string players can duplicate or leave unset.
func (r *Resolver) Guild(id gvas.GUID) (*Guild, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("resolve: a guild id is required")
	}
	var found *Guild
	err := r.resolveGuilds(map[gvas.GUID]bool{id: true}, func(guild *Guild) error {
		found = guild
		return nil
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf(
			"resolve: the world save has no guild with the group id %s", id)
	}
	return found, nil
}

// Guilds resolves every guild in the world save, calling visit once per guild in
// the order the save lists them.
func (r *Resolver) Guilds(visit func(*Guild) error) error {
	return r.resolveGuilds(nil, visit)
}

// resolveGuilds is the pass both entry points share. wanted selects guilds by
// group id; nil resolves all of them.
//
// Like a player resolve, nothing is emitted until every collection has been
// read: a guild's bases are named by one collection, described by a second,
// populated by a third and detailed by a fourth. Streaming the first guild before
// the last collection is read would mean emitting bases with no workers in them.
func (r *Resolver) resolveGuilds(wanted map[gvas.GUID]bool, visit func(*Guild) error) error {
	scan, err := r.readGroups(wanted)
	if err != nil {
		return err
	}
	if len(scan.order) == 0 {
		return nil
	}
	if err := scan.readBases(r); err != nil {
		return err
	}
	if err := scan.placeWorkers(r); err != nil {
		return err
	}
	if err := scan.readWorkers(r); err != nil {
		return err
	}
	for _, guild := range scan.order {
		guild.finish()
		if err := visit(guild.document); err != nil {
			return err
		}
	}
	return nil
}

// guildScan is one guild being resolved: the document so far, and the bases still
// to be found.
type guildScan struct {
	document *Guild
	// bases is indexed by base camp id, so a camp found in the world save can be
	// matched to the guild that claimed it.
	bases map[gvas.GUID]*Base
	// baseOrder keeps the guild's own order of its base ids, so output does not
	// depend on the order the base camp collection happens to be in.
	baseOrder []gvas.GUID
}

func (g *guildScan) warn(format string, arguments ...any) {
	g.document.Warnings = append(g.document.Warnings, fmt.Sprintf(format, arguments...))
}

// finish puts the bases in the guild's own order and each base's workers in slot
// order, and counts what was found.
func (g *guildScan) finish() {
	for _, id := range g.baseOrder {
		base := g.bases[id]
		if base == nil {
			continue
		}
		sort.SliceStable(base.Workers, func(left, right int) bool {
			return base.Workers[left].Slot < base.Workers[right].Slot
		})
		g.document.Bases = append(g.document.Bases, *base)
		g.document.Counts.Workers += len(base.Workers)
	}
	g.document.Counts.Bases = len(g.document.Bases)
}

// workerTarget is one pal a base camp's container places, before the record that
// describes it has been read.
type workerTarget struct {
	base *Base
	slot int32
}

// guildScanState is the state of one pass over the world save. Every wanted id
// set is built from the collection that names it before the collection that
// describes it is touched, which is memory rule R2.
type guildScanState struct {
	order []*guildScan
	// camps maps a base camp id to the guild that claimed it.
	camps map[gvas.GUID]*guildScan
	// containers maps a worker container id to the base that named it.
	containers map[gvas.GUID]*Base
	// containerOwner keeps the guild a container belongs to, for warnings.
	containerOwner map[gvas.GUID]*guildScan
	// workers maps a character instance id to the base slot holding it.
	workers map[gvas.GUID]workerTarget
	// workerOwner keeps the guild a worker belongs to, for warnings.
	workerOwner map[gvas.GUID]*guildScan
}

// readGroups reads GroupSaveDataMap and builds the half of each guild that comes
// from its own record: name, admin, members and population.
//
// Organizations are skipped. They share the record's first half but are the
// world's fixed factions rather than player guilds, and asking for one by id is
// an error rather than an empty answer -- the id came from somewhere, and
// "resolved to nothing" would hide the mistake.
func (r *Resolver) readGroups(wanted map[gvas.GUID]bool) (*guildScanState, error) {
	groups, err := r.worldMap(groupCollection)
	if err != nil {
		return nil, err
	}
	scan := &guildScanState{
		camps:          map[gvas.GUID]*guildScan{},
		containers:     map[gvas.GUID]*Base{},
		containerOwner: map[gvas.GUID]*guildScan{},
		workers:        map[gvas.GUID]workerTarget{},
		workerOwner:    map[gvas.GUID]*guildScan{},
	}
	iterator := groups.Iterator()
	for iterator.Next() {
		id, ok := iterator.Entry().Key.(gvas.GUID)
		if !ok {
			continue
		}
		if wanted != nil && !wanted[id] {
			continue
		}
		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			continue
		}
		kind, _ := enumName(properties, "GroupType")
		if kind != guildGroupType {
			if wanted != nil {
				return nil, fmt.Errorf(
					"resolve: the group %s is a %s, not a guild", id, orGroup(kind))
			}
			continue
		}
		data, ok := blob(properties, "RawData")
		if !ok {
			return nil, fmt.Errorf("resolve: the group %s has no RawData", id)
		}
		guild, err := newGuildScan(id, data)
		if err != nil {
			return nil, err
		}
		scan.order = append(scan.order, guild)
		for _, campID := range guild.baseOrder {
			if existing, taken := scan.camps[campID]; taken {
				guild.warn("the base camp %s is already claimed by the guild %s",
					campID, existing.document.GroupID)
				continue
			}
			scan.camps[campID] = guild
		}
	}
	if err := iterator.Err(); err != nil {
		return nil, fmt.Errorf("resolve: groups: %w", err)
	}
	return scan, nil
}

// orGroup names an unexpected group type for an error message.
func orGroup(kind string) string {
	if kind == "" {
		return "group of no stated type"
	}
	return kind
}

// newGuildScan decodes one group record into the guild half of a document.
func newGuildScan(id gvas.GUID, data []byte) (*guildScan, error) {
	group, err := palworld.DecodeGroup(data)
	if err != nil {
		return nil, fmt.Errorf("resolve: guild %s: %w", id, err)
	}
	record, err := palworld.DecodeGuild(group.Remainder)
	if err != nil {
		return nil, fmt.Errorf("resolve: guild %s: %w", id, err)
	}
	document := &Guild{
		GroupID:       id,
		Name:          record.Name,
		BaseCampLevel: record.BaseCampLevel,
		Members:       []GuildMember{},
		Bases:         []Base{},
	}
	if !record.Admin.IsZero() {
		admin := record.Admin
		document.Admin = &admin
	}
	for _, member := range record.Members {
		document.Members = append(document.Members, newGuildMember(member))
	}
	// The group's own id and the map key are two statements of the same fact, so
	// a disagreement is worth reporting rather than preferring one silently.
	guild := &guildScan{document: document, bases: map[gvas.GUID]*Base{}}
	if group.ID != id {
		guild.warn("the group record calls itself %s but the world save keys it as %s",
			group.ID, id)
	}
	for _, handle := range group.Handles {
		document.Counts.Characters++
		if handle.PlayerUID.IsZero() {
			document.Counts.Pals++
		} else {
			document.Counts.Players++
		}
	}
	if document.Counts.Players != len(document.Members) {
		guild.warn("the guild lists %d members but %d of its characters carry an account id",
			len(document.Members), document.Counts.Players)
	}
	if len(record.BasePoints) != len(group.BaseIDs) {
		guild.warn("the guild names %d base camps and %d base camp points",
			len(group.BaseIDs), len(record.BasePoints))
	}
	for _, campID := range group.BaseIDs {
		if campID.IsZero() {
			continue
		}
		if guild.bases[campID] != nil {
			guild.warn("the guild names the base camp %s twice", campID)
			continue
		}
		guild.bases[campID] = &Base{ID: campID, Workers: []Pal{}}
		guild.baseOrder = append(guild.baseOrder, campID)
	}
	return guild, nil
}

func newGuildMember(member palworld.GuildMember) GuildMember {
	out := GuildMember{
		PlayerUID: member.PlayerUID,
		Name:      member.Name,
		Role:      member.Role,
	}
	if member.LastOnlineTicks != 0 {
		lastOnline := elapsed(member.LastOnlineTicks)
		out.LastOnline = &lastOnline
	}
	return out
}

// readBases reads the base camps the guilds named, and each camp's worker
// director, which is what names the container holding the camp's workers.
func (s *guildScanState) readBases(r *Resolver) error {
	camps, err := r.worldMap(baseCampCollection)
	if err != nil {
		return err
	}
	found := map[gvas.GUID]bool{}
	iterator := camps.Iterator()
	for iterator.Next() {
		id, ok := iterator.Entry().Key.(gvas.GUID)
		if !ok {
			continue
		}
		guild, wanted := s.camps[id]
		if !wanted {
			continue
		}
		found[id] = true
		base := guild.bases[id]

		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			guild.warn("the base camp %s is not a property list", id)
			continue
		}
		data, ok := blob(properties, "RawData")
		if !ok {
			guild.warn("the base camp %s has no RawData", id)
			continue
		}
		camp, err := palworld.DecodeBaseCamp(data)
		if err != nil {
			guild.warn("the base camp %s did not decode: %v", id, err)
			continue
		}
		base.Name = camp.Name
		base.AreaRange = camp.AreaRange
		base.Location = &Position{
			X: camp.Transform.Translation.X,
			Y: camp.Transform.Translation.Y,
			Z: camp.Transform.Translation.Z,
		}
		if !camp.OwnerMapObjectID.IsZero() {
			owner := camp.OwnerMapObjectID
			base.OwnerMapObjectID = &owner
		}
		// The guild named this camp and the camp names a guild back. Both are
		// read, so a disagreement is a finding rather than a silent choice.
		if camp.GroupID != guild.document.GroupID {
			guild.warn("the base camp %s says it belongs to the group %s", id, camp.GroupID)
		}

		director, ok := field(properties, "WorkerDirector").(gvas.Properties)
		if !ok {
			guild.warn("the base camp %s has no worker director, so its workers are unknown", id)
			continue
		}
		directorData, ok := blob(director, "RawData")
		if !ok {
			guild.warn("the base camp %s worker director has no RawData", id)
			continue
		}
		worker, err := palworld.DecodeWorkerDirector(directorData)
		if err != nil {
			guild.warn("the base camp %s worker director did not decode: %v", id, err)
			continue
		}
		if worker.ContainerID.IsZero() {
			guild.warn("the base camp %s names no worker container", id)
			continue
		}
		if existing, taken := s.containers[worker.ContainerID]; taken {
			guild.warn("the base camp %s names the worker container %s, which the base camp %s also names",
				id, worker.ContainerID, existing.ID)
			continue
		}
		s.containers[worker.ContainerID] = base
		s.containerOwner[worker.ContainerID] = guild
	}
	if err := iterator.Err(); err != nil {
		return fmt.Errorf("resolve: base camps: %w", err)
	}
	for id, guild := range s.camps {
		if !found[id] {
			guild.warn("the base camp %s is not in the world save", id)
		}
	}
	return nil
}

// placeWorkers reads the base camps' worker containers and turns their slots into
// the set of character records worth decoding.
func (s *guildScanState) placeWorkers(r *Resolver) error {
	if len(s.containers) == 0 {
		return nil
	}
	containers, err := r.worldMap(containerCollection)
	if err != nil {
		return err
	}
	found := map[gvas.GUID]bool{}
	iterator := containers.Iterator()
	for iterator.Next() {
		key, ok := iterator.Entry().Key.(gvas.Properties)
		if !ok {
			continue
		}
		id, ok := guid(key, "ID")
		if !ok {
			continue
		}
		base, wanted := s.containers[id]
		if !wanted {
			continue
		}
		found[id] = true
		guild := s.containerOwner[id]

		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			guild.warn("the worker container of the base camp %s is not a property list", base.ID)
			continue
		}
		slots, ok := structArray(properties, "Slots")
		if !ok {
			guild.warn("the worker container of the base camp %s has no slots", base.ID)
			continue
		}
		elements := slots.Iterator()
		for elements.Next() {
			slotProperties, ok := elements.Value().(gvas.Properties)
			if !ok {
				continue
			}
			data, ok := blob(slotProperties, "RawData")
			if !ok {
				guild.warn("a worker slot of the base camp %s has no RawData", base.ID)
				continue
			}
			slot, err := palworld.DecodeCharacterSlot(data)
			if err != nil {
				guild.warn("a worker slot of the base camp %s did not decode: %v", base.ID, err)
				continue
			}
			if slot.Empty() {
				continue
			}
			index, _ := value[int32](slotProperties, "SlotIndex")
			s.workers[slot.InstanceID] = workerTarget{base: base, slot: index}
			s.workerOwner[slot.InstanceID] = guild
		}
		if err := elements.Err(); err != nil {
			guild.warn("the worker slots of the base camp %s did not decode: %v", base.ID, err)
		}
	}
	if err := iterator.Err(); err != nil {
		return fmt.Errorf("resolve: character containers: %w", err)
	}
	for id, base := range s.containers {
		if !found[id] {
			s.containerOwner[id].warn(
				"the worker container %s of the base camp %s is not in the world save", id, base.ID)
		}
	}
	return nil
}

// readWorkers is the expensive pass, and the one the three before it exist to
// make cheap: every character record in the world is visited, and only the ones a
// base camp placed are decoded.
func (s *guildScanState) readWorkers(r *Resolver) error {
	if len(s.workers) == 0 {
		return nil
	}
	characters, err := r.worldMap(characterCollection)
	if err != nil {
		return err
	}
	found := map[gvas.GUID]bool{}
	iterator := characters.Iterator()
	for iterator.Next() {
		key, ok := iterator.Entry().Key.(gvas.Properties)
		if !ok {
			continue
		}
		instance, _ := guid(key, "InstanceId")
		target, wanted := s.workers[instance]
		if !wanted {
			continue
		}
		found[instance] = true
		guild := s.workerOwner[instance]

		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			continue
		}
		data, ok := blob(properties, "RawData")
		if !ok {
			guild.warn("the record of the worker in base camp %s slot %d has no RawData",
				target.base.ID, target.slot)
			continue
		}
		character, err := palworld.DecodeCharacterWithOptions(data, r.options.Save.GVAS)
		if err != nil {
			guild.warn("the record of the worker in base camp %s slot %d did not decode: %v",
				target.base.ID, target.slot, err)
			continue
		}
		parameters := character.Parameters()
		if parameters == nil {
			guild.warn("the record of the worker in base camp %s slot %d has no SaveParameter",
				target.base.ID, target.slot)
			continue
		}
		target.base.Workers = append(target.base.Workers, newPal(
			instance,
			palTarget{location: PalAtBase, slot: target.slot},
			parameters,
		))
	}
	if err := iterator.Err(); err != nil {
		return fmt.Errorf("resolve: character records: %w", err)
	}
	// A slot that references a character the save no longer holds is not
	// hypothetical -- 7 of the fixture world's 3,347 slot references name no
	// record -- but all 7 are in containers nothing points at, so no base camp
	// reaches one. Saying so anyway is the difference between a base with fewer
	// workers than slots and a base whose workers went missing.
	for instance, target := range s.workers {
		if !found[instance] {
			s.workerOwner[instance].warn(
				"the base camp %s has a worker in slot %d, but the world save has no character record for %s",
				target.base.ID, target.slot, instance)
		}
	}
	return nil
}

// guildNames fills in the name and member count of the guilds a set of player
// documents refer to.
//
// This is a separate pass rather than part of the guild resolve because it
// answers a different question: a player resolve knows a group id and wants a
// label for it, not the group's bases and workers. Only the wanted records are
// decoded, and there are 15 groups in the fixture world against 3,349 character
// records, so the pass is the cheapest of the resolve.
func (r *Resolver) guildNames(wanted map[gvas.GUID][]*GuildRef) error {
	if len(wanted) == 0 {
		return nil
	}
	groups, err := r.worldMap(groupCollection)
	if err != nil {
		return err
	}
	iterator := groups.Iterator()
	for iterator.Next() {
		id, ok := iterator.Entry().Key.(gvas.GUID)
		if !ok {
			continue
		}
		references := wanted[id]
		if len(references) == 0 {
			continue
		}
		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			continue
		}
		if kind, _ := enumName(properties, "GroupType"); kind != guildGroupType {
			continue
		}
		data, ok := blob(properties, "RawData")
		if !ok {
			continue
		}
		group, err := palworld.DecodeGroup(data)
		if err != nil {
			continue
		}
		record, err := palworld.DecodeGuild(group.Remainder)
		if err != nil {
			continue
		}
		for _, reference := range references {
			reference.Name = record.Name
			reference.MemberCount = len(record.Members)
		}
	}
	if err := iterator.Err(); err != nil {
		return fmt.Errorf("resolve: groups: %w", err)
	}
	return nil
}

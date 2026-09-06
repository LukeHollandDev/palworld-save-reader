// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"fmt"
	"sort"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/palworld"
)

// The player-save fields a resolve reads. Naming them here keeps the paths that
// encode Palworld's schema in one visible list rather than scattered through the
// code that uses them, which is the list to check against a new game build.
const (
	playerRoot           = "SaveData"
	playerUIDPath        = playerRoot + ".PlayerUId"
	playerInstancePath   = playerRoot + ".IndividualId.InstanceId"
	playerPlatformPath   = playerRoot + ".PlayerPlatform"
	playerLastOnlinePath = playerRoot + ".LastOnlineDateTime"
	playerPositionPath   = playerRoot + ".LastTransform.Translation"
	playerTechnologyPath = playerRoot + ".TechnologyPoint"
	playerFastTravelPath = playerRoot + ".RecordData.FastTravelPointUnlockFlag"
	playerAreaPath       = playerRoot + ".RecordData.FindAreaFlagMap"
	playerBossPath       = playerRoot + ".RecordData.NormalBossDefeatFlag"
	playerTowerPath      = playerRoot + ".RecordData.TowerBossDefeatFlag"
	playerNotePath       = playerRoot + ".RecordData.NoteObtainForInstanceFlag"
	playerRelicPath      = playerRoot + ".RecordData.RelicObtainForInstanceFlag"
	playerItemPickupPath = playerRoot + ".RecordData.ItemPickupObtainForInstanceFlag"
	playerPartyPath      = playerRoot + ".OtomoCharacterContainerId.ID"
	playerStoragePath    = playerRoot + ".PalStorageContainerId.ID"
	inventoryRoot        = playerRoot + ".InventoryInfo"
)

// The character-record fields a resolve reads, relative to the
// PalIndividualCharacterSaveParameter that palworld.Character.Parameters returns.
const (
	recordIsPlayer  = "IsPlayer"
	recordNickname  = "NickName"
	recordSpecies   = "CharacterID"
	recordGender    = "Gender"
	recordLevel     = "Level"
	recordExp       = "Exp"
	recordHP        = "Hp.Value"
	recordStomach   = "FullStomach"
	recordPassives  = "PassiveSkillList"
	recordArenaRP   = "ArenaRankPoint"
	recordTalentHP  = "Talent_HP"
	recordTalentSho = "Talent_Shot"
	recordTalentDef = "Talent_Defense"
)

// defaultLevel is what an absent Level property means. Unreal omits a property
// whose value equals its default, and this one is not guesswork: across the 2,259
// character records of the fixture world Level is present 1,808 times and its
// smallest present value is 2 -- never 0, never 1. Exp is absent 442 times and
// never present as zero, so its default is 0 and needs no constant.
const defaultLevel = 1

// Player resolves one player by their account id, joining their save to the world
// save.
//
// The id is matched against what each player.sav says its PlayerUId is, not
// against the file name. Palworld does name the file after the id, but the file
// name is a convention and the property is the fact.
func (r *Resolver) Player(uid gvas.GUID) (*Player, error) {
	if uid.IsZero() {
		return nil, fmt.Errorf("resolve: a player id is required")
	}
	var found *Player
	err := r.resolve(map[gvas.GUID]bool{uid: true}, func(player *Player) error {
		found = player
		return nil
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("resolve: no player save in %s has the id %s", r.set.Directory, uid)
	}
	return found, nil
}

// Players resolves every player with a save file in the set, calling visit once
// per player in file order.
//
// visit is called after the world save has been read rather than during, so a
// caller may encode each document as it arrives without holding the whole set as
// JSON. The scan itself cannot be streamed: a pal is placed by one collection and
// described by another.
func (r *Resolver) Players(visit func(*Player) error) error {
	return r.resolve(nil, visit)
}

// resolve is the single pass over the world save that both entry points share.
// wanted selects players by account id; nil resolves every save in the set.
func (r *Resolver) resolve(wanted map[gvas.GUID]bool, visit func(*Player) error) error {
	scan, err := r.readPlayerSaves(wanted)
	if err != nil {
		return err
	}
	if len(scan.order) == 0 {
		return nil
	}
	// Order matters, and it is not the file's order. The character containers say
	// which of the world's 2,259 character records are wanted, so they are read
	// first even though they are serialized later. Each collection is lazy and
	// independently addressed, so reading them out of order costs nothing.
	if err := scan.placePals(r); err != nil {
		return err
	}
	if err := scan.readCharacters(r); err != nil {
		return err
	}
	if err := scan.readInventories(r); err != nil {
		return err
	}
	// Last, because it depends on what the character records said: the group ids
	// are not known until they have been read.
	if err := r.guildNames(scan.guilds()); err != nil {
		return err
	}
	for _, player := range scan.order {
		player.finish()
		if err := visit(player.document); err != nil {
			return err
		}
	}
	return nil
}

// playerScan is one player being resolved: the document so far, plus what has to
// be found in the world save to finish it.
type playerScan struct {
	document *Player
	// instance is the character the player save claims to be. The record is found
	// by account id, so this is the cross-check that the two halves agree.
	instance gvas.GUID
	// party and storage are the two character containers the player save names.
	party   gvas.GUID
	storage gvas.GUID
	// inventory pairs each item container the player save named with the part of
	// the document its stacks belong to.
	inventory []itemSource
	// recorded guards against a second character record claiming the same
	// account, which would mean the map key is not the identity it looks like.
	recorded bool
}

func (p *playerScan) warn(format string, arguments ...any) {
	p.document.Warnings = append(p.document.Warnings, fmt.Sprintf(format, arguments...))
}

// finish puts the collected pals in a stable order. The world save lists
// character records in an order that is neither party-first nor by slot, and
// output that reshuffles between runs is hard to diff.
func (p *playerScan) finish() {
	pals := p.document.Pals
	sort.SliceStable(pals, func(left, right int) bool {
		if pals[left].Location != pals[right].Location {
			// Party before storage, rather than alphabetically.
			return pals[left].Location == PalInParty
		}
		return pals[left].Slot < pals[right].Slot
	})
}

// itemSource is one item container a player save named, and where its stacks
// belong in the document. The pointer is into the Inventory field itself, so the
// scan fills the document in place.
type itemSource struct {
	name   string
	id     gvas.GUID
	stacks *[]ItemStack
}

// containerTarget is a character container being looked for, and what its slots
// mean for the player that named it.
type containerTarget struct {
	owner    *playerScan
	location string
}

// palTarget is one pal the scan is looking for. A slot places it before the
// record describing it has been read, which is the whole point of reading the
// containers first.
type palTarget struct {
	owner    *playerScan
	location string
	slot     int32
}

// scan is the state of one pass over the world save. Every wanted id set is built
// from the player saves before the world save is touched: that is memory rule R2,
// and it is what stops a resolve from decoding all 2,259 character records to find
// the handful it needs.
type scan struct {
	order      []*playerScan
	byUID      map[gvas.GUID]*playerScan
	items      map[gvas.GUID]itemSource
	itemOwner  map[gvas.GUID]*playerScan
	containers map[gvas.GUID]containerTarget
	pals       map[gvas.GUID]palTarget
}

// guilds collects the guild references the character records produced, grouped by
// group id so one group record answers for every player in it.
func (s *scan) guilds() map[gvas.GUID][]*GuildRef {
	wanted := map[gvas.GUID][]*GuildRef{}
	for _, player := range s.order {
		if reference := player.document.Guild; reference != nil {
			wanted[reference.ID] = append(wanted[reference.ID], reference)
		}
	}
	return wanted
}

// readPlayerSaves parses the player saves, builds the half of each document that
// comes from them, and collects the ids the world save has to fill in.
//
// Every save in the set is parsed even when one id is wanted, because the id is
// read from inside the file rather than taken from its name. A player.sav is a few
// hundred kilobytes against a world save's hundreds of megabytes, so this is not
// the cost worth optimising away.
func (r *Resolver) readPlayerSaves(wanted map[gvas.GUID]bool) (*scan, error) {
	scan := &scan{
		byUID:      map[gvas.GUID]*playerScan{},
		items:      map[gvas.GUID]itemSource{},
		itemOwner:  map[gvas.GUID]*playerScan{},
		containers: map[gvas.GUID]containerTarget{},
		pals:       map[gvas.GUID]palTarget{},
	}
	for _, path := range r.set.Players {
		save, err := palworld.LoadWithOptions(path, r.options.Save)
		if err != nil {
			return nil, fmt.Errorf("resolve: player save %s: %w", path, err)
		}
		uid, ok := guid(save.Properties, playerUIDPath)
		if !ok {
			return nil, fmt.Errorf("resolve: player save %s has no %s", path, playerUIDPath)
		}
		if wanted != nil && !wanted[uid] {
			continue
		}
		if scan.byUID[uid] != nil {
			return nil, fmt.Errorf(
				"resolve: two player saves in %s claim the id %s",
				r.set.Directory, uid,
			)
		}
		player := newPlayerScan(save.Properties, uid)
		scan.order = append(scan.order, player)
		scan.byUID[uid] = player
		scan.want(player)
	}
	return scan, nil
}

// newPlayerScan reads everything a player.sav can answer on its own.
func newPlayerScan(properties gvas.Properties, uid gvas.GUID) *playerScan {
	document := &Player{
		PlayerUID: uid,
		Inventory: Inventory{
			// Present but empty rather than absent, so a consumer can index the
			// six containers without checking each one.
			Common:    []ItemStack{},
			DropSlot:  []ItemStack{},
			Essential: []ItemStack{},
			Weapons:   []ItemStack{},
			Armor:     []ItemStack{},
			Food:      []ItemStack{},
		},
		Pals: []Pal{},
	}
	player := &playerScan{document: document}
	readPlayerProgress(properties, document)

	if instance, ok := guid(properties, playerInstancePath); ok && !instance.IsZero() {
		player.instance = instance
		document.InstanceID = &instance
	}
	if platform, ok := enumName(properties, playerPlatformPath); ok {
		document.Platform = platform
	}
	if lastOnline, ok := stamp(properties, playerLastOnlinePath); ok {
		document.LastOnline = &lastOnline
	}
	if position, ok := value[gvas.Vector](properties, playerPositionPath); ok {
		document.Position = &Position{X: position.X, Y: position.Y, Z: position.Z}
	}
	if points, ok := value[int32](properties, playerTechnologyPath); ok {
		document.TechnologyPoints = &points
	}
	player.party, _ = guid(properties, playerPartyPath)
	player.storage, _ = guid(properties, playerStoragePath)

	// The mapping this package exists to encode: which property of a player's
	// InventoryInfo names which part of their inventory. The names on the left are
	// the tool's; the paths on the right are Palworld's.
	for _, source := range []struct {
		name   string
		path   string
		stacks *[]ItemStack
	}{
		{"common", inventoryRoot + ".CommonContainerId.ID", &document.Inventory.Common},
		{"dropSlot", inventoryRoot + ".DropSlotContainerId.ID", &document.Inventory.DropSlot},
		{"essential", inventoryRoot + ".EssentialContainerId.ID", &document.Inventory.Essential},
		{"weapons", inventoryRoot + ".WeaponLoadOutContainerId.ID", &document.Inventory.Weapons},
		{"armor", inventoryRoot + ".PlayerEquipArmorContainerId.ID", &document.Inventory.Armor},
		{"food", inventoryRoot + ".FoodEquipContainerId.ID", &document.Inventory.Food},
	} {
		id, ok := guid(properties, source.path)
		if !ok || id.IsZero() {
			player.warn("the player save names no %s inventory container", source.name)
			continue
		}
		player.inventory = append(player.inventory, itemSource{
			name:   source.name,
			id:     id,
			stacks: source.stacks,
		})
	}
	return player
}

// want registers the world-save ids one player needs.
func (s *scan) want(player *playerScan) {
	// A container belongs to one player. Refusing to overwrite rather than
	// silently reassigning means a save set that breaks that assumption produces a
	// warning instead of one player quietly inheriting another's things.
	for _, source := range player.inventory {
		if existing, taken := s.items[source.id]; taken {
			player.warn("the %s inventory container %s is already claimed as another player's %s",
				source.name, source.id, existing.name)
			continue
		}
		s.items[source.id] = source
		s.itemOwner[source.id] = player
	}
	for _, container := range []struct {
		id       gvas.GUID
		location string
	}{
		{player.party, PalInParty},
		{player.storage, PalInStorage},
	} {
		if container.id.IsZero() {
			player.warn("the player save names no %s pal container", container.location)
			continue
		}
		if existing, taken := s.containers[container.id]; taken {
			player.warn("the %s pal container %s is already claimed as another player's %s",
				container.location, container.id, existing.location)
			continue
		}
		s.containers[container.id] = containerTarget{owner: player, location: container.location}
	}
}

// placePals reads the character containers the players named and turns their slots
// into the set of pal records worth decoding. The slots hold references, not pals,
// which is why this is a separate pass from reading the records.
func (s *scan) placePals(r *Resolver) error {
	containers, err := r.worldMap("CharacterContainerSaveData")
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
		target, wanted := s.containers[id]
		if !wanted {
			continue
		}
		found[id] = true

		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			target.owner.warn("the %s pal container is not a property list", target.location)
			continue
		}
		slots, ok := structArray(properties, "Slots")
		if !ok {
			target.owner.warn("the %s pal container has no slots", target.location)
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
				target.owner.warn("a slot of the %s pal container has no RawData", target.location)
				continue
			}
			slot, err := palworld.DecodeCharacterSlot(data)
			if err != nil {
				target.owner.warn("a slot of the %s pal container did not decode: %v", target.location, err)
				continue
			}
			if slot.Empty() {
				continue
			}
			// The slot index is a property of the slot rather than part of the
			// reference, so it is read from the property list.
			index, _ := value[int32](slotProperties, "SlotIndex")
			s.pals[slot.InstanceID] = palTarget{
				owner:    target.owner,
				location: target.location,
				slot:     index,
			}
		}
		if err := elements.Err(); err != nil {
			target.owner.warn("the %s pal container's slots did not decode: %v", target.location, err)
		}
	}
	if err := iterator.Err(); err != nil {
		return fmt.Errorf("resolve: character containers: %w", err)
	}
	for id, target := range s.containers {
		if !found[id] {
			target.owner.warn("the %s pal container %s is not in the world save", target.location, id)
		}
	}
	return nil
}

// readCharacters is the pass that pays for itself. Every one of the world's
// character records is visited, but only the wanted ones are decoded: the map key
// carries the identity, and the ~3.4KB nested property stream beside it is left
// untouched unless a player or one of their pals is behind it.
func (s *scan) readCharacters(r *Resolver) error {
	characters, err := r.worldMap("CharacterSaveParameterMap")
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
		uid, _ := guid(key, "PlayerUId")

		pal, isPal := s.pals[instance]
		owner := s.byUID[uid]
		isPlayer := owner != nil && !uid.IsZero()
		if !isPal && !isPlayer {
			continue
		}
		if isPal {
			found[instance] = true
		}

		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			continue
		}
		data, ok := blob(properties, "RawData")
		if !ok {
			continue
		}
		character, err := palworld.DecodeCharacterWithOptions(data, r.options.Save.GVAS)
		if err != nil {
			s.recordWarning(pal, owner, isPal, "did not decode: %v", err)
			continue
		}
		parameters := character.Parameters()
		if parameters == nil {
			s.recordWarning(pal, owner, isPal, "has no SaveParameter")
			continue
		}
		if isPal {
			pal.owner.document.Pals = append(pal.owner.document.Pals, newPal(instance, pal, parameters))
			continue
		}
		owner.readRecord(instance, character, parameters)
	}
	if err := iterator.Err(); err != nil {
		return fmt.Errorf("resolve: character records: %w", err)
	}
	for _, player := range s.order {
		if !player.recorded {
			player.warn("the world save has no character record for this player, so no name, level or guild is available")
		}
	}
	// A container slot that references a character the save no longer holds was
	// silently dropped until phase 4, which is when it became clear the case is
	// real: the fixture world has 3,347 slot references, and 7 of them name no
	// record. Those 7 sit in containers nothing points at, so a resolve never
	// reaches them -- every reference a player or a base camp makes does resolve.
	// The warning is here because the difference between "no pal" and "the pal is
	// gone" belongs in the output rather than in this comment.
	for instance, target := range s.pals {
		if !found[instance] {
			target.owner.warn(
				"%s slot %d holds the pal %s, but the world save has no character record for it",
				target.location, target.slot, instance)
		}
	}
	return nil
}

// recordWarning attaches a character-record problem to whichever player it
// belongs to.
func (s *scan) recordWarning(pal palTarget, owner *playerScan, isPal bool, format string, arguments ...any) {
	subject := "this player's character record "
	target := owner
	if isPal {
		subject = fmt.Sprintf("the record of the pal in %s slot %d ", pal.location, pal.slot)
		target = pal.owner
	}
	if target != nil {
		target.warn(subject+format, arguments...)
	}
}

// readRecord fills in the half of a player that only the world save knows.
func (p *playerScan) readRecord(instance gvas.GUID, character palworld.Character, parameters gvas.Properties) {
	if p.recorded {
		p.warn("the world save has more than one character record for this player; the first was used")
		return
	}
	p.recorded = true

	// The player save named a character instance, and the record was found by
	// account id. Disagreement means one of the two identities is not what it
	// looks like, which is worth saying rather than silently preferring one.
	if !p.instance.IsZero() && p.instance != instance {
		p.warn("the character record found for this account is instance %s, but the player save names %s",
			instance, p.instance)
	}
	if flag, ok := value[bool](parameters, recordIsPlayer); !ok || !flag {
		p.warn("the character record for this account is not flagged as a player")
	}

	document := p.document
	record := &Character{Level: defaultLevel}
	record.Nickname, _ = value[string](parameters, recordNickname)
	if level, ok := value[uint8](parameters, recordLevel); ok {
		record.Level = level
	}
	record.Exp, _ = value[int64](parameters, recordExp)
	record.HP, _ = value[int64](parameters, recordHP)
	record.FullStomach, _ = value[float32](parameters, recordStomach)
	if points, ok := value[int32](parameters, recordArenaRP); ok {
		record.ArenaRankPoints = &points
	}
	document.Character = record

	if !character.GroupID.IsZero() {
		document.Guild = &GuildRef{ID: character.GroupID}
	}
}

// newPal builds one pal from the slot that placed it and the record that
// describes it.
func newPal(instance gvas.GUID, target palTarget, parameters gvas.Properties) Pal {
	pal := Pal{
		InstanceID: instance,
		Level:      defaultLevel,
		Location:   target.location,
		Slot:       target.slot,
	}
	pal.Species, _ = value[string](parameters, recordSpecies)
	pal.Nickname, _ = value[string](parameters, recordNickname)
	pal.Gender, _ = enumName(parameters, recordGender)
	if level, ok := value[uint8](parameters, recordLevel); ok {
		pal.Level = level
	}
	pal.Exp, _ = value[int64](parameters, recordExp)
	pal.HP, _ = value[int64](parameters, recordHP)
	pal.PassiveSkills = names(parameters, recordPassives)

	hp, hasHP := value[uint8](parameters, recordTalentHP)
	shot, hasShot := value[uint8](parameters, recordTalentSho)
	defense, hasDefense := value[uint8](parameters, recordTalentDef)
	if hasHP || hasShot || hasDefense {
		pal.Talents = &Talents{HP: hp, Shot: shot, Defense: defense}
	}
	return pal
}

// readInventories fills in the item containers the players named. Like the
// character records, every container is visited once and only the wanted ones have
// their slots decoded.
func (s *scan) readInventories(r *Resolver) error {
	containers, err := r.worldMap("ItemContainerSaveData")
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
		source, wanted := s.items[id]
		if !wanted {
			continue
		}
		found[id] = true
		owner := s.itemOwner[id]

		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			owner.warn("the %s inventory container is not a property list", source.name)
			continue
		}
		slots, ok := structArray(properties, "Slots")
		if !ok {
			owner.warn("the %s inventory container has no slots", source.name)
			continue
		}
		stacks := *source.stacks
		elements := slots.Iterator()
		for elements.Next() {
			slotProperties, ok := elements.Value().(gvas.Properties)
			if !ok {
				continue
			}
			data, ok := blob(slotProperties, "RawData")
			if !ok {
				owner.warn("a slot of the %s inventory container has no RawData", source.name)
				continue
			}
			slot, err := palworld.DecodeItemSlot(data)
			if err != nil {
				owner.warn("a slot of the %s inventory container did not decode: %v", source.name, err)
				continue
			}
			// Palworld does not serialize empty slots, so one here would be a
			// surprise rather than a hole to report.
			if slot.Empty() {
				continue
			}
			stack := ItemStack{Slot: slot.SlotIndex, ItemID: slot.ItemID, Count: slot.Count}
			if !slot.DynamicItemID.IsZero() {
				id := slot.DynamicItemID
				stack.DynamicItemID = &id
			}
			stacks = append(stacks, stack)
		}
		if err := elements.Err(); err != nil {
			owner.warn("the %s inventory container's slots did not decode: %v", source.name, err)
		}
		sort.SliceStable(stacks, func(left, right int) bool {
			return stacks[left].Slot < stacks[right].Slot
		})
		*source.stacks = stacks
	}
	if err := iterator.Err(); err != nil {
		return fmt.Errorf("resolve: item containers: %w", err)
	}
	for id, source := range s.items {
		if !found[id] {
			s.itemOwner[id].warn("the %s inventory container %s is not in the world save", source.name, id)
		}
	}
	return nil
}

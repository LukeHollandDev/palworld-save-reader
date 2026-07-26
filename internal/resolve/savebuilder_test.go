// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// A synthetic save set, built byte by byte.
//
// The fixture-backed tests in this package prove the joins against a real world,
// but they skip without a private save directory, so they cannot be the only
// coverage: the logic that decides which container belongs to whom has to be
// exercised by `go test ./...` on a clean checkout. Building the saves here also
// makes the negative cases reachable at all -- a container missing from the world
// save, a pal owned by nobody, a record with no Level property -- none of which
// the fixture world contains.

// builder writes Unreal's serialization. It is the same shape as the helpers in
// gvas's and cmd's tests, kept separate because a test package cannot import
// another's.
type builder struct {
	data []byte
}

func (b *builder) bytes() []byte { return b.data }

func (b *builder) raw(value []byte) { b.data = append(b.data, value...) }

func (b *builder) u8(value byte) { b.data = append(b.data, value) }

func (b *builder) u16(value uint16) {
	b.data = binary.LittleEndian.AppendUint16(b.data, value)
}

func (b *builder) u32(value uint32) {
	b.data = binary.LittleEndian.AppendUint32(b.data, value)
}

func (b *builder) i32(value int32) { b.u32(uint32(value)) }

func (b *builder) u64(value uint64) {
	b.data = binary.LittleEndian.AppendUint64(b.data, value)
}

func (b *builder) i64(value int64)   { b.u64(uint64(value)) }
func (b *builder) f32(value float32) { b.u32(math.Float32bits(value)) }
func (b *builder) f64(value float64) { b.u64(math.Float64bits(value)) }

func (b *builder) fstring(value string) {
	b.i32(int32(len(value) + 1))
	b.raw([]byte(value))
	b.u8(0)
}

func (b *builder) guid(value gvas.GUID) {
	b.u32(value.A)
	b.u32(value.B)
	b.u32(value.C)
	b.u32(value.D)
}

// none writes the sentinel that ends a property list.
func (b *builder) none() { b.fstring("None") }

// tag writes one property: the name, type, size and array index, then whatever
// the type adds to the tag, then the optional-GUID flag, then the payload.
func (b *builder) tag(name, propertyType string, payload []byte, meta func(*builder)) {
	b.fstring(name)
	b.fstring(propertyType)
	b.u32(uint32(len(payload)))
	b.u32(0)
	if meta != nil {
		meta(b)
	}
	b.u8(0)
	b.raw(payload)
}

func (b *builder) property(name, propertyType string, payload []byte) {
	b.tag(name, propertyType, payload, nil)
}

func (b *builder) integer(name string, value int32) {
	var payload builder
	payload.i32(value)
	b.property(name, "IntProperty", payload.bytes())
}

func (b *builder) integer64(name string, value int64) {
	var payload builder
	payload.i64(value)
	b.property(name, "Int64Property", payload.bytes())
}

func (b *builder) str(name, value string) {
	var payload builder
	payload.fstring(value)
	b.property(name, "StrProperty", payload.bytes())
}

func (b *builder) name(propertyName, value string) {
	var payload builder
	payload.fstring(value)
	b.property(propertyName, "NameProperty", payload.bytes())
}

func (b *builder) boolean(name string, value bool) {
	// A BoolProperty carries its value in the tag and has an empty payload.
	b.fstring(name)
	b.fstring("BoolProperty")
	b.u32(0)
	b.u32(0)
	if value {
		b.u8(1)
	} else {
		b.u8(0)
	}
	b.u8(0)
}

func (b *builder) byteValue(name string, value byte) {
	var payload builder
	payload.u8(value)
	b.tag(name, "ByteProperty", payload.bytes(), func(tag *builder) { tag.fstring("None") })
}

func (b *builder) float(name string, value float32) {
	var payload builder
	payload.f32(value)
	b.property(name, "FloatProperty", payload.bytes())
}

func (b *builder) enum(name, enumType, value string) {
	var payload builder
	payload.fstring(value)
	b.tag(name, "EnumProperty", payload.bytes(), func(tag *builder) { tag.fstring(enumType) })
}

// structProperty writes a StructProperty whose payload is whatever the struct
// type serializes to: a property list for a game struct, or a fixed body for a
// native one such as Guid.
func (b *builder) structProperty(name, structType string, payload []byte) {
	b.tag(name, "StructProperty", payload, func(tag *builder) {
		tag.fstring(structType)
		tag.guid(gvas.GUID{})
	})
}

// list writes a StructProperty holding a property list, which is how every
// Palworld game struct is serialized.
func (b *builder) list(name, structType string, inner func(*builder)) {
	var payload builder
	inner(&payload)
	payload.none()
	b.structProperty(name, structType, payload.bytes())
}

func (b *builder) guidStruct(name string, value gvas.GUID) {
	var payload builder
	payload.guid(value)
	b.structProperty(name, "Guid", payload.bytes())
}

func (b *builder) dateTime(name string, ticks int64) {
	var payload builder
	payload.i64(ticks)
	b.structProperty(name, "DateTime", payload.bytes())
}

func (b *builder) vector(name string, value gvas.Vector) {
	var payload builder
	payload.f64(value.X)
	payload.f64(value.Y)
	payload.f64(value.Z)
	b.structProperty(name, "Vector", payload.bytes())
}

// containerID writes a PalContainerId, which is a struct wrapping one Guid.
func (b *builder) containerID(name string, value gvas.GUID) {
	b.list(name, "PalContainerId", func(inner *builder) {
		inner.guidStruct("ID", value)
	})
}

func (b *builder) byteArray(name string, data []byte) {
	var payload builder
	payload.u32(uint32(len(data)))
	payload.raw(data)
	b.tag(name, "ArrayProperty", payload.bytes(), func(tag *builder) { tag.fstring("ByteProperty") })
}

func (b *builder) nameArray(propertyName string, values []string) {
	var payload builder
	payload.u32(uint32(len(values)))
	for _, value := range values {
		payload.fstring(value)
	}
	b.tag(propertyName, "ArrayProperty", payload.bytes(), func(tag *builder) {
		tag.fstring("NameProperty")
	})
}

// structArray writes an ArrayProperty<StructProperty>: a count, then a full
// property tag describing the elements whose declared size must equal the bytes
// that follow, then the element bodies concatenated.
func (b *builder) structArray(propertyName, structType string, elements [][]byte) {
	var bodies builder
	for _, element := range elements {
		bodies.raw(element)
	}
	var payload builder
	payload.u32(uint32(len(elements)))
	payload.fstring(propertyName)
	payload.fstring("StructProperty")
	payload.u32(uint32(len(bodies.bytes())))
	payload.u32(0)
	payload.fstring(structType)
	payload.guid(gvas.GUID{})
	payload.u8(0)
	payload.raw(bodies.bytes())
	b.tag(propertyName, "ArrayProperty", payload.bytes(), func(tag *builder) {
		tag.fstring("StructProperty")
	})
}

// mapEntry is one serialized key/value pair of a MapProperty. Both sides are
// written bare: no property tag, which is exactly why Palworld needs type hints
// to say what is in them.
type mapEntry struct {
	key   []byte
	value []byte
}

func (b *builder) mapProperty(name, keyType, valueType string, entries []mapEntry) {
	var payload builder
	payload.u32(0) // removed keys
	payload.u32(uint32(len(entries)))
	for _, entry := range entries {
		payload.raw(entry.key)
		payload.raw(entry.value)
	}
	b.tag(name, "MapProperty", payload.bytes(), func(tag *builder) {
		tag.fstring(keyType)
		tag.fstring(valueType)
	})
}

// bareList is a property list with no tag in front of it, which is how a hinted
// StructProperty map key or value is written.
func bareList(inner func(*builder)) []byte {
	var payload builder
	inner(&payload)
	payload.none()
	return payload.bytes()
}

// archive wraps a property list in a GVAS header and the PlM/0x31 container
// Palworld 1.X writes, producing the bytes of a .sav file.
func archive(className string, properties []byte) []byte {
	var gvasBytes builder
	gvasBytes.raw([]byte("GVAS"))
	gvasBytes.i32(3)
	gvasBytes.i32(522)
	gvasBytes.i32(1008)
	gvasBytes.u16(5)
	gvasBytes.u16(1)
	gvasBytes.u16(1)
	gvasBytes.u32(0)
	gvasBytes.fstring("++UE5+Release-5.1")
	gvasBytes.i32(3)
	gvasBytes.u32(0)
	gvasBytes.fstring(className)
	gvasBytes.raw(properties)

	// A single Oodle block header, 0x4c for a raw block and 0x0a for the Mermaid
	// decoder, followed by one quantum stored verbatim.
	body := append([]byte{0x4c, 0x0a}, gvasBytes.bytes()...)
	var container builder
	container.u32(uint32(len(gvasBytes.bytes())))
	container.u32(uint32(len(body)))
	container.raw([]byte("PlM"))
	container.u8(0x31)
	container.raw(body)
	return container.bytes()
}

// id builds a recognisable GUID from a small number, so a failure message names
// which fixture object it is about.
func id(n uint32) gvas.GUID { return gvas.GUID{A: n, B: 0x11111111} }

// palFixture is one pal record to put in the synthetic world.
type palFixture struct {
	instance gvas.GUID
	species  string
	nickname string
	gender   string
	// level of 0 omits the Level property entirely, which is what Palworld does
	// for a level-1 pal and what defaultLevel exists to interpret.
	level    byte
	exp      int64
	hp       int64
	talents  []byte
	passives []string
	// owner is the character container this pal sits in, and slot its index.
	owner gvas.GUID
	slot  int32
}

// record writes the RawData of a character record: a headerless property stream
// followed by four zero bytes, the group id, and four more.
func (pal palFixture) record(group gvas.GUID) []byte {
	var stream builder
	stream.list("SaveParameter", "PalIndividualCharacterSaveParameter", func(inner *builder) {
		inner.name("CharacterID", pal.species)
		if pal.gender != "" {
			inner.enum("Gender", "EPalGenderType", "EPalGenderType::"+pal.gender)
		}
		if pal.level != 0 {
			inner.byteValue("Level", pal.level)
		}
		if pal.exp != 0 {
			inner.integer64("Exp", pal.exp)
		}
		if pal.hp != 0 {
			inner.list("Hp", "FixedPoint64", func(hp *builder) {
				hp.integer64("Value", pal.hp)
			})
		}
		if pal.nickname != "" {
			inner.str("NickName", pal.nickname)
		}
		if len(pal.talents) == 3 {
			inner.byteValue("Talent_HP", pal.talents[0])
			inner.byteValue("Talent_Shot", pal.talents[1])
			inner.byteValue("Talent_Defense", pal.talents[2])
		}
		if len(pal.passives) != 0 {
			inner.nameArray("PassiveSkillList", pal.passives)
		}
	})
	stream.none()
	var trailer builder
	trailer.u32(0)
	trailer.guid(group)
	trailer.u32(0)
	stream.raw(trailer.bytes())
	return stream.bytes()
}

// playerFixture is one player: their own save, plus the world-save records that
// belong to them.
type playerFixture struct {
	uid      gvas.GUID
	instance gvas.GUID
	platform string
	// stem names the save file. Palworld uses the uid with its dashes stripped;
	// setting it to something else is how the tests check the id is read from
	// inside the file rather than off the file name.
	stem       string
	technology int32
	position   gvas.Vector
	lastOnline int64
	nickname   string
	level      byte
	exp        int64
	hp         int64
	stomach    float32
	group      gvas.GUID

	// The eight containers a player save names. A zero id is left out of the
	// save, which is what produces a warning.
	common, dropSlot, essential, weapons, armor, food gvas.GUID
	party, storage                                    gvas.GUID

	// isPlayer omits the IsPlayer flag when false, which must be reported.
	isPlayer bool
}

// save writes the player's own .sav file.
func (player playerFixture) save() []byte {
	var properties builder
	properties.integer("Version", 100)
	properties.list("SaveData", "PalWorldPlayerSaveData", func(save *builder) {
		save.guidStruct("PlayerUId", player.uid)
		save.list("IndividualId", "PalInstanceID", func(individual *builder) {
			individual.guidStruct("PlayerUId", player.uid)
			individual.guidStruct("InstanceId", player.instance)
		})
		save.list("LastTransform", "Transform", func(transform *builder) {
			transform.vector("Translation", player.position)
		})
		save.containerID("OtomoCharacterContainerId", player.party)
		save.list("InventoryInfo", "PalPlayerDataInventoryInfo", func(inventory *builder) {
			for _, container := range []struct {
				name string
				id   gvas.GUID
			}{
				{"CommonContainerId", player.common},
				{"DropSlotContainerId", player.dropSlot},
				{"EssentialContainerId", player.essential},
				{"WeaponLoadOutContainerId", player.weapons},
				{"PlayerEquipArmorContainerId", player.armor},
				{"FoodEquipContainerId", player.food},
			} {
				if container.id.IsZero() {
					continue
				}
				inventory.containerID(container.name, container.id)
			}
		})
		save.integer("TechnologyPoint", player.technology)
		save.containerID("PalStorageContainerId", player.storage)
		save.dateTime("LastOnlineDateTime", player.lastOnline)
		if player.platform != "" {
			save.enum("PlayerPlatform", "EPalPlayerPlatform", "EPalPlayerPlatform::"+player.platform)
		}
	})
	properties.none()
	return archive("/Script/Pal.PalWorldPlayerSaveGame", properties.bytes())
}

// record writes the player's character record, the half of them that lives in
// the world save.
func (player playerFixture) record() []byte {
	var stream builder
	stream.list("SaveParameter", "PalIndividualCharacterSaveParameter", func(inner *builder) {
		if player.level != 0 {
			inner.byteValue("Level", player.level)
		}
		inner.integer64("Exp", player.exp)
		inner.str("NickName", player.nickname)
		inner.list("Hp", "FixedPoint64", func(hp *builder) {
			hp.integer64("Value", player.hp)
		})
		inner.float("FullStomach", player.stomach)
		if player.isPlayer {
			inner.boolean("IsPlayer", true)
		}
	})
	stream.none()
	var trailer builder
	trailer.u32(0)
	trailer.guid(player.group)
	trailer.u32(0)
	stream.raw(trailer.bytes())
	return stream.bytes()
}

// itemFixture is one occupied item slot.
type itemFixture struct {
	slot    uint32
	itemID  string
	count   uint32
	dynamic gvas.GUID
}

// record writes the RawData of an item slot: index, count, the id, sixteen zero
// bytes, the dynamic-item id, and a zero trailer.
func (item itemFixture) record() []byte {
	var record builder
	record.u32(item.slot)
	record.u32(item.count)
	record.fstring(item.itemID)
	record.raw(make([]byte, 16))
	record.guid(item.dynamic)
	record.raw(make([]byte, 20))
	return record.bytes()
}

// worldFixture is everything the synthetic Level.sav holds.
type worldFixture struct {
	revision   int32
	savedAt    int64
	gameTicks  int64
	realTicks  int64
	players    []playerFixture
	pals       []palFixture
	items      map[gvas.GUID][]itemFixture
	itemOrder  []gvas.GUID
	characters []gvas.GUID
	// unowned is a character record with no container slot and no player, like a
	// base camp's worker: it must be counted and never resolved to anybody.
	unowned []palFixture
	// mapObjects is a count only, to exercise the struct-array counter.
	mapObjects int
}

// slotBody writes one element of a character container's Slots array.
func characterSlotBody(index int32, instance gvas.GUID) []byte {
	var raw builder
	raw.guid(gvas.GUID{})
	raw.guid(instance)
	raw.raw(make([]byte, 6))

	var slot builder
	slot.integer("SlotIndex", index)
	slot.byteArray("RawData", raw.bytes())
	slot.none()
	return slot.bytes()
}

// itemSlotBody writes one element of an item container's Slots array.
func itemSlotBody(item itemFixture) []byte {
	var slot builder
	slot.integer("SlotIndex", int32(item.slot))
	slot.byteArray("RawData", item.record())
	slot.none()
	return slot.bytes()
}

// save writes the synthetic Level.sav.
func (world worldFixture) save() []byte {
	// The character map: every player record and every pal record, keyed the way
	// Palworld keys them -- a player's key carries their account id, a pal's key
	// carries the zero GUID.
	characters := []mapEntry{}
	for _, player := range world.players {
		characters = append(characters, mapEntry{
			key: bareList(func(key *builder) {
				key.guidStruct("PlayerUId", player.uid)
				key.guidStruct("InstanceId", player.instance)
			}),
			value: bareList(func(value *builder) {
				value.byteArray("RawData", player.record())
			}),
		})
	}
	for _, pal := range append(append([]palFixture{}, world.pals...), world.unowned...) {
		group := gvas.GUID{}
		for _, player := range world.players {
			if player.party == pal.owner || player.storage == pal.owner {
				group = player.group
			}
		}
		characters = append(characters, mapEntry{
			key: bareList(func(key *builder) {
				key.guidStruct("PlayerUId", gvas.GUID{})
				key.guidStruct("InstanceId", pal.instance)
			}),
			value: bareList(func(value *builder) {
				value.byteArray("RawData", pal.record(group))
			}),
		})
	}

	// The character containers, in the order the fixture declares them.
	slotsByContainer := map[gvas.GUID][][]byte{}
	for _, pal := range world.pals {
		slotsByContainer[pal.owner] = append(slotsByContainer[pal.owner], characterSlotBody(pal.slot, pal.instance))
	}
	containers := []mapEntry{}
	for _, container := range world.characters {
		slots := slotsByContainer[container]
		containers = append(containers, mapEntry{
			key: bareList(func(key *builder) {
				key.guidStruct("ID", container)
			}),
			value: bareList(func(value *builder) {
				value.structArray("Slots", "PalContainerCharacterSlotSaveData", slots)
				value.integer("SlotNum", int32(len(slots)))
			}),
		})
	}

	items := []mapEntry{}
	for _, container := range world.itemOrder {
		bodies := [][]byte{}
		for _, item := range world.items[container] {
			bodies = append(bodies, itemSlotBody(item))
		}
		items = append(items, mapEntry{
			key: bareList(func(key *builder) {
				key.guidStruct("ID", container)
			}),
			value: bareList(func(value *builder) {
				value.structArray("Slots", "PalItemSlotSaveData", bodies)
				value.integer("SlotNum", int32(len(bodies)))
			}),
		})
	}

	var properties builder
	properties.integer("Version", 100)
	properties.integer("Revision", world.revision)
	properties.dateTime("Timestamp", world.savedAt)
	properties.list("worldSaveData", "PalWorldSaveData", func(save *builder) {
		save.mapProperty("CharacterSaveParameterMap", "StructProperty", "StructProperty", characters)
		save.mapProperty("CharacterContainerSaveData", "StructProperty", "StructProperty", containers)
		save.mapProperty("ItemContainerSaveData", "StructProperty", "StructProperty", items)
		save.mapProperty("GroupSaveDataMap", "StructProperty", "StructProperty", nil)
		save.mapProperty("BaseCampSaveData", "StructProperty", "StructProperty", nil)
		empty := make([][]byte, 0, world.mapObjects)
		for i := 0; i < world.mapObjects; i++ {
			empty = append(empty, bareList(func(*builder) {}))
		}
		save.structArray("MapObjectSaveData", "PalMapObjectSaveData", empty)
		save.list("GameTimeSaveData", "PalGameTimeSaveData", func(gameTime *builder) {
			gameTime.integer64("GameDateTimeTicks", world.gameTicks)
			gameTime.integer64("RealDateTimeTicks", world.realTicks)
		})
	})
	properties.none()
	return archive("/Script/Pal.PalWorldSaveGame", properties.bytes())
}

// meta writes a synthetic LevelMeta.sav.
func metaSave(worldName string, inGameDay int32) []byte {
	var properties builder
	properties.integer("Version", 100)
	properties.list("SaveData", "PalWorldBaseInfoSaveData", func(save *builder) {
		save.str("WorldName", worldName)
		save.integer("InGameDay", inGameDay)
	})
	properties.none()
	return archive("/Script/Pal.PalWorldBaseInfoSaveGame", properties.bytes())
}

// writeSaveSet lays the fixture out on disk the way Palworld does, and returns the
// directory.
//
// A player with no stem gets a character record in the world save but no save
// file, which is how a player who has been removed from the directory is
// represented.
func writeSaveSet(t *testing.T, world worldFixture, meta []byte) string {
	t.Helper()
	directory := t.TempDir()
	writeFile(t, directory, levelName, world.save())
	if meta != nil {
		writeFile(t, directory, levelMetaName, meta)
	}
	for _, player := range world.players {
		if player.stem == "" {
			continue
		}
		writeFile(t, directory, filepath.Join(playersDir, player.stem+".sav"), player.save())
	}
	return directory
}

// writeFile writes one save into the set, creating Players/ as needed.
func writeFile(t *testing.T, directory, name string, data []byte) {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

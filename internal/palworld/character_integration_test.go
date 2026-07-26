// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefixtures"
)

// These tests decode real player and pal records, which is private data: player
// names, account identifiers and progression. Nothing here logs a decoded name or
// identifier -- counts and shapes only. See savefixtures for how the save set is
// located and why it is never committed.

// worldMap walks to a named map property of worldSaveData. It fails rather than
// skips when the path is missing, because a Level.sav without it means the schema
// moved and the tests below would otherwise pass while examining nothing.
func worldMap(t *testing.T, save *gvas.Archive, name string) *gvas.MapValue {
	t.Helper()
	world := save.Properties.Find("worldSaveData")
	if world == nil {
		t.Fatal("Level.sav has no worldSaveData")
	}
	structValue, ok := world.Value.(gvas.StructValue)
	if !ok {
		t.Fatalf("worldSaveData is %T, want gvas.StructValue", world.Value)
	}
	properties, ok := structValue.Value.(gvas.Properties)
	if !ok {
		t.Fatalf("worldSaveData holds %T, want gvas.Properties", structValue.Value)
	}
	property := properties.Find(name)
	if property == nil {
		t.Fatalf("worldSaveData has no %s", name)
	}
	value, ok := property.Value.(*gvas.MapValue)
	if !ok {
		t.Fatalf("%s is %T, want *gvas.MapValue", name, property.Value)
	}
	return value
}

// characterRecords decodes every CharacterSaveParameterMap entry, iterating
// rather than collecting: memory rule R1 forbids Entries() on a Level.sav
// collection. visit receives the entry key alongside the decoded record, because
// the key is what carries the identity a caller joins on.
func characterRecords(t *testing.T, characters *gvas.MapValue, visit func(index int, key gvas.Properties, character Character)) int {
	t.Helper()
	index := 0
	iterator := characters.Iterator()
	for iterator.Next() {
		key, ok := iterator.Entry().Key.(gvas.Properties)
		if !ok {
			t.Fatalf("character %d key is %T, want gvas.Properties", index, iterator.Entry().Key)
		}
		properties, ok := iterator.Entry().Value.(gvas.Properties)
		if !ok {
			t.Fatalf("character %d value is %T, want gvas.Properties", index, iterator.Entry().Value)
		}
		rawData := properties.Find("RawData")
		if rawData == nil {
			t.Fatalf("character %d has no RawData", index)
		}
		array, ok := rawData.Value.(gvas.ArrayValue)
		if !ok {
			t.Fatalf("character %d RawData is %T", index, rawData.Value)
		}
		blob, ok := array.Values.([]byte)
		if !ok {
			t.Fatalf("character %d RawData holds %T, want []byte", index, array.Values)
		}
		character, err := DecodeCharacter(blob)
		if err != nil {
			t.Fatalf("character %d (%d bytes): %v", index, len(blob), err)
		}
		visit(index, key, character)
		index++
	}
	if err := iterator.Err(); err != nil {
		t.Fatalf("character iteration: %v", err)
	}
	return index
}

// keyGUID reads one of the GUID-valued properties of a character map key.
func keyGUID(t *testing.T, key gvas.Properties, name string) gvas.GUID {
	t.Helper()
	property := key.Find(name)
	if property == nil {
		t.Fatalf("character key has no %s", name)
	}
	structValue, ok := property.Value.(gvas.StructValue)
	if !ok {
		t.Fatalf("%s is %T, want gvas.StructValue", name, property.Value)
	}
	guid, ok := structValue.Value.(gvas.GUID)
	if !ok {
		t.Fatalf("%s holds %T, want gvas.GUID", name, structValue.Value)
	}
	return guid
}

// TestDecodeCharacterAgainstWorldSave is what makes Character a claim about
// Palworld rather than about a hand-built stream. Every record in the fixture
// world must decode, and must decode completely: a property that fell back to
// gvas.UndecodedValue would mean the nested stream needs a type hint the table
// does not have, which is exactly the kind of gap that otherwise goes unnoticed.
func TestDecodeCharacterAgainstWorldSave(t *testing.T) {
	root := savefixtures.Root(t)
	save, err := Load(filepath.Join(root, "Level.sav"))
	if err != nil {
		t.Fatal(err)
	}

	var players, pals, named, framed int
	undecoded := map[string]int{}
	total := characterRecords(t, worldMap(t, save.Archive, "CharacterSaveParameterMap"),
		func(index int, key gvas.Properties, character Character) {
			parameters := character.Parameters()
			if parameters == nil {
				t.Fatalf("character %d has no SaveParameter", index)
			}
			collectUndecoded(parameters, "", undecoded)
			if character.PaddingBeyondGroupID() {
				framed++
			}
			if isPlayer(parameters) {
				players++
			} else {
				pals++
			}
			// Do not log the name: assert that one is there.
			if property := parameters.Find("NickName"); property != nil {
				if name, _ := property.Value.(string); name != "" {
					named++
				}
			}
		})

	if total == 0 {
		t.Fatal("fixture world has no character records to exercise")
	}
	t.Logf("characters=%d players=%d pals=%d named=%d framingBeyondGroupID=%d",
		total, players, pals, named, framed)
	if players == 0 {
		t.Error("no character record decoded to a player, so IsPlayer is not being read")
	}
	if pals == 0 {
		t.Error("no character record decoded to a pal")
	}
	if len(undecoded) != 0 {
		for reason, count := range undecoded {
			t.Errorf("%d properties fell back to raw bytes: %s", count, reason)
		}
	}
}

// isPlayer reports whether a decoded record is a player rather than a pal. A pal
// omits the property rather than setting it false.
func isPlayer(parameters gvas.Properties) bool {
	property := parameters.Find("IsPlayer")
	if property == nil {
		return false
	}
	flag, _ := property.Value.(bool)
	return flag
}

// collectUndecoded records every property gvas could not decode, keyed by its
// path and reason so a failure names what to fix. It walks lazily for the same
// reason characterRecords does.
func collectUndecoded(value any, path string, out map[string]int) {
	switch typed := value.(type) {
	case gvas.Properties:
		for i := range typed {
			child := path + "." + typed[i].Name
			if undecodedValue, ok := typed[i].Value.(gvas.UndecodedValue); ok {
				out[child+": "+undecodedValue.Reason]++
				continue
			}
			collectUndecoded(typed[i].Value, child, out)
		}
	case gvas.StructValue:
		collectUndecoded(typed.Value, path, out)
	case gvas.ArrayValue:
		if typed.Structs != nil {
			collectUndecoded(typed.Structs, path, out)
		}
	case *gvas.StructArray:
		iterator := typed.Iterator()
		for iterator.Next() {
			collectUndecoded(iterator.Value(), path+"."+typed.Name, out)
		}
		if err := iterator.Err(); err != nil {
			out[path+": "+err.Error()]++
		}
	case *gvas.MapValue:
		iterator := typed.Iterator()
		for iterator.Next() {
			collectUndecoded(iterator.Entry().Key, path+".Key", out)
			collectUndecoded(iterator.Entry().Value, path+".Value", out)
		}
		if err := iterator.Err(); err != nil {
			out[path+": "+err.Error()]++
		}
	case *gvas.SetValue:
		iterator := typed.Iterator()
		for iterator.Next() {
			collectUndecoded(iterator.Value(), path+"."+typed.ElementType, out)
		}
		if err := iterator.Err(); err != nil {
			out[path+": "+err.Error()]++
		}
	case []any:
		for _, element := range typed {
			collectUndecoded(element, path, out)
		}
	}
}

// TestCharacterGroupIDsResolve checks the field Character documents as a join key
// actually joins. Sixteen bytes read at the wrong offset can look like a
// plausible GUID, so the test that the offset is right is that every value found
// there names a real group.
func TestCharacterGroupIDsResolve(t *testing.T) {
	root := savefixtures.Root(t)
	save, err := Load(filepath.Join(root, "Level.sav"))
	if err != nil {
		t.Fatal(err)
	}

	groups := map[gvas.GUID]bool{}
	iterator := worldMap(t, save.Archive, "GroupSaveDataMap").Iterator()
	for iterator.Next() {
		key, ok := iterator.Entry().Key.(gvas.GUID)
		if !ok {
			t.Fatalf("group key is %T, want gvas.GUID", iterator.Entry().Key)
		}
		groups[key] = true
	}
	if err := iterator.Err(); err != nil {
		t.Fatal(err)
	}
	if len(groups) == 0 {
		t.Fatal("fixture world has no groups to resolve against")
	}

	distinct := map[gvas.GUID]int{}
	var missing, zero int
	characterRecords(t, worldMap(t, save.Archive, "CharacterSaveParameterMap"),
		func(index int, key gvas.Properties, character Character) {
			if character.GroupID.IsZero() {
				zero++
				return
			}
			distinct[character.GroupID]++
			if !groups[character.GroupID] {
				missing++
				if missing <= 3 {
					t.Errorf("character %d has GroupID %s, which matches no group", index, character.GroupID)
				}
			}
		})

	t.Logf("distinctGroupIDs=%d unresolved=%d withoutAGroup=%d groupMapKeys=%d",
		len(distinct), missing, zero, len(groups))
	if len(distinct) == 0 {
		t.Fatal("no character carried a group id, so the offset is not being exercised")
	}
	if missing != 0 {
		t.Errorf("%d character group ids resolved to nothing", missing)
	}
}

// TestPlayerCharactersJoinToTheirSaveFiles is the payoff of the nested decode,
// stated as a test. A player's name is not in their own player.sav at all -- every
// string in it is a GUID, an enum, or an asset name -- so the only way to reach it
// is through the world save's character record. This walks that join in both
// directions: every record flagged as a player must correspond to a save file on
// disk, and every save file must have a record.
func TestPlayerCharactersJoinToTheirSaveFiles(t *testing.T) {
	root := savefixtures.Root(t)
	save, err := Load(filepath.Join(root, "Level.sav"))
	if err != nil {
		t.Fatal(err)
	}

	// Player save files are named for the player UID with its dashes stripped.
	// The _dps.sav companions are a different save type and are not players.
	entries, err := os.ReadDir(filepath.Join(root, "Players"))
	if err != nil {
		t.Fatalf("fixture save set has no Players directory: %v", err)
	}
	files := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sav") || strings.HasSuffix(name, "_dps.sav") {
			continue
		}
		files[strings.ToLower(strings.TrimSuffix(name, ".sav"))] = true
	}
	if len(files) == 0 {
		t.Fatal("fixture save set has no player saves")
	}

	matched := map[string]bool{}
	var playerRecords, namedPlayers, palsWithAUID int
	characterRecords(t, worldMap(t, save.Archive, "CharacterSaveParameterMap"),
		func(index int, key gvas.Properties, character Character) {
			parameters := character.Parameters()
			uid := keyGUID(t, key, "PlayerUId")
			if !isPlayer(parameters) {
				// A pal's key carries the zero UID. One that did not would mean
				// PlayerUId does not mean what this test assumes.
				if !uid.IsZero() {
					palsWithAUID++
				}
				return
			}
			playerRecords++
			if uid.IsZero() {
				t.Errorf("player record %d has no PlayerUId", index)
				return
			}
			stem := strings.ReplaceAll(uid.String(), "-", "")
			if !files[stem] {
				t.Errorf("player record %d has a PlayerUId matching no save file", index)
				return
			}
			matched[stem] = true
			// The name is the thing phase 2 unlocks. Assert it is there without
			// putting it in the log.
			property := parameters.Find("NickName")
			if property == nil {
				t.Errorf("player record %d has no NickName", index)
				return
			}
			if name, _ := property.Value.(string); name != "" {
				namedPlayers++
			} else {
				t.Errorf("player record %d has an empty NickName", index)
			}
		})

	t.Logf("playerSaves=%d playerRecords=%d matched=%d named=%d palsCarryingAPlayerUId=%d",
		len(files), playerRecords, len(matched), namedPlayers, palsWithAUID)
	if palsWithAUID != 0 {
		t.Errorf("%d records without IsPlayer carry a PlayerUId", palsWithAUID)
	}
	if len(matched) != len(files) {
		t.Errorf("%d of %d player saves have a character record", len(matched), len(files))
	}
	if namedPlayers != len(files) {
		t.Errorf("%d of %d players have a readable name", namedPlayers, len(files))
	}
}

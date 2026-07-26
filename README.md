# palworld-save-reader

[![CI](https://github.com/LukeHollandDev/palworld-save-reader/actions/workflows/ci.yml/badge.svg)](https://github.com/LukeHollandDev/palworld-save-reader/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/LukeHollandDev/palworld-save-reader)](go.mod)
[![License](https://img.shields.io/github/license/LukeHollandDev/palworld-save-reader)](LICENSE)

`palworld-save-reader` is a standalone command-line tool for reading Palworld 1.X saves and writing JSON. It can expand one save's complete decoded property tree for inspection, apply a caller-provided projection that requests only the serialized fields and structure an application needs, or resolve a whole save directory into a player's inventory, pals, and name.

The decoder is implemented in pure Go and does not require cgo, proprietary Oodle libraries, or third-party Go dependencies. The executable is the supported interface; every package lives under `internal/` and cannot be imported by other projects.

## Features

- Reads the Mermaid-compressed saves written by Palworld 1.X
- Selects fields with caller-provided, versioned JSON projection documents
- Preserves the requested JSON keys, nesting, array structure, and key order
- Rejects missing, incompatible, or ambiguous matches by default
- Produces a complete expanded property tree for format inspection
- Decodes item-container `RawData` slots to item identifiers and stack counts
- Decodes character `RawData` records to readable player and pal detail
- Resolves a save directory into a player with their inventory, pals, and name
- Applies bounded input, collection, parser, and projection limits
- Never writes to or modifies a source save
- Builds for Linux, macOS, and Windows

## Installation

Go 1.26.5 or later is required when building from source.

```sh
go install github.com/LukeHollandDev/palworld-save-reader/cmd/palworld-save-reader@latest
```

Prebuilt executables are also available from successful GitHub Actions runs and published GitHub Releases.

To build from a clone:

```sh
git clone https://github.com/LukeHollandDev/palworld-save-reader.git
cd palworld-save-reader
make build
```

The executable is written to `bin/palworld-save-reader`.

## Usage

Every operation requires an explicit mode:

```text
palworld-save-reader --full [--decode-raw] FILE
palworld-save-reader --schema PROJECTION.json [--allow-partial] [--explain] FILE
palworld-save-reader --preset NAME [--allow-partial] [--explain] FILE
palworld-save-reader --resolve player --id UID --saves DIR
palworld-save-reader --resolve players|world --saves DIR
palworld-save-reader --list-presets
```

### Project selected fields

A projection document describes the exact JSON shape to return. It is not an official Palworld schema or a conventional JSON Schema, and its output keys are not application-specific aliases: requested keys must match serialized save-property names exactly and are matched case-sensitively.

For example, this projection requests an identifier, last-online value, and position from a compatible structure in a player save:

```json
{
  "$schema": "https://github.com/LukeHollandDev/palworld-save-reader/raw/main/projection-v1.schema.json",
  "projectionVersion": 1,
  "name": "player-location",
  "gameVersion": "1.0.1.100619",
  "saveType": "player.sav",
  "shape": {
    "PlayerUId": "",
    "LastOnlineDateTime": 0,
    "LastTransform": {
      "Translation": {
        "X": 0,
        "Y": 0,
        "Z": 0
      }
    }
  }
}
```

Apply it to one save:

```sh
palworld-save-reader --schema player-location.json /path/to/player.sav
```

The result keeps the requested keys, nesting, and key order, with each placeholder replaced by the matched save value:

```json
{
  "PlayerUId": "00000000-0000-0000-0000-000000000000",
  "LastOnlineDateTime": 639200000000000000,
  "LastTransform": {
    "Translation": {
      "X": -184343.5,
      "Y": 256561.11,
      "Z": -1378.54
    }
  }
}
```

The matcher searches the decoded save for structures containing the requested field names and compatible nested values. It selects a result only when there is one highest-scoring compatible structure; it never selects a field solely because it has the requested primitive type.

Projection format version 1 uses these shape values:

| Projection value | Requested decoded value |
| --- | --- |
| `{ "Field": ... }` | An object containing the exact child name |
| `[ ... ]` | A repeated collection whose elements use the single element shape |
| `null` | Any JSON-compatible value |
| `""` | A string |
| `0` | A number |
| `false` | A boolean |

Objects and arrays must not be empty, and a projection array must contain exactly one element shape. Placeholder values are type hints and are replaced by values from the save.

For matching purposes, an Unreal property list is an object keyed by its serialized property names, a struct wrapper is transparent, a map is an array of `{ "Key": ..., "Value": ... }` objects, and arrays and sets are JSON arrays. An undecoded raw value is represented as an object containing base64 `raw`, byte-count `bytes`, and explanatory `reason` fields.

Use `--explain` to write JSON diagnostics to standard error while keeping projected JSON on standard output:

```sh
palworld-save-reader --schema player-location.json --explain /path/to/player.sav > player.json
```

Strict matching is the default. `--allow-partial` writes `null` for unresolved or incompatible fields and reports each problem to standard error:

```sh
palworld-save-reader --schema player-location.json --allow-partial /path/to/player.sav
```

Projection version 1 operates on one physical save at a time. It does not join `Level.sav` with files under `Players/`, rename fields, calculate values, aggregate records, sort, or filter. That is deliberate: a projection's guarantee is that the shape it returns is the shape the save holds. For the join across files, use [`--resolve`](#resolve-a-player-across-files), which is hand-written Go with an output shape this tool versions; everything else remains the calling application's.

### Inspect a complete save

Use `--full` to expand one save into a JSON property tree:

```sh
palworld-save-reader --full /path/to/Level.sav
```

Full mode is useful for discovering serialized field names and researching format changes before writing a projection. Large world saves can produce hundreds of megabytes of JSON and require substantial memory. Output may include player names, identifiers, and other private save content, so review it before sharing.

### Decode `RawData` byte arrays

A world save keeps much of its detail in `RawData` byte arrays. Unreal tags each one as nothing more than an array of bytes, so they are emitted as base64 by default. `--decode-raw` additionally interprets the ones whose layout is known:

```sh
palworld-save-reader --full --decode-raw /path/to/Level.sav
```

A recognised blob gains a `decoded` object beside the base64, which is kept so nothing is lost:

```json
{
  "innerType": "ByteProperty",
  "values": "BgAAAAEAAAANAAAARnVyQXJtb3JDb2xkAAAA…",
  "decoded": {
    "kind": "itemSlot",
    "slotIndex": 6,
    "count": 1,
    "itemId": "FurArmorCold",
    "dynamicItemId": "4cf65985-51e4-45c1-9f27-5250ed5c7fcd"
  }
}
```

`dynamicItemId` appears only when the item has a per-instance record in `worldSaveData.DynamicItemSaveData`, which is where durability and similar state lives. A `trailer` field appears when a slot carries bytes that are not decoded yet. A blob that fails to decode reports `decodeError` next to its base64 rather than failing the dump.

Three layouts are decoded. The first is the item-container slot above, at `worldSaveData.ItemContainerSaveData`. Together with `player-containers` it makes a player's inventory readable: that preset returns the container identifiers, and those identifiers key the containers whose slots now carry item names and stack counts.

The second is the character record at `worldSaveData.CharacterSaveParameterMap`, which holds one player or one pal. It is not a bespoke record but a complete Unreal property stream written without a header, so it comes out as the same property list the rest of the dump uses:

```json
{
  "innerType": "ByteProperty",
  "values": "DgAAAFNhdmVQYXJhbWV0ZXIADwAAAFN0cnVjdFByb3BlcnR5…",
  "decoded": {
    "kind": "character",
    "groupId": "7bf717b9-4a4b-4838-b10a-0a54e4cc168f",
    "properties": [
      {
        "name": "SaveParameter",
        "type": "StructProperty",
        "value": {
          "structType": "PalIndividualCharacterSaveParameter",
          "value": [
            { "name": "Level", "type": "ByteProperty", "enumType": "None", "value": 46 },
            { "name": "Exp", "type": "Int64Property", "value": 1641238 },
            { "name": "NickName", "type": "StrProperty", "value": "…" },
            { "name": "IsPlayer", "type": "BoolProperty", "value": true }
          ]
        }
      }
    ]
  }
}
```

A pal's record carries `CharacterID` naming its species instead of `IsPlayer`, along with its level, IVs, passive skills, and any nickname given to it.

This is where a player's name lives, and it is the only place: nothing in a `player.sav` holds it, so no preset can reach it. `groupId` keys `worldSaveData.GroupSaveDataMap`, the guild the character belongs to. Against fixtures from `1.0.1.100619`, all 2,259 records decoded, all eight distinct group ids resolved to a group, and the nine records flagged as players matched the nine save files under `Players/` one for one.

The third is the character-container slot at `worldSaveData.CharacterContainerSaveData`, which is a reference rather than a record: it says which character record occupies a slot of a pal party, a storage box, or a base camp's worker list.

```json
{
  "innerType": "ByteProperty",
  "values": "AAAAAAAAAAAAAAAAAAAAACQHFWvvTBgheKBagQe/lfsAAAAAAAA=",
  "decoded": {
    "kind": "characterSlot",
    "instanceId": "6b150724-2118-4cef-815a-a078fb95bf07",
    "empty": false
  }
}
```

That `instanceId` keys `CharacterSaveParameterMap`, so a container's slots and the records above join into "which pals does this box hold, and what are they". In the fixture world all 2,250 slots resolved to a record, each record exactly once, and the total matched the number of pals in the world exactly.

The flag is opt-in because it is not free: on a 3.3MB world save it adds about 21% to the JSON and a fifth again to the run time. It only applies to `--full`; pairing it with a projection mode is a usage error rather than a silently ignored flag. Every other `RawData` blob — guilds, base camps, map objects, foliage — is still base64.

### Resolve a player across files

The modes above read one file. `--resolve` reads a save directory and answers a question that spans all of it:

```sh
palworld-save-reader --resolve player --id 1A2B3C4D000000000000000000000000 --saves /path/to/SaveGames/0/WORLDID
palworld-save-reader --resolve players --saves /path/to/SaveGames/0/WORLDID
palworld-save-reader --resolve world --saves /path/to/SaveGames/0/WORLDID
```

`--saves` names the directory holding `Level.sav`, `LevelMeta.sav` and `Players/`, so a caller points at a save rather than naming files. `--id` accepts a player UID in either spelling the game uses: the dashed form these documents print, or the dash-free form a player's save file is named after. The id is matched against the `PlayerUId` inside each player save rather than against its file name, so a renamed file still resolves.

A resolved player is composed rather than projected. The result is a document this tool defines, carrying its own `resolveVersion`:

```json
{
  "resolveVersion": 1,
  "kind": "player",
  "player": {
    "playerUId": "1a2b3c4d-0000-0000-0000-000000000000",
    "instanceId": "9c0e4d21-7a63-4f10-8b2e-51d7c6a90f34",
    "platform": "Steam",
    "lastOnline": { "ticks": 639204643642480000, "utc": "2026-07-24T04:32:44Z" },
    "position": { "x": -184343.5, "y": 256561.11, "z": -1378.54 },
    "technologyPoints": 48,
    "character": { "nickname": "…", "level": 56, "exp": 4563093, "hp": 2734000, "fullStomach": 56.72 },
    "guild": { "id": "7bf717b9-4a4b-4838-b10a-0a54e4cc168f" },
    "inventory": {
      "common": [
        { "slot": 0, "itemId": "Money", "count": 70434 },
        { "slot": 1, "itemId": "FurArmorCold", "count": 1, "dynamicItemId": "4cf65985-51e4-45c1-9f27-5250ed5c7fcd" }
      ],
      "dropSlot": [],
      "essential": [],
      "weapons": [],
      "armor": [],
      "food": []
    },
    "pals": [
      {
        "instanceId": "6b150724-2118-4cef-815a-a078fb95bf07",
        "species": "Suzaku",
        "gender": "Female",
        "level": 42,
        "exp": 695203,
        "hp": 3419000,
        "location": "party",
        "slot": 1,
        "talents": { "hp": 25, "shot": 15, "defense": 76 },
        "passiveSkills": ["PAL_sadist"]
      }
    ]
  }
}
```

The document is grouped by where each part came from, because that is what a reader needs to know when something is missing. `playerUId` down to `technologyPoints` are from the player's own save. `character`, `guild` and `pals` are from the world save, and are absent when the join found nothing. `nickname` is in `character` for exactly that reason: a `player.sav` does not contain a name.

`species` is Palworld's internal `CharacterID`, not the displayed species name, and `hp` is Unreal's `FixedPoint64` value as stored — mapping either to what the game shows needs game data this tool does not ship. The six inventory containers and `pals` are always present, empty rather than absent, so a consumer can index them without checking. `location` is `party` or `storage`, and `slot` is the index within that container, so the two together are unique while `slot` alone is not.

`--resolve players` returns the same documents as an array under `"players"`, streamed one at a time as they are produced. `--resolve world` returns the world name, in-game day, save time, and entity counts, including the player/pal split of the character map:

```json
{
  "resolveVersion": 1,
  "kind": "world",
  "world": {
    "name": "My World",
    "saveVersion": 100,
    "revision": 100619,
    "savedAt": { "ticks": 639204910905580000, "utc": "2026-07-24T11:58:10Z" },
    "inGameDay": 601,
    "gameTime": { "ticks": 519302980000000, "seconds": 51930298, "days": 601 },
    "realTime": { "ticks": 11444635700000, "seconds": 1144463, "days": 13 },
    "counts": {
      "playerSaves": 9, "characters": 2259, "players": 9, "pals": 2250,
      "itemContainers": 8723, "characterContainers": 35, "baseCamps": 16,
      "groups": 15, "dynamicItems": 581, "mapObjects": 10877, "foliageGrids": 1264
    }
  }
}
```

`revision` is the Palworld build that wrote the save, which is the value to quote when a decode goes wrong. `gameTime` and `realTime` are elapsed times rather than dates, which is why they are reported in seconds and days rather than as a timestamp.

A join that finds nothing is reported rather than silently omitted. Every unresolved reference adds a line to a `warnings` array on the document it belongs to, so "this player has no pals" is distinguishable from "this player's pal container is not in the world save".

Two limits are worth knowing. `guild` carries only an identifier: a guild's name and member list live in `GroupSaveDataMap`, whose byte layout is not decoded yet. And a resolve reads the world save as it finds it, so a player who has never logged in has no character record and no name.

Resolving is cheap, unlike `--full`. Against a 3.3MB world save containing 2,259 character records, 8,723 item containers and 111,579 `RawData` blobs, resolving all nine players took 0.08s and peaked at 124MB of memory, against `--full`'s 1.07s and 1.6GB on the same file. That is a deliberate property rather than luck: every collection is iterated instead of collected, each is read exactly once per invocation, and a character record's nested property stream is decoded only when the record belongs to a player being resolved. A test asserts the peak heap stays under a fixed budget so a regression to eager decoding fails the build.

Because the output names things like `pals` and `inventory`, it encodes an interpretation that a Palworld update can invalidate. `--full`, `--schema` and `--preset` stay free of that by design and remain the escape hatch; `resolveVersion` marks changes to this shape.

### Use bundled presets

Bundled presets are projection documents tested against a private fixture from one exact Palworld version. List the available names, along with the version each was verified against and the save type it reads:

```sh
palworld-save-reader --list-presets
```

The result is a JSON array. Select a preset by name:

```sh
palworld-save-reader --preset player-details /path/to/player.sav
```

All bundled presets were verified against private fixtures from Palworld `1.0.1.100619`:

| Preset | Save type | Requests |
| --- | --- | --- |
| `player-details` | `player.sav` | Identifier, position and facing, capture record, Paldeck flags, last-online time |
| `player-identity` | `player.sav` | Account and character identifiers, platform, last-online time |
| `player-containers` | `player.sav` | Inventory, equipment, and pal-storage container identifiers |
| `player-progression` | `player.sav` | Technology points, unlocked recipes, completed quests, craft counts |
| `player-quests` | `player.sav` | In-progress quests with their block index and progress counters |
| `player-exploration` | `player.sav` | Fast-travel, area-discovery, and note flags |
| `player-appearance` | `player.sav` | Body, head, and hair meshes, character colours, voice |
| `world-meta` | `LevelMeta.sav` | World name, in-game day, save timestamp and version |

`player-identity` is the one that exposes `IndividualId.InstanceId`, the key that identifies a player's character inside a world save. No preset returns a player's name, because a `player.sav` does not contain one — every string in it is an identifier, an enum, or an asset name. The name is in the world save, reachable with [`--full --decode-raw`](#decode-rawdata-byte-arrays) or, already joined, with [`--resolve player`](#resolve-a-player-across-files).

`player-progression` reports quests a player has finished; `player-quests` reports the ones still open, which is a separate array carrying per-objective counters.

A preset reads one file, so joining a player to the world is not something a preset does. The identifiers each one returns are the keys for that join: against fixtures from `1.0.1.100619`, `IndividualId.InstanceId` matched a `CharacterSaveParameterMap` key, the six `InventoryInfo` container identifiers matched `ItemContainerSaveData` keys, and `PalStorageContainerId` and `OtomoCharacterContainerId` matched `CharacterContainerSaveData` keys — each exactly once. A player save's filename is its `PlayerUId` with the dashes removed. `--full --decode-raw` makes all three of those readable rather than base64, and [`--resolve player`](#resolve-a-player-across-files) walks the join for you. Use the presets when you want the identifiers and nothing interpreted; use `--resolve` when you want the answer.

Presets request only fields that appear in **every** fixture save of their type. Palworld omits properties still holding their default value, so a field like `OilrigClearCount` exists only in saves whose player has cleared one; requesting it would fail strict matching elsewhere. Put such fields in your own `--schema` document with `--allow-partial`.

A preset's `gameVersion` is provenance, not a selector: it records the build the document was verified against and is reported by `--list-presets`, but it plays no part in choosing a preset and does not guarantee compatibility with another game release. Because a name identifies a preset on its own, two bundled documents must not share one. New presets go in [`internal/projection/presets/`](internal/projection/presets) and are only added after being exercised against a private save from the version they declare.

### Projection does not work on `Level.sav`

Projection normalizes the whole decoded property tree before matching, and a world save exceeds the built-in ceiling of 10,000,000 nodes, so `--schema` and `--preset` fail on `Level.sav` regardless of how small the requested shape is:

```text
error: projection: decoded tree exceeds 10000000 values
```

Use `--full` or `--resolve` for world saves. `LevelMeta.sav` and files under `Players/` are far smaller and project normally. Note also that a world save keeps most per-character and per-guild detail inside `RawData` byte blobs, so even a working projection would expose only the identifiers and enums stored outside them. `--full --decode-raw` opens the item-container slots, the character records, and the character-container slots; the rest are still base64.

### Fetch the schema over HTTP

[`projection-v1.schema.json`](projection-v1.schema.json) sits at the top level of the repository, so a tool or editor can read the format contract straight from GitHub instead of vendoring a copy:

```text
https://github.com/LukeHollandDev/palworld-save-reader/raw/main/projection-v1.schema.json
```

The filename carries the format version deliberately. A document declaring `projectionVersion: 1` must keep validating against the v1 contract, so a future version becomes a new file rather than an edit to this one. Pin a release tag instead of `main` when a build needs a copy that cannot change underneath it.

Nothing in the executable reads the schema — `palworld-save-reader` validates projection documents with its own parser, and the schema exists for editors and external tooling. The preset documents are embedded, and are also readable at [`internal/projection/presets/`](internal/projection/presets) if you want one as a starting point.

### Use it from another application

The command writes result JSON to standard output and diagnostics to standard error, making it suitable for pipelines and optional external-process integrations:

```sh
palworld-save-reader --schema wanted.json /path/to/save.sav | jq .
```

An application can discover a configured `palworld-save-reader` executable, invoke it against an immutable save snapshot, parse standard output, and continue without save-derived features if the executable is unavailable. This model works for dashboards, administration tools, backup auditors, data exporters, and monitoring systems written in any language. [Palworld Live Map](https://github.com/LukeHollandDev/palworld-live-map) is one example.

The process exits with status `0` on success, `1` for save decoding, projection matching, or resolve failures, and `2` for invalid command-line use or an invalid projection document. A `--resolve` run writes nothing to standard output until the join has succeeded, so a failed resolve does not leave a partial document behind.

## Save handling

The decoder only performs read operations. When reading saves from a running server, create a separate immutable snapshot first so the game cannot change the file during decoding. Save files, expanded JSON, projected JSON, resolved documents, and matching diagnostics can contain private player information and should not be committed to a public repository. `--resolve` output is the most concentrated form of it: names, account identifiers, coordinates and progression in one document per player.

## Supported formats

This reader targets Palworld 1.X, which writes exactly one container form:

- `PlM` type `0x31`, covering the Mermaid subset used by tested Palworld saves
- GVAS save-game version 3 and custom-version format 3

The Mermaid decoder supports the raw, memset, mode-1 LZ, raw/RLE entropy, and newer Huffman forms found in tested saves. Unsupported Oodle modes are rejected rather than treated as valid.

Nothing else is read. The pre-1.0 `PlZ` zlib containers and the optional 12-byte CNK prefix added by some external save tools are refused with an explicit error rather than decoded on a guess. The project does not encode, recompress, repair, or modify saves.

## Development

The Makefile contains the small set of commands used locally and in CI:

```sh
make build    # build bin/palworld-save-reader for the current platform
make test     # run the test suite
make ci       # check formatting, run go vet, and run race-enabled tests
make dist     # build all supported release executables under dist/
make clean    # remove bin/ and dist/
```

Optional integration tests can run against private save fixtures without adding them to the repository:

```sh
PALWORLD_SAVE_FIXTURES=/path/to/fixtures go test ./...
```

The fixture directory must contain `Level.sav`, `LevelMeta.sav`, and both normal and `_dps.sav` files under `Players/`; [`internal/savefixtures`](internal/savefixtures) validates that layout so a partly populated directory fails instead of quietly reducing coverage. Real saves, player names, account identifiers, projected output, and private test data must remain outside the repository.

## License and provenance

This repository is distributed under **GPL-3.0-or-later**. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

The Mermaid-compatible decoder is a Go reimplementation based on [Powzix's `ooz`](https://github.com/powzix/ooz), via the GPL-labelled [`palsav` package in PalworldSaveTools](https://github.com/deafdudecomputers/PalworldSaveTools/tree/f35b4d740259a7a75b11cccf2b7c35f928c1ab77/src/palsav). The original `ooz` repository describes itself as open source but does not include an explicit license file.

Parts of the container and GVAS property reader were adapted from [Palhelm's Apache-2.0 save package](https://github.com/8tp/palhelm/tree/e099e8afe4823d6cf6b371e5e3938955e5a1becd/backend/internal/sav). The complete executable is distributed as a GPL-3.0-or-later work.

The standalone executable and JSON interface provide a process boundary for consumers that do not import this repository's Go packages.

This is an unofficial compatible reader. It bundles no proprietary Oodle code and is not affiliated with or endorsed by Epic Games, RAD Game Tools, or Pocketpair.

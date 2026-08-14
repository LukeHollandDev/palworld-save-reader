# Palworld Save Reader

[![CI](https://github.com/LukeHollandDev/palworld-save-reader/actions/workflows/ci.yml/badge.svg)](https://github.com/LukeHollandDev/palworld-save-reader/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/LukeHollandDev/palworld-save-reader)](go.mod)
[![License](https://img.shields.io/github/license/LukeHollandDev/palworld-save-reader)](LICENSE)

Turn Palworld 1.X saves into JSON. Resolve a whole save directory into players,
guilds, and world metadata, inspect one save's complete property tree, or select
only the fields another application needs.

## Features

- Joins data across saves to resolve players, inventories, Pals, guilds, bases,
  and workers
- Reports world metadata, timestamps, and entity counts
- Expands complete saves into readable JSON
- Decodes known item, character, group, and base-camp `RawData` layouts
- Reads current and legacy Mermaid Huffman codebooks used by Palworld 1.X
- Selects fields with versioned JSON projections and bundled presets
- Preserves unknown data as base64 instead of discarding it
- Applies bounded input, parser, collection, and projection limits
- Uses pure Go with no cgo, proprietary Oodle libraries, or third-party modules

## Install

Building from source requires Go 1.26.5 or later:

```bash
go install github.com/LukeHollandDev/palworld-save-reader/cmd/palworld-save-reader@latest
```

Prebuilt executables are also available from GitHub Releases and successful
GitHub Actions runs.

To build a clone:

```bash
git clone https://github.com/LukeHollandDev/palworld-save-reader.git
cd palworld-save-reader
make build
```

The executable is written to `bin/palworld-save-reader`.

## Compatibility

The reader accepts the `PlM`/`0x31` Oodle Mermaid container used by Palworld
1.X, including both current and legacy Huffman code-length encodings. It is a
read-only decoder and does not compress or modify saves. Because Palworld's
untagged property schema can grow with game updates, complete-tree support is
continuously checked against private current and historical save corpora.

## Resolve a save directory

Use `--resolve` for interpreted answers that join `Level.sav` with the files
under `Players/`:

```bash
palworld-save-reader --resolve world --saves /path/to/WORLDID
palworld-save-reader --resolve players --saves /path/to/WORLDID
palworld-save-reader --resolve roster --saves /path/to/WORLDID
palworld-save-reader --resolve player --id PLAYER_UID --saves /path/to/WORLDID
palworld-save-reader --resolve guilds --saves /path/to/WORLDID
palworld-save-reader --resolve guild --id GROUP_ID --saves /path/to/WORLDID
```

The directory should contain `Level.sav`, optionally `LevelMeta.sav`, and a
`Players/` directory. Player IDs may use the dashed GUID form or the dash-free
form used in save filenames.

Each result carries a `resolveVersion`. Missing joins are reported in the
document's `warnings` array so an empty collection can be distinguished from
data that could not be resolved.

`roster` is the compact integration-oriented result: player ID, nickname,
level, and guild. Use it when those fields are sufficient; `players` retains
the complete inventory and Pal detail intended for standalone inspection.

## Inspect a complete save

Use `--full` to expand one save into a JSON property tree:

```bash
palworld-save-reader --full /path/to/Level.sav > Level.sav.json
```

Add `--decode-raw` to interpret known `RawData` layouts while preserving the
original base64 bytes:

```bash
palworld-save-reader --full --decode-raw /path/to/Level.sav > Level.sav.json
```

Known layouts include item slots, character records, character-container
references, groups, base camps, and base-camp worker directors. Unknown layouts
remain base64. Full world output can be hundreds of megabytes and use
substantial memory.

> **Privacy:** decoded output can include account identifiers, player names,
> locations, progression, guild details, and server metadata. `*.sav.json` is
> ignored by default; never publish a real decoded save in an issue, example,
> or test fixture.

## Select fields with projections

List the bundled projections:

```bash
palworld-save-reader --list-presets
```

Then apply one by name:

```bash
palworld-save-reader --preset player-details /path/to/player.sav
```

The presets cover player identity, details, containers, progression, quests,
exploration, appearance, and world metadata. Their reported `gameVersion`
records the fixture version they were verified against; it is not a
compatibility guarantee.

For a custom shape, create a projection document such as
`player-location.json`:

```json
{
  "$schema": "https://github.com/LukeHollandDev/palworld-save-reader/raw/main/projection-v1.schema.json",
  "projectionVersion": 1,
  "name": "player-location",
  "gameVersion": "1.0.1.100619",
  "saveType": "player.sav",
  "shape": {
    "PlayerUId": "",
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

```bash
palworld-save-reader --schema player-location.json /path/to/player.sav
```

Requested field names are case-sensitive serialized property names. Projection
placeholders describe the expected value:

| Placeholder | Requested value |
| --- | --- |
| `{ "Field": ... }` | Object containing that exact child name |
| `[ ... ]` | Repeated collection using one element shape |
| `null` | Any JSON-compatible value |
| `""` | String |
| `0` | Number |
| `false` | Boolean |

Matching is strict by default. `--allow-partial` emits `null` for unresolved or
incompatible fields, while `--explain` writes matching diagnostics to standard
error.

Projection normalizes the complete decoded tree and cannot process a large
`Level.sav` within its built-in node limit. Use `--full` or `--resolve` for
world saves. The projection format contract is
[`projection-v1.schema.json`](projection-v1.schema.json).

## Command reference

Every invocation requires one explicit mode:

```text
palworld-save-reader --full [--decode-raw] FILE
palworld-save-reader --schema PROJECTION.json [--allow-partial] [--explain] FILE
palworld-save-reader --preset NAME [--allow-partial] [--explain] FILE
palworld-save-reader --resolve player --id UID --saves DIR
palworld-save-reader --resolve guild --id GROUPID --saves DIR
palworld-save-reader --resolve players|roster|guilds|world --saves DIR
palworld-save-reader --list-presets
palworld-save-reader --list-resolvers
palworld-save-reader --version
```

Results are written to standard output and diagnostics to standard error. Exit
status `0` means success, `1` means the save could not be decoded, projected, or
resolved, and `2` means the command or projection document was invalid.

The executable and its JSON formats are the supported compatibility boundary.
All Go packages are under `internal/` and are implementation details rather
than an importable API.

## Development

```bash
make build    # build bin/palworld-save-reader
make test     # run the test suite
make ci       # check formatting, vet, and run race-enabled tests
make dist     # build release executables under dist/
make clean    # remove bin/ and dist/
```

Integration tests can use private fixtures without adding them to the
repository:

```bash
PALWORLD_SAVE_FIXTURES=/path/to/fixtures go test ./...
```

The fixture directory must contain `Level.sav`, `LevelMeta.sav`, and normal and
`_dps.sav` files under `Players/`.

To exercise only the container and Mermaid decoder against every save in a
larger backup collection, point `PALWORLD_SAVE_CORPUS` at its root:

```bash
PALWORLD_SAVE_CORPUS=/path/to/save/backups go test ./internal/savefile -run SuppliedSaveCorpus
```

## License

Palworld Save Reader is licensed under **GPL-3.0-or-later**. See
[LICENSE](LICENSE) and [NOTICE](NOTICE) for the licence and upstream
attributions.

This is an independent, fan-made project. It bundles no proprietary Oodle code
and is not affiliated with or endorsed by Epic Games, RAD Game Tools, or
Pocketpair.

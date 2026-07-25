# palworld-save-reader

[![CI](https://github.com/LukeHollandDev/palworld-save-reader/actions/workflows/ci.yml/badge.svg)](https://github.com/LukeHollandDev/palworld-save-reader/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/LukeHollandDev/palworld-save-reader)](go.mod)
[![License](https://img.shields.io/github/license/LukeHollandDev/palworld-save-reader)](LICENSE)

`palworld-save-reader` is a standalone command-line tool for reading one Palworld `.sav` file and writing JSON. It can expand the complete decoded property tree for inspection or apply a caller-provided projection that requests only the serialized fields and structure an application needs.

The decoder is implemented in pure Go and does not require cgo, proprietary Oodle libraries, or third-party Go dependencies. The executable is the supported interface; packages under `internal/` are implementation details and cannot be imported by other projects.

## Features

- Reads Mermaid-compressed and legacy zlib-compressed Palworld saves
- Selects fields with caller-provided, versioned JSON projection documents
- Preserves the requested JSON keys, nesting, array structure, and key order
- Rejects missing, incompatible, or ambiguous matches by default
- Produces a complete expanded property tree for format inspection
- Applies bounded input, collection, parser, and projection limits
- Never writes to or modifies a source save
- Builds for Linux, macOS, and Windows

## Installation

Go 1.26.5 or later is required when building from source.

```sh
go install github.com/LukeHollandDev/palworld-save-reader/cmd/savedecode@latest
```

Prebuilt executables are also available from successful GitHub Actions runs and published GitHub Releases.

To build from a clone:

```sh
git clone https://github.com/LukeHollandDev/palworld-save-reader.git
cd palworld-save-reader
make build
```

The executable is written to `bin/savedecode`.

## Usage

Every operation requires an explicit mode:

```text
savedecode --full FILE
savedecode --schema PROJECTION.json [--allow-partial] [--explain] FILE
savedecode --preset NAME --game-version VERSION [--allow-partial] [--explain] FILE
savedecode --list-presets
```

### Project selected fields

A projection document describes the exact JSON shape to return. It is not an official Palworld schema or a conventional JSON Schema, and its output keys are not application-specific aliases: requested keys must match serialized save-property names exactly and are matched case-sensitively.

For example, this projection requests an identifier, last-online value, and position from a compatible structure in a player save:

```json
{
  "$schema": "./internal/projection/assets/projection.schema.json",
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
savedecode --schema player-location.json /path/to/player.sav
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
savedecode --schema player-location.json --explain /path/to/player.sav > player.json
```

Strict matching is the default. `--allow-partial` writes `null` for unresolved or incompatible fields and reports each problem to standard error:

```sh
savedecode --schema player-location.json --allow-partial /path/to/player.sav
```

Projection version 1 operates on one physical save at a time. It does not join `Level.sav` with files under `Players/`, rename fields, calculate values, aggregate records, sort, or filter; the calling application remains responsible for those operations.

### Inspect a complete save

Use `--full` to expand one save into a JSON property tree:

```sh
savedecode --full /path/to/Level.sav
```

Full mode is useful for discovering serialized field names and researching format changes before writing a projection. Large world saves can produce hundreds of megabytes of JSON and require substantial memory. Output may include player names, identifiers, and other private save content, so review it before sharing.

### Use bundled presets

Bundled presets are projection documents tested against a private fixture from one exact Palworld version. List the available name, version, and save-type combinations:

```sh
savedecode --list-presets
```

The result is a JSON array. Select a preset with an exact version rather than an implicit latest alias:

```sh
savedecode --preset player-details --game-version 1.0.1.100619 /path/to/player.sav
```

The repository currently includes `player-details` for Palworld `1.0.1.100619`. It extracts raw identifier, position, capture-record, Paldeck, and last-online fields from one player save without calculating summary values. A preset should be added under `internal/projection/assets/palworld/VERSION/` only after it has been exercised against a private save from that version. A preset documents tested compatibility and does not guarantee compatibility with another game release.

### Use it from another application

The command writes result JSON to standard output and diagnostics to standard error, making it suitable for pipelines and optional external-process integrations:

```sh
savedecode --schema wanted.json /path/to/save.sav | jq .
```

An application can discover a configured `savedecode` executable, invoke it against an immutable save snapshot, parse standard output, and continue without save-derived features if the executable is unavailable. This model works for dashboards, administration tools, backup auditors, data exporters, and monitoring systems written in any language. [Palworld Live Map](https://github.com/LukeHollandDev/palworld-live-map) is one example.

The process exits with status `0` on success, `1` for save decoding or projection matching failures, and `2` for invalid command-line use or an invalid projection document.

## Save handling

The decoder only performs read operations. When reading saves from a running server, create a separate immutable snapshot first so the game cannot change the file during decoding. Save files, expanded JSON, projected JSON, and matching diagnostics can contain private player information and should not be committed to a public repository.

## Supported formats

- `PlM` type `0x31`, covering the Mermaid subset used by tested Palworld saves
- `PlZ` type `0x31` for single zlib compression
- `PlZ` type `0x32` for double zlib compression
- The optional 12-byte CNK prefix used by some save tools
- GVAS save-game version 3 and custom-version format 3

The Mermaid decoder supports the raw, memset, mode-1 LZ, raw/RLE entropy, and newer Huffman forms found in tested saves. Unsupported Oodle modes are rejected rather than treated as valid. The project does not encode, recompress, repair, or modify saves.

## Development

The Makefile contains the small set of commands used locally and in CI:

```sh
make build    # build bin/savedecode for the current platform
make test     # run the test suite
make ci       # check formatting, run go vet, and run race-enabled tests
make dist     # build all supported release executables under dist/
make clean    # remove bin/ and dist/
```

Optional integration tests can run against private save fixtures without adding them to the repository:

```sh
PALWORLD_SAVE_FIXTURES=/path/to/fixtures go test ./internal/palsav ./internal/projection
```

The fixture directory may contain `Level.sav`, `LevelMeta.sav`, and files under `Players/`. Real saves, player names, account identifiers, projected output, and private test data must remain outside the repository.

## Project layout

```text
cmd/savedecode/             command-line interface
internal/palsav/            container decompression and GVAS property parser
internal/projection/        projection contract, matcher, output, and embedded assets
```

## License and provenance

This repository is distributed under **GPL-3.0-or-later**. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

The Mermaid-compatible decoder is a Go reimplementation based on [Powzix's `ooz`](https://github.com/powzix/ooz), via the GPL-labelled [`palsav` package in PalworldSaveTools](https://github.com/deafdudecomputers/PalworldSaveTools/tree/f35b4d740259a7a75b11cccf2b7c35f928c1ab77/src/palsav). The original `ooz` repository describes itself as open source but does not include an explicit license file.

Parts of the container and GVAS property reader were adapted from [Palhelm's Apache-2.0 save package](https://github.com/8tp/palhelm/tree/e099e8afe4823d6cf6b371e5e3938955e5a1becd/backend/internal/sav). The complete executable is distributed as a GPL-3.0-or-later work.

The standalone executable and JSON interface provide a process boundary for consumers that do not import this repository's Go packages.

This is an unofficial compatible reader. It bundles no proprietary Oodle code and is not affiliated with or endorsed by Epic Games, RAD Game Tools, or Pocketpair.

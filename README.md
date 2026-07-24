# palworld-save-reader

A standalone, pure-Go (no cgo, no C++, no proprietary Oodle runtime, no helper
process) reader for Palworld `.sav` files. Decodes both `PlZ` (zlib) and `PlM`
(Oodle "Mermaid") containers, parses Unreal's GVAS archive, and projects a
compact, bounded, read-only player roster for a host application.

Go 1.26. Module path `github.com/LukeHollandDev/palworld-save-reader`
(not yet hosted — consume it locally with a `replace`, see below).

## Packages

- **`palsav`** — the decoder core: container decompression (zlib + Oodle-Mermaid)
  and a full GVAS property-tree parser. Zero third-party dependencies.
  `palsav.Load(path) (*Save, error)` gives the complete decoded tree; lazy
  map/set/struct-array collections are materialised on demand via iterators.
- **`savegame`** — a bounded, read-only, **selective** reader over `palsav`.
  `ReadSnapshot(ctx, dir)` extracts only the fields a live map needs (player
  GUID, display name, level, guild, X/Y, last-online, capture totals, unique
  Pals, Paldeck count) without materialising the whole save.
- **`cmd/savedecode`** — a thin CLI (see below).

## CLI

```sh
# Compact roster JSON — the stable machine contract (what a host app consumes):
savedecode <snapshot-dir>            # dir containing Level.sav (+ optional Players/)

# Full expanded decode of ONE .sav — human inspection of EVERYTHING available:
savedecode --full <file.sav>
```

Roster mode is selective and cheap (a real 3.4 MB `Level.sav` + 9 players decodes
in ~0.4 s, ~80 MB RAM, ~4 KB JSON). Full mode materialises the entire property
tree and is correspondingly heavy on a world save (hundreds of MB JSON, ~1.5 GB
RAM) — it is an inspection tool, not the production path.

## Using it from another module locally (before it is hosted)

In the consuming module's `go.mod`:

```
require github.com/LukeHollandDev/palworld-save-reader v0.0.0
replace github.com/LukeHollandDev/palworld-save-reader => ../palworld-save-reader
```

Then `import ".../palworld-save-reader/savegame"` and call `ReadSnapshot`, or
build the `savedecode` sidecar and exec it. When you later push this to GitHub,
drop the `replace` and pin a tag.

## Licensing

**GPL-3.0-or-later.** The Oodle-Mermaid decompressor (`palsav/entropy.go`,
`palsav/mermaid.go`) is a Go port of Powzix `ooz` (via PalworldSaveTools); the
GVAS/property reader and `savegame` are Apache-2.0 (derived from Palhelm).
Because the GPL code is compiled in, the module as a whole is GPL-3.0-or-later.
See [NOTICE](NOTICE) and [LICENSE](LICENSE) for exact provenance.

Keeping this in its own module lets a permissively-licensed host application use
it at arm's length — build it as a separate `savedecode` binary and exec it, so
the GPL stays isolated to this component and does not relicense the host.

This is an unofficial compatible reader. It is not affiliated with or endorsed by
Epic Games, RAD Game Tools, or Pocketpair, and bundles no proprietary Oodle code.

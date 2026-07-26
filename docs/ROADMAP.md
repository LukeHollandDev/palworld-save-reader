# Roadmap: nested decoding, package layout, and automatic joins

Status: **phases 0 and 1 are done**; phases 2–4 are not started. Written
2026-07-25 against the working tree that narrows the decoder to Palworld 1.X and
names the executable `palworld-save-reader`.

The executable is named for the module and repository exactly. A short name was
considered and rejected: the reason to prefer one was distance from the old
`internal/palsav` package, and phase 0 deleted that package. The documented
integration model is another program spawning this binary, where an exact name
beats a terse one, and "reader" keeps the read-only guarantee visible. Humans
working interactively can alias it.

## Why

The tool answers "what does this one file contain". The higher-value question is
"what does this player have", and that answer spans two files plus a layer of
undecoded blobs:

```
player.sav  →  CommonContainerId: e0d93f36-…      ← all we can return today
Level.sav   →  ItemContainerSaveData[e0d93f36-…]  ← Money x70434, PalSphere x684, …
```

A measured proof of concept resolved one player's five inventory containers
across both files in **64ms using 100MB of heap**, scanning all 8,723 containers
with `gvas.MapValue.Iterator`. For comparison, `--schema` on the same `Level.sav`
allocates **1.9GB and then fails** at the 10,000,000-node ceiling in
`internal/projection/data.go`. The decoder is not the problem; the projection
layer's eager `normalizeSource` is.

## What is actually in the way

### Two, possibly three, flavours of `RawData`

Most interesting world-save content sits in `RawData` byte arrays that the
decoder currently surfaces as base64. They are not one format:

| Flavour | Where | Layout | Status |
| --- | --- | --- | --- |
| Fixed record | `ItemContainerSaveData` slots | `uint32 slotIndex`, `uint32 count`, `FString itemId`, 16 zero bytes, 16-byte dynamic-item id, trailer | **Decoded** in phase 1; see below |
| Nested GVAS | `CharacterSaveParameterMap` values | A complete Unreal property stream: `SaveParameter` / `StructProperty` / `PalIndividualCharacterSaveParameter`, then `Level` / `ByteProperty`, … | **Header verified.** Exact framing (trailer bytes, terminators) still to work out |
| Bespoke binary | `GroupSaveDataMap` values | Opens with a 16-byte GUID then counters; no GVAS framing | **Unverified.** Needs reverse engineering |

### What phase 1 found

The item-slot layout was close to the description above but not identical, and
three details only became clear by running the decoder over every slot rather
than reading a few by hand:

- **There is no padding, there is a field.** The 16 bytes at trailer offset 16
  are a dynamic-item id keying `worldSaveData.DynamicItemSaveData`, where
  durability and similar per-instance state lives. It is non-zero on 422 of
  27,094 slots, and all 422 resolve to a record.
- **Empty slots are not stored.** All 27,094 slots in the fixture world are
  occupied; a container lists only what it holds. `ItemSlot.Empty` exists for a
  zero-length id, but no fixture slot exercises it.
- **The record is not fixed-width.** 26,572 slots end in an all-zero trailer, 455
  carry the dynamic-item id, and 67 carry a further undecoded structure. The
  trailer is preserved rather than assumed to be padding.

The path table was also wrong on the first attempt, in a way that failed
silently: a struct array repeats its own name for its elements, so the slots sit
at `…ItemContainerSaveData.Value.Slots.Slots.RawData`, not `….Slots.RawData`. The
wrong path simply matched nothing while every test still passed.
`TestRawDataPathsExistInWorldSave` now walks a real save and requires every
declared path to be found, which is the check that would have caught it.

`CharacterContainerSaveData` has a `Slots.Slots.RawData` of its own — 2,250
blobs of 39 bytes, holding pal references rather than items. It is a different
layout and is not decoded.

The nested-GVAS case is the big unlock: it is the same encoding
`internal/gvas`'s property reader already handles, so pal levels, IVs,
nicknames, HP and passives are behind a re-entrant call into existing code rather
than a new parser.

### Volume

`Level.sav` holds 111,579 `RawData` blobs. Character blobs are ~3.4KB each, so
eagerly decoding everything would add hundreds of megabytes to a `--full` dump
that is already 213MB. **Nested decoding must be lazy** — decoded on access, the
way `MapValue.Iterator` already works — and opt-in for `--full`.

Phase 1 found laziness came free: `gvas` already hands back a `RawData` blob
without touching it, so a decoder that is a plain function over bytes costs
nothing until it is called. No wrapper type was needed. The measured cost of
`--full --decode-raw` on the fixture world is +3.5% JSON (223.8MB to 231.7MB),
+0.28s (1.08s to 1.36s), and +3% peak RSS. Without the flag the output is
byte-identical to before, which is checked rather than assumed.

Where the blobs are, from a walk of the fixture world:

| Path | Blobs | Bytes each | Phase |
| --- | --- | --- | --- |
| `MapObjectSaveData` (several sub-paths) | 63,724 | 0–3,213 | — |
| `ItemContainerSaveData.Value.Slots.Slots` | 27,094 | 21–351 | **1, done** |
| `FoliageGridSaveDataMap` | 6,273 | 51–108 | — |
| `CharacterContainerSaveData.Value.Slots.Slots` | 2,250 | 39 | — |
| `CharacterSaveParameterMap.Value` | 2,259 | 2,628–4,140 | **2** |
| `ItemContainerSaveData.Value` | 8,723 | — | — |
| `DynamicItemSaveData` | 581 | 57–1,518 | 2 or 3 |
| `GroupSaveDataMap.Value` | 15 | 39–16,287 | **4** |
| `BaseCampSaveData` (several sub-paths) | 192 | 0–1,512 | **4** |

## Package layout

The old layout had two problems: `internal/palsav` differed from the executable
by a single letter, and it bundled three unrelated jobs (container
decompression, Unreal deserialization, Palworld specifics) that the new work
needs to extend independently.

### Before

```
cmd/palworld-save-reader/   CLI
internal/palsav/            container + GVAS + Palworld hints + paths + values
internal/palsav/oodle/      Mermaid decompressor
internal/projection/        projection contract, matcher, output, presets
```

### Now, as of phase 0

```
cmd/palworld-save-reader/       CLI and output encoding
internal/savefile/              .sav container: PlM/0x31 header, body extraction, byte limits
internal/savefile/mermaid/      Oodle Mermaid decompressor
internal/gvas/                  Unreal GVAS: header, properties, values, lazy collections
internal/palworld/              Palworld specifics: type hints, save entry points
internal/savefixtures/          private-fixture discovery for tests
internal/projection/            declarative shape extraction from one save
internal/resolve/               cross-save joins — the automatic layer (phase 3)
```

Each name states its job, and nothing is called `palsav`. Where the old files
went:

| Before | Now |
| --- | --- |
| `palsav/container.go` | `savefile/container.go` |
| — | `savefile/savefile.go` (new: `Options`, `Save`, `Read`, `Open`) |
| `palsav/oodle/*` | `savefile/mermaid/*` |
| `palsav/gvas.go` | `gvas/archive.go` |
| `palsav/reader.go`, `properties.go`, `values.go`, `structs.go`, `collections.go`, `types.go`, `paths.go` | `gvas/*` |
| `palsav/palworld_hints.go` | `palworld/hints.go` |
| — | `palworld/palworld.go` (new: `Load`, `TypeHints`) |
| — | `savefixtures/savefixtures.go` (new, extracted from three test copies) |

### Dependency direction

Direct internal imports, verified with `go list`:

| Package | Imports |
| --- | --- |
| `gvas` | — |
| `savefile/mermaid` | — |
| `savefile` | `gvas`, `savefile/mermaid` |
| `palworld` | `savefile` |
| `savefixtures` | — |
| `projection` | `gvas` |
| `cmd/palworld-save-reader` | `gvas`, `palworld`, `projection` |

`savefile` imports `gvas` rather than the reverse. That was the one real design
decision in phase 0: `gvas` has to be the generic leaf if it is to stay free of
game knowledge, so the package that understands "a .sav file" sits above it and
composes container-unwrap with parse. `savefile.Read` returns a `savefile.Save`
that embeds the `*gvas.Archive`, so `save.Properties` still reads directly.

`palworld` is the only package allowed to know game semantics. Moving the
type-hint table there means `gvas` now ships **zero** built-in hints, and
`TestNoBuiltInTypeHints` fails the build if any come back — the layering is
enforced rather than merely documented. `palworld.Load` merges the table under
any caller hints, so a Palworld update can be worked around from outside.

`projection` turned out to need only `gvas`: it matches shapes against already
decoded properties and never loads a file, so `cmd` does the loading. That falls
out of the split rather than being designed, and it is worth keeping.

### Naming changes

Renamed while moving, since the old names either stuttered or described the wrong
scope:

| Before | Now |
| --- | --- |
| `palsav.Save` | `gvas.Archive`, wrapped by `savefile.Save` |
| `palsav.GVASHeader` | `gvas.Header` |
| `palsav.ParseGVAS` / `ParseGVASWithOptions` | `gvas.Parse` / `gvas.ParseWithOptions` |
| `palsav.Load` | `palworld.Load` (hints applied) or `savefile.Open` (raw) |
| `palsav.Decode` | `palworld.Read` or `savefile.Read` |
| `palsav.Limits{MaxInputBytes, MaxOutputBytes}` | `savefile.Limits{MaxFileBytes, MaxArchiveBytes}` |
| `palsav.Options.Limits` embedded in GVAS options | `savefile.Options{Limits, GVAS}`; `gvas.Options.MaxArchiveBytes` |

`gvas.LimitError` stayed one type and lost its package prefix, because
`savefile` raises it for the byte limits it owns and "gvas: container input
bytes …" would have been wrong. `Kind` names the subject instead.

Phase 0 changed no decoding behaviour, and that was checked rather than assumed:
`internal/gvas/testdata/decode_golden.json` differs only in Go type names and
the error prefix, and the 47 projected outputs in the private fixture set are
byte-identical before and after.

## CLI surface

Phase 1 added one flag, `--full --decode-raw`, which annotates a recognised
`RawData` blob with a `decoded` object beside its base64 rather than replacing
it. It is opt-in, rejected with the projection modes, and reports a bad blob as
`decodeError` inline rather than failing the dump. Phase 2 extends the same hook
with a second layout; no new mode is needed for it.

One new mode is still to come:

```text
palworld-save-reader --resolve KIND [--id VALUE] --saves DIR
```

| KIND | Returns |
| --- | --- |
| `player` | One fully resolved player; `--id` takes a player UID |
| `players` | Every player in the save set |
| `guild` | One guild with members; `--id` takes a group id |
| `guilds` | Every guild |
| `world` | World metadata, in-game time, and entity counts |

`--saves DIR` discovers `Level.sav`, `LevelMeta.sav` and `Players/*.sav`, so a
caller points at a save directory rather than naming files. A resolved player
document is composed, not projected:

```json
{
  "playerUId": "…", "instanceId": "…", "platform": "…", "lastOnline": …,
  "position": { "x": …, "y": …, "z": … },
  "character": { "level": …, "exp": …, "hp": … },
  "technologyPoints": …,
  "inventory": {
    "common":    [ { "slot": 0, "itemId": "Money", "count": 70434 }, … ],
    "essential": [ … ], "weapons": [ … ], "armor": [ … ], "food": [ … ]
  },
  "pals": [ { "instanceId": "…", "species": "…", "level": …, "nickname": "…" }, … ],
  "guild": { "id": "…", "name": "…", "memberCount": … }
}
```

## Keep projection dumb

Joins do **not** go into the projection document format. Projection's guarantee
is "shape in, same shape out, no semantics", and cross-file joins would require
join keys, cardinality rules, missing-reference policy and merge naming — a query
language, which the README explicitly disclaims. `--resolve` is hand-written Go
with an output shape versioned by the tool. That also isolates the layer that
encodes game semantics and will therefore break on Palworld updates.

The consequence for presets: `player-containers` stays useful as the dumb view,
and `--resolve player` is the one that follows those identifiers through.

## Memory rules

These are requirements, not aspirations, and R4 makes them enforceable.

- **R1** Never call `Entries()`, `Values()` or `normalizeSource` on a `Level.sav`
  collection. Iterate.
- **R2** One pass over `Level.sav` per invocation, no matter how many entities
  are requested. Collect the wanted id set first, then fill it in during a single
  scan — resolving nine players must not scan 8,723 containers nine times.
- **R3** Stream output. `--resolve players` encodes each element as it is
  produced rather than building the whole array first.
- **R4** A CI test asserts peak heap stays under a fixed budget while resolving
  the whole fixture save set, so a regression to eager decoding fails the build.

## Phases

| Phase | Work | Size | Unblocks | State |
| --- | --- | --- | --- | --- |
| 0 | Executable rename to `palworld-save-reader`, then package rename and split; pure motion, no logic change | S | Coherent homes for phases 1–4 | **done** |
| 1 | `palworld/rawdata.go`: item-slot decoding, lazily; `--full` opt-in flag | S | Container contents visible | **done** |
| 2 | Nested GVAS `RawData` decoding via a re-entrant `gvas` call | M | Pal and character detail — the big one | next |
| 3 | `internal/resolve` + `--resolve player\|players\|world`; R1–R4 | M | The automatic joins | |
| 4 | `GroupSaveDataMap` and `BaseCampSaveData` reverse engineering; `--resolve guild\|guilds` | ? | Guilds and bases | |

Phase 0 went first because phases 1–4 need `palworld/` and `resolve/` to exist;
adding them alongside `internal/palsav` would have been incoherent. It is a large
diff but a mechanical one — review with `git diff --find-renames`.

## Risks

- **Phase 4 is unestimated** because the group blob layout is unverified. It may
  be a short afternoon or a long slog; treat the phase table as four items plus
  one unknown.
- **Semantic drift.** Once `--resolve` names things like `pals` and `inventory`,
  a Palworld update can change what those mean. Mitigation: keep `--full`,
  `--schema` and `--preset` semantics-free so there is always a raw escape hatch,
  and version the resolve output shape.
- **Phase 2 framing.** The nested stream header is confirmed, but hand-parsing it
  broke a few properties in during investigation. Budget time for the trailer and
  terminator details, and lean on the fixtures.
- **Fixture coverage.** All of this is verified against one world and nine player
  saves from `1.0.1.100619`. Guild and base work in particular would benefit from
  a save set with more social structures in it.

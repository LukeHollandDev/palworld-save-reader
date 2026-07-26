# Roadmap: nested decoding, package layout, and automatic joins

Status: **phases 0, 1 and 2 are done**; phases 3 and 4 are not started. Written
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
| Nested GVAS | `CharacterSaveParameterMap` values | A complete Unreal property stream with no header: one `SaveParameter` / `StructProperty` / `PalIndividualCharacterSaveParameter`, then a 24-byte trailer | **Decoded** in phase 2; see below |
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
- **The record is not fixed-width.** Of 27,094 slots, 26,572 carry nothing after
  the item id, 375 carry only the dynamic-item id, 47 carry the id and a
  non-zero trailer, and 100 carry a trailer with no id. The trailer is preserved
  rather than assumed to be padding.

  (An earlier version of this section split those four figures as 26,572 / 455 /
  67. That was wrong: it was written from a partial reading rather than
  recomputed, and phase 2 re-ran the census because the numbers did not
  reconcile with the 422 non-zero dynamic-item ids reported two bullets up. The
  four figures above sum to 27,094 and agree with 422 = 375 + 47.)

The path table was also wrong on the first attempt, in a way that failed
silently: a struct array repeats its own name for its elements, so the slots sit
at `…ItemContainerSaveData.Value.Slots.Slots.RawData`, not `….Slots.RawData`. The
wrong path simply matched nothing while every test still passed.
`TestRawDataPathsExistInWorldSave` now walks a real save and requires every
declared path to be found, which is the check that would have caught it.

`CharacterContainerSaveData` has a `Slots.Slots.RawData` of its own — 2,250
blobs of 39 bytes, holding pal references rather than items. It is a different
layout and is not decoded.

### What phase 2 found

The nested-GVAS case was the big unlock, and it was the same encoding
`internal/gvas`'s property reader already handles, so it cost a new entry point
rather than a new parser:

```go
func gvas.ParseProperties(data []byte, path string, options Options) (Properties, []byte, error)
```

That is the whole of the re-entrancy. `Parse` reads a header then a property
list; `ParseProperties` reads the property list alone and hands back whatever
followed the terminator. `palworld.DecodeCharacter` calls it with the blob's own
property path, which is what lets one type-hint table serve both an archive and
the streams inside it — and a test checks the same stream decodes differently
when parsed at the root, so the path is doing real work rather than being
decoration.

Measured against all 2,259 character blobs in the fixture world:

- **The framing is 24 bytes, identical in shape everywhere.** Four zero bytes, a
  16-byte group id, four more zero bytes. The eight zero bytes are kept rather
  than skipped, because nothing here knows what they are; `--decode-raw` reports
  the trailer only when something in it is non-zero, so today it never appears.
- **The group id is a real join key.** Eight distinct values, and all eight
  resolve to a `GroupSaveDataMap` entry. Sixteen bytes read at the wrong offset
  can look like a plausible GUID, so resolving is the test that the offset is
  right, exactly as it was for the item slots' `DynamicItemID`.
- **Nothing inside needed a new type hint.** Not one property in any of the 2,259
  streams fell back to `gvas.UndecodedValue`. That was not the expectation — the
  risk noted below was that hand-parsing had broken properties during
  investigation — and it is asserted rather than assumed, so a Palworld update
  that introduces an untagged struct fails the build instead of silently
  degrading.
- **The player/pal split is clean.** 9 records carry `IsPlayer`, 2,250 do not, and
  a pal omits the property rather than setting it false. The 9 player records'
  `PlayerUId` keys match the 9 files under `Players/` one for one, and all 9
  carry a non-empty `NickName`.

The last point is the phase's actual deliverable: **a player's name is not in
their own save at all.** Every string in a `player.sav` is a GUID, an enum, or an
asset name, so no projection of it can return a name. The name is in the world
save's character record, which is why this phase is what makes `--resolve` worth
building.

The trailer is also, unexpectedly, the only bespoke part. There was no need to
work out terminators or property framing by hand; the risk register below
budgeted time for that and it did not materialise.

`DecodeCharacter` is deliberately permissive about the framing and strict about
the stream. An unfamiliar trailer width still yields a readable character,
because a game update that extends the framing should not make a save
unreadable; a stream that will not parse is an error, because that is the claim
the decoder is making.

### Volume

`Level.sav` holds 111,579 `RawData` blobs. Character blobs are ~3.4KB each, so
eagerly decoding everything would add hundreds of megabytes to a `--full` dump
that is already 213MB. **Nested decoding must be lazy** — decoded on access, the
way `MapValue.Iterator` already works — and opt-in for `--full`.

Phase 1 found laziness came free: `gvas` already hands back a `RawData` blob
without touching it, so a decoder that is a plain function over bytes costs
nothing until it is called. No wrapper type was needed, and phase 2 needed none
either — a nested parse is a function over the same bytes.

Measured on the fixture world, cumulatively:

| `--full` | JSON | Time | Peak RSS |
| --- | --- | --- | --- |
| default | 223,826,982 B | 1.16s | baseline |
| `--decode-raw`, phase 1 | 231,733,689 B (+3.5%) | 1.36s | +3% |
| `--decode-raw`, phase 2 | 270,196,593 B (+20.7%) | 1.66s | +6% |

Character blobs are ~3.4KB of dense property data, so they cost far more JSON per
blob than the item slots did despite being twelve times fewer. Without the flag
the output is byte-identical to before both phases, checked by hash rather than
assumed.

Where the blobs are, from a walk of the fixture world:

| Path | Blobs | Bytes each | Phase |
| --- | --- | --- | --- |
| `MapObjectSaveData` (several sub-paths) | 63,724 | 0–3,213 | — |
| `ItemContainerSaveData.Value.Slots.Slots` | 27,094 | 21–351 | **1, done** |
| `FoliageGridSaveDataMap` | 6,273 | 51–108 | — |
| `CharacterContainerSaveData.Value.Slots.Slots` | 2,250 | 39 | — |
| `CharacterSaveParameterMap.Value` | 2,259 | 2,628–4,140 | **2, done** |
| `ItemContainerSaveData.Value` | 8,723 | — | — |
| `DynamicItemSaveData` | 581 | 57–1,518 | 3 |
| `GroupSaveDataMap.Value` | 15 | 39–16,287 | **4** |
| `BaseCampSaveData` (several sub-paths) | 192 | 0–1,512 | **4** |

The 2,250 `CharacterContainerSaveData` slots and the 2,250 pal records are the
same population counted two ways, which is a useful cross-check: those 39-byte
slots hold the pal references that turn a player's `PalStorageContainerId` into a
list of the records phase 2 now decodes. They are the smallest remaining piece of
the player picture and the natural first thing to reach for in phase 3.

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
internal/palworld/              Palworld specifics: type hints, save entry points, RawData layouts
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
| `palworld` | `gvas`, `savefile` |
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

`palworld` gained a direct `gvas` import in phase 1 and leans on it in phase 2:
its `RawData` decoders return `gvas` types and, for the nested streams, call back
into `gvas.ParseProperties`. That is the layering working as intended rather than
a leak — the arrow still points down, and the game knowledge (which path holds
which layout, and how the bytes after the terminator are framed) stays here.

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
`decodeError` inline rather than failing the dump.

Phase 2 added no flag at all: the character layout is a second entry in the same
table behind the same flag. Its `decoded` object carries a `properties` list
rendered by the same expander as the rest of the dump, at the blob's own property
path, so a consumer walks a nested character exactly as it walks anything else and
a `RawData` nested deeper still would be classified by where it actually sits.
Adding a third layout in phase 4 should again be a table entry and a decoder.

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
| 1 | `palworld/itemslot.go`: item-slot decoding, lazily; `--full` opt-in flag | S | Container contents visible | **done** |
| 2 | `gvas.ParseProperties` and `palworld/character.go`: nested property streams via a re-entrant call | M | Pal and character detail — the big one | **done** |
| 3 | `internal/resolve` + `--resolve player\|players\|world`; R1–R4 | M | The automatic joins | next |
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
- ~~**Phase 2 framing.** The nested stream header is confirmed, but hand-parsing
  it broke a few properties in during investigation. Budget time for the trailer
  and terminator details, and lean on the fixtures.~~ Did not materialise. The
  stream needed no hand-parsing at all once it was handed to the existing property
  reader, and the trailer turned out to be 24 bytes of fixed shape. The earlier
  breakage was an artefact of parsing by hand during investigation, not a property
  of the format.
- **Phase 3 memory.** Phase 2 makes it cheap to decode 2,259 dense records, which
  is exactly the temptation R1–R4 exist to resist. `--resolve players` must
  collect the wanted id set first and decode only the records it needs, not
  decode every character and filter.
- **Fixture coverage.** All of this is verified against one world and nine player
  saves from `1.0.1.100619`. Guild and base work in particular would benefit from
  a save set with more social structures in it.

# Roadmap: nested decoding, package layout, and automatic joins

Status: **phases 0 to 4 are done**; nothing after phase 4 is planned yet. Written
2026-07-25 and revised 2026-07-26 against the working tree that narrows the decoder
to Palworld 1.X and names the executable `palworld-save-reader`.

The executable is named for the module and repository exactly. A short name was
considered and rejected: the reason to prefer one was distance from the old
`internal/palsav` package, and phase 0 deleted that package. The documented
integration model is another program spawning this binary, where an exact name
beats a terse one, and "reader" keeps the read-only guarantee visible. Humans
working interactively can alias it.

## Why

The tool answered "what does this one file contain". The higher-value question is
"what does this player have", and that answer spans two files plus a layer of
undecoded blobs:

```
player.sav  →  CommonContainerId: e0d93f36-…      ← all we could return
Level.sav   →  ItemContainerSaveData[e0d93f36-…]  ← Money x70434, PalSphere x684, …
```

A measured proof of concept resolved one player's five inventory containers
across both files in **64ms using 100MB of heap**, scanning all 8,723 containers
with `gvas.MapValue.Iterator`. For comparison, `--schema` on the same `Level.sav`
allocates **1.9GB and then fails** at the 10,000,000-node ceiling in
`internal/projection/data.go`. The decoder is not the problem; the projection
layer's eager `normalizeSource` is.

Phase 3 turned that proof of concept into `--resolve`. The estimate held: the real
thing resolves all nine players, their 2,080 pals and their 467 item stacks in
**0.08s and 111MB of heap**.

## What is actually in the way

### Five flavours of `RawData`

Most interesting world-save content sits in `RawData` byte arrays that the
decoder currently surfaces as base64. They are not one format:

| Flavour | Where | Layout | Status |
| --- | --- | --- | --- |
| Fixed record | `ItemContainerSaveData` slots | `uint32 slotIndex`, `uint32 count`, `FString itemId`, 16 zero bytes, 16-byte dynamic-item id, trailer | **Decoded** in phase 1; see below |
| Nested GVAS | `CharacterSaveParameterMap` values | A complete Unreal property stream with no header: one `SaveParameter` / `StructProperty` / `PalIndividualCharacterSaveParameter`, then a 24-byte trailer | **Decoded** in phase 2; see below |
| Flat reference | `CharacterContainerSaveData` slots | An `FPalInstanceID` written flat: 16-byte player uid, 16-byte instance id, then six bytes | **Decoded** in phase 3; see below |
| Bespoke binary | `GroupSaveDataMap` values | A 16-byte id, an Unreal string, a counted array of character handles, then a guild record for a guild and four zero bytes for a faction | **Decoded** in phase 4; see below |
| Bespoke binary | `BaseCampSaveData` values and their `WorkerDirector` | An id, a string, a state byte, two `FTransform`s, a radius, the owning guild, and the map object the camp is built around | **Decoded** in phase 4; see below |

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
blobs, holding pal references rather than items. It is a different layout, and
phase 3 decoded it.

(Phase 1 recorded those blobs as 39 bytes each. They are 38: phase 3 measured
every one of them while writing the decoder. The figure was written down from a
partial reading rather than counted, which is the same mistake the trailer census
above records.)

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

### What phase 3 found

The pal-reference slot was the small piece the phase needed, and it behaved:
16 bytes of player uid that are zero in all 2,250 fixture slots, 16 bytes of
instance id, and six more zero bytes. Every slot resolves to a character record,
each record is referenced exactly once, and the 2,250 distinct references are
exactly the world's 2,250 pals. Two counts of the same population by two routes.

The join then held up against a third, independent statement of the same fact. A
pal's own record carries `OwnerPlayerUId`, which this tool does not use — it
decides ownership from the container a player's save names. **2,080 pals resolve
to a player through containers, and 2,080 records carry a non-zero
`OwnerPlayerUId`, with no mismatches.** The remaining 170 are base camp workers,
which have a container but no owner. Container placement and the record's own
field agree completely, which is much stronger evidence than either alone.

Three things were genuinely surprising:

- **Unreal omits a property that equals its default, and the defaults are
  provable.** `Level` is present on only 1,808 of 2,259 records, and its smallest
  present value is **2** — never 0, never 1. So an absent `Level` means level one,
  and 451 pals would otherwise have been reported as level 0. `Rank` is the same
  shape (2,193 absent, smallest present value 2), and `Exp` is absent 442 times
  and never present as zero. The distribution is what turns "probably the default"
  into a fact.
- **Reading the collections out of the file's order is free, and necessary.** A
  pal is placed by `CharacterContainerSaveData` and described by
  `CharacterSaveParameterMap`, but the descriptions are serialized first. Because
  each collection is lazy and independently addressed, a scan can read the 35
  character containers first, build the set of wanted instance ids, and then make
  one pass over the 2,259 records decoding only the few hundred that matter. R2
  says "one pass per collection", not "one pass in file order", and the difference
  is what makes resolving one player cheap.
- **`GameTimeSaveData` holds durations, not dates.** Both of its `DateTime` fields
  land in the year 1 when read as timestamps, which is how they were spotted. The
  game figure is 601.045 days and `LevelMeta`'s `InGameDay` is 601, which is the
  check that the reading is right. `Level.sav`'s own `Revision` also turned out to
  be the game build — 100619 for 1.0.1.100619 — so a resolved world reports the
  version that wrote it.

`gvas.ParseGUID` was added for `--id`: the inverse of `GUID.String`, accepting the
dashed form the documents print and the dash-free form Palworld names a player's
save file after. It is fuzzed against `String` for round-tripping, because an
identifier that parses to the wrong value resolves to nothing rather than failing.

The one thing phase 3 could not deliver was the guild. `Guild` carried an id and
nothing else, because `GroupSaveDataMap`'s payload was the bespoke layout phase 4
owed. Phase 4 paid it: a player's `guild` now carries a name and a member count,
and `--resolve guild` returns the rest.

### What phase 4 found

The group record was the last bespoke layout, and it came apart cleanly. Two group
kinds share it — a `Guild`, which is a player's guild, and an `Organization`, which
is one of the world's fixed factions — and only a guild carries the second half:

```text
16 bytes  group id            -- equals the map key in all 15 fixture records
FString   name                -- "" for a faction; for a guild, the admin's account id in hex
uint32 n  n x 32-byte handle  -- 16-byte account id (zero for a pal) + 16-byte character id
uint32    ?                   -- zero in all 15
byte      organization type   -- 0 for all 8 guilds; 2 to 8, one each, for the 7 factions
uint32 n  n x 16-byte base id -- keys BaseCampSaveData
--- guild only ---
uint32    ?                   -- zero in all 8
int32     base camp level     -- 3 to 22 across the fixture guilds
uint32 n  n x 16-byte point   -- one per base id; each is that camp's owner map object
FString   guild name          -- "Unnamed Guild" until a player sets one
16 bytes  ?                   -- an account id; see below
14 bytes  ?                   -- byte-identical in all 8
16 bytes  admin account id
uint32 n  n x member          -- account id, int64 last-online, FString name, role byte
30 bytes  ?                   -- byte-identical in all 8
```

Whether the guild half is there is stated by the sibling `GroupType` property, not
by the record, so decoding is two calls: `DecodeGroup` reads the shared part and
hands back the remainder, and `DecodeGuild` reads the remainder. Deciding it from
the bytes left over would be a guess dressed up as a decoder — and the CLI's
`--decode-raw`, which sees a path and a blob but no sibling property, is exactly the
caller that has to guess. It attempts the guild half and reports it when it decodes,
which is safe only because `DecodeGuild` is strict enough to reject a faction's
four-byte remainder. A test asserts it does.

The base camp record has no such ambiguity:

```text
16 bytes  id                  -- equals the map key in all 19 fixture camps
FString   name                -- UTF-16 in all 19: the game's Japanese default
1 byte    state               -- 1 in all 19
80 bytes  FTransform          -- ten float64s, rotation first; translation is the camp's position
4 bytes   area range          -- float32, 3500 in all 19
16 bytes  owning guild
80 bytes  FTransform          -- fast-travel, in local space
16 bytes  owner map object id
4 bytes                       -- zero in all 19
```

Four things are worth recording.

- **Every id names something, and most name each other.** A guild's `baseIds` are
  base camps, and each camp's own record names the guild back: 19 references, 19
  camps, no disagreements. A guild's `basePoints` are the camps' owner map objects,
  one for one. That is the standard phases 1 to 3 set — sixteen bytes read at the
  wrong offset are a plausible GUID — and it is much harder to satisfy in both
  directions than in one.
- **A guild's handles are the whole character population, partitioned.** The eight
  guilds list 3,349 characters between them, each exactly once, and the world save
  holds exactly 3,349 character records. The 9 handles carrying a non-zero account id
  are exactly the 9 players, and exactly the 9 guild members. Three counts of two
  populations, none derived from the others.
- **The worker director closes phase 3's open question.** Each base camp's
  `WorkerDirector` names a character container, and those 19 containers hold 205
  pals — precisely the pals no player owns. 3,135 pals sit in a player's party or box
  and 205 at a base, which is every one of the world's 3,340. Phase 3 could count the
  base camp workers but not attribute them; now `--resolve guild` reports them with
  their species, level and talents.
- **A rotation is the cheapest possible offset check.** An `FTransform` read at the
  wrong offset does not produce a unit quaternion. All 38 rotations in the fixture
  world's camps and directors are unit quaternions to within 0.001, which says the
  80-byte block is what this thinks it is without needing to know what any of the
  values mean.

Two fields are read and reported without being understood. The 14 bytes before the
admin and the 30 after the members are byte-identical in all eight fixture guilds;
the 30 are even legible as a count of three followed by three byte-keyed lists, but
legible is not decoded, so they are preserved as opaque bytes and a test asserts
they do not vary. Dropping them would make a change in them invisible.

The third is more interesting. There is an account id immediately before the admin's,
and it is **zero in exactly the three fixture guilds still called "Unnamed Guild"
and equal to the admin in the other five**. An exact correlation across eight guilds
is evidence, not a decoding, so the field is called `NamedBy`, documented as an
inference, and nothing depends on it.

`lastOnline` on a member is not a date. It is measured in the same clock as
`worldSaveData.GameTimeSaveData.RealDateTimeTicks` — elapsed real time since the
world began — and the check is that two members carry *exactly* the world's current
value, because they were online when the save was written. Reading it as a date
would have put it in the year 1, the same trap phase 3 hit with game time.

Phase 4 also found a defect in phase 3 rather than in the game. A container slot can
reference a character the save no longer holds, and the resolver dropped such a slot
silently. It is not hypothetical: 7 of the fixture world's 3,347 slot references name
no record. All 7 sit in containers nothing points at, so no resolve reaches one, but
the case is now a warning in both the player and the guild path — the difference
between "no pals" and "the pal is gone" belongs in the output.

### The fixture world moved

Phases 1 to 3 were measured against one save of the fixture world. Phase 4 was
measured against a later save of the same world, because the directory it lived in
became a running server and the file is now rewritten every few minutes; the figures
here come from an immutable backup rather than the live file.

That is not just bookkeeping. Between the two saves the world grew from 2,259
characters to 3,349 and from 16 base camps to 19, and it acquired the 7 dangling
slot references above — which **broke a test**. Phase 3 asserted that every
container slot reference in the save resolves, which was true of the save it was
written against. The assertion is now the invariant that actually holds: every
reference a player or a base camp can reach resolves, and together they reach every
pal. A fixture that changes under a test is a good way to find out that the test
was describing a sample rather than a rule.

Figures quoted in phase 1 to 3 sections are left as they were measured. Where a
count appears in both, the phase 4 number is the current one.

### Where R2 and R3 pull against each other

R2 wants one pass over the world save for however many entities are asked for.
R3 wants each output element encoded as it is produced. Those cannot both be taken
literally: a pal is placed by one collection and described by another, so nothing
can be emitted until the scan has read both.

R2 wins, because the scan is the expensive half. The compromise is that the
resolver collects compact documents during its single pass and then hands them to
the caller one at a time, and `cmd` encodes each straight to standard output. The
JSON array is never assembled in memory, which is what R3 was protecting against;
the documents themselves are bounded by the number of players asked for. It also
means nothing reaches standard output until the resolve has succeeded, so a
failure cannot leave half an envelope behind — which turned out to be worth more
than the memory saving.

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

| `--full` | JSON |
| --- | --- |
| default | 223,826,982 B |
| `--decode-raw`, phase 1 | 231,733,689 B (+3.5%) |
| `--decode-raw`, phase 2 | 270,196,593 B (+20.7%) |
| `--decode-raw`, phase 3 | 270,817,593 B (+21.0%) |

Phase 4 is not a row in that table, because it was measured on a later save of the
same world and the two are not comparable at that precision. On the phase 4 save,
`--full` is 265,629,318 B and `--full --decode-raw` is 334,515,321 B (+25.9%), of
which the three layouts phase 4 added account for 567,157 B — 15 groups, 19 camps
and 19 worker directors are a rounding error next to 34,187 item slots and 3,349
character records. What matters is that all six layouts covered 40,936 of that
save's 273,120 blobs and **not one of them failed to decode**.

Character blobs are ~3.4KB of dense property data, so they cost far more JSON per
blob than the item slots did despite being twelve times fewer; the phase 3
references are 38 bytes each and barely register. Without the flag the output is
byte-identical to before all three phases — 223,826,982 bytes, sha256
`c8a03edc05fdb352…` — checked by hash rather than assumed.

Times are quoted separately because the earlier rows were single measurements
taken on separate runs and are not comparable with each other at that precision.
Re-measured together on the phase 3 tree, two runs each: `--full` 1.07s and
`--full --decode-raw` 1.28s, both peaking around 1.6GB of RSS. On the phase 4 save:
1.27s and 1.57s, peaking at 1.7GB and 2.0GB.

That 1.7GB is worth stating plainly, because it is the number `--resolve` exists to
avoid. `--full` materialises the whole tree as JSON-ready values. On the same file,
resolving all nine players takes **0.12s and peaks at 160MB**, and resolving all
eight guilds with their 19 bases and 205 workers takes **0.06s and 108MB** — the
guild mode is cheaper than the player mode because it reads no player saves at all.
Peak-heap tests fail the build if either regresses. The modes that answer the useful
questions are an order of magnitude faster and an order of magnitude smaller than
the mode that dumps everything.

Where the blobs are, from a walk of the fixture world:

| Path | Blobs | Bytes each | Phase |
| --- | --- | --- | --- |
| `MapObjectSaveData` (several sub-paths) | 63,724 | 0–3,213 | — |
| `ItemContainerSaveData.Value.Slots.Slots` | 27,094 | 21–351 | **1, done** |
| `FoliageGridSaveDataMap` | 6,273 | 51–108 | — |
| `CharacterContainerSaveData.Value.Slots.Slots` | 2,250 | 38 | **3, done** |
| `CharacterSaveParameterMap.Value` | 2,259 | 2,628–4,140 | **2, done** |
| `ItemContainerSaveData.Value` | 8,723 | — | — |
| `DynamicItemSaveData` | 581 | 57–1,518 | — |
| `GroupSaveDataMap.Value` | 15 | 37–16,286 | **4, done** |
| `BaseCampSaveData` (several sub-paths) | 192 | 0–1,512 | **4, partly** |

Those counts are the phase 1 walk, kept for comparison; the phase 4 save holds
273,120 blobs rather than 111,579, mostly more item containers. Two things in the
phase 4 row are worth spelling out. `GroupSaveDataMap`'s smallest blob is 37 bytes,
not the 39 phase 1 recorded — a faction with no members is 16 + 4 + 4 + 1 + 4 + 4 —
and only three of the twelve blobs a base camp holds are decoded. The other nine are
its `ModuleMap` entries, keyed by `EPalBaseCampModuleType`, and its `WorkCollection`.

The work collection was deliberately left alone. It parses cleanly on all 19 camps —
a camp id, a counted array of ids, four zero bytes — but its 488 references can only
be checked against `WorkSaveData`'s own undecoded `RawData`, and shipping a decoder
whose join nobody has verified is exactly what phases 1 to 3 refused to do. It is a
candidate for a later phase, not a gap in this one.

The 2,250 `CharacterContainerSaveData` slots and the 2,250 pal records are the
same population counted two ways, which phase 3 used as its first cross-check:
those 38-byte slots hold the pal references that turn a player's
`PalStorageContainerId` into a list of the records phase 2 decodes. They were the
smallest remaining piece of the player picture, and decoding them was the first
thing phase 3 did.

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
internal/resolve/               cross-save joins — the automatic layer, added in phase 3
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
| `resolve` | `gvas`, `palworld`, `savefile` |
| `cmd/palworld-save-reader` | `gvas`, `palworld`, `projection`, `resolve` |

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

`resolve` is a second package that knows game semantics, which needs saying
because the rule above is "`palworld` is the only package allowed to". The two own
different kinds of knowledge. `palworld` knows how to **decode**: which concrete
struct hides behind an untagged property, and which byte layout a blob holds.
`resolve` knows what the decoded values **mean**: that
`InventoryInfo.CommonContainerId` names an entry of `ItemContainerSaveData`, that a
pal's presence in a container slot is what makes it a player's, and that an absent
`Level` means level one. Both break on a Palworld update, so they are the two
places to look when one lands; keeping them separate means a schema change and a
semantic change are separate diffs.

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

Phase 3 was the third layout, and it was exactly a table entry plus a decoder, as
predicted. Phase 4 was three more table entries and three more decoders, plus a
shared reader for the bespoke encoding: the group and base camp records interleave
counted arrays, Unreal strings and transforms, which is more than a handful of named
offsets can state legibly. `palworld/record.go` is that reader — sticky error, every
read bounds-checked, every array count checked against the bytes left before
anything is allocated, and fuzzed for 24 million executions because a `RawData` blob
is untrusted input.

Phase 3 also added the mode this was all for, and phase 4 completed it:

```text
palworld-save-reader --resolve player --id UID --saves DIR
palworld-save-reader --resolve guild --id GROUPID --saves DIR
palworld-save-reader --resolve players|guilds|world --saves DIR
```

| KIND | Returns | State |
| --- | --- | --- |
| `player` | One fully resolved player; `--id` takes a player UID | **done** |
| `players` | Every player in the save set | **done** |
| `world` | World metadata, in-game time, and entity counts | **done** |
| `guild` | One guild with its members, bases and workers; `--id` takes a group id | **done** |
| `guilds` | Every guild | **done** |

`--saves DIR` discovers `Level.sav`, `LevelMeta.sav` and `Players/*.sav`, so a
caller points at a save directory rather than naming files. `--id` accepts a player
UID in either spelling the game uses, and matches it against the `PlayerUId` inside
each player save rather than against the file name — the name is a convention, the
property is the fact.

Every answer is wrapped in a versioned envelope, `{"resolveVersion", "kind",
<kind>}`, where the payload field is named for the kind. `--resolve players` and
`--resolve guilds` stream their arrays element by element through one shared writer;
the single-document kinds go out through `encoding/json` unchanged. The version is
now 2: the guild document arrived and a player's guild gained a name, which is
additive, but a consumer that branches on "does a player's guild have a name" needs
a number to branch on. A resolved player document is composed, not projected:

```json
{
  "playerUId": "…", "instanceId": "…", "platform": "…",
  "lastOnline": { "ticks": …, "utc": "…" },
  "position": { "x": …, "y": …, "z": … },
  "technologyPoints": …,
  "character": { "nickname": "…", "level": …, "exp": …, "hp": …, "fullStomach": … },
  "guild": { "id": "…", "name": "…", "memberCount": … },
  "inventory": {
    "common":    [ { "slot": 0, "itemId": "Money", "count": 70434 }, … ],
    "dropSlot": [ … ], "essential": [ … ], "weapons": [ … ], "armor": [ … ], "food": [ … ]
  },
  "pals": [ {
    "instanceId": "…", "species": "…", "nickname": "…", "gender": "…",
    "level": …, "exp": …, "hp": …, "location": "party", "slot": 1,
    "talents": { "hp": …, "shot": …, "defense": … }, "passiveSkills": [ … ]
  }, … ],
  "warnings": [ … ]
}
```

One departure from the sketch this section used to carry: every document can carry
`warnings`, which the sketch had no place for. A join that finds nothing says so,
because "no pals" and "the pal container is missing from the world save" are
otherwise the same output. The fields are grouped by provenance — `playerUId` to
`technologyPoints` from the player's own save, `character`, `guild` and `pals` from
the world save — since that is what a reader needs to know when a section is absent.

The other departure, `guild` carrying an id and nothing else, is closed. Phase 4
gave it a name and a member count for one extra pass over a 15-entry collection, and
added the guild document itself:

```json
{
  "groupId": "…", "name": "…", "admin": "…", "baseCampLevel": …,
  "members": [ { "playerUId": "…", "name": "…",
                 "lastOnline": { "ticks": …, "seconds": …, "days": … }, "role": 1 }, … ],
  "bases":   [ { "id": "…", "name": "…", "location": { "x": …, "y": …, "z": … },
                 "areaRange": 3500, "ownerMapObjectId": "…",
                 "workers": [ { "instanceId": "…", "species": "…", "location": "base", … }, … ] }, … ],
  "counts": { "characters": …, "players": …, "pals": …, "bases": …, "workers": … },
  "warnings": [ … ]
}
```

`counts` comes from the group's own handle list rather than from the join, which is
what makes it a check on the join rather than a summary of it. A guild resolve reads
the world save and nothing else — no `player.sav` is opened — because the group
record carries its members' names itself. That makes it the mode to use on a server
whose player files you do not have, and a test asserts it works with `Players/`
absent entirely.

## Keep projection dumb

Joins do **not** go into the projection document format. Projection's guarantee
is "shape in, same shape out, no semantics", and cross-file joins would require
join keys, cardinality rules, missing-reference policy and merge naming — a query
language, which the README explicitly disclaims. `--resolve` is hand-written Go
with an output shape versioned by the tool. That also isolates the layer that
encodes game semantics and will therefore break on Palworld updates.

The consequence for presets: `player-containers` stays useful as the dumb view,
and `--resolve player` is the one that follows those identifiers through.

Phase 3 kept to that, and the split turned out sharper than expected. Projection
never had to change: `--resolve` reads the player saves through `palworld.Load` and
walks the properties with a handful of typed accessors, and never touches
`internal/projection` at all. The two are alternatives rather than layers, which is
why `--schema` can keep failing on `Level.sav` without that blocking anything.

## Memory rules

These are requirements, not aspirations, and R4 makes them enforceable.

- **R1** Never call `Entries()`, `Values()` or `normalizeSource` on a `Level.sav`
  collection. Iterate.
- **R2** One pass over `Level.sav` per invocation, no matter how many entities
  are requested. Collect the wanted id set first, then fill it in during a single
  scan — resolving nine players must not scan 8,723 containers nine times. Read the
  collections in dependency order rather than file order; each is independently
  addressed, so this is free. A guild resolve is the longest such chain: groups name
  base camps, camps name worker containers, containers name character records, and
  the character records come *first* in the file.
- **R3** Stream output. `--resolve players` encodes each element as it is
  produced rather than building the whole array first. See the note above on where
  this and R2 pull against each other.
- **R4** CI tests assert peak heap stays under a fixed budget while resolving the
  whole fixture save set, so a regression to eager decoding fails the build. Held at
  320MB against a measured 137MB for players and 94MB for guilds. They catch an R1
  violation, which is the expensive one; the wanted-id discipline of R2 is too cheap
  to show up in a heap budget, so it is checked separately by comparing allocations
  for one player against all nine, and for one guild against all eight.

## Phases

| Phase | Work | Size | Unblocks | State |
| --- | --- | --- | --- | --- |
| 0 | Executable rename to `palworld-save-reader`, then package rename and split; pure motion, no logic change | S | Coherent homes for phases 1–4 | **done** |
| 1 | `palworld/itemslot.go`: item-slot decoding, lazily; `--full` opt-in flag | S | Container contents visible | **done** |
| 2 | `gvas.ParseProperties` and `palworld/character.go`: nested property streams via a re-entrant call | M | Pal and character detail — the big one | **done** |
| 3 | `palworld/characterslot.go`, `internal/resolve` + `--resolve player\|players\|world`; R1–R4 | M | The automatic joins | **done** |
| 4 | `palworld/record.go`, `group.go`, `basecamp.go`: `GroupSaveDataMap` and `BaseCampSaveData` reverse engineering; `--resolve guild\|guilds` | M | Guilds, bases, and the pals no player owns | **done** |

Phase 0 went first because phases 1–4 need `palworld/` and `resolve/` to exist;
adding them alongside `internal/palsav` would have been incoherent. It is a large
diff but a mechanical one — review with `git diff --find-renames`.

Phase 4 came in at the short end of its unestimated range: the group record gave up
its shape in an afternoon, mostly because every field it contains is an id that
either resolves against another collection or does not. What is left undecoded after
it is listed under Volume above — map objects, foliage, work assignments, dynamic
items, base camp modules — and none of it is needed to answer "what does this player
have" or "what is in this guild", which were the questions this roadmap set out to
answer. Anything after this is a new goal rather than the rest of an old one.

## Risks

- ~~**Phase 4 is unestimated** because the group blob layout is unverified. It may
  be a short afternoon or a long slog; treat the phase table as four items plus
  one unknown.~~ The short end. The record opens with its own id, which matches the
  map key, and every subsequent field is a counted array or a string, so each guess
  was immediately falsifiable against 15 records. The part that took longest was not
  the layout but deciding what *not* to claim: three runs of bytes and one account id
  are read and reported without being understood, rather than named after a guess.
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
- ~~**Phase 3 memory.** Phase 2 makes it cheap to decode 2,259 dense records, which
  is exactly the temptation R1–R4 exist to resist. `--resolve players` must
  collect the wanted id set first and decode only the records it needs, not
  decode every character and filter.~~ Held. The wanted-id set is built from the
  player saves before the world save is touched, and resolving the whole fixture
  set peaks at 111MB. The risk was real, though: the obvious implementation —
  decode every record, then filter by `OwnerPlayerUId` — is simpler to write and
  would have passed every functional test.
- **Default values are invisible.** Unreal omits a property equal to its default,
  so a resolved document has to know that an absent `Level` means one rather than
  zero. Phase 3 proved that from the fixture distribution for `Level`, `Rank` and
  `Exp`, but the same trap applies to every numeric field added later, and getting
  it wrong produces a plausible wrong answer rather than an error.
- **Fixture coverage.** All of this is verified against one world and nine player
  saves from `1.0.1.100619`, now at two points in that world's life. Every resolve
  figure quoted here comes from it; the synthetic save sets built in
  `internal/resolve`'s tests cover the shapes it does not contain — a container
  missing from the world save, a pal owned by nobody, a record with no `Level`, a
  base camp naming another guild, a guild whose base camp is gone — but they are a
  description of the format rather than evidence about the game. The gap phase 4
  leaves is the same one phase 3 did, narrowed: the fixture world has eight guilds
  but only one with more than a single member, so `role` has exactly two observed
  values and the member ordering is barely exercised. A world with real multi-member
  guilds would be worth more than any amount of further reading of this one.
- **A live fixture is not a fixed fixture.** The save directory is now a running
  server, so the world changes between measurements and a test written against a
  snapshot can start failing on facts about the game rather than about the code —
  which is what happened to phase 3's slot-reference assertion. Measure from an
  immutable backup, and write assertions about relationships rather than counts.

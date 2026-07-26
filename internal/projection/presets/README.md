Bundled projection documents. `presets.go` embeds every `.json` file here with `//go:embed presets/*.json`, which is what lets `--preset` and `--list-presets` work without a checkout or a network. This README is not embedded.

A document is added here only after it has been exercised against private save fixtures from the exact Palworld version its `gameVersion` declares. Real saves and fixture-derived output must not be committed.

Each preset is identified by its `name` alone, so two documents here must not share a name. `gameVersion` records which build the document was verified against and is reported by `--list-presets`; it does not select a preset.

## Only request fields that every save carries

Palworld omits properties that still hold their default value, so which fields exist varies with how far a player has progressed. Across nine fixture player saves, `OilrigClearCount` appeared in four, `TowerBossDefeatFlag` in seven, and `bossTechnologyPoint` in seven. A preset requesting any of them fails strict matching on the remaining saves.

Matching is strict by default, so a preset must request only fields present in **every** fixture of its save type. `TestBundledPresetsAgainstPrivateFixtures` enforces that by applying each preset to each applicable fixture in strict mode. Fields that are optional in practice belong in a caller's own `--schema` document used with `--allow-partial`, not in a bundled preset.

## Flag maps are really sets

`FastTravelPointUnlockFlag`, `FindAreaFlagMap`, `NoteObtainForInstanceFlag`, and
`PaldeckUnlockFlag` are `MapProperty` values whose `Value` was `true` in all
2,071 entries across the nine fixture player saves. Palworld appears to write an
entry only once the flag is set, which makes the value redundant and roughly
doubles the projected bytes.

The presets still request `{ "Key": "", "Value": false }` rather than the
`{ "Key": "" }` the format also accepts. Nine saves from one world is evidence
that a `false` is rare, not that it is impossible, and a key-only projection
would report a cleared flag as a set one. A caller who has measured the
difference and accepts that can drop `Value` in its own `--schema` document.

`UnlockedWorldMapFlags` was dropped from `player-exploration` on the same
evidence and the opposite conclusion: it held the single entry `MainMap: true`
in every fixture, so it carried no information whether or not the value was
requested.

## Save types

`saveType` must be a value the integration test can find fixtures for: `player.sav`, `Level.sav`, or `LevelMeta.sav`. Any other value fails that test rather than leaving the preset unexercised. DPS saves (`*_dps.sav`) sit beside player saves but carry a different root structure, so player presets are not applied to them.

No `Level.sav` preset is bundled. Projection normalizes the entire decoded tree before matching, and a world save exceeds the 10,000,000-node ceiling in `data.go`, so `--schema` and `--preset` cannot process `Level.sav` at any shape. Use `--full` for world saves.

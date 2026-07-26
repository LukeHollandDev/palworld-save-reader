// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestTimestampConvertsUnrealDateTimes states the epoch offset independently of
// the constant the code uses: the expected strings come from Go's own calendar, so
// an offset that is off by a day fails here rather than shipping dates that look
// plausible.
func TestTimestampConvertsUnrealDateTimes(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		ticks int64
		want  string
	}{
		{
			// The fixture world's save time, cross-checked against the file's own
			// modification date when it was read.
			name:  "a real save time",
			ticks: 639204910905580000,
			want:  "2026-07-24T11:58:10Z",
		},
		{
			name:  "the Unreal epoch",
			ticks: 0,
			want:  "0001-01-01T00:00:00Z",
		},
		{
			name:  "the Unix epoch",
			ticks: unrealEpochOffset * ticksPerSecond,
			want:  "1970-01-01T00:00:00Z",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stamp := timestamp(testCase.ticks)
			if stamp.Ticks != testCase.ticks {
				t.Errorf("Ticks = %d, want %d", stamp.Ticks, testCase.ticks)
			}
			if stamp.UTC != testCase.want {
				t.Errorf("UTC = %q, want %q", stamp.UTC, testCase.want)
			}
		})
	}

	// The offset is also checked against time.Time's own arithmetic rather than
	// against the number written in the source.
	epoch := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	if seconds := -int64(epoch.Unix()); seconds != unrealEpochOffset {
		t.Errorf("unrealEpochOffset = %d, but 0001-01-01 is %d seconds before the Unix epoch",
			unrealEpochOffset, seconds)
	}
}

// TestTimestampLeavesUnrepresentableDatesUnnamed is not hypothetical: the same
// DateTime type carries elapsed times, and rendering one as a date puts it in the
// year 1 or before, which RFC 3339 cannot express.
func TestTimestampLeavesUnrepresentableDatesUnnamed(t *testing.T) {
	for _, ticks := range []int64{-1, -ticksPerSecond * 86400, (maxRFC3339Seconds + unrealEpochOffset + 1) * ticksPerSecond} {
		stamp := timestamp(ticks)
		if stamp.UTC != "" {
			t.Errorf("timestamp(%d).UTC = %q, want no date", ticks, stamp.UTC)
		}
		if stamp.Ticks != ticks {
			t.Errorf("timestamp(%d) dropped the ticks", ticks)
		}
	}
}

// TestElapsedReadsGameTimeAsADuration pins the reading that the fixture world
// confirmed: GameDateTimeTicks is elapsed in-game time, and 519302980000000 ticks
// is 601 days, which is exactly the InGameDay the metadata save reports.
func TestElapsedReadsGameTimeAsADuration(t *testing.T) {
	game := elapsed(519302980000000)
	if game.Seconds != 51930298 {
		t.Errorf("Seconds = %d", game.Seconds)
	}
	if game.Days != 601 {
		t.Errorf("Days = %d, want 601 to match the fixture world's InGameDay", game.Days)
	}
	// The same value read as a date lands in year 2, which is how these two fields
	// were identified as durations in the first place.
	if date := timestamp(519302980000000); !strings.HasPrefix(date.UTC, "0002-") {
		t.Errorf("as a date, the same ticks gave %q; the year is the tell", date.UTC)
	}
	if zero := elapsed(0); zero != (Elapsed{}) {
		t.Errorf("elapsed(0) = %+v", zero)
	}
}

// TestPlayerJSONShapeIsStable covers the parts of the document a consumer indexes
// blindly: the six inventory containers and the pal list are always present, and
// the optional fields disappear rather than appearing as null.
func TestPlayerJSONShapeIsStable(t *testing.T) {
	player := newPlayerScan(nil, id(1)).document
	encoded, err := json.Marshal(player)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{
		`"playerUId":"00000001-1111-1111-0000-000000000000"`,
		`"common":[]`, `"dropSlot":[]`, `"essential":[]`,
		`"weapons":[]`, `"armor":[]`, `"food":[]`,
		`"pals":[]`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("encoded player %s does not contain %s", text, want)
		}
	}
	// A player with nothing resolved must not carry null placeholders: an absent
	// join is absent, which is what the warnings are for.
	for _, unwanted := range []string{"null", "character", "guild", "instanceId", "platform", "lastOnline"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("encoded player %s mentions %q", text, unwanted)
		}
	}
}

// TestVersionIsTheShapesOwn guards the one thing a consumer keys off. It is not
// the save's version, which is reported separately in World.
func TestVersionIsTheShapesOwn(t *testing.T) {
	if Version != 1 {
		t.Errorf("Version = %d; changing it is a deliberate break, so update this test with the reason", Version)
	}
}

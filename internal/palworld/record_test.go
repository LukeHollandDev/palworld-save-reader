// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"math"
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// TestRecordErrorIsSticky is the property the two bespoke decoders rely on to
// read a layout as a straight list of fields: once a read has failed, every later
// read returns a zero value and the first error is the one reported.
func TestRecordErrorIsSticky(t *testing.T) {
	r := newRecord("test record", []byte{1, 2})
	if got := r.u32("first"); got != 0 || r.err == nil {
		t.Fatalf("reading 4 bytes from 2 gave %d and error %v", got, r.err)
	}
	first := r.err
	if got := r.u8("second"); got != 0 {
		t.Errorf("a read after a failure returned %d, want 0", got)
	}
	if got := r.text("third"); got != "" {
		t.Errorf("a string read after a failure returned %q", got)
	}
	if got := r.guids("fourth"); got != nil {
		t.Errorf("an array read after a failure returned %v", got)
	}
	if r.rest() != nil {
		t.Error("rest() after a failure returned bytes")
	}
	if r.err != first {
		t.Errorf("error = %v, want the first one (%v)", r.err, first)
	}
	if !strings.Contains(first.Error(), "first") {
		t.Errorf("error %q does not name the field that failed", first)
	}
}

// TestRecordReadsEveryWidth covers the readers themselves, at values where a
// wrong sign or a wrong endianness would show.
func TestRecordReadsEveryWidth(t *testing.T) {
	blob := new(blobBuilder).
		u32(0xfffffffe).
		i32(-2).
		i64(-3).
		u8(0xff).
		float32(-1.5).
		float64(0.25).
		done()
	r := newRecord("test record", blob)
	if got := r.u32("u32"); got != 0xfffffffe {
		t.Errorf("u32 = %#x", got)
	}
	if got := r.i32("i32"); got != -2 {
		t.Errorf("i32 = %d", got)
	}
	if got := r.i64("i64"); got != -3 {
		t.Errorf("i64 = %d", got)
	}
	if got := r.u8("u8"); got != 0xff {
		t.Errorf("u8 = %#x", got)
	}
	if got := r.float32("float32"); got != -1.5 {
		t.Errorf("float32 = %v", got)
	}
	if got := r.float64("float64"); got != 0.25 {
		t.Errorf("float64 = %v", got)
	}
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.rest()) != 0 {
		t.Errorf("%d bytes left over", len(r.rest()))
	}
}

func TestRecordText(t *testing.T) {
	for _, testCase := range []struct {
		name string
		blob []byte
		want string
		fail bool
	}{
		{name: "empty", blob: new(blobBuilder).utf8("").done(), want: ""},
		{name: "ascii", blob: new(blobBuilder).utf8("Kestrel Company").done(), want: "Kestrel Company"},
		{name: "utf16", blob: new(blobBuilder).utf16("拠点1").done(), want: "拠点1"},
		{
			// Unreal always writes the terminator, so a string without one means
			// the length was read at the wrong offset.
			name: "unterminated ascii",
			blob: new(blobBuilder).i32(3).bytes('a', 'b', 'c').done(),
			fail: true,
		},
		{
			name: "unterminated utf16",
			blob: new(blobBuilder).i32(-2).bytes('a', 0, 'b', 0).done(),
			fail: true,
		},
		{
			name: "length beyond the record",
			blob: new(blobBuilder).i32(64).bytes('a').done(),
			fail: true,
		},
		{
			// Negating math.MinInt32 overflows, so it is rejected before the width
			// is computed rather than after.
			name: "the length that cannot be negated",
			blob: new(blobBuilder).i32(math.MinInt32).done(),
			fail: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := newRecord("test record", testCase.blob)
			got := r.text("Name")
			switch {
			case testCase.fail && r.err == nil:
				t.Fatalf("text = %q, want an error", got)
			case testCase.fail:
				return
			case r.err != nil:
				t.Fatal(r.err)
			case got != testCase.want:
				t.Errorf("text = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestRecordArrayRejectsACountItCannotHold is the allocation guard. The count is
// attacker-controlled, so it is checked against the bytes actually left before a
// slice is made.
func TestRecordArrayRejectsACountItCannotHold(t *testing.T) {
	for _, count := range []uint32{1, 2, math.MaxUint32} {
		blob := new(blobBuilder).u32(count).guid(1, 2, 3, 4).done()
		r := newRecord("test record", blob)
		ids := r.guids("Ids")
		if count == 1 {
			if r.err != nil || len(ids) != 1 {
				t.Errorf("a one-entry array gave %v and %v", ids, r.err)
			}
			continue
		}
		if r.err == nil {
			t.Errorf("a count of %d decoded %d entries", count, len(ids))
		}
	}
}

// FuzzDecodeRecords is the blunt instrument behind the layout tests: the four
// bespoke decoders take untrusted bytes, and none of them may panic, hang, or
// return a value alongside an error. It found nothing, which is the point.
func FuzzDecodeRecords(f *testing.F) {
	f.Add(guildBlob())
	f.Add(organizationBlob())
	f.Add(baseCampBlob())
	f.Add(workerDirectorBlob())
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if group, err := DecodeGroup(data); err != nil {
			// A failed decode returns the zero value, so a caller that ignores the
			// error reads nothing rather than half a group.
			if group.ID != (gvas.GUID{}) || len(group.Handles) != 0 || group.Remainder != nil {
				t.Errorf("DecodeGroup returned %+v alongside %v", group, err)
			}
		} else {
			// A successful decode must not claim more handles than the blob could
			// hold, which is the invariant the count check exists to keep.
			if len(group.Handles)*characterHandleBytes > len(data) {
				t.Errorf("%d handles from %d bytes", len(group.Handles), len(data))
			}
			_, _ = DecodeGuild(group.Remainder)
		}
		_, _ = DecodeGuild(data)
		_, _ = DecodeBaseCamp(data)
		_, _ = DecodeWorkerDirector(data)
	})
}

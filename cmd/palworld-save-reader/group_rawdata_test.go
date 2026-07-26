// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unicode/utf16"
)

// A minimal writer for the bespoke record encoding, so these tests state the bytes
// they mean. It is deliberately not the one in palworld's tests: a test package
// cannot import another's, and a second independent writer is worth more here than
// a shared one anyway.
type recordBytes struct{ out []byte }

func (r *recordBytes) u32(value uint32) *recordBytes {
	r.out = binary.LittleEndian.AppendUint32(r.out, value)
	return r
}

func (r *recordBytes) i32(value int32) *recordBytes { return r.u32(uint32(value)) }

func (r *recordBytes) i64(value int64) *recordBytes {
	r.out = binary.LittleEndian.AppendUint64(r.out, uint64(value))
	return r
}

func (r *recordBytes) u8(value byte) *recordBytes {
	r.out = append(r.out, value)
	return r
}

func (r *recordBytes) zeros(count int) *recordBytes {
	r.out = append(r.out, make([]byte, count)...)
	return r
}

func (r *recordBytes) f32(value float32) *recordBytes {
	return r.u32(math.Float32bits(value))
}

func (r *recordBytes) f64(value float64) *recordBytes {
	r.out = binary.LittleEndian.AppendUint64(r.out, math.Float64bits(value))
	return r
}

func (r *recordBytes) guid(a, b, c, d uint32) *recordBytes {
	return r.u32(a).u32(b).u32(c).u32(d)
}

func (r *recordBytes) text(value string) *recordBytes {
	r.i32(int32(len(value) + 1))
	r.out = append(r.out, value...)
	return r.u8(0)
}

func (r *recordBytes) text16(value string) *recordBytes {
	codes := utf16.Encode([]rune(value))
	r.i32(-int32(len(codes) + 1))
	for _, code := range append(codes, 0) {
		r.out = binary.LittleEndian.AppendUint16(r.out, code)
	}
	return r
}

func (r *recordBytes) transform(translation [3]float64) *recordBytes {
	for _, value := range []float64{0, 0, 0.8628, 0.5056} {
		r.f64(value)
	}
	for _, value := range translation {
		r.f64(value)
	}
	for i := 0; i < 3; i++ {
		r.f64(1)
	}
	return r
}

func (r *recordBytes) done() []byte { return r.out }

// guildRecord builds a one-member, one-base guild.
func guildRecord() []byte {
	return new(recordBytes).
		guid(0xdead0001, 0, 0, 0).
		text("BEEF000200000000000000000000000000").
		u32(1).
		guid(0xbeef0002, 0, 0, 0).
		guid(0xfeed0003, 0, 0, 0).
		u32(0).
		u8(0).
		u32(1).
		guid(0xcafe0004, 0, 0, 0).
		u32(0).
		i32(13).
		u32(1).
		guid(0xcafe0005, 0, 0, 0).
		text("Kestrel Company").
		guid(0xbeef0002, 0, 0, 0).
		zeros(14).
		guid(0xbeef0002, 0, 0, 0).
		u32(1).
		guid(0xbeef0002, 0, 0, 0).
		i64(12_000_000_000_000).
		text("Ada").
		u8(1).
		zeros(4).
		done()
}

func organizationRecord() []byte {
	return new(recordBytes).
		guid(0xdead0009, 0, 0, 0).
		text("").
		u32(0).
		u32(0).
		u8(5).
		u32(0).
		zeros(4).
		done()
}

func baseCampRecord() []byte {
	return new(recordBytes).
		guid(0xcafe0004, 0, 0, 0).
		text16("拠点1").
		u8(1).
		transform([3]float64{-1, 2, -3}).
		f32(3500).
		guid(0xdead0001, 0, 0, 0).
		transform([3]float64{0, 0, 0}).
		guid(0xcafe0005, 0, 0, 0).
		zeros(4).
		done()
}

func workerDirectorRecord() []byte {
	return new(recordBytes).
		guid(0xcafe0004, 0, 0, 0).
		transform([3]float64{-1, 2, -3}).
		zeros(2).
		guid(0x5c3e8d10, 0, 0, 0).
		zeros(4).
		done()
}

// TestDecodeRawRendersAGuild covers the renderer for the layout phase 4 decoded,
// including the part it has to infer: --decode-raw sees a path and a blob, not the
// sibling GroupType property, so it attempts the guild half and reports it when it
// decodes.
func TestDecodeRawRendersAGuild(t *testing.T) {
	node := byteArrayAt(t, true, groupPath, guildRecord())
	decoded, ok := node["decoded"].(map[string]any)
	if !ok {
		t.Fatalf("no decoded block: %v", node)
	}
	if decoded["kind"] != "group" {
		t.Errorf("kind = %v", decoded["kind"])
	}
	if decoded["groupId"] != "dead0001-0000-0000-0000-000000000000" {
		t.Errorf("groupId = %v", decoded["groupId"])
	}
	handles, ok := decoded["handles"].([]any)
	if !ok || len(handles) != 1 {
		t.Fatalf("handles = %v", decoded["handles"])
	}
	handle, _ := handles[0].(map[string]any)
	if handle["instanceId"] != "feed0003-0000-0000-0000-000000000000" {
		t.Errorf("handle = %v", handle)
	}
	if handle["playerUId"] != "beef0002-0000-0000-0000-000000000000" {
		t.Errorf("a handle with an account id did not report it: %v", handle)
	}
	guild, ok := decoded["guild"].(map[string]any)
	if !ok {
		t.Fatalf("no guild block: %v", decoded)
	}
	if guild["name"] != "Kestrel Company" || guild["baseCampLevel"] != int32(13) {
		t.Errorf("guild = %v", guild)
	}
	members, ok := guild["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("members = %v", guild["members"])
	}
	member, _ := members[0].(map[string]any)
	if member["name"] != "Ada" || member["lastOnlineTicks"] != int64(12_000_000_000_000) {
		t.Errorf("member = %v", member)
	}
	// The base64 is never replaced, only added to: --decode-raw must not lose the
	// bytes it is interpreting.
	if node["values"] == nil {
		t.Error("the raw byte array was dropped")
	}
}

// TestDecodeRawRendersAnOrganizationWithoutAGuild is the other side of the
// inference: an organization must not acquire an empty guild block.
func TestDecodeRawRendersAnOrganizationWithoutAGuild(t *testing.T) {
	node := byteArrayAt(t, true, groupPath, organizationRecord())
	decoded, ok := node["decoded"].(map[string]any)
	if !ok {
		t.Fatalf("no decoded block: %v", node)
	}
	if _, hasGuild := decoded["guild"]; hasGuild {
		t.Errorf("an organization was rendered with a guild block: %v", decoded)
	}
	if decoded["organizationType"] != uint8(5) {
		t.Errorf("organizationType = %v", decoded["organizationType"])
	}
	// The four zero bytes an organization ends with carry nothing, so they are not
	// reported -- the same rule every other trailer follows.
	if _, hasRemainder := decoded["remainder"]; hasRemainder {
		t.Errorf("a zero remainder was reported: %v", decoded)
	}
	if _, hasName := decoded["name"]; hasName {
		t.Errorf("an empty internal name was reported: %v", decoded)
	}
}

func TestDecodeRawRendersABaseCampAndItsWorkerDirector(t *testing.T) {
	camp, ok := byteArrayAt(t, true, baseCampPath, baseCampRecord())["decoded"].(map[string]any)
	if !ok {
		t.Fatal("no decoded base camp")
	}
	if camp["kind"] != "baseCamp" || camp["name"] != "拠点1" {
		t.Errorf("camp = %v", camp)
	}
	if camp["groupId"] != "dead0001-0000-0000-0000-000000000000" {
		t.Errorf("groupId = %v", camp["groupId"])
	}
	if camp["areaRange"] != float32(3500) {
		t.Errorf("areaRange = %v", camp["areaRange"])
	}
	transform, ok := camp["transform"].(map[string]any)
	if !ok {
		t.Fatalf("transform = %v", camp["transform"])
	}
	if _, ok := transform["translation"]; !ok {
		t.Errorf("transform has no translation: %v", transform)
	}

	director, ok := byteArrayAt(t, true, workerDirectorPath, workerDirectorRecord())["decoded"].(map[string]any)
	if !ok {
		t.Fatal("no decoded worker director")
	}
	if director["kind"] != "workerDirector" {
		t.Errorf("kind = %v", director["kind"])
	}
	if director["containerId"] != "5c3e8d10-0000-0000-0000-000000000000" {
		t.Errorf("containerId = %v", director["containerId"])
	}
	if director["id"] != camp["id"] {
		t.Errorf("the director says it belongs to %v but the camp is %v", director["id"], camp["id"])
	}
}

// TestDecodeRawReportsABadGroupBlobInline keeps a truncated record from failing the
// whole dump: --full is a view of the file, so one unreadable blob is reported where
// it sits.
func TestDecodeRawReportsABadGroupBlobInline(t *testing.T) {
	for _, testCase := range []struct {
		name string
		path string
	}{
		{name: "group", path: groupPath},
		{name: "base camp", path: baseCampPath},
		{name: "worker director", path: workerDirectorPath},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			node := byteArrayAt(t, true, testCase.path, []byte{1, 2, 3})
			message, ok := node["decodeError"].(string)
			if !ok {
				t.Fatalf("a truncated blob produced %v", node)
			}
			if !strings.Contains(message, "palworld:") {
				t.Errorf("decodeError = %q", message)
			}
			if _, decoded := node["decoded"]; decoded {
				t.Error("a failed decode still produced a decoded block")
			}
		})
	}
}

// TestDecodeRawGroupOutputIsJSONEncodable is the check that the renderer's maps
// hold nothing encoding/json refuses, which is easy to break with a new field.
func TestDecodeRawGroupOutputIsJSONEncodable(t *testing.T) {
	for name, node := range map[string]map[string]any{
		"group":          byteArrayAt(t, true, groupPath, guildRecord()),
		"organization":   byteArrayAt(t, true, groupPath, organizationRecord()),
		"baseCamp":       byteArrayAt(t, true, baseCampPath, baseCampRecord()),
		"workerDirector": byteArrayAt(t, true, workerDirectorPath, workerDirectorRecord()),
	} {
		if _, err := json.Marshal(node); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

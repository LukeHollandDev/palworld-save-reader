// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf16"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// The item-slot and character-slot layouts are short enough to read with a
// handful of named offsets. The group and base camp records are not: they
// interleave counted arrays, Unreal strings and transforms, so an offset for
// every field would be an offset for every field of every record before it.
//
// record is the reader those two use. Its error is sticky, so a decoder reads
// the whole layout as a straight list of fields and checks once at the end --
// the alternative, an error check between every field, buries the layout the
// decoder exists to state. Every read bounds-checks first and returns a zero
// value once the reader has failed, so a truncated or hostile blob cannot walk
// off the end or allocate on a length it never had the bytes for.
type record struct {
	// what names the record being read, so an error says which layout failed.
	what string
	data []byte
	at   int
	err  error
}

func newRecord(what string, data []byte) *record {
	return &record{what: what, data: data}
}

// fail records the first error. Later failures are dropped: the first one is
// where the layout and the bytes parted company, and the ones after it are its
// consequences.
func (r *record) fail(format string, arguments ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("palworld: "+r.what+": "+format, arguments...)
	}
}

// take advances over n bytes and returns them, or fails and returns nil.
func (r *record) take(n int, field string) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.data)-r.at {
		r.fail("%s needs %d bytes at offset %d, but the record is %d bytes",
			field, n, r.at, len(r.data))
		return nil
	}
	out := r.data[r.at : r.at+n]
	r.at += n
	return out
}

func (r *record) u32(field string) uint32 {
	raw := r.take(4, field)
	if raw == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(raw)
}

func (r *record) i32(field string) int32 { return int32(r.u32(field)) }

func (r *record) i64(field string) int64 {
	raw := r.take(8, field)
	if raw == nil {
		return 0
	}
	return int64(binary.LittleEndian.Uint64(raw))
}

func (r *record) u8(field string) uint8 {
	raw := r.take(1, field)
	if raw == nil {
		return 0
	}
	return raw[0]
}

func (r *record) float32(field string) float32 {
	raw := r.take(4, field)
	if raw == nil {
		return 0
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(raw))
}

func (r *record) float64(field string) float64 {
	raw := r.take(8, field)
	if raw == nil {
		return 0
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(raw))
}

func (r *record) guid(field string) gvas.GUID {
	raw := r.take(guidBytes, field)
	if raw == nil {
		return gvas.GUID{}
	}
	return guidAt(raw)
}

// text reads Unreal's FString: an int32 count of code units including the NUL
// terminator, negative when the payload is UTF-16.
//
// This is the same encoding gvas reads, deliberately not shared with it: gvas
// reads from an archive with its own limits and error vocabulary, and a bespoke
// RawData record is a plain byte slice. The duplication is a dozen lines and it
// keeps the layer boundary intact.
func (r *record) text(field string) string {
	if r.err != nil {
		return ""
	}
	units := r.i32(field + " length")
	switch {
	case units == 0:
		return ""
	case units > 0:
		raw := r.take(int(units), field)
		if raw == nil {
			return ""
		}
		if raw[len(raw)-1] != 0 {
			r.fail("%s of %d bytes is not NUL-terminated", field, units)
			return ""
		}
		return string(raw[:len(raw)-1])
	case units == math.MinInt32:
		// Negating this would overflow, so it is rejected before the width is
		// computed rather than after.
		r.fail("%s declares %d code units", field, units)
		return ""
	default:
		raw := r.take(-2*int(units), field)
		if raw == nil {
			return ""
		}
		codes := make([]uint16, len(raw)/2)
		for i := range codes {
			codes[i] = binary.LittleEndian.Uint16(raw[2*i:])
		}
		if codes[len(codes)-1] != 0 {
			r.fail("%s of %d code units is not NUL-terminated", field, -units)
			return ""
		}
		return string(utf16.Decode(codes[:len(codes)-1]))
	}
}

// guids reads a counted array of GUIDs. The count is checked against the bytes
// actually left before anything is allocated, so a blob claiming four billion
// entries fails instead of asking for 64GB.
func (r *record) guids(field string) []gvas.GUID {
	raw := r.array(field, guidBytes)
	if raw == nil {
		return nil
	}
	out := make([]gvas.GUID, len(raw)/guidBytes)
	for i := range out {
		out[i] = guidAt(raw[i*guidBytes:])
	}
	return out
}

// array reads a counted array of fixed-width elements and returns the whole
// payload, or nil when the record has failed or the count does not fit.
func (r *record) array(field string, elementBytes int) []byte {
	if r.err != nil {
		return nil
	}
	count := r.u32(field + " count")
	if r.err != nil {
		return nil
	}
	if uint64(count) > uint64(len(r.data)-r.at)/uint64(elementBytes) {
		r.fail("%s declares %d entries of %d bytes with %d bytes left",
			field, count, elementBytes, len(r.data)-r.at)
		return nil
	}
	return r.take(int(count)*elementBytes, field)
}

// rest returns the bytes after the last field read, aliasing the input.
func (r *record) rest() []byte {
	if r.err != nil {
		return nil
	}
	return r.data[r.at:]
}

// Transform is Unreal's FTransform as a bespoke record writes it: ten float64s,
// rotation first.
//
// Nothing here normalises or converts. Translation is the useful part -- a base
// camp's position in the world -- and the rotation is kept because it is how the
// offsets were confirmed: a wrong split would not produce a unit quaternion, and
// all 32 transforms in the fixture world's base camps do.
type Transform struct {
	Rotation    gvas.Quat
	Translation gvas.Vector
	Scale       gvas.Vector
}

// transformBytes is the serialized width: ten float64s.
const transformBytes = 10 * 8

func (r *record) transform(field string) Transform {
	return Transform{
		Rotation: gvas.Quat{
			X: r.float64(field + ".Rotation.X"),
			Y: r.float64(field + ".Rotation.Y"),
			Z: r.float64(field + ".Rotation.Z"),
			W: r.float64(field + ".Rotation.W"),
		},
		Translation: r.vector(field + ".Translation"),
		Scale:       r.vector(field + ".Scale"),
	}
}

func (r *record) vector(field string) gvas.Vector {
	return gvas.Vector{
		X: r.float64(field + ".X"),
		Y: r.float64(field + ".Y"),
		Z: r.float64(field + ".Z"),
	}
}

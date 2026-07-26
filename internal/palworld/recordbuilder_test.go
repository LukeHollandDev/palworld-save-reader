// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"encoding/binary"
	"math"
	"unicode/utf16"
)

// blobBuilder writes the bespoke record encoding by hand, so a test states the
// bytes it means rather than a hex literal -- and states them without using
// record, which is the reader under test. If both sides shared one encoder, an
// offset could move in lockstep and every assertion would still pass.
type blobBuilder struct {
	out []byte
}

func (b *blobBuilder) u32(value uint32) *blobBuilder {
	b.out = binary.LittleEndian.AppendUint32(b.out, value)
	return b
}

func (b *blobBuilder) i32(value int32) *blobBuilder { return b.u32(uint32(value)) }

func (b *blobBuilder) i64(value int64) *blobBuilder {
	b.out = binary.LittleEndian.AppendUint64(b.out, uint64(value))
	return b
}

func (b *blobBuilder) u8(value uint8) *blobBuilder {
	b.out = append(b.out, value)
	return b
}

func (b *blobBuilder) bytes(value ...byte) *blobBuilder {
	b.out = append(b.out, value...)
	return b
}

func (b *blobBuilder) zeros(count int) *blobBuilder {
	b.out = append(b.out, make([]byte, count)...)
	return b
}

func (b *blobBuilder) float32(value float32) *blobBuilder {
	return b.u32(math.Float32bits(value))
}

func (b *blobBuilder) float64(value float64) *blobBuilder {
	b.out = binary.LittleEndian.AppendUint64(b.out, math.Float64bits(value))
	return b
}

// guid writes the four little-endian words Unreal writes, taking the GUID as its
// four components so a test can name the value it expects to read back.
func (b *blobBuilder) guid(a, c, d, e uint32) *blobBuilder {
	return b.u32(a).u32(c).u32(d).u32(e)
}

// utf8 writes an FString as Unreal writes ASCII: a positive length that counts
// the NUL.
func (b *blobBuilder) utf8(value string) *blobBuilder {
	if value == "" {
		return b.i32(0)
	}
	b.i32(int32(len(value) + 1))
	b.out = append(b.out, value...)
	return b.u8(0)
}

// utf16 writes an FString as Unreal writes anything outside ASCII: a negative
// length in code units, again counting the NUL.
func (b *blobBuilder) utf16(value string) *blobBuilder {
	codes := utf16.Encode([]rune(value))
	b.i32(-int32(len(codes) + 1))
	for _, code := range append(codes, 0) {
		b.out = binary.LittleEndian.AppendUint16(b.out, code)
	}
	return b
}

// transform writes an FTransform: ten float64s, rotation first. The caller gives
// the values it wants to read back rather than a plausible-looking blob.
func (b *blobBuilder) transform(rotation [4]float64, translation, scale [3]float64) *blobBuilder {
	for _, value := range rotation {
		b.float64(value)
	}
	for _, value := range translation {
		b.float64(value)
	}
	for _, value := range scale {
		b.float64(value)
	}
	return b
}

func (b *blobBuilder) done() []byte { return b.out }

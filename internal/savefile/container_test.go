// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package savefile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

func TestDecodeContainerReadsMermaidRawQuantum(t *testing.T) {
	raw := []byte("GVAS synthetic payload")
	body := mermaidRawBody(raw)
	decoded, header, err := DecodeContainer(testContainer(raw, containerMagic, containerSaveType, uint32(len(body)), body))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatalf("decoded = %q, want %q", decoded, raw)
	}
	if header.Magic != containerMagic || header.SaveType != containerSaveType {
		t.Fatalf("container = %q/%#x", header.Magic, header.SaveType)
	}
	if header.RawSize != uint32(len(raw)) || header.CompressedSize != uint32(len(body)) {
		t.Fatalf("header sizes = %d/%d, want %d/%d", header.RawSize, header.CompressedSize, len(raw), len(body))
	}
}

func TestMermaidVerbatimQuantum(t *testing.T) {
	raw := []byte("GVAS synthetic Mermaid payload")
	stored := len(raw) - 1
	body := []byte{
		0x8c, 0x0a,
		byte(stored >> 16), byte(stored >> 8), byte(stored),
	}
	body = append(body, raw...)
	container := testContainer(raw, containerMagic, containerSaveType, uint32(len(body)), body)
	decoded, _, err := DecodeContainer(container)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatalf("decoded = %q, want %q", decoded, raw)
	}
}

// TestDecodeContainerRejectsUnsupportedContainers pins the narrowed format
// scope: only the PlM/0x31 container written by Palworld 1.X is accepted, so
// the pre-1.0 zlib forms are refused rather than decoded on a guess.
func TestDecodeContainerRejectsUnsupportedContainers(t *testing.T) {
	raw := []byte("GVAS synthetic payload")
	body := mermaidRawBody(raw)
	tests := []struct {
		name     string
		magic    string
		saveType byte
	}{
		{name: "legacy single zlib", magic: "PlZ", saveType: 0x31},
		{name: "legacy double zlib", magic: "PlZ", saveType: 0x32},
		{name: "unknown magic", magic: "PlX", saveType: 0x31},
		{name: "unknown save type", magic: containerMagic, saveType: 0x32},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			container := testContainer(raw, test.magic, test.saveType, uint32(len(body)), body)
			_, _, err := DecodeContainer(container)
			if err == nil {
				t.Fatalf("DecodeContainer accepted a %s/%#x container", test.magic, test.saveType)
			}
			if !strings.Contains(err.Error(), "unsupported container") {
				t.Fatalf("error = %v, want an unsupported-container error", err)
			}
		})
	}
}

// TestDecodeContainerRejectsCNKPrefix covers the 12-byte prefix some external
// save tools add. The header is read at a fixed offset, so a prefixed file no
// longer decodes.
func TestDecodeContainerRejectsCNKPrefix(t *testing.T) {
	raw := []byte("GVAS synthetic payload")
	body := mermaidRawBody(raw)
	prefix := []byte{0, 0, 0, 0, 0, 0, 0, 0, 'C', 'N', 'K', 0}
	container := append(prefix, testContainer(raw, containerMagic, containerSaveType, uint32(len(body)), body)...)
	if _, _, err := DecodeContainer(container); err == nil {
		t.Fatal("DecodeContainer accepted a CNK-prefixed container")
	}
}

func TestDecodeContainerRejectsBodySizeMismatch(t *testing.T) {
	raw := []byte("GVAS synthetic payload")
	body := mermaidRawBody(raw)

	trailing := testContainer(raw, containerMagic, containerSaveType, uint32(len(body)), append(body, 0xff))
	if _, _, err := DecodeContainer(trailing); err == nil {
		t.Fatal("DecodeContainer accepted bytes after the Mermaid body")
	}

	oversized := testContainer(raw, containerMagic, containerSaveType, uint32(len(body)+1), body)
	if _, _, err := DecodeContainer(oversized); err == nil {
		t.Fatal("DecodeContainer accepted a compressed size beyond the container")
	}
}

func TestDecodeContainerRejectsShortHeader(t *testing.T) {
	if _, _, err := DecodeContainer(make([]byte, containerHeaderBytes-1)); err == nil {
		t.Fatal("DecodeContainer accepted a container shorter than its header")
	}
}

func TestContainerInputLimitError(t *testing.T) {
	_, _, err := DecodeContainerWithLimits(make([]byte, 13), Limits{
		MaxFileBytes:    12,
		MaxArchiveBytes: 12,
	})
	var limitErr *gvas.LimitError
	if !errors.As(err, &limitErr) || limitErr.Kind != "container input bytes" {
		t.Fatalf("error = %v, want container-input LimitError", err)
	}
}

// FuzzDecodeContainer keeps the header, size, and Mermaid paths from panicking
// on arbitrary bytes. The seeds cover a rejected file and a well-formed
// verbatim-quantum container so the fuzzer starts on both sides of the check.
func FuzzDecodeContainer(f *testing.F) {
	f.Add([]byte("not a save"))
	raw := []byte("GVAS fuzz seed")
	stored := len(raw) - 1
	body := []byte{
		0x8c, 0x0a,
		byte(stored >> 16), byte(stored >> 8), byte(stored),
	}
	body = append(body, raw...)
	f.Add(testContainer(raw, containerMagic, containerSaveType, uint32(len(body)), body))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = DecodeContainerWithLimits(data, Limits{
			MaxFileBytes:    1 << 20,
			MaxArchiveBytes: 1 << 20,
		})
	})
}

// mermaidRawBody wraps raw in the Oodle block header that stores a whole
// quantum verbatim: 0x4c is a non-restart raw block and 0x0a names the Mermaid
// decoder with checksums off.
func mermaidRawBody(raw []byte) []byte {
	return append([]byte{0x4c, 0x0a}, raw...)
}

func testContainer(
	raw []byte,
	magic string,
	saveType byte,
	compressedSize uint32,
	body []byte,
) []byte {
	data := make([]byte, containerHeaderBytes)
	binary.LittleEndian.PutUint32(data, uint32(len(raw)))
	binary.LittleEndian.PutUint32(data[4:], compressedSize)
	copy(data[8:11], magic)
	data[11] = saveType
	return append(data, body...)
}

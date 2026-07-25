// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palsav

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"testing"
)

func TestZlibContainers(t *testing.T) {
	raw := []byte("GVAS synthetic payload")
	inner := testZlib(t, raw)
	outer := testZlib(t, inner)

	tests := []struct {
		name           string
		saveType       byte
		compressedSize uint32
		body           []byte
		prefix         []byte
	}{
		{
			name:           "single",
			saveType:       0x31,
			compressedSize: uint32(len(inner)),
			body:           inner,
		},
		{
			name:           "double",
			saveType:       0x32,
			compressedSize: uint32(len(inner)),
			body:           outer,
		},
		{
			name:           "CNK wrapped",
			saveType:       0x31,
			compressedSize: uint32(len(inner)),
			body:           inner,
			prefix:         []byte{0, 0, 0, 0, 0, 0, 0, 0, 'C', 'N', 'K', 0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			container := testContainer(
				raw,
				"PlZ",
				test.saveType,
				test.compressedSize,
				test.body,
				test.prefix,
			)
			decoded, header, err := DecodeContainer(container)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, raw) {
				t.Fatalf("decoded = %q, want %q", decoded, raw)
			}
			if header.Offset != len(test.prefix) {
				t.Fatalf("header offset = %d, want %d", header.Offset, len(test.prefix))
			}
		})
	}
}

func TestZlibContainerRejectsTrailingBytes(t *testing.T) {
	raw := []byte("GVAS synthetic payload")
	body := append(testZlib(t, raw), 0xff)
	container := testContainer(raw, "PlZ", 0x31, uint32(len(body)), body, nil)
	if _, _, err := DecodeContainer(container); err == nil {
		t.Fatal("DecodeContainer accepted bytes after the zlib stream")
	}
}

func TestContainerInputLimitError(t *testing.T) {
	_, _, err := DecodeContainerWithLimits(make([]byte, 13), Limits{
		MaxInputBytes:  12,
		MaxOutputBytes: 12,
	})
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || limitErr.Kind != "container input bytes" {
		t.Fatalf("error = %v, want container-input LimitError", err)
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
	container := testContainer(raw, "PlM", 0x31, uint32(len(body)), body, nil)
	decoded, _, err := DecodeContainer(container)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatalf("decoded = %q, want %q", decoded, raw)
	}
}

func testZlib(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zlib.NewWriter(&buffer)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testContainer(
	raw []byte,
	magic string,
	saveType byte,
	compressedSize uint32,
	body []byte,
	prefix []byte,
) []byte {
	data := append([]byte(nil), prefix...)
	header := make([]byte, 12)
	binary.LittleEndian.PutUint32(header, uint32(len(raw)))
	binary.LittleEndian.PutUint32(header[4:], compressedSize)
	copy(header[8:11], magic)
	header[11] = saveType
	data = append(data, header...)
	data = append(data, body...)
	return data
}

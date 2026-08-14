// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package mermaid

import (
	"bytes"
	"math/bits"
	"strings"
	"testing"
)

func TestMermaidMode1NearMatches(t *testing.T) {
	history := []byte("ABCDEFGH")
	commands := []byte{120, 248, 248, 248, 248, 248, 248, 248}
	table := append([]byte(nil), history...)
	table = append(table, 0x80, 0x00) // empty raw literal stream
	table = append(table, 0x80, byte(len(commands)))
	table = append(table, commands...)
	table = append(table, 1, 0, 8, 0) // one near offset, distance eight
	table = append(table, 0, 0, 0)    // no far offsets

	chunkWord := 0x800000 | 1<<19 | len(table)
	quantum := []byte{
		byte(chunkWord >> 16), byte(chunkWord >> 8), byte(chunkWord),
	}
	quantum = append(quantum, table...)
	stored := len(quantum) - 1
	body := []byte{
		0x8c, 0x0a,
		byte(stored >> 16), byte(stored >> 8), byte(stored),
	}
	body = append(body, quantum...)

	decoded, err := Decompress(body, 128)
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range decoded {
		if want := history[index%len(history)]; value != want {
			t.Fatalf("decoded[%d] = %q, want %q", index, value, want)
		}
	}
}

func TestMermaidMemsetAndMultipleRawQuanta(t *testing.T) {
	memset, err := Decompress([]byte{
		0x8c, 0x0a,
		0x07, 0xff, 0xff,
		0x5a,
	}, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(memset, bytes.Repeat([]byte{0x5a}, 64)) {
		t.Fatalf("memset output = %x", memset)
	}

	raw := make([]byte, oodleQuantumSize+17)
	copy(raw, "GVAS")
	for index := 4; index < len(raw); index++ {
		raw[index] = byte(index)
	}
	body := []byte{0xcc, 0x0a}
	body = append(body, raw[:oodleQuantumSize]...)
	body = append(body, 0x4c, 0x0a)
	body = append(body, raw[oodleQuantumSize:]...)
	decoded, err := Decompress(body, len(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatal("multiple raw Mermaid quanta did not round-trip")
	}
}

func TestRLEEntropyFill(t *testing.T) {
	// Short entropy header: type 3, compressed=1, decoded=64.
	result, err := decodeEntropy([]byte{0xb0, 0xf8, 0x01, 0x5a}, 64, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.used != 4 || !bytes.Equal(result.data, bytes.Repeat([]byte{0x5a}, 64)) {
		t.Fatalf("RLE result = used:%d data:%x", result.used, result.data)
	}
}

func TestNewHuffmanEntropy(t *testing.T) {
	var codebook testMSBBits
	codebook.write(1, 1) // new code lengths
	codebook.write(0, 1) // supported codebook form
	codebook.write(0, 2) // no forced low bits
	codebook.write(1, 8) // two symbols
	codebook.write(1, 2) // one range-description value
	codebook.unary(13)   // first canonical code length is one
	codebook.unary(9)    // second canonical code length is one
	codebook.unary(5)    // six-bit initial symbol offset
	codebook.write(2, 6) // first symbol is ASCII A

	payload := codebook.bytes()
	payload = append(payload,
		1, 0, // one byte in the first bitstream
		2,    // first stream: A, B
		2, 1, // forward stream A, B; backward stream B, A
	)
	decoded, err := decodeHuffman(payload, 6, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "ABABAB" {
		t.Fatalf("Huffman output = %q", decoded)
	}
}

func TestLegacySparseHuffmanEntropy(t *testing.T) {
	var codebook testMSBBits
	codebook.write(0, 1) // legacy code lengths
	codebook.write(0, 1) // sparse symbol encoding
	codebook.write(2, 8) // two symbols
	codebook.write(0, 3) // fixed code length one
	codebook.write('A', 8)
	codebook.write('B', 8)

	assertABLegacyHuffman(t, codebook.bytes())
}

func TestLegacyDenseHuffmanEntropy(t *testing.T) {
	var codebook testMSBBits
	codebook.write(0, 1) // legacy code lengths
	codebook.write(1, 1) // dense symbol encoding
	codebook.write(0, 2) // no forced low bits
	codebook.write(0, 1) // begin with a zero-symbol run
	codebook.legacyGamma(65)
	codebook.legacyGamma(2)
	codebook.unary(13) // A has canonical length one from initial average eight
	codebook.unary(9)  // B has canonical length one from updated average six
	codebook.legacyGamma(189)

	assertABLegacyHuffman(t, codebook.bytes())
}

func TestLegacySingleSymbolHuffmanEntropy(t *testing.T) {
	var codebook testMSBBits
	codebook.write(0, 1) // legacy code lengths
	codebook.write(0, 1) // sparse symbol encoding
	codebook.write(1, 8) // one symbol
	codebook.write('Z', 8)

	decoded, err := decodeHuffman(codebook.bytes(), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "ZZZZZZZ" {
		t.Fatalf("Huffman output = %q", decoded)
	}
}

func TestLegacyHuffmanRejectsMalformedCodebooks(t *testing.T) {
	tests := []struct {
		name string
		bits func(*testMSBBits)
		want string
	}{
		{
			name: "no symbols",
			bits: func(codebook *testMSBBits) {
				codebook.write(0, 1)
				codebook.write(0, 1)
				codebook.write(0, 8)
			},
			want: "has no symbols",
		},
		{
			name: "invalid sparse width",
			bits: func(codebook *testMSBBits) {
				codebook.write(0, 1)
				codebook.write(0, 1)
				codebook.write(2, 8)
				codebook.write(5, 3)
			},
			want: "code-length width 5",
		},
		{
			name: "incomplete sparse tree",
			bits: func(codebook *testMSBBits) {
				codebook.write(0, 1)
				codebook.write(0, 1)
				codebook.write(2, 8)
				codebook.write(1, 3)
				codebook.write('A', 8)
				codebook.write(1, 1)
				codebook.write('B', 8)
				codebook.write(1, 1)
			},
			want: "incomplete Huffman lookup table",
		},
		{
			name: "dense run overflow",
			bits: func(codebook *testMSBBits) {
				codebook.write(0, 1)
				codebook.write(1, 1)
				codebook.write(0, 2)
				codebook.write(1, 1)
				codebook.legacyGamma(257)
			},
			want: "run exceeds alphabet",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var codebook testMSBBits
			test.bits(&codebook)
			_, _, err := readHuffmanCodebook(&msbReader{data: codebook.bytes()})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestLegacyHuffmanRejectsTruncation(t *testing.T) {
	for _, data := range [][]byte{nil, {0}, {0, 1}} {
		if _, _, err := readHuffmanCodebook(&msbReader{data: data}); err == nil {
			t.Fatalf("readHuffmanCodebook accepted %x", data)
		}
	}
}

func FuzzHuffmanCodebook(f *testing.F) {
	f.Add([]byte{0})

	var sparse testMSBBits
	sparse.write(0, 1)
	sparse.write(0, 1)
	sparse.write(2, 8)
	sparse.write(0, 3)
	sparse.write('A', 8)
	sparse.write('B', 8)
	f.Add(sparse.bytes())

	var dense testMSBBits
	dense.write(0, 1)
	dense.write(1, 1)
	dense.write(0, 2)
	dense.write(0, 1)
	dense.legacyGamma(65)
	dense.legacyGamma(2)
	dense.unary(13)
	dense.unary(9)
	dense.legacyGamma(189)
	f.Add(dense.bytes())

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = readHuffmanCodebook(&msbReader{data: data})
	})
}

func assertABLegacyHuffman(t *testing.T, codebook []byte) {
	t.Helper()
	payload := append([]byte(nil), codebook...)
	payload = append(payload,
		1, 0, // one byte in the first bitstream
		2,    // first stream: A, B
		2, 1, // forward stream A, B; backward stream B, A
	)
	decoded, err := decodeHuffman(payload, 6, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "ABABAB" {
		t.Fatalf("Huffman output = %q", decoded)
	}
}

// TestDecompressRejectsNegativeSize covers the one guard that the container
// layer can no longer reach now that Decompress is a package boundary.
func TestDecompressRejectsNegativeSize(t *testing.T) {
	if _, err := Decompress(nil, -1); err == nil {
		t.Fatal("Decompress accepted a negative output size")
	}
}

type testMSBBits struct {
	bits []byte
}

func (writer *testMSBBits) write(value, count int) {
	for bit := count - 1; bit >= 0; bit-- {
		writer.bits = append(writer.bits, byte(value>>bit)&1)
	}
}

func (writer *testMSBBits) unary(zeros int) {
	writer.bits = append(writer.bits, make([]byte, zeros)...)
	writer.bits = append(writer.bits, 1)
}

func (writer *testMSBBits) legacyGamma(value int) {
	raw := value + 1
	zeros := bits.Len(uint(raw)) - 2
	writer.write(raw, 2*(zeros+1))
}

func (writer *testMSBBits) bytes() []byte {
	value := make([]byte, (len(writer.bits)+7)/8)
	for index, bit := range writer.bits {
		value[index/8] |= bit << (7 - (index % 8))
	}
	return value
}

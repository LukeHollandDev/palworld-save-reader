// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package savefile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/savefixtures"
)

// TestDecodeSuppliedSaves decompresses every private fixture save. Golden
// digests are optional: without them the test still proves the container and
// Mermaid paths accept real saves, and with them it proves the decompressed
// bytes have not drifted.
func TestDecodeSuppliedSaves(t *testing.T) {
	root := savefixtures.Root(t)
	paths := savefixtures.AllSaves(t, root)
	golden := savefixtures.GoldenHashes(t, len(paths))
	for index, path := range paths {
		t.Run(fmt.Sprintf("save-%02d", index+1), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			raw, header, err := DecodeContainer(data)
			if err != nil {
				t.Fatal(err)
			}
			if header.Magic != containerMagic || header.SaveType != containerSaveType {
				t.Fatalf("container = %q/%#x", header.Magic, header.SaveType)
			}
			if len(raw) != int(header.RawSize) || !bytes.HasPrefix(raw, []byte("GVAS")) {
				t.Fatalf("decoded size/prefix = %d/%q", len(raw), raw[:min(4, len(raw))])
			}
			if golden != nil {
				sum := sha256.Sum256(raw)
				if hex.EncodeToString(sum[:]) != golden[index] {
					t.Fatal("decoded payload does not match its private golden hash")
				}
			}
		})
	}
}

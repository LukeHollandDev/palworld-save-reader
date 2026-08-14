// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package savefile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// TestDecodeSuppliedSaveCorpus recursively decompresses an arbitrary private
// save corpus. It complements the structured fixture tests above: backups do
// not always contain a complete world directory, but every .sav can still
// protect the container and Mermaid decoder from format regressions.
func TestDecodeSuppliedSaveCorpus(t *testing.T) {
	root := os.Getenv("PALWORLD_SAVE_CORPUS")
	if root == "" {
		t.Skip("set PALWORLD_SAVE_CORPUS to a directory containing external saves")
	}
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sav" {
			return nil
		}
		count++
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		t.Run(relative, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			raw, header, err := DecodeContainer(data)
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) != int(header.RawSize) || !bytes.HasPrefix(raw, []byte("GVAS")) {
				t.Fatalf("decoded size/prefix = %d/%q", len(raw), raw[:min(4, len(raw))])
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("private save corpus does not contain any .sav files")
	}
	t.Logf("decoded %d private saves", count)
}

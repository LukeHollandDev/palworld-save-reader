// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LukeHollandDev/palworld-save-reader/internal/projection"
)

func TestRunRequiresExplicitMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := run(nil, &stdout, &stderr); status != 2 {
		t.Fatalf("status = %d, want 2", status)
	}
	if !strings.Contains(stderr.String(), "exactly one") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunListsPresetsAsJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := run([]string{"--list-presets"}, &stdout, &stderr); status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	var presets []projection.Preset
	if err := json.Unmarshal(stdout.Bytes(), &presets); err != nil {
		t.Fatalf("stdout is not preset JSON: %v\n%s", err, stdout.String())
	}
}

func TestRunInvalidProjectionUsesStatusTwo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte(`{"projectionVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := run([]string{"--schema", path, "missing.sav"}, &stdout, &stderr)
	if status != 2 {
		t.Fatalf("status = %d, want 2; stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "invalid projection") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunDecodeFailureUsesStatusOne(t *testing.T) {
	projectionPath := filepath.Join(t.TempDir(), "projection.json")
	document := `{
		"projectionVersion": 1,
		"name": "test",
		"gameVersion": "test",
		"saveType": "test.sav",
		"shape": {"Value": null}
	}`
	if err := os.WriteFile(projectionPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := run([]string{"--schema", projectionPath, filepath.Join(t.TempDir(), "missing.sav")}, &stdout, &stderr)
	if status != 1 {
		t.Fatalf("status = %d, want 1; stderr = %q", status, stderr.String())
	}
}

func TestRunProjectsSyntheticSaveEndToEnd(t *testing.T) {
	directory := t.TempDir()
	projectionPath := filepath.Join(directory, "projection.json")
	savePath := filepath.Join(directory, "player.sav")
	document := `{
		"projectionVersion": 1,
		"name": "identity",
		"gameVersion": "test",
		"saveType": "player.sav",
		"shape": {"NickName": "", "Level": 0}
	}`
	if err := os.WriteFile(projectionPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savePath, syntheticSave(t), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	status := run([]string{"--schema", projectionPath, "--explain", savePath}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	var output struct {
		NickName string
		Level    int
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("stdout = %q: %v", stdout.String(), err)
	}
	if output.NickName != "Synthetic Player" || output.Level != 42 {
		t.Fatalf("output = %#v", output)
	}
	var explanation struct {
		Diagnostics []projection.Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &explanation); err != nil {
		t.Fatalf("stderr is not structured JSON: %v\n%s", err, stderr.String())
	}
	if len(explanation.Diagnostics) == 0 {
		t.Fatal("explanation contains no diagnostics")
	}

	stdout.Reset()
	stderr.Reset()
	if status := run([]string{"--full", savePath}, &stdout, &stderr); status != 0 {
		t.Fatalf("--full status = %d, stderr = %q", status, stderr.String())
	}
	var full struct {
		Properties []any `json:"properties"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &full); err != nil {
		t.Fatalf("--full stdout = %q: %v", stdout.String(), err)
	}
	if len(full.Properties) != 2 {
		t.Fatalf("--full properties = %d, want 2", len(full.Properties))
	}
}

func TestRunValidatesModeSpecificFlags(t *testing.T) {
	tests := [][]string{
		{"--full", "--allow-partial", "Level.sav"},
		{"--preset", "players", "Level.sav"},
		{"--list-presets", "Level.sav"},
		{"--full", "--schema", "projection.json", "Level.sav"},
	}
	for _, arguments := range tests {
		var stdout, stderr bytes.Buffer
		if status := run(arguments, &stdout, &stderr); status != 2 {
			t.Errorf("run(%q) status = %d, want 2", arguments, status)
		}
	}
}

type syntheticArchive struct {
	data []byte
}

func (archive *syntheticArchive) u8(value byte) {
	archive.data = append(archive.data, value)
}

func (archive *syntheticArchive) u16(value uint16) {
	archive.data = binary.LittleEndian.AppendUint16(archive.data, value)
}

func (archive *syntheticArchive) u32(value uint32) {
	archive.data = binary.LittleEndian.AppendUint32(archive.data, value)
}

func (archive *syntheticArchive) i32(value int32) {
	archive.u32(uint32(value))
}

func (archive *syntheticArchive) fstring(value string) {
	archive.i32(int32(len(value) + 1))
	archive.data = append(archive.data, value...)
	archive.u8(0)
}

func (archive *syntheticArchive) property(name, propertyType string, payload []byte) {
	archive.fstring(name)
	archive.fstring(propertyType)
	archive.u32(uint32(len(payload)))
	archive.u32(0)
	archive.u8(0)
	archive.data = append(archive.data, payload...)
}

func syntheticSave(t *testing.T) []byte {
	t.Helper()
	var stringPayload syntheticArchive
	stringPayload.fstring("Synthetic Player")
	var integerPayload syntheticArchive
	integerPayload.i32(42)

	var properties syntheticArchive
	properties.property("NickName", "StrProperty", stringPayload.data)
	properties.property("Level", "IntProperty", integerPayload.data)
	properties.fstring("None")

	var gvas syntheticArchive
	gvas.data = append(gvas.data, "GVAS"...)
	gvas.i32(3)
	gvas.i32(522)
	gvas.i32(1008)
	gvas.u16(5)
	gvas.u16(1)
	gvas.u16(1)
	gvas.u32(0)
	gvas.fstring("++UE5+Release-5.1")
	gvas.i32(3)
	gvas.u32(0)
	gvas.fstring("/Script/Pal.SyntheticSaveGame")
	gvas.data = append(gvas.data, properties.data...)

	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(gvas.data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var container syntheticArchive
	container.u32(uint32(len(gvas.data)))
	container.u32(uint32(compressed.Len()))
	container.data = append(container.data, "PlZ"...)
	container.u8(0x31)
	container.data = append(container.data, compressed.Bytes()...)
	return container.data
}

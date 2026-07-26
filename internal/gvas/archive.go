// Copyright (C) 2026 Luke Holland
// Portions copyright 2026 Palhelm contributors and licensed under Apache-2.0.
// Adapted and substantially modified on 2026-07-23. See NOTICE.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package gvas reads Unreal Engine's GVAS save-game serialization.
//
// It knows Unreal's container-independent property encoding and nothing about
// any particular game: callers name the concrete struct behind an untagged
// StructProperty through Options.TypeHints. Compressed .sav wrappers are
// internal/savefile's concern, and Palworld's own schema is
// internal/palworld's.
//
// The package uses only the Go standard library and never writes to a save.
// Large collections decode one element at a time through the Iterator methods
// on MapValue, SetValue, and StructArray; the eager Entries and Values helpers
// are for small properties only.
package gvas

import (
	"bytes"
	"fmt"
)

// Parse reads an already decompressed Unreal SaveGame archive. It retains data
// without copying; callers must not mutate it while the Archive is in use.
func Parse(data []byte) (*Archive, error) {
	return ParseWithOptions(data, Options{})
}

// ParseWithOptions is Parse with explicit limits and type hints.
func ParseWithOptions(data []byte, options Options) (*Archive, error) {
	cfg, err := options.normalized()
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > cfg.MaxArchiveBytes {
		return nil, &LimitError{
			Kind:  "GVAS archive bytes",
			Value: uint64(len(data)),
			Limit: uint64(cfg.MaxArchiveBytes),
		}
	}
	return parse(data, cfg)
}

func parse(data []byte, cfg *decodeConfig) (*Archive, error) {
	reader := newArchiveReader(data, cfg)
	header, err := readHeader(reader)
	if err != nil {
		return nil, err
	}
	properties, err := readPropertyList(reader, "")
	if err != nil {
		return nil, err
	}
	trailer, err := reader.take(reader.remaining())
	if err != nil {
		return nil, err
	}
	return &Archive{
		Header:     header,
		Properties: properties,
		Trailer:    trailer,
		Raw:        data,
	}, nil
}

func readHeader(reader *archiveReader) (Header, error) {
	var header Header
	magic, err := reader.take(4)
	if err != nil {
		return header, err
	}
	if !bytes.Equal(magic, []byte("GVAS")) {
		return header, fmt.Errorf("gvas: invalid GVAS magic %q", magic)
	}
	if header.SaveGameVersion, err = reader.i32(); err != nil {
		return header, err
	}
	if header.SaveGameVersion != 3 {
		return header, fmt.Errorf(
			"gvas: unsupported save-game version %d (want 3)",
			header.SaveGameVersion,
		)
	}
	if header.PackageUE4, err = reader.i32(); err != nil {
		return header, err
	}
	if header.PackageUE5, err = reader.i32(); err != nil {
		return header, err
	}
	if header.Engine.Major, err = reader.u16(); err != nil {
		return header, err
	}
	if header.Engine.Minor, err = reader.u16(); err != nil {
		return header, err
	}
	if header.Engine.Patch, err = reader.u16(); err != nil {
		return header, err
	}
	if header.Engine.ChangeList, err = reader.u32(); err != nil {
		return header, err
	}
	if header.Engine.Branch, err = reader.fstring(); err != nil {
		return header, err
	}
	if header.CustomFormat, err = reader.i32(); err != nil {
		return header, err
	}
	if header.CustomFormat != 3 {
		return header, fmt.Errorf(
			"gvas: unsupported custom-version format %d (want 3)",
			header.CustomFormat,
		)
	}
	count, err := reader.u32()
	if err != nil {
		return header, err
	}
	if err := validateCount(reader.state.cfg, "custom-version", count, reader.remaining(), 20); err != nil {
		return header, err
	}
	header.CustomVersions = make([]CustomVersion, count)
	for i := range header.CustomVersions {
		if header.CustomVersions[i].ID, err = reader.guid(); err != nil {
			return header, err
		}
		if header.CustomVersions[i].Version, err = reader.i32(); err != nil {
			return header, err
		}
	}
	if header.ClassName, err = reader.fstring(); err != nil {
		return header, err
	}
	return header, nil
}

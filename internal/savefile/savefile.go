// Copyright (C) 2026 Luke Holland
// Portions copyright 2026 Palhelm contributors and licensed under Apache-2.0.
// Adapted and substantially modified on 2026-07-23. See NOTICE.
// SPDX-License-Identifier: GPL-3.0-or-later

package savefile

import (
	"fmt"
	"io"
	"math"
	"os"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// Options controls both halves of reading a save: the byte limits this package
// applies to the container, and the limits and type hints gvas applies to the
// archive inside it. A zero field selects the relevant package default.
type Options struct {
	Limits Limits
	GVAS   gvas.Options
}

// Save is one decoded .sav file: Palworld's container header and the Unreal
// archive it wrapped. The embedded *gvas.Archive is what callers read, so
// save.Properties and save.Header work directly.
type Save struct {
	Container ContainerHeader
	*gvas.Archive
}

// Read decompresses and parses a complete .sav file held in memory.
func Read(data []byte, options Options) (*Save, error) {
	limits, err := options.Limits.normalized()
	if err != nil {
		return nil, err
	}
	raw, container, err := DecodeContainerWithLimits(data, limits)
	if err != nil {
		return nil, err
	}
	// Both packages bound the archive, so default gvas to the limit the caller
	// set here. Otherwise Limits.MaxArchiveBytes and GVAS.MaxArchiveBytes would
	// be two knobs for one quantity, and lowering the obvious one would do
	// nothing once the container check passed.
	gvasOptions := options.GVAS
	if gvasOptions.MaxArchiveBytes == 0 {
		gvasOptions.MaxArchiveBytes = limits.MaxArchiveBytes
	}
	archive, err := gvas.ParseWithOptions(raw, gvasOptions)
	if err != nil {
		return nil, err
	}
	return &Save{Container: container, Archive: archive}, nil
}

// Open reads, decompresses, and parses a .sav file from disk.
func Open(path string, options Options) (*Save, error) {
	limits, err := options.Limits.normalized()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("savefile: open %q: %w", path, err)
	}
	defer file.Close()

	// Read one byte past the limit so an oversized file is reported as such
	// rather than silently truncated to exactly the limit.
	readLimit := limits.MaxFileBytes
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	data, err := io.ReadAll(io.LimitReader(file, readLimit))
	if err != nil {
		return nil, fmt.Errorf("savefile: read %q: %w", path, err)
	}
	if int64(len(data)) > limits.MaxFileBytes {
		return nil, &gvas.LimitError{
			Kind:  "file input bytes",
			Value: uint64(len(data)),
			Limit: uint64(limits.MaxFileBytes),
		}
	}
	return Read(data, options)
}

// Copyright (C) 2026 Luke Holland
// Portions copyright 2026 Palhelm contributors and licensed under Apache-2.0.
// Adapted and substantially modified on 2026-07-23. See NOTICE.
// SPDX-License-Identifier: GPL-3.0-or-later
//

// Package savefile unwraps the compressed container Palworld writes around an
// Unreal GVAS archive.
//
// Palworld 1.X writes exactly one container form, so this package accepts only
// that one and rejects every other rather than guessing. Read and Open hand the
// decompressed archive to internal/gvas; the type hints that archive needs to
// resolve Palworld's untagged structs come from internal/palworld, which is the
// entry point most callers want.
//
// The package uses only the Go standard library. It is a read-only decoder: it
// never modifies a save and does not implement Oodle compression.
package savefile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/savefile/mermaid"
)

const (
	defaultMaxFileBytes    = 1 << 30
	defaultMaxArchiveBytes = 1 << 30
)

// Palworld 1.X writes exactly one container form: a 12-byte header naming the
// magic "PlM" and save type 0x31, followed by an Oodle Mermaid stream.
const (
	containerHeaderBytes = 12
	containerMagic       = "PlM"
	containerSaveType    = 0x31
)

// Limits bounds the bytes read and produced while decoding an untrusted save. A
// zero field selects the package default.
type Limits struct {
	// MaxFileBytes bounds the compressed .sav file.
	MaxFileBytes int64
	// MaxArchiveBytes bounds the GVAS archive the container declares.
	MaxArchiveBytes int64
}

func (l Limits) normalized() (Limits, error) {
	if l.MaxFileBytes == 0 {
		l.MaxFileBytes = defaultMaxFileBytes
	}
	if l.MaxArchiveBytes == 0 {
		l.MaxArchiveBytes = defaultMaxArchiveBytes
	}
	if l.MaxFileBytes < containerHeaderBytes {
		return Limits{}, fmt.Errorf("savefile: MaxFileBytes must be at least %d", containerHeaderBytes)
	}
	if l.MaxArchiveBytes < 1 {
		return Limits{}, fmt.Errorf("savefile: MaxArchiveBytes must be positive")
	}
	return l, nil
}

// ContainerHeader describes Palworld's wrapper around a GVAS payload.
type ContainerHeader struct {
	RawSize uint32
	// CompressedSize is the on-disk size of the Mermaid body that follows the
	// header, and must account for every remaining byte in the file.
	CompressedSize uint32
	Magic          string
	SaveType       byte
}

// DecodeContainer unwraps and decompresses a complete Palworld .sav file into
// its GVAS archive. It supports the single PlM/0x31 Oodle Mermaid container
// written by Palworld 1.X; every other container form is rejected rather than
// guessed at.
func DecodeContainer(data []byte) ([]byte, ContainerHeader, error) {
	return DecodeContainerWithLimits(data, Limits{})
}

// DecodeContainerWithLimits is DecodeContainer with explicit resource limits.
func DecodeContainerWithLimits(data []byte, limits Limits) ([]byte, ContainerHeader, error) {
	limits, err := limits.normalized()
	if err != nil {
		return nil, ContainerHeader{}, err
	}
	if int64(len(data)) > limits.MaxFileBytes {
		return nil, ContainerHeader{}, &gvas.LimitError{
			Kind:  "container input bytes",
			Value: uint64(len(data)),
			Limit: uint64(limits.MaxFileBytes),
		}
	}
	header, err := parseContainerHeader(data)
	if err != nil {
		return nil, header, err
	}
	if int64(header.RawSize) > limits.MaxArchiveBytes {
		return nil, header, &gvas.LimitError{
			Kind:  "declared archive bytes",
			Value: uint64(header.RawSize),
			Limit: uint64(limits.MaxArchiveBytes),
		}
	}
	if uint64(header.RawSize) > uint64(math.MaxInt) {
		return nil, header, &gvas.LimitError{
			Kind:  "declared archive bytes for this platform",
			Value: uint64(header.RawSize),
			Limit: uint64(math.MaxInt),
		}
	}
	body, err := containerBody(data, header.CompressedSize)
	if err != nil {
		return nil, header, err
	}
	raw, err := mermaid.Decompress(body, int(header.RawSize))
	if err != nil {
		return nil, header, err
	}
	if len(raw) != int(header.RawSize) {
		return nil, header, fmt.Errorf("savefile: decoded %d bytes, expected %d", len(raw), header.RawSize)
	}
	if !bytes.HasPrefix(raw, []byte("GVAS")) {
		return nil, header, errors.New("savefile: decoded payload does not begin with GVAS")
	}
	return raw, header, nil
}

func parseContainerHeader(data []byte) (ContainerHeader, error) {
	if len(data) < containerHeaderBytes {
		return ContainerHeader{}, fmt.Errorf(
			"savefile: container is %d bytes, shorter than its %d-byte header",
			len(data),
			containerHeaderBytes,
		)
	}
	magic, saveType := string(data[8:11]), data[11]
	if magic != containerMagic || saveType != containerSaveType {
		return ContainerHeader{}, fmt.Errorf(
			"savefile: unsupported container %q/%#x, want %q/%#x as written by Palworld 1.X",
			magic,
			saveType,
			containerMagic,
			containerSaveType,
		)
	}
	return ContainerHeader{
		RawSize:        binary.LittleEndian.Uint32(data),
		CompressedSize: binary.LittleEndian.Uint32(data[4:]),
		Magic:          magic,
		SaveType:       saveType,
	}, nil
}

func containerBody(data []byte, compressedSize uint32) ([]byte, error) {
	body := data[containerHeaderBytes:]
	if uint64(compressedSize) > uint64(len(body)) {
		return nil, fmt.Errorf("savefile: compressed size %d exceeds the container", compressedSize)
	}
	if int(compressedSize) != len(body) {
		return nil, fmt.Errorf("savefile: %d trailing container bytes", len(body)-int(compressedSize))
	}
	return body, nil
}

// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld

import (
	"strings"
	"testing"
)

// The record layout, stated here rather than read out of basecamp.go.
const (
	wantTransformBytes           = 80
	wantWorkerReservedBytes      = 2
	wantBaseCampFixedBytes       = 16 + 1 + 80 + 4 + 16 + 80 + 16 // everything but the name
	wantWorkerDirectorFixedBytes = 16 + 80 + 2 + 16
)

// The values the blob below carries, named so the test asserts what it wrote.
// All of them are invented: a real camp's coordinates say where a real player
// built their base, which is not something this repository should carry.
var (
	testCampRotation    = [4]float64{0, 0, 0.8628, 0.5056}
	testCampTranslation = [3]float64{-122500.5, 48250.25, 2600.75}
	testCampScale       = [3]float64{1, 1, 1}
	testFastTravel      = [3]float64{12, 34, 56}
	testCampName        = "拠点1"
	testContainer       = [4]uint32{0x5c3e8d10, 0x62a74f19, 0x8b40d2ec, 0x37519ab6}
	testContainerText   = "5c3e8d10-62a7-4f19-8b40-d2ec37519ab6"
)

func baseCampBlob() []byte {
	return new(blobBuilder).
		guid(testBase[0], testBase[1], testBase[2], testBase[3]).
		utf16(testCampName).
		u8(1). // state
		transform(testCampRotation, testCampTranslation, testCampScale).
		float32(3500).
		guid(testGroupID[0], testGroupID[1], testGroupID[2], testGroupID[3]).
		transform(testCampRotation, testFastTravel, testCampScale).
		guid(testPoint[0], testPoint[1], testPoint[2], testPoint[3]).
		zeros(4).
		done()
}

func workerDirectorBlob() []byte {
	return new(blobBuilder).
		guid(testBase[0], testBase[1], testBase[2], testBase[3]).
		transform(testCampRotation, testCampTranslation, testCampScale).
		zeros(wantWorkerReservedBytes).
		guid(testContainer[0], testContainer[1], testContainer[2], testContainer[3]).
		zeros(4).
		done()
}

func TestDecodeBaseCamp(t *testing.T) {
	camp, err := DecodeBaseCamp(baseCampBlob())
	if err != nil {
		t.Fatal(err)
	}
	if got := camp.ID.String(); got != testBaseText {
		t.Errorf("ID = %s, want %s", got, testBaseText)
	}
	// The fixture world's camp names are all Japanese, so UTF-16 is the case that
	// matters rather than the exotic one.
	if camp.Name != testCampName {
		t.Errorf("Name = %q, want %q", camp.Name, testCampName)
	}
	if camp.State != 1 {
		t.Errorf("State = %d, want 1", camp.State)
	}
	if camp.AreaRange != 3500 {
		t.Errorf("AreaRange = %v, want 3500", camp.AreaRange)
	}
	if got := camp.GroupID.String(); got != testGroupIDText {
		t.Errorf("GroupID = %s, want %s", got, testGroupIDText)
	}
	if got := camp.OwnerMapObjectID.String(); got != testPointText {
		t.Errorf("OwnerMapObjectID = %s, want %s", got, testPointText)
	}
	// The transform is ten doubles in one order, and the translation is the part
	// anything uses, so reading it back is the check that the order is right.
	got := camp.Transform.Translation
	if got.X != testCampTranslation[0] || got.Y != testCampTranslation[1] || got.Z != testCampTranslation[2] {
		t.Errorf("Translation = %+v, want %v", got, testCampTranslation)
	}
	if camp.Transform.Rotation.W != testCampRotation[3] {
		t.Errorf("Rotation = %+v, want %v", camp.Transform.Rotation, testCampRotation)
	}
	if camp.Transform.Scale.X != 1 {
		t.Errorf("Scale = %+v, want ones", camp.Transform.Scale)
	}
	// The two transforms must not be confused for one another: they are 80 bytes
	// apart with four bytes and a GUID between them, so a wrong width here shows
	// up as the wrong translation rather than as an error.
	if camp.FastTravel.Translation.X != testFastTravel[0] {
		t.Errorf("FastTravel.Translation = %+v, want %v", camp.FastTravel.Translation, testFastTravel)
	}
	if len(camp.Trailer) != 4 {
		t.Errorf("Trailer = %d bytes, want 4", len(camp.Trailer))
	}
	if got := transformBytes; got != wantTransformBytes {
		t.Errorf("a transform is %d bytes, want %d", got, wantTransformBytes)
	}
}

func TestDecodeWorkerDirector(t *testing.T) {
	director, err := DecodeWorkerDirector(workerDirectorBlob())
	if err != nil {
		t.Fatal(err)
	}
	if got := director.ID.String(); got != testBaseText {
		t.Errorf("ID = %s, want %s", got, testBaseText)
	}
	if got := director.ContainerID.String(); got != testContainerText {
		t.Errorf("ContainerID = %s, want %s", got, testContainerText)
	}
	if len(director.Reserved) != wantWorkerReservedBytes {
		t.Errorf("Reserved = %d bytes, want %d", len(director.Reserved), wantWorkerReservedBytes)
	}
	if director.Transform.Translation.Y != testCampTranslation[1] {
		t.Errorf("Translation = %+v", director.Transform.Translation)
	}
	if len(director.Trailer) != 4 {
		t.Errorf("Trailer = %d bytes, want 4", len(director.Trailer))
	}
}

// TestBaseCampRecordsRejectTruncation covers every prefix of both records. The
// trailers are the only optional part, so a prefix that stops before the last
// field must be an error rather than a camp at the wrong place in the world.
func TestBaseCampRecordsRejectTruncation(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		blob   []byte
		fixed  int
		decode func([]byte) error
		names  string
	}{
		{
			name:  "base camp",
			blob:  baseCampBlob(),
			fixed: wantBaseCampFixedBytes,
			decode: func(data []byte) error {
				_, err := DecodeBaseCamp(data)
				return err
			},
			names: "base camp record",
		},
		{
			name:  "worker director",
			blob:  workerDirectorBlob(),
			fixed: wantWorkerDirectorFixedBytes,
			decode: func(data []byte) error {
				_, err := DecodeWorkerDirector(data)
				return err
			},
			names: "base camp worker director",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			for length := 0; length < testCase.fixed; length++ {
				err := testCase.decode(testCase.blob[:length])
				if err == nil {
					t.Errorf("%d bytes decoded, want an error", length)
					continue
				}
				if !strings.Contains(err.Error(), testCase.names) {
					t.Errorf("%d bytes: error %q does not name the record", length, err)
				}
			}
			// The whole record with no trailer at all is valid: a trailer is
			// whatever remains, and there may be none.
			if err := testCase.decode(testCase.blob[:len(testCase.blob)-4]); err != nil {
				t.Errorf("a record with no trailer: %v", err)
			}
		})
	}
}

// TestDecodeBaseCampAliasesTheTrailer documents that the trailer is a window onto
// the caller's bytes, the same contract every other RawData decoder has.
func TestDecodeBaseCampAliasesTheTrailer(t *testing.T) {
	blob := baseCampBlob()
	camp, err := DecodeBaseCamp(blob)
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)-1] = 7
	if camp.Trailer[len(camp.Trailer)-1] != 7 {
		t.Error("Trailer does not alias the input")
	}
}

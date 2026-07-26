// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package gvas

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestParseGUIDReadsBothSpellings states the text and the four words
// independently. Deriving either from the other would only prove ParseGUID and
// String agree with each other, not that they agree with Unreal's layout.
func TestParseGUIDReadsBothSpellings(t *testing.T) {
	want := GUID{A: 0x03ce233b, B: 0x11223344, C: 0x55667788, D: 0x99aabbcc}
	for _, text := range []string{
		"03ce233b-1122-3344-5566-778899aabbcc",
		"03ce233b112233445566778899aabbcc",
		"03CE233B112233445566778899AABBCC",
		"03CE233B-1122-3344-5566-778899AABBCC",
	} {
		got, err := ParseGUID(text)
		if err != nil {
			t.Errorf("ParseGUID(%q): %v", text, err)
			continue
		}
		if got != want {
			t.Errorf("ParseGUID(%q) = %+v, want %+v", text, got, want)
		}
	}

	// The file-name spelling Palworld uses, which is the reason the dash-free form
	// is accepted at all.
	stem := "03CE233B000000000000000000000000"
	guid, err := ParseGUID(stem)
	if err != nil {
		t.Fatal(err)
	}
	if guid.String() != "03ce233b-0000-0000-0000-000000000000" {
		t.Errorf("ParseGUID(%q).String() = %s", stem, guid)
	}
}

func TestParseGUIDRejectsMalformedText(t *testing.T) {
	for _, testCase := range []struct {
		name string
		text string
		want string
	}{
		{name: "empty", text: "", want: "hex digits"},
		{name: "too short", text: "03ce233b", want: "hex digits"},
		{name: "too long", text: strings.Repeat("a", 33), want: "hex digits"},
		{name: "dashes in the wrong places", text: "03ce233b1-122-3344-5566-778899aabbcc", want: "dashes"},
		{name: "one dash", text: "03ce233b-112233445566778899aabbcc", want: "dashes"},
		{name: "trailing dash", text: "03ce233b-1122-3344-5566-778899aabbcc-", want: "dashes"},
		{name: "not hex", text: "03ce233b-1122-3344-5566-778899aabbcz", want: "is not a GUID"},
		{name: "hex with a sign", text: "+3ce233b-1122-3344-5566-778899aabbcc", want: "is not a GUID"},
		{name: "spaces", text: " 3ce233b-1122-3344-5566-778899aabbcc", want: "is not a GUID"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseGUID(testCase.text)
			if err == nil {
				t.Fatalf("ParseGUID(%q) returned no error", testCase.text)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("error %q does not mention %q", err, testCase.want)
			}
		})
	}
}

// FuzzParseGUIDRoundTrip is the property that matters: whatever String writes,
// ParseGUID must read back. The two halves are written independently -- one
// formats four words, the other parses eight-digit groups -- so a mistake in
// either direction shows up here rather than as an identifier that resolves to
// nothing.
func FuzzParseGUIDRoundTrip(f *testing.F) {
	f.Add(uint32(0), uint32(0), uint32(0), uint32(0))
	f.Add(uint32(0x03ce233b), uint32(0), uint32(0), uint32(0))
	f.Add(^uint32(0), ^uint32(0), ^uint32(0), ^uint32(0))
	f.Fuzz(func(t *testing.T, a, b, c, d uint32) {
		guid := GUID{A: a, B: b, C: c, D: d}
		text := guid.String()
		if len(text) != guidTextBytes {
			t.Fatalf("String() = %q, want %d characters", text, guidTextBytes)
		}
		parsed, err := ParseGUID(text)
		if err != nil {
			t.Fatalf("ParseGUID(%q): %v", text, err)
		}
		if parsed != guid {
			t.Fatalf("round trip of %+v through %q gave %+v", guid, text, parsed)
		}
		// The dash-free spelling must decode to the same value, since it is the
		// same digits in the same order.
		bare, err := ParseGUID(strings.ReplaceAll(text, "-", ""))
		if err != nil {
			t.Fatalf("ParseGUID of the dash-free form of %q: %v", text, err)
		}
		if bare != guid {
			t.Fatalf("dash-free round trip of %+v gave %+v", guid, bare)
		}
	})
}

// TestGUIDRoundTripsThroughJSON is why UnmarshalText exists: MarshalText already
// made a GUID readable in a dump, and a consumer that reads its own output back
// should not have to reimplement the parse.
func TestGUIDRoundTripsThroughJSON(t *testing.T) {
	want := GUID{A: 0x03ce233b, B: 0x11223344, C: 0x55667788, D: 0x99aabbcc}
	encoded, err := json.Marshal(struct{ ID GUID }{want})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"03ce233b-1122-3344-5566-778899aabbcc"`) {
		t.Fatalf("encoded = %s", encoded)
	}
	var decoded struct{ ID GUID }
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != want {
		t.Errorf("decoded = %+v, want %+v", decoded.ID, want)
	}
	if err := json.Unmarshal([]byte(`{"ID":"not-a-guid"}`), &decoded); err == nil {
		t.Error("an invalid GUID unmarshalled without error")
	}
}

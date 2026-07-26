// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"strings"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
)

// Reading one named field out of a decoded save takes three steps every time:
// find the property, unwrap the struct Unreal wrapped it in, and assert the Go
// type gvas decoded it to. These helpers do that in one call so the resolvers
// below read as a list of the fields they want rather than as a walk.
//
// This is deliberately not projection's matcher. Projection matches a declared
// shape and reports why a field did not resolve; this asks for one exact path
// and takes "absent" for an answer, because a resolver knows which fields are
// optional and what to say when one is missing.

// field walks a dotted property path and returns the value with its struct
// wrappers removed, or nil if any segment is missing.
//
// A StructProperty contributes no path segment of its own, matching how gvas
// builds paths for type hints: ".LastTransform.Translation" reaches the Vector
// inside the Transform inside the property.
func field(properties gvas.Properties, path string) any {
	current := any(properties)
	for _, name := range strings.Split(path, ".") {
		list, ok := unwrap(current).(gvas.Properties)
		if !ok {
			return nil
		}
		property := list.Find(name)
		if property == nil {
			return nil
		}
		current = property.Value
	}
	return unwrap(current)
}

// unwrap removes gvas.StructValue wrappers. It loops because a struct can hold a
// struct, and the wrapper carries only the Unreal type name, which a caller
// asking for a path by name already knows.
func unwrap(value any) any {
	for {
		structValue, ok := value.(gvas.StructValue)
		if !ok {
			return value
		}
		value = structValue.Value
	}
}

// value reads a field of a known Go type. The second result is false when the
// path is missing or decoded to a different type, which a caller must treat as
// "the schema moved" rather than as a zero value.
func value[T any](properties gvas.Properties, path string) (T, bool) {
	typed, ok := field(properties, path).(T)
	return typed, ok
}

// guid reads a Guid-valued struct property.
func guid(properties gvas.Properties, path string) (gvas.GUID, bool) {
	return value[gvas.GUID](properties, path)
}

// enumName reads an EnumProperty and strips the Unreal enum type prefix, so
// "EPalPlayerPlatform::PS5" is reported as "PS5". The prefix repeats the type
// name in every value, and the type is implied by the field.
func enumName(properties gvas.Properties, path string) (string, bool) {
	enum, ok := value[gvas.EnumValue](properties, path)
	if !ok {
		return "", false
	}
	name := enum.Value
	if index := strings.LastIndex(name, "::"); index >= 0 {
		name = name[index+2:]
	}
	return name, name != ""
}

// stamp reads a DateTime property, which gvas decodes to a tick count.
func stamp(properties gvas.Properties, path string) (Timestamp, bool) {
	ticks, ok := value[int64](properties, path)
	if !ok {
		return Timestamp{}, false
	}
	return timestamp(ticks), true
}

// blob reads a byte-array property such as a RawData payload. The result aliases
// the archive, which is what makes decoding one lazy.
func blob(properties gvas.Properties, path string) ([]byte, bool) {
	array, ok := value[gvas.ArrayValue](properties, path)
	if !ok {
		return nil, false
	}
	data, ok := array.Values.([]byte)
	return data, ok
}

// structArray reads an ArrayProperty<StructProperty>, which is the shape of a
// container's Slots. The result is lazy: nothing is decoded until it is
// iterated, which is what memory rule R1 requires of a Level.sav collection.
func structArray(properties gvas.Properties, path string) (*gvas.StructArray, bool) {
	array, ok := value[gvas.ArrayValue](properties, path)
	if !ok || array.Structs == nil {
		return nil, false
	}
	return array.Structs, true
}

// names reads an ArrayProperty of NameProperty or StrProperty, which gvas
// decodes to a []string.
func names(properties gvas.Properties, path string) []string {
	array, ok := value[gvas.ArrayValue](properties, path)
	if !ok {
		return nil
	}
	values, _ := array.Values.([]string)
	return values
}

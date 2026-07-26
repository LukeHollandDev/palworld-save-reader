// Copyright (C) 2026 Luke Holland
// Portions copyright 2026 Palhelm contributors and licensed under Apache-2.0.
// Adapted and substantially modified on 2026-07-23. See NOTICE.
// SPDX-License-Identifier: GPL-3.0-or-later

package gvas

import "fmt"

const (
	defaultMaxArchiveBytes       = 1 << 30
	defaultMaxStringBytes        = 16 << 20
	defaultMaxPathBytes          = 64 << 10
	defaultMaxCollectionElements = 10_000_000
	defaultMaxDepth              = 128
	defaultMaxProperties         = 10_000_000
)

// Options bounds the work Parse performs on an untrusted archive. A zero field
// selects the package default.
type Options struct {
	// MaxArchiveBytes bounds the whole archive handed to Parse.
	MaxArchiveBytes int64
	// MaxStringBytes bounds one serialized FString, including its terminator.
	MaxStringBytes int
	// MaxPathBytes bounds one constructed nested property path.
	MaxPathBytes int
	// MaxCollectionElements bounds one array, map, or set.
	MaxCollectionElements uint32
	// MaxDepth bounds nested property lists and collection values.
	MaxDepth int
	// MaxProperties bounds the number of eagerly decoded tagged properties.
	MaxProperties uint64

	// TypeHints names the concrete struct behind a StructProperty whose tag
	// does not carry one, keyed by property path such as
	// ".worldSaveData.BaseCampSaveData.Key". Unreal's legacy tags omit that
	// identity for map keys, map values, and set elements, so only a caller who
	// knows the game's schema can supply it. This package ships no hints of its
	// own; see internal/palworld for Palworld's table.
	TypeHints map[string]string
}

// decodeConfig is an Options that has been through normalized: every zero
// field has been replaced by its default, every value has been range-checked,
// and TypeHints holds a private copy of the caller's map. It is a distinct type
// so an unvalidated Options cannot reach the decoder by mistake.
type decodeConfig struct {
	Options
}

// normalized validates o and returns the config the decoder reads. The
// receiver is a copy and TypeHints is cloned, so a caller's Options and map are
// left untouched and cannot change under an in-flight parse.
func (o Options) normalized() (*decodeConfig, error) {
	if o.MaxArchiveBytes == 0 {
		o.MaxArchiveBytes = defaultMaxArchiveBytes
	}
	if o.MaxStringBytes == 0 {
		o.MaxStringBytes = defaultMaxStringBytes
	}
	if o.MaxPathBytes == 0 {
		o.MaxPathBytes = defaultMaxPathBytes
	}
	if o.MaxCollectionElements == 0 {
		o.MaxCollectionElements = defaultMaxCollectionElements
	}
	if o.MaxDepth == 0 {
		o.MaxDepth = defaultMaxDepth
	}
	if o.MaxProperties == 0 {
		o.MaxProperties = defaultMaxProperties
	}
	if o.MaxArchiveBytes < 1 || o.MaxStringBytes < 1 || o.MaxPathBytes < 1 ||
		o.MaxCollectionElements < 1 || o.MaxDepth < 1 || o.MaxProperties < 1 {
		return nil, fmt.Errorf("gvas: limits must be positive")
	}
	hints := make(map[string]string, len(o.TypeHints))
	for path, hint := range o.TypeHints {
		if path == "" || hint == "" {
			return nil, fmt.Errorf("gvas: empty type-hint path or value")
		}
		hints[path] = hint
	}
	o.TypeHints = hints
	return &decodeConfig{Options: o}, nil
}

// Archive is a parsed Unreal SaveGame object.
type Archive struct {
	Header     Header
	Properties Properties
	Trailer    []byte
	// Raw is the complete GVAS buffer. Property payloads and Trailer alias it,
	// so callers should treat it as immutable.
	Raw []byte
}

// Header is Unreal's SaveGame archive header.
type Header struct {
	SaveGameVersion int32
	PackageUE4      int32
	PackageUE5      int32
	Engine          EngineVersion
	CustomFormat    int32
	CustomVersions  []CustomVersion
	ClassName       string
}

type EngineVersion struct {
	Major      uint16
	Minor      uint16
	Patch      uint16
	ChangeList uint32
	Branch     string
}

type CustomVersion struct {
	ID      GUID
	Version int32
}

// GUID is Unreal's four-uint32 FGuid representation.
type GUID struct {
	A uint32
	B uint32
	C uint32
	D uint32
}

func (g GUID) String() string {
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%04x%08x",
		g.A,
		g.B>>16,
		g.B&0xffff,
		g.C>>16,
		g.C&0xffff,
		g.D,
	)
}

func (g GUID) IsZero() bool { return g.A|g.B|g.C|g.D == 0 }

func (g GUID) MarshalText() ([]byte, error) { return []byte(g.String()), nil }

// Properties preserves Unreal's serialized property order and array indices.
type Properties []Property

// Find returns the first property with name, or nil.
func (p Properties) Find(name string) *Property {
	for i := range p {
		if p[i].Name == name {
			return &p[i]
		}
	}
	return nil
}

// Property is one legacy Unreal FPropertyTag and its value.
type Property struct {
	Name       string
	Type       string
	Size       uint32
	ArrayIndex uint32
	Meta       PropertyMeta

	PropertyGUID *GUID
	Value        any
	// Raw is the exact size-counted value payload. It aliases the decoded GVAS
	// buffer and remains available even when Value is an UndecodedValue.
	Raw []byte
	// Offset is the absolute byte offset of the property name in Save.Raw.
	Offset int
}

// PropertyMeta is the type-specific part of an Unreal property tag.
type PropertyMeta struct {
	StructType string
	StructGUID GUID
	BoolValue  bool
	EnumType   string
	InnerType  string
	KeyType    string
	ValueType  string
}

// UndecodedValue preserves a payload that is unknown or could not be interpreted.
type UndecodedValue struct {
	Data   []byte
	Reason string
}

// StructValue associates a concrete Unreal struct identity with its value.
// Value is usually Properties, GUID, Vector, Quat, LinearColor, Color, or int64.
type StructValue struct {
	Type  string
	Value any
}

type EnumValue struct {
	Type  string
	Value string
}

type Vector struct {
	X float64
	Y float64
	Z float64
}

type Quat struct {
	X float64
	Y float64
	Z float64
	W float64
}

type LinearColor struct {
	R float32
	G float32
	B float32
	A float32
}

type Color struct {
	B byte
	G byte
	R byte
	A byte
}

type IntPoint struct {
	X int32
	Y int32
}

type IntVector struct {
	X int32
	Y int32
	Z int32
}

type Vector2D struct {
	X float64
	Y float64
}

type Rotator struct {
	Pitch float64
	Yaw   float64
	Roll  float64
}

// ArrayValue represents an ArrayProperty. Primitive arrays use compact typed
// slices in Values. Struct arrays are exposed through Structs.
type ArrayValue struct {
	InnerType string
	Values    any
	Structs   *StructArray
}

type MapEntry struct {
	Key   any
	Value any
}

// MapValue is a lazy, ordered MapProperty. Use Iterator or Entries to decode.
type MapValue struct {
	KeyType         string
	ValueType       string
	KeyStructType   string
	ValueStructType string
	Removed         []any
	Count           uint32

	raw       []byte
	base      int
	path      string
	keyPath   string
	valuePath string
	cfg       *decodeConfig
}

// SetValue is a lazy, ordered SetProperty.
type SetValue struct {
	ElementType       string
	ElementStructType string
	Removed           []any
	Count             uint32

	raw  []byte
	base int
	path string
	cfg  *decodeConfig
}

// StructArray lazily exposes the concatenated values in an
// ArrayProperty<StructProperty>.
type StructArray struct {
	Count uint32
	Name  string
	Type  string
	// Size is the descriptor's aggregate byte size for every array element,
	// not the size of one element.
	Size         uint32
	ArrayIndex   uint32
	StructType   string
	StructGUID   GUID
	PropertyGUID *GUID

	raw  []byte
	base int
	path string
	cfg  *decodeConfig
}

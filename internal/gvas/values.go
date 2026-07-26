// Copyright (C) 2026 Luke Holland
// Portions copyright 2026 Palhelm contributors and licensed under Apache-2.0.
// Adapted and substantially modified on 2026-07-23. See NOTICE.
// SPDX-License-Identifier: GPL-3.0-or-later

package gvas

import "fmt"

func readArrayValue(reader *archiveReader, property *Property, path string) (any, error) {
	count, err := reader.u32()
	if err != nil {
		return nil, err
	}
	innerType := property.Meta.InnerType
	if innerType == "StructProperty" {
		descriptor, err := readPropertyTag(reader)
		if err != nil {
			return nil, fmt.Errorf("struct-array descriptor: %w", err)
		}
		if descriptor.Type != "StructProperty" {
			return nil, fmt.Errorf("struct-array descriptor type is %q", descriptor.Type)
		}
		if uint64(descriptor.Size) != uint64(reader.remaining()) {
			return nil, fmt.Errorf(
				"struct-array descriptor declares %d bytes, have %d",
				descriptor.Size,
				reader.remaining(),
			)
		}
		if err := validateCount(
			reader.state.cfg,
			"struct array",
			count,
			reader.remaining(),
			minimumStructSize(descriptor.Meta.StructType),
		); err != nil {
			return nil, err
		}
		rawBase := reader.offset()
		raw, err := reader.take(reader.remaining())
		if err != nil {
			return nil, err
		}
		elementPath, err := joinPath(reader.state.cfg, path, descriptor.Name)
		if err != nil {
			return nil, err
		}
		return ArrayValue{
			InnerType: innerType,
			Structs: &StructArray{
				Count:        count,
				Name:         descriptor.Name,
				Type:         descriptor.Type,
				Size:         descriptor.Size,
				ArrayIndex:   descriptor.ArrayIndex,
				StructType:   descriptor.Meta.StructType,
				StructGUID:   descriptor.Meta.StructGUID,
				PropertyGUID: descriptor.PropertyGUID,
				raw:          raw,
				base:         rawBase,
				path:         elementPath,
				cfg:          reader.state.cfg,
			},
		}, nil
	}
	if err := validateCount(
		reader.state.cfg,
		"array",
		count,
		reader.remaining(),
		minimumBareSize(innerType, ""),
	); err != nil {
		return nil, err
	}
	values, err := readTypedArray(reader, innerType, count, path)
	if err != nil {
		return nil, err
	}
	return ArrayValue{InnerType: innerType, Values: values}, nil
}

// readTypedArray decodes count primitive elements into a slice of the matching
// Go type. The element type is deliberately concrete rather than []any:
// consumers rely on []int16 staying []int16.
//
// count is safe to narrow to int because readArrayValue calls validateCount,
// which rejects any count above maxInt, before reaching here.
func readTypedArray(reader *archiveReader, valueType string, count uint32, path string) (any, error) {
	elements := int(count)
	switch valueType {
	case "ByteProperty", "UInt8Property":
		return reader.take(elements)
	case "Int8Property":
		return readSlice(reader, elements, (*archiveReader).i8)
	case "BoolProperty":
		return readSlice(reader, elements, func(r *archiveReader) (bool, error) {
			value, err := r.u8()
			return value != 0, err
		})
	case "Int16Property":
		return readSlice(reader, elements, (*archiveReader).i16)
	case "UInt16Property":
		return readSlice(reader, elements, (*archiveReader).u16)
	case "IntProperty", "FixedPoint64Property":
		return readSlice(reader, elements, (*archiveReader).i32)
	case "UInt32Property":
		return readSlice(reader, elements, (*archiveReader).u32)
	case "Int64Property":
		return readSlice(reader, elements, (*archiveReader).i64)
	case "UInt64Property":
		return readSlice(reader, elements, (*archiveReader).u64)
	case "FloatProperty":
		return readSlice(reader, elements, (*archiveReader).f32)
	case "DoubleProperty":
		return readSlice(reader, elements, (*archiveReader).f64)
	case "EnumProperty", "NameProperty", "StrProperty", "ObjectProperty":
		return readSlice(reader, elements, (*archiveReader).fstring)
	default:
		if valueType != "Guid" {
			return nil, fmt.Errorf("unsupported array element type %q", valueType)
		}
		return readSlice(reader, elements, func(r *archiveReader) (any, error) {
			return readBareValue(r, valueType, "", path)
		})
	}
}

func readMapValue(reader *archiveReader, property *Property, path string) (any, error) {
	keyPath, err := joinPath(reader.state.cfg, path, "Key")
	if err != nil {
		return nil, err
	}
	valuePath, err := joinPath(reader.state.cfg, path, "Value")
	if err != nil {
		return nil, err
	}
	keyHint := structHint(reader.state.cfg, keyPath, property.Meta.KeyType)
	valueHint := structHint(reader.state.cfg, valuePath, property.Meta.ValueType)

	removedCount, err := reader.u32()
	if err != nil {
		return nil, err
	}
	if err := validateCount(
		reader.state.cfg,
		"map removed-key",
		removedCount,
		reader.remaining(),
		minimumBareSize(property.Meta.KeyType, keyHint),
	); err != nil {
		return nil, err
	}
	removed := make([]any, removedCount)
	for i := range removed {
		removed[i], err = readBareValue(reader, property.Meta.KeyType, keyHint, keyPath)
		if err != nil {
			return nil, fmt.Errorf("removed map key %d: %w", i, err)
		}
	}

	count, err := reader.u32()
	if err != nil {
		return nil, err
	}
	minimum := minimumBareSize(property.Meta.KeyType, keyHint) +
		minimumBareSize(property.Meta.ValueType, valueHint)
	if err := validateCount(reader.state.cfg, "map", count, reader.remaining(), minimum); err != nil {
		return nil, err
	}
	rawBase := reader.offset()
	raw, err := reader.take(reader.remaining())
	if err != nil {
		return nil, err
	}
	return &MapValue{
		KeyType:         property.Meta.KeyType,
		ValueType:       property.Meta.ValueType,
		KeyStructType:   keyHint,
		ValueStructType: valueHint,
		Removed:         removed,
		Count:           count,
		raw:             raw,
		base:            rawBase,
		path:            path,
		keyPath:         keyPath,
		valuePath:       valuePath,
		cfg:             reader.state.cfg,
	}, nil
}

func readSetValue(reader *archiveReader, property *Property, path string) (any, error) {
	elementPath, err := joinPath(reader.state.cfg, path, property.Meta.InnerType)
	if err != nil {
		return nil, err
	}
	elementHint := structHint(reader.state.cfg, elementPath, property.Meta.InnerType)

	removedCount, err := reader.u32()
	if err != nil {
		return nil, err
	}
	if err := validateCount(
		reader.state.cfg,
		"set removed-element",
		removedCount,
		reader.remaining(),
		minimumBareSize(property.Meta.InnerType, elementHint),
	); err != nil {
		return nil, err
	}
	removed := make([]any, removedCount)
	for i := range removed {
		removed[i], err = readBareValue(reader, property.Meta.InnerType, elementHint, elementPath)
		if err != nil {
			return nil, fmt.Errorf("removed set element %d: %w", i, err)
		}
	}

	count, err := reader.u32()
	if err != nil {
		return nil, err
	}
	if err := validateCount(
		reader.state.cfg,
		"set",
		count,
		reader.remaining(),
		minimumBareSize(property.Meta.InnerType, elementHint),
	); err != nil {
		return nil, err
	}
	rawBase := reader.offset()
	raw, err := reader.take(reader.remaining())
	if err != nil {
		return nil, err
	}
	return &SetValue{
		ElementType:       property.Meta.InnerType,
		ElementStructType: elementHint,
		Removed:           removed,
		Count:             count,
		raw:               raw,
		base:              rawBase,
		path:              elementPath,
		cfg:               reader.state.cfg,
	}, nil
}

func readBareValue(reader *archiveReader, valueType, structType, path string) (any, error) {
	switch valueType {
	case "StructProperty":
		if structType == "" {
			structType = "StructProperty"
		}
		if structType == "StructProperty" {
			return readPropertyList(reader, path)
		}
		return readStructBody(reader, structType, path)
	case "Guid":
		return reader.guid()
	case "BoolProperty":
		value, err := reader.u8()
		return value != 0, err
	case "ByteProperty", "UInt8Property":
		return reader.u8()
	case "Int8Property":
		return reader.i8()
	case "Int16Property":
		return reader.i16()
	case "UInt16Property":
		return reader.u16()
	case "IntProperty", "FixedPoint64Property":
		return reader.i32()
	case "UInt32Property":
		return reader.u32()
	case "Int64Property":
		return reader.i64()
	case "UInt64Property":
		return reader.u64()
	case "FloatProperty":
		return reader.f32()
	case "DoubleProperty":
		return reader.f64()
	case "EnumProperty", "NameProperty", "StrProperty", "ObjectProperty":
		return reader.fstring()
	default:
		return nil, fmt.Errorf("unsupported bare value type %q at %s", valueType, displayPath(path))
	}
}

func structHint(cfg *decodeConfig, path, valueType string) string {
	if valueType != "StructProperty" {
		return ""
	}
	if hint := cfg.TypeHints[path]; hint != "" {
		return hint
	}
	return "StructProperty"
}

func minimumBareSize(valueType, structType string) int {
	switch valueType {
	case "BoolProperty", "ByteProperty", "Int8Property", "UInt8Property":
		return 1
	case "Int16Property", "UInt16Property":
		return 2
	case "IntProperty", "UInt32Property", "FloatProperty", "FixedPoint64Property":
		return 4
	case "Int64Property", "UInt64Property", "DoubleProperty":
		return 8
	case "Guid":
		return 16
	case "StructProperty":
		return minimumStructSize(structType)
	case "EnumProperty", "NameProperty", "StrProperty", "ObjectProperty":
		return 4
	default:
		return 1
	}
}

// Copyright (C) 2026 Luke Holland
// Portions copyright 2026 Palhelm contributors and licensed under Apache-2.0.
// Adapted and substantially modified on 2026-07-23. See NOTICE.
// SPDX-License-Identifier: GPL-3.0-or-later

package gvas

import (
	"errors"
	"fmt"
)

func readPropertyList(reader *archiveReader, path string) (Properties, error) {
	if err := reader.state.enter("property"); err != nil {
		return nil, err
	}
	defer reader.state.leave()

	var properties Properties
	for {
		offset := reader.offset()
		name, err := reader.fstring()
		if err != nil {
			return nil, fmt.Errorf("gvas: property name at %s: %w", displayPath(path), err)
		}
		if name == "None" {
			return properties, nil
		}
		propertyPath, err := joinPath(reader.state.cfg, path, name)
		if err != nil {
			return nil, err
		}
		if reader.state.properties >= reader.state.cfg.MaxProperties {
			return nil, &LimitError{
				Kind:  "property count",
				Value: reader.state.properties + 1,
				Limit: reader.state.cfg.MaxProperties,
			}
		}
		reader.state.properties++

		property, err := readPropertyTagAfterName(reader, name, offset)
		if err != nil {
			return nil, fmt.Errorf("gvas: property %s: %w", propertyPath, err)
		}
		if uint64(property.Size) > uint64(reader.remaining()) {
			return nil, fmt.Errorf(
				"gvas: property %s declares %d bytes with %d remaining",
				propertyPath,
				property.Size,
				reader.remaining(),
			)
		}
		payloadOffset := reader.offset()
		payload, err := reader.take(int(property.Size))
		if err != nil {
			return nil, err
		}
		property.Raw = payload

		// A known tag is still useful when a new Palworld value encoding is
		// encountered. Decode through a bounded sub-reader, and preserve the
		// exact payload if semantic decoding fails.
		propertyCount := reader.state.properties
		valueReader := reader.sub(payload, payloadOffset)
		value, decodeErr := readPropertyPayload(valueReader, &property, propertyPath)
		if decodeErr == nil && valueReader.remaining() != 0 {
			decodeErr = fmt.Errorf("%d unconsumed payload bytes", valueReader.remaining())
		}
		if decodeErr != nil {
			var limitErr *LimitError
			if errors.As(decodeErr, &limitErr) {
				return nil, decodeErr
			}
			reader.state.properties = propertyCount
			property.Value = UndecodedValue{Data: payload, Reason: decodeErr.Error()}
		} else {
			property.Value = value
		}
		properties = append(properties, property)
	}
}

func readPropertyTag(reader *archiveReader) (Property, error) {
	offset := reader.offset()
	name, err := reader.fstring()
	if err != nil {
		return Property{}, err
	}
	if name == "None" {
		return Property{}, fmt.Errorf("unexpected None property tag")
	}
	return readPropertyTagAfterName(reader, name, offset)
}

func readPropertyTagAfterName(reader *archiveReader, name string, offset int) (Property, error) {
	property := Property{Name: name, Offset: offset}
	var err error
	if property.Type, err = reader.fstring(); err != nil {
		return Property{}, fmt.Errorf("type: %w", err)
	}
	if property.Size, err = reader.u32(); err != nil {
		return Property{}, fmt.Errorf("size: %w", err)
	}
	if property.ArrayIndex, err = reader.u32(); err != nil {
		return Property{}, fmt.Errorf("array index: %w", err)
	}

	switch property.Type {
	case "StructProperty":
		if property.Meta.StructType, err = reader.fstring(); err != nil {
			return Property{}, fmt.Errorf("struct type: %w", err)
		}
		if property.Meta.StructGUID, err = reader.guid(); err != nil {
			return Property{}, fmt.Errorf("struct GUID: %w", err)
		}
	case "BoolProperty":
		value, readErr := reader.u8()
		if readErr != nil {
			return Property{}, fmt.Errorf("bool tag value: %w", readErr)
		}
		property.Meta.BoolValue = value != 0
	case "ByteProperty", "EnumProperty":
		if property.Meta.EnumType, err = reader.fstring(); err != nil {
			return Property{}, fmt.Errorf("enum type: %w", err)
		}
	case "ArrayProperty", "SetProperty":
		if property.Meta.InnerType, err = reader.fstring(); err != nil {
			return Property{}, fmt.Errorf("inner type: %w", err)
		}
	case "MapProperty":
		if property.Meta.KeyType, err = reader.fstring(); err != nil {
			return Property{}, fmt.Errorf("key type: %w", err)
		}
		if property.Meta.ValueType, err = reader.fstring(); err != nil {
			return Property{}, fmt.Errorf("value type: %w", err)
		}
	default:
		if !supportedPlainPropertyType(property.Type) {
			return Property{}, fmt.Errorf("unsupported property tag type %q", property.Type)
		}
	}
	if property.PropertyGUID, err = reader.optionalGUID(); err != nil {
		return Property{}, fmt.Errorf("property GUID: %w", err)
	}
	return property, nil
}

func supportedPlainPropertyType(propertyType string) bool {
	switch propertyType {
	case "Int8Property", "Int16Property", "IntProperty", "Int64Property",
		"UInt8Property", "UInt16Property", "UInt32Property", "UInt64Property",
		"FixedPoint64Property", "FloatProperty", "DoubleProperty",
		"NameProperty", "StrProperty", "TextProperty", "ObjectProperty",
		"SoftObjectProperty", "WeakObjectProperty", "LazyObjectProperty",
		"InterfaceProperty", "DelegateProperty", "MulticastDelegateProperty":
		return true
	default:
		return false
	}
}

func readPropertyPayload(reader *archiveReader, property *Property, path string) (any, error) {
	switch property.Type {
	case "BoolProperty":
		if property.Size != 0 {
			return nil, fmt.Errorf("BoolProperty has nonzero size %d", property.Size)
		}
		return property.Meta.BoolValue, nil
	case "ByteProperty":
		if property.Meta.EnumType == "" || property.Meta.EnumType == "None" {
			return reader.u8()
		}
		value, err := reader.fstring()
		return EnumValue{Type: property.Meta.EnumType, Value: value}, err
	case "EnumProperty":
		value, err := reader.fstring()
		return EnumValue{Type: property.Meta.EnumType, Value: value}, err
	case "Int8Property":
		return reader.i8()
	case "Int16Property":
		return reader.i16()
	case "IntProperty", "FixedPoint64Property":
		return reader.i32()
	case "Int64Property":
		return reader.i64()
	case "UInt8Property":
		return reader.u8()
	case "UInt16Property":
		return reader.u16()
	case "UInt32Property":
		return reader.u32()
	case "UInt64Property":
		return reader.u64()
	case "FloatProperty":
		return reader.f32()
	case "DoubleProperty":
		return reader.f64()
	case "NameProperty", "StrProperty", "ObjectProperty":
		return reader.fstring()
	case "StructProperty":
		value, err := readStructBody(reader, property.Meta.StructType, path)
		return StructValue{Type: property.Meta.StructType, Value: value}, err
	case "ArrayProperty":
		return readArrayValue(reader, property, path)
	case "MapProperty":
		return readMapValue(reader, property, path)
	case "SetProperty":
		return readSetValue(reader, property, path)
	default:
		return nil, fmt.Errorf("unsupported property type %q", property.Type)
	}
}

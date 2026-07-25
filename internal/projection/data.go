// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"encoding"
	"encoding/base64"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/LukeHollandDev/palworld-save-reader/internal/palsav"
)

const (
	maxDecodedNodes = 10_000_000
	maxDecodedDepth = 128
)

type dataNode struct {
	kind   Kind
	fields []dataField
	items  []*dataNode
	value  any
	path   string
}

type dataField struct {
	name string
	node *dataNode
}

type normalizer struct {
	nodes int
}

func normalizeProperties(properties palsav.Properties) (*dataNode, error) {
	return (&normalizer{}).value(properties, "$", 0)
}

func (normalizer *normalizer) value(value any, path string, depth int) (*dataNode, error) {
	if depth > maxDecodedDepth {
		return nil, fmt.Errorf("projection: decoded tree exceeds %d levels at %s", maxDecodedDepth, path)
	}
	if err := normalizer.consumeNode(); err != nil {
		return nil, err
	}

	switch typed := value.(type) {
	case nil:
		return &dataNode{kind: KindNull, path: path}, nil
	case palsav.Properties:
		node := &dataNode{kind: KindObject, path: path}
		for index := range typed {
			property := &typed[index]
			childPath := propertyPath(path, property.Name)
			child, err := normalizer.value(property.Value, childPath, depth+1)
			if err != nil {
				return nil, err
			}
			node.fields = append(node.fields, dataField{name: property.Name, node: child})
		}
		return node, nil
	case palsav.StructValue:
		return normalizer.value(typed.Value, path, depth)
	case palsav.EnumValue:
		return &dataNode{kind: KindString, value: typed.Value, path: path}, nil
	case palsav.RawValue:
		if err := normalizer.consumeNodes(3); err != nil {
			return nil, err
		}
		return &dataNode{
			kind: KindObject,
			fields: []dataField{
				{
					name: "raw",
					node: &dataNode{
						kind:  KindString,
						value: base64.StdEncoding.EncodeToString(typed.Data),
						path:  propertyPath(path, "raw"),
					},
				},
				{
					name: "bytes",
					node: &dataNode{
						kind:  KindNumber,
						value: len(typed.Data),
						path:  propertyPath(path, "bytes"),
					},
				},
				{
					name: "reason",
					node: &dataNode{
						kind:  KindString,
						value: typed.Reason,
						path:  propertyPath(path, "reason"),
					},
				},
			},
			path: path,
		}, nil
	case *palsav.MapValue:
		if typed == nil {
			return &dataNode{kind: KindNull, path: path}, nil
		}
		node := &dataNode{kind: KindArray, path: path}
		iterator := typed.Iterator()
		index := 0
		for iterator.Next() {
			entry := iterator.Entry()
			entryPath := indexedPath(path, index)
			if err := normalizer.consumeNode(); err != nil {
				return nil, err
			}
			key, err := normalizer.value(entry.Key, propertyPath(entryPath, "Key"), depth+2)
			if err != nil {
				return nil, err
			}
			mapValue, err := normalizer.value(entry.Value, propertyPath(entryPath, "Value"), depth+2)
			if err != nil {
				return nil, err
			}
			node.items = append(node.items, &dataNode{
				kind: KindObject,
				fields: []dataField{
					{name: "Key", node: key},
					{name: "Value", node: mapValue},
				},
				path: entryPath,
			})
			index++
		}
		if err := iterator.Err(); err != nil {
			return nil, err
		}
		return node, nil
	case *palsav.SetValue:
		if typed == nil {
			return &dataNode{kind: KindNull, path: path}, nil
		}
		node := &dataNode{kind: KindArray, path: path}
		iterator := typed.Iterator()
		index := 0
		for iterator.Next() {
			child, err := normalizer.value(iterator.Value(), indexedPath(path, index), depth+1)
			if err != nil {
				return nil, err
			}
			node.items = append(node.items, child)
			index++
		}
		if err := iterator.Err(); err != nil {
			return nil, err
		}
		return node, nil
	case palsav.ArrayValue:
		if typed.Structs != nil {
			return normalizer.value(typed.Structs, path, depth)
		}
		return normalizer.reflectValue(reflect.ValueOf(typed.Values), path, depth)
	case *palsav.StructArray:
		if typed == nil {
			return &dataNode{kind: KindNull, path: path}, nil
		}
		node := &dataNode{kind: KindArray, path: path}
		iterator := typed.Iterator()
		index := 0
		for iterator.Next() {
			child, err := normalizer.value(iterator.Value(), indexedPath(path, index), depth+1)
			if err != nil {
				return nil, err
			}
			node.items = append(node.items, child)
			index++
		}
		if err := iterator.Err(); err != nil {
			return nil, err
		}
		return node, nil
	case palsav.GUID:
		return &dataNode{kind: KindString, value: typed.String(), path: path}, nil
	case bool:
		return &dataNode{kind: KindBoolean, value: typed, path: path}, nil
	case string:
		return &dataNode{kind: KindString, value: typed, path: path}, nil
	}

	return normalizer.reflectValue(reflect.ValueOf(value), path, depth)
}

func (normalizer *normalizer) consumeNode() error {
	return normalizer.consumeNodes(1)
}

func (normalizer *normalizer) consumeNodes(count int) error {
	if count > maxDecodedNodes-normalizer.nodes {
		return fmt.Errorf("projection: decoded tree exceeds %d values", maxDecodedNodes)
	}
	normalizer.nodes += count
	return nil
}

func (normalizer *normalizer) reflectValue(value reflect.Value, path string, depth int) (*dataNode, error) {
	if !value.IsValid() {
		return &dataNode{kind: KindNull, path: path}, nil
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return &dataNode{kind: KindNull, path: path}, nil
		}
		return normalizer.value(value.Elem().Interface(), path, depth)
	}
	switch value.Kind() {
	case reflect.Bool:
		return &dataNode{kind: KindBoolean, value: value.Bool(), path: path}, nil
	case reflect.String:
		return &dataNode{kind: KindString, value: value.String(), path: path}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &dataNode{kind: KindNumber, value: value.Int(), path: path}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &dataNode{kind: KindNumber, value: value.Uint(), path: path}, nil
	case reflect.Float32, reflect.Float64:
		return &dataNode{kind: KindNumber, value: value.Interface(), path: path}, nil
	case reflect.Slice, reflect.Array:
		node := &dataNode{kind: KindArray, path: path}
		for index := 0; index < value.Len(); index++ {
			child, err := normalizer.value(value.Index(index).Interface(), indexedPath(path, index), depth+1)
			if err != nil {
				return nil, err
			}
			node.items = append(node.items, child)
		}
		return node, nil
	case reflect.Struct:
		if value.CanInterface() {
			if marshaler, ok := value.Interface().(encoding.TextMarshaler); ok {
				text, err := marshaler.MarshalText()
				if err != nil {
					return nil, err
				}
				return &dataNode{kind: KindString, value: string(text), path: path}, nil
			}
		}
		node := &dataNode{kind: KindObject, path: path}
		valueType := value.Type()
		for index := 0; index < value.NumField(); index++ {
			fieldType := valueType.Field(index)
			if !fieldType.IsExported() {
				continue
			}
			name := jsonFieldName(fieldType)
			if name == "" {
				continue
			}
			child, err := normalizer.value(value.Field(index).Interface(), propertyPath(path, name), depth+1)
			if err != nil {
				return nil, err
			}
			node.fields = append(node.fields, dataField{name: name, node: child})
		}
		return node, nil
	default:
		return &dataNode{kind: kindUnsupported, value: fmt.Sprint(value.Interface()), path: path}, nil
	}
}

func jsonFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	if tag != "" {
		if name, _, found := strings.Cut(tag, ","); found && name != "" {
			return name
		} else if !found && tag != "" {
			return tag
		}
	}
	return field.Name
}

func propertyPath(parent, name string) string {
	if parent == "$" {
		return "$." + name
	}
	return parent + "." + name
}

func indexedPath(parent string, index int) string {
	return parent + "[" + strconv.Itoa(index) + "]"
}

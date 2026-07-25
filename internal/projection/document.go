// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const (
	FormatVersion    = 1
	maxDocumentBytes = 1 << 20
	maxShapeDepth    = 64
	maxShapeNodes    = 100_000
)

var presetNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// InvalidDocumentError reports a projection contract violation.
type InvalidDocumentError struct {
	Path    string
	Message string
}

func (err *InvalidDocumentError) Error() string {
	if err.Path == "" {
		return "invalid projection: " + err.Message
	}
	return fmt.Sprintf("invalid projection at %s: %s", err.Path, err.Message)
}

// IsInvalidDocument reports whether err is a projection-document validation
// error rather than a save decoding or matching error.
func IsInvalidDocument(err error) bool {
	var target *InvalidDocumentError
	return errors.As(err, &target)
}

// Document is one validated projection document.
type Document struct {
	Schema            string
	ProjectionVersion int
	Name              string
	GameVersion       string
	SaveType          string
	Shape             *Shape
}

// Kind identifies one shape or decoded-data node.
type Kind uint8

const (
	KindNull Kind = iota
	KindObject
	KindArray
	KindString
	KindNumber
	KindBoolean
	kindUnsupported
)

func (kind Kind) String() string {
	switch kind {
	case KindNull:
		return "any"
	case KindObject:
		return "object"
	case KindArray:
		return "array"
	case KindString:
		return "string"
	case KindNumber:
		return "number"
	case KindBoolean:
		return "boolean"
	default:
		return "unsupported"
	}
}

// Field preserves a requested object's serialized key order.
type Field struct {
	Name  string
	Shape *Shape
}

// Shape is the ordered, validated representation of a requested JSON shape.
type Shape struct {
	Kind    Kind
	Fields  []Field
	Element *Shape
}

type syntaxValue struct {
	kind   Kind
	fields []syntaxField
	items  []*syntaxValue
	value  any
}

type syntaxField struct {
	name  string
	value *syntaxValue
}

// Parse validates and parses a projection document.
func Parse(data []byte) (*Document, error) {
	if len(data) > maxDocumentBytes {
		return nil, invalid("", "document exceeds %d bytes", maxDocumentBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	root, err := readSyntaxValue(decoder, "$", 0, &nodes)
	if err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, invalid("$", "unexpected trailing JSON token %v", token)
		}
		return nil, invalid("$", "trailing JSON: %v", err)
	}
	if root.kind != KindObject {
		return nil, invalid("$", "document must be an object")
	}

	document := &Document{}
	var shapeValue *syntaxValue
	seen := make(map[string]struct{}, len(root.fields))
	for _, field := range root.fields {
		if _, exists := seen[field.name]; exists {
			return nil, invalid("$."+field.name, "duplicate key")
		}
		seen[field.name] = struct{}{}
		switch field.name {
		case "$schema":
			document.Schema, err = requireString(field.value, "$.$schema")
		case "projectionVersion":
			document.ProjectionVersion, err = requireInteger(field.value, "$.projectionVersion")
		case "name":
			document.Name, err = requireString(field.value, "$.name")
		case "gameVersion":
			document.GameVersion, err = requireString(field.value, "$.gameVersion")
		case "saveType":
			document.SaveType, err = requireString(field.value, "$.saveType")
		case "shape":
			shapeValue = field.value
		default:
			return nil, invalid("$."+field.name, "unknown metadata field")
		}
		if err != nil {
			return nil, err
		}
	}

	if document.ProjectionVersion == 0 {
		return nil, invalid("$.projectionVersion", "field is required")
	}
	if document.ProjectionVersion != FormatVersion {
		return nil, invalid("$.projectionVersion", "unsupported version %d (want %d)", document.ProjectionVersion, FormatVersion)
	}
	if document.Name == "" {
		return nil, invalid("$.name", "field is required and must not be empty")
	}
	if !presetNamePattern.MatchString(document.Name) {
		return nil, invalid("$.name", "must contain lowercase letters, digits, and single hyphens only")
	}
	if document.GameVersion == "" {
		return nil, invalid("$.gameVersion", "field is required and must not be empty")
	}
	if document.SaveType == "" {
		return nil, invalid("$.saveType", "field is required and must not be empty")
	}
	if shapeValue == nil {
		return nil, invalid("$.shape", "field is required")
	}
	document.Shape, err = buildShape(shapeValue, "$.shape")
	if err != nil {
		return nil, err
	}
	if document.Shape.Kind != KindObject && document.Shape.Kind != KindArray {
		return nil, invalid("$.shape", "top-level shape must be an object or array")
	}
	return document, nil
}

// ParseReader reads and parses a projection document without reading more than
// the document-size limit into memory.
func ParseReader(reader io.Reader) (*Document, error) {
	if reader == nil {
		return nil, invalid("$", "nil document reader")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxDocumentBytes+1))
	if err != nil {
		return nil, invalid("$", "read document: %v", err)
	}
	return Parse(data)
}

func readSyntaxValue(decoder *json.Decoder, path string, depth int, nodes *int) (*syntaxValue, error) {
	if depth > maxShapeDepth {
		return nil, invalid(path, "nesting exceeds %d levels", maxShapeDepth)
	}
	*nodes++
	if *nodes > maxShapeNodes {
		return nil, invalid(path, "document exceeds %d values", maxShapeNodes)
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, invalid(path, "invalid JSON: %v", err)
	}
	switch value := token.(type) {
	case nil:
		return &syntaxValue{kind: KindNull}, nil
	case bool:
		return &syntaxValue{kind: KindBoolean, value: value}, nil
	case string:
		return &syntaxValue{kind: KindString, value: value}, nil
	case json.Number:
		return &syntaxValue{kind: KindNumber, value: value}, nil
	case json.Delim:
		switch value {
		case '{':
			object := &syntaxValue{kind: KindObject}
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, keyErr := decoder.Token()
				if keyErr != nil {
					return nil, invalid(path, "object key: %v", keyErr)
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, invalid(path, "object key is not a string")
				}
				childPath := path + "." + key
				if _, exists := seen[key]; exists {
					return nil, invalid(childPath, "duplicate key")
				}
				seen[key] = struct{}{}
				child, childErr := readSyntaxValue(decoder, childPath, depth+1, nodes)
				if childErr != nil {
					return nil, childErr
				}
				object.fields = append(object.fields, syntaxField{name: key, value: child})
			}
			if _, err := decoder.Token(); err != nil {
				return nil, invalid(path, "object terminator: %v", err)
			}
			return object, nil
		case '[':
			array := &syntaxValue{kind: KindArray}
			for index := 0; decoder.More(); index++ {
				child, childErr := readSyntaxValue(decoder, fmt.Sprintf("%s[%d]", path, index), depth+1, nodes)
				if childErr != nil {
					return nil, childErr
				}
				array.items = append(array.items, child)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, invalid(path, "array terminator: %v", err)
			}
			return array, nil
		default:
			return nil, invalid(path, "unexpected delimiter %q", value)
		}
	default:
		return nil, invalid(path, "unsupported JSON token %T", token)
	}
}

func buildShape(value *syntaxValue, path string) (*Shape, error) {
	shape := &Shape{Kind: value.kind}
	switch value.kind {
	case KindNull:
		return shape, nil
	case KindString:
		if value.value.(string) != "" {
			return nil, invalid(path, `string placeholder must be ""`)
		}
		return shape, nil
	case KindNumber:
		if value.value.(json.Number).String() != "0" {
			return nil, invalid(path, "number placeholder must be 0")
		}
		return shape, nil
	case KindBoolean:
		if value.value.(bool) {
			return nil, invalid(path, "boolean placeholder must be false")
		}
		return shape, nil
	case KindObject:
		if len(value.fields) == 0 {
			return nil, invalid(path, "object must request at least one field")
		}
		for _, field := range value.fields {
			child, err := buildShape(field.value, path+"."+field.name)
			if err != nil {
				return nil, err
			}
			shape.Fields = append(shape.Fields, Field{Name: field.name, Shape: child})
		}
		return shape, nil
	case KindArray:
		if len(value.items) != 1 {
			return nil, invalid(path, "array must contain exactly one element shape")
		}
		element, err := buildShape(value.items[0], path+"[0]")
		if err != nil {
			return nil, err
		}
		shape.Element = element
		return shape, nil
	default:
		return nil, invalid(path, "unsupported shape value")
	}
}

func requireString(value *syntaxValue, path string) (string, error) {
	if value.kind != KindString {
		return "", invalid(path, "must be a string")
	}
	return value.value.(string), nil
}

func requireInteger(value *syntaxValue, path string) (int, error) {
	if value.kind != KindNumber {
		return 0, invalid(path, "must be an integer")
	}
	number := value.value.(json.Number)
	integer, err := number.Int64()
	if err != nil {
		return 0, invalid(path, "must be an integer")
	}
	return int(integer), nil
}

func invalid(path, format string, values ...any) error {
	return &InvalidDocumentError{Path: path, Message: fmt.Sprintf(format, values...)}
}

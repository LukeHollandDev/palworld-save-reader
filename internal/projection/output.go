// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"bytes"
	"encoding/json"
)

// Value is a projected JSON value. Its custom marshaler preserves projection
// object key order.
type Value struct {
	kind   Kind
	fields []valueField
	items  []*Value
	scalar any
}

type valueField struct {
	name  string
	value *Value
}

func nullValue() *Value {
	return &Value{kind: KindNull}
}

// MarshalJSON preserves the ordering in the projection shape.
func (value *Value) MarshalJSON() ([]byte, error) {
	if value == nil || value.kind == KindNull {
		return []byte("null"), nil
	}
	switch value.kind {
	case KindObject:
		var output bytes.Buffer
		output.WriteByte('{')
		for index, field := range value.fields {
			if index != 0 {
				output.WriteByte(',')
			}
			name, err := json.Marshal(field.name)
			if err != nil {
				return nil, err
			}
			child, err := field.value.MarshalJSON()
			if err != nil {
				return nil, err
			}
			output.Write(name)
			output.WriteByte(':')
			output.Write(child)
		}
		output.WriteByte('}')
		return output.Bytes(), nil
	case KindArray:
		var output bytes.Buffer
		output.WriteByte('[')
		for index, item := range value.items {
			if index != 0 {
				output.WriteByte(',')
			}
			child, err := item.MarshalJSON()
			if err != nil {
				return nil, err
			}
			output.Write(child)
		}
		output.WriteByte(']')
		return output.Bytes(), nil
	default:
		return json.Marshal(value.scalar)
	}
}

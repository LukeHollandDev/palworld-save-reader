// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"bytes"
	"encoding/json"
)

// Output is the third of the package's three parallel trees, all keyed by the
// same Kind: documentNode is what the caller asked for, sourceNode is what the
// save contains, and Output is the result of matching one against the other.
//
// Its custom marshaler preserves projection object key order.
type Output struct {
	kind   Kind
	fields []outputField
	items  []*Output
	scalar any
}

type outputField struct {
	name  string
	value *Output
}

func nullOutput() *Output {
	return &Output{kind: KindNull}
}

// MarshalJSON preserves the ordering in the projection shape.
func (value *Output) MarshalJSON() ([]byte, error) {
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

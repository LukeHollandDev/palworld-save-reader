// Copyright (C) 2026 Luke Holland
// Portions copyright 2026 Palhelm contributors and licensed under Apache-2.0.
// Adapted and substantially modified on 2026-07-23. See NOTICE.
// SPDX-License-Identifier: GPL-3.0-or-later

package palsav

func readStructBody(reader *archiveReader, structType, path string) (any, error) {
	switch structType {
	case "Guid":
		return reader.guid()
	case "DateTime", "Timespan":
		return reader.i64()
	case "Vector":
		v, err := reader.f64s(3)
		if err != nil {
			return nil, err
		}
		return Vector{X: v[0], Y: v[1], Z: v[2]}, nil
	case "Vector2D":
		v, err := reader.f64s(2)
		if err != nil {
			return nil, err
		}
		return Vector2D{X: v[0], Y: v[1]}, nil
	case "Quat":
		v, err := reader.f64s(4)
		if err != nil {
			return nil, err
		}
		return Quat{X: v[0], Y: v[1], Z: v[2], W: v[3]}, nil
	case "Rotator":
		v, err := reader.f64s(3)
		if err != nil {
			return nil, err
		}
		return Rotator{Pitch: v[0], Yaw: v[1], Roll: v[2]}, nil
	case "LinearColor":
		v, err := reader.f32s(4)
		if err != nil {
			return nil, err
		}
		return LinearColor{R: v[0], G: v[1], B: v[2], A: v[3]}, nil
	case "Color":
		// Unreal serializes FColor in BGRA order.
		v, err := reader.take(4)
		if err != nil {
			return nil, err
		}
		return Color{B: v[0], G: v[1], R: v[2], A: v[3]}, nil
	case "IntPoint":
		v, err := reader.i32s(2)
		if err != nil {
			return nil, err
		}
		return IntPoint{X: v[0], Y: v[1]}, nil
	case "IntVector":
		v, err := reader.i32s(3)
		if err != nil {
			return nil, err
		}
		return IntVector{X: v[0], Y: v[1], Z: v[2]}, nil
	default:
		return readPropertyList(reader, path)
	}
}

func minimumStructSize(structType string) int {
	switch structType {
	case "Guid":
		return 16
	case "DateTime", "Timespan":
		return 8
	case "Vector", "Rotator":
		return 24
	case "Vector2D":
		return 16
	case "Quat":
		return 32
	case "LinearColor":
		return 16
	case "Color":
		return 4
	case "IntPoint":
		return 8
	case "IntVector":
		return 12
	default:
		// The FString "None" terminator of an empty tagged property list.
		return 9
	}
}

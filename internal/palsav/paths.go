// Copyright (C) 2026 Luke Holland
// Portions copyright 2026 Palhelm contributors and licensed under Apache-2.0.
// Adapted and substantially modified on 2026-07-23. See NOTICE.
// SPDX-License-Identifier: GPL-3.0-or-later

package palsav

func joinPath(cfg *decodeConfig, parent, child string) (string, error) {
	size := uint64(len(parent)) + 1 + uint64(len(child))
	if size > uint64(cfg.MaxPathBytes) {
		return "", &LimitError{
			Kind:  "property path bytes",
			Value: size,
			Limit: uint64(cfg.MaxPathBytes),
		}
	}
	if parent == "" {
		return "." + child, nil
	}
	return parent + "." + child, nil
}

func displayPath(path string) string {
	if path == "" {
		return "<root>"
	}
	return path
}

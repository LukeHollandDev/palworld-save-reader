// Command savedecode is a thin CLI over the palworld-save-reader packages.
//
// Two modes:
//
//	savedecode <snapshot-dir>          # compact roster JSON (what the live map uses)
//	savedecode --full <file.sav>       # fully-expanded decode of ONE .sav (everything available)
//
// The roster mode is the stable machine contract a host application consumes.
// The full mode is a human-facing inspection tool: it drives palsav's lazy
// map/set/struct-array iterators so every property is materialised, letting you
// see the complete schema a save exposes — not only the fields the map projects.
//
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/LukeHollandDev/palworld-save-reader/palsav"
	"github.com/LukeHollandDev/palworld-save-reader/savegame"
)

func main() {
	full := flag.Bool("full", false, "fully expand ONE .sav file to JSON (everything available)")
	timeout := flag.Duration("timeout", 30*time.Second, "decode timeout (roster mode)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage:\n  savedecode <snapshot-dir>        roster JSON\n  savedecode --full <file.sav>     full expanded decode")
		os.Exit(2)
	}
	var err error
	if *full {
		err = dumpFull(flag.Arg(0))
	} else {
		err = dumpRoster(flag.Arg(0), *timeout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// dumpRoster emits the compact, projected roster the live map consumes.
func dumpRoster(dir string, timeout time.Duration) error {
	reader, err := savegame.NewReader(savegame.Options{})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	snap, err := reader.ReadSnapshot(ctx, dir)
	if err != nil {
		return err
	}
	return write(snap)
}

// dumpFull decodes one .sav and emits the entire property tree with every lazy
// collection expanded, so the reader can see all available fields.
func dumpFull(path string) error {
	save, err := palsav.Load(path)
	if err != nil {
		return err
	}
	out := map[string]any{
		"container":  save.Container,
		"header":     save.Header,
		"properties": expandProperties(save.Properties),
	}
	return write(out)
}

func write(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// expandProperties turns palsav's ordered property slice into a readable list,
// dropping the Raw/Offset bookkeeping fields and recursively materialising
// values via expand.
func expandProperties(props palsav.Properties) []any {
	out := make([]any, 0, len(props))
	for i := range props {
		p := &props[i]
		node := map[string]any{"name": p.Name, "type": p.Type}
		if v := expand(p.Value); v != nil {
			node["value"] = v
		}
		if p.Meta.EnumType != "" {
			node["enumType"] = p.Meta.EnumType
		}
		out = append(out, node)
	}
	return out
}

// expand recursively resolves any palsav value, driving the lazy iterators so
// maps, sets and struct arrays are fully materialised.
func expand(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case palsav.Properties:
		return expandProperties(t)
	case palsav.StructValue:
		return map[string]any{"structType": t.Type, "value": expand(t.Value)}
	case palsav.EnumValue:
		return map[string]any{"enum": t.Type, "value": t.Value}
	case palsav.RawValue:
		return map[string]any{
			"raw":    base64.StdEncoding.EncodeToString(t.Data),
			"bytes":  len(t.Data),
			"reason": t.Reason,
		}
	case *palsav.MapValue:
		entries, err := t.Entries()
		if err != nil {
			return map[string]any{"mapError": err.Error(), "keyType": t.KeyType, "valueType": t.ValueType}
		}
		items := make([]any, 0, len(entries))
		for _, e := range entries {
			items = append(items, map[string]any{"key": expand(e.Key), "value": expand(e.Value)})
		}
		return map[string]any{"keyType": t.KeyType, "valueType": t.ValueType, "count": t.Count, "entries": items}
	case *palsav.SetValue:
		vals, err := t.Values()
		if err != nil {
			return map[string]any{"setError": err.Error(), "elementType": t.ElementType}
		}
		return map[string]any{"elementType": t.ElementType, "count": t.Count, "values": expandSlice(vals)}
	case palsav.ArrayValue:
		if t.Structs != nil {
			vals, err := t.Structs.Values()
			if err != nil {
				return map[string]any{"arrayError": err.Error(), "innerType": t.InnerType}
			}
			return map[string]any{"innerType": t.InnerType, "structs": expandSlice(vals)}
		}
		return map[string]any{"innerType": t.InnerType, "values": t.Values}
	case *palsav.StructArray:
		vals, err := t.Values()
		if err != nil {
			return map[string]any{"structArrayError": err.Error()}
		}
		return expandSlice(vals)
	case []any:
		return expandSlice(t)
	default:
		// Scalars and concrete geometry structs (Vector, Quat, GUID, ...) marshal fine as-is.
		return v
	}
}

func expandSlice(in []any) []any {
	out := make([]any, 0, len(in))
	for _, e := range in {
		out = append(out, expand(e))
	}
	return out
}

// Command palworld-save-reader reads Palworld saves as complete or projected JSON.
//
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/palworld"
	"github.com/LukeHollandDev/palworld-save-reader/internal/projection"
)

const usageText = `usage:
  palworld-save-reader --full [--decode-raw] FILE
  palworld-save-reader --schema PROJECTION.json [--allow-partial] [--explain] FILE
  palworld-save-reader --preset NAME [--allow-partial] [--explain] FILE
  palworld-save-reader --list-presets
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("palworld-save-reader", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { _, _ = io.WriteString(stderr, usageText) }

	full := flags.Bool("full", false, "fully expand one .sav file")
	decodeRaw := flags.Bool("decode-raw", false, "with --full, also decode RawData blobs of a known layout")
	schemaPath := flags.String("schema", "", "apply a projection document")
	presetName := flags.String("preset", "", "apply a bundled projection preset")
	listPresets := flags.Bool("list-presets", false, "list bundled projection presets")
	allowPartial := flags.Bool("allow-partial", false, "emit null for unresolved projected fields")
	explain := flags.Bool("explain", false, "write JSON matching diagnostics to standard error")

	if err := flags.Parse(arguments); err != nil {
		return 2
	}

	modeCount := boolInt(*full) + boolInt(*schemaPath != "") + boolInt(*presetName != "") + boolInt(*listPresets)
	if modeCount != 1 {
		return usageError(stderr, "exactly one of --full, --schema, --preset, or --list-presets is required")
	}
	if *decodeRaw && !*full {
		return usageError(stderr, "--decode-raw is only valid with --full")
	}
	if *listPresets {
		if flags.NArg() != 0 || *allowPartial || *explain {
			return usageError(stderr, "--list-presets does not accept an input file or projection options")
		}
		presets, err := projection.Presets()
		if err != nil {
			return runtimeError(stderr, err)
		}
		if err := writeJSON(stdout, presets); err != nil {
			return runtimeError(stderr, err)
		}
		return 0
	}
	if flags.NArg() != 1 {
		return usageError(stderr, "the selected decode mode requires exactly one input .sav file")
	}
	if *full {
		if *allowPartial || *explain {
			return usageError(stderr, "--full does not accept projection options")
		}
		if err := dumpFull(stdout, flags.Arg(0), *decodeRaw); err != nil {
			return runtimeError(stderr, err)
		}
		return 0
	}

	var (
		document *projection.Document
		err      error
	)
	if *schemaPath != "" {
		document, err = readProjection(*schemaPath)
		if err != nil {
			return invalidProjectionError(stderr, err)
		}
	} else {
		document, err = projection.ResolvePreset(*presetName)
		if err != nil {
			return usageError(stderr, err.Error())
		}
	}

	diagnostics, err := dumpProjection(
		stdout,
		flags.Arg(0),
		document,
		projection.ApplyOptions{AllowPartial: *allowPartial, Explain: *explain},
	)
	if len(diagnostics) != 0 {
		if diagnosticErr := writeDiagnostics(stderr, diagnostics); diagnosticErr != nil {
			return runtimeError(stderr, diagnosticErr)
		}
	}
	if err != nil {
		return runtimeError(stderr, err)
	}
	return 0
}

func readProjection(path string) (*projection.Document, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read projection %s: %w", path, err)
	}
	defer file.Close()
	document, err := projection.ParseReader(file)
	if err != nil {
		return nil, err
	}
	return document, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func usageError(stderr io.Writer, message string) int {
	_, _ = fmt.Fprintln(stderr, "error:", message)
	_, _ = io.WriteString(stderr, usageText)
	return 2
}

func invalidProjectionError(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintln(stderr, "error:", err)
	return 2
}

func runtimeError(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintln(stderr, "error:", err)
	return 1
}

func dumpProjection(
	stdout io.Writer,
	path string,
	document *projection.Document,
	options projection.ApplyOptions,
) ([]projection.Diagnostic, error) {
	save, err := palworld.Load(path)
	if err != nil {
		return nil, err
	}
	output, diagnostics, err := projection.Apply(save.Properties, document, options)
	if err != nil {
		return diagnostics, err
	}
	return diagnostics, writeJSON(stdout, output)
}

// dumpFull decodes one .sav and emits the entire property tree with every lazy
// collection expanded. decodeRaw additionally interprets the RawData blobs whose
// layout internal/palworld knows; without it they stay base64, which is the
// long-standing output shape.
func dumpFull(stdout io.Writer, path string, decodeRaw bool) error {
	save, err := palworld.Load(path)
	if err != nil {
		return err
	}
	expander := &expander{decodeRaw: decodeRaw}
	output := map[string]any{
		"container":  save.Container,
		"header":     save.Header,
		"properties": expander.properties(save.Properties, ""),
	}
	return writeJSON(stdout, output)
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func writeDiagnostics(writer io.Writer, diagnostics []projection.Diagnostic) error {
	return json.NewEncoder(writer).Encode(struct {
		Diagnostics []projection.Diagnostic `json:"diagnostics"`
	}{Diagnostics: diagnostics})
}

// expander materialises a decoded save into JSON-ready values.
//
// It carries the property path alongside the value because that is the only way
// to know which RawData blob is which: Unreal tags them all as "array of
// ByteProperty", so the layout is identified by where the blob sits.
// internal/palworld owns that mapping. Paths are built exactly as internal/gvas
// builds them for type hints -- notably a struct array repeats its own name for
// its elements -- and expanderPathsMatchHints in the tests holds the two
// conventions together.
type expander struct {
	decodeRaw bool
}

// child appends one property name to a path.
func child(path, name string) string { return path + "." + name }

// properties turns gvas's ordered property slice into a readable list, dropping
// the Raw/Offset bookkeeping fields and recursively materialising values.
func (e *expander) properties(properties gvas.Properties, path string) []any {
	output := make([]any, 0, len(properties))
	for index := range properties {
		property := &properties[index]
		node := map[string]any{"name": property.Name, "type": property.Type}
		if value := e.value(property.Value, child(path, property.Name)); value != nil {
			node["value"] = value
		}
		if property.Meta.EnumType != "" {
			node["enumType"] = property.Meta.EnumType
		}
		output = append(output, node)
	}
	return output
}

// value recursively resolves any gvas value, driving the lazy iterators so
// maps, sets, and struct arrays are fully materialised.
func (e *expander) value(value any, path string) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case gvas.Properties:
		return e.properties(typed, path)
	case gvas.StructValue:
		// A struct wrapper adds no path segment, matching readStructBody.
		return map[string]any{"structType": typed.Type, "value": e.value(typed.Value, path)}
	case gvas.EnumValue:
		return map[string]any{"enum": typed.Type, "value": typed.Value}
	case gvas.UndecodedValue:
		return map[string]any{
			"raw":    base64.StdEncoding.EncodeToString(typed.Data),
			"bytes":  len(typed.Data),
			"reason": typed.Reason,
		}
	case *gvas.MapValue:
		entries, err := typed.Entries()
		if err != nil {
			return map[string]any{"mapError": err.Error(), "keyType": typed.KeyType, "valueType": typed.ValueType}
		}
		keyPath, valuePath := child(path, "Key"), child(path, "Value")
		items := make([]any, 0, len(entries))
		for _, entry := range entries {
			items = append(items, map[string]any{
				"key":   e.value(entry.Key, keyPath),
				"value": e.value(entry.Value, valuePath),
			})
		}
		return map[string]any{"keyType": typed.KeyType, "valueType": typed.ValueType, "count": typed.Count, "entries": items}
	case *gvas.SetValue:
		values, err := typed.Values()
		if err != nil {
			return map[string]any{"setError": err.Error(), "elementType": typed.ElementType}
		}
		return map[string]any{
			"elementType": typed.ElementType,
			"count":       typed.Count,
			"values":      e.slice(values, child(path, typed.ElementType)),
		}
	case gvas.ArrayValue:
		if typed.Structs != nil {
			values, err := typed.Structs.Values()
			if err != nil {
				return map[string]any{"arrayError": err.Error(), "innerType": typed.InnerType}
			}
			return map[string]any{
				"innerType": typed.InnerType,
				"structs":   e.slice(values, child(path, typed.Structs.Name)),
			}
		}
		return e.byteArray(typed, path)
	case *gvas.StructArray:
		values, err := typed.Values()
		if err != nil {
			return map[string]any{"structArrayError": err.Error()}
		}
		return e.slice(values, child(path, typed.Name))
	case []any:
		return e.slice(typed, path)
	default:
		return value
	}
}

// byteArray renders a primitive ArrayProperty. A RawData blob whose layout
// internal/palworld recognises is decoded alongside the base64 rather than
// instead of it, so --decode-raw only ever adds information.
func (e *expander) byteArray(array gvas.ArrayValue, path string) any {
	node := map[string]any{"innerType": array.InnerType, "values": array.Values}
	if !e.decodeRaw {
		return node
	}
	data, ok := array.Values.([]byte)
	if !ok {
		return node
	}
	switch palworld.ClassifyRawData(path) {
	case palworld.RawDataItemSlot:
		slot, err := palworld.DecodeItemSlot(data)
		if err != nil {
			node["decodeError"] = err.Error()
			return node
		}
		decoded := map[string]any{
			"kind":      palworld.RawDataItemSlot.String(),
			"slotIndex": slot.SlotIndex,
			"count":     slot.Count,
			"itemId":    slot.ItemID,
		}
		if !slot.DynamicItemID.IsZero() {
			decoded["dynamicItemId"] = slot.DynamicItemID.String()
		}
		// Only report a trailer that carries something. Reporting 52 zero bytes
		// on every slot would triple the output for no information.
		if trailer := trimZero(slot.Trailer); len(trailer) != 0 {
			decoded["trailer"] = base64.StdEncoding.EncodeToString(slot.Trailer)
		}
		node["decoded"] = decoded
	}
	return node
}

// trimZero returns input with trailing zero bytes removed.
func trimZero(input []byte) []byte {
	end := len(input)
	for end > 0 && input[end-1] == 0 {
		end--
	}
	return input[:end]
}

func (e *expander) slice(input []any, path string) []any {
	output := make([]any, 0, len(input))
	for _, element := range input {
		output = append(output, e.value(element, path))
	}
	return output
}

// Command savedecode reads Palworld saves as complete or projected JSON.
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

	"github.com/LukeHollandDev/palworld-save-reader/internal/palsav"
	"github.com/LukeHollandDev/palworld-save-reader/internal/projection"
)

const usageText = `usage:
  savedecode --full FILE
  savedecode --schema PROJECTION.json [--allow-partial] [--explain] FILE
  savedecode --preset NAME --game-version VERSION [--allow-partial] [--explain] FILE
  savedecode --list-presets
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("savedecode", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { _, _ = io.WriteString(stderr, usageText) }

	full := flags.Bool("full", false, "fully expand one .sav file")
	schemaPath := flags.String("schema", "", "apply a projection document")
	presetName := flags.String("preset", "", "apply a bundled projection preset")
	gameVersion := flags.String("game-version", "", "exact Palworld version for --preset")
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
	if *listPresets {
		if flags.NArg() != 0 || *gameVersion != "" || *allowPartial || *explain {
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
		if *gameVersion != "" || *allowPartial || *explain {
			return usageError(stderr, "--full does not accept projection options")
		}
		if err := dumpFull(stdout, flags.Arg(0)); err != nil {
			return runtimeError(stderr, err)
		}
		return 0
	}
	if *schemaPath != "" && *gameVersion != "" {
		return usageError(stderr, "--game-version is only valid with --preset")
	}
	if *presetName != "" && *gameVersion == "" {
		return usageError(stderr, "--preset requires --game-version")
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
		document, err = projection.ResolvePreset(*presetName, *gameVersion)
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
	save, err := palsav.Load(path)
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
// collection expanded.
func dumpFull(stdout io.Writer, path string) error {
	save, err := palsav.Load(path)
	if err != nil {
		return err
	}
	output := map[string]any{
		"container":  save.Container,
		"header":     save.Header,
		"properties": expandProperties(save.Properties),
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

// expandProperties turns palsav's ordered property slice into a readable list,
// dropping the Raw/Offset bookkeeping fields and recursively materialising
// values via expand.
func expandProperties(properties palsav.Properties) []any {
	output := make([]any, 0, len(properties))
	for index := range properties {
		property := &properties[index]
		node := map[string]any{"name": property.Name, "type": property.Type}
		if value := expand(property.Value); value != nil {
			node["value"] = value
		}
		if property.Meta.EnumType != "" {
			node["enumType"] = property.Meta.EnumType
		}
		output = append(output, node)
	}
	return output
}

// expand recursively resolves any palsav value, driving the lazy iterators so
// maps, sets, and struct arrays are fully materialised.
func expand(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case palsav.Properties:
		return expandProperties(typed)
	case palsav.StructValue:
		return map[string]any{"structType": typed.Type, "value": expand(typed.Value)}
	case palsav.EnumValue:
		return map[string]any{"enum": typed.Type, "value": typed.Value}
	case palsav.UndecodedValue:
		return map[string]any{
			"raw":    base64.StdEncoding.EncodeToString(typed.Data),
			"bytes":  len(typed.Data),
			"reason": typed.Reason,
		}
	case *palsav.MapValue:
		entries, err := typed.Entries()
		if err != nil {
			return map[string]any{"mapError": err.Error(), "keyType": typed.KeyType, "valueType": typed.ValueType}
		}
		items := make([]any, 0, len(entries))
		for _, entry := range entries {
			items = append(items, map[string]any{"key": expand(entry.Key), "value": expand(entry.Value)})
		}
		return map[string]any{"keyType": typed.KeyType, "valueType": typed.ValueType, "count": typed.Count, "entries": items}
	case *palsav.SetValue:
		values, err := typed.Values()
		if err != nil {
			return map[string]any{"setError": err.Error(), "elementType": typed.ElementType}
		}
		return map[string]any{"elementType": typed.ElementType, "count": typed.Count, "values": expandSlice(values)}
	case palsav.ArrayValue:
		if typed.Structs != nil {
			values, err := typed.Structs.Values()
			if err != nil {
				return map[string]any{"arrayError": err.Error(), "innerType": typed.InnerType}
			}
			return map[string]any{"innerType": typed.InnerType, "structs": expandSlice(values)}
		}
		return map[string]any{"innerType": typed.InnerType, "values": typed.Values}
	case *palsav.StructArray:
		values, err := typed.Values()
		if err != nil {
			return map[string]any{"structArrayError": err.Error()}
		}
		return expandSlice(values)
	case []any:
		return expandSlice(typed)
	default:
		return value
	}
}

func expandSlice(input []any) []any {
	output := make([]any, 0, len(input))
	for _, element := range input {
		output = append(output, expand(element))
	}
	return output
}

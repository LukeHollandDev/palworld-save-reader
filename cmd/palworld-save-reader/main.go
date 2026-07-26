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
  palworld-save-reader --resolve player --id UID --saves DIR
  palworld-save-reader --resolve guild --id GROUPID --saves DIR
  palworld-save-reader --resolve players|guilds|world --saves DIR
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
	resolveKind := flags.String("resolve", "", "join a save directory into one document: "+resolveKindNames())
	resolveID := flags.String("id", "", "with --resolve player or guild, the UID to resolve")
	savesDir := flags.String("saves", "", "with --resolve, the save directory holding Level.sav and Players/")

	if err := flags.Parse(arguments); err != nil {
		return 2
	}

	modeCount := boolInt(*full) + boolInt(*schemaPath != "") + boolInt(*presetName != "") +
		boolInt(*listPresets) + boolInt(*resolveKind != "")
	if modeCount != 1 {
		return usageError(stderr, "exactly one of --full, --schema, --preset, --resolve, or --list-presets is required")
	}
	if *decodeRaw && !*full {
		return usageError(stderr, "--decode-raw is only valid with --full")
	}
	if *resolveKind == "" && (*resolveID != "" || *savesDir != "") {
		return usageError(stderr, "--id and --saves are only valid with --resolve")
	}
	if *resolveKind != "" {
		return runResolveMode(stdout, stderr, *resolveKind, *resolveID, *savesDir, flags.NArg(), *allowPartial || *explain)
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
	var (
		decoded any
		err     error
	)
	switch palworld.ClassifyRawData(path) {
	case palworld.RawDataItemSlot:
		decoded, err = itemSlotNode(data)
	case palworld.RawDataCharacter:
		decoded, err = e.characterNode(data, path)
	case palworld.RawDataCharacterSlot:
		decoded, err = characterSlotNode(data)
	case palworld.RawDataGroup:
		decoded, err = groupNode(data)
	case palworld.RawDataBaseCamp:
		decoded, err = baseCampNode(data)
	case palworld.RawDataWorkerDirector:
		decoded, err = workerDirectorNode(data)
	default:
		return node
	}
	if err != nil {
		node["decodeError"] = err.Error()
		return node
	}
	node["decoded"] = decoded
	return node
}

func itemSlotNode(data []byte) (any, error) {
	slot, err := palworld.DecodeItemSlot(data)
	if err != nil {
		return nil, err
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
	return decoded, nil
}

// characterNode renders a nested character stream: the same property list every
// other part of the dump uses, plus the group id the blob's framing carries.
//
// The nested properties are expanded at the blob's own path, which is what gvas
// used to resolve type hints inside it. Keeping the two the same means a RawData
// nested deeper still would be classified by where it actually sits.
func (e *expander) characterNode(data []byte, path string) (any, error) {
	character, err := palworld.DecodeCharacter(data)
	if err != nil {
		return nil, err
	}
	decoded := map[string]any{
		"kind":       palworld.RawDataCharacter.String(),
		"properties": e.properties(character.Properties, path),
	}
	if !character.GroupID.IsZero() {
		decoded["groupId"] = character.GroupID.String()
	}
	// The framing is eight zero bytes around the group id in every fixture
	// blob, so report it only when it is carrying something else.
	if character.PaddingBeyondGroupID() {
		decoded["trailer"] = base64.StdEncoding.EncodeToString(character.Trailer)
	}
	return decoded, nil
}

// characterSlotNode renders a pal reference: which character record occupies a
// slot of a party, storage box, or base camp. The instance id is reported even
// when it is zero, because an empty slot is information here rather than an
// absent field.
func characterSlotNode(data []byte) (any, error) {
	slot, err := palworld.DecodeCharacterSlot(data)
	if err != nil {
		return nil, err
	}
	decoded := map[string]any{
		"kind":       palworld.RawDataCharacterSlot.String(),
		"instanceId": slot.InstanceID.String(),
		"empty":      slot.Empty(),
	}
	if !slot.PlayerUID.IsZero() {
		decoded["playerUId"] = slot.PlayerUID.String()
	}
	if trailer := trimZero(slot.Trailer); len(trailer) != 0 {
		decoded["trailer"] = base64.StdEncoding.EncodeToString(slot.Trailer)
	}
	return decoded, nil
}

// groupNode renders a guild or an organization.
//
// Which of the two it is comes from the sibling GroupType property, which this
// renderer does not have: a RawData blob is expanded knowing only its own path.
// So the guild half is attempted and reported when it decodes. That is a
// heuristic, and it is a safe one because DecodeGuild is strict -- an
// organization's four-byte remainder cannot satisfy the guild fields -- but it is
// still the renderer guessing where --resolve guild is told.
func groupNode(data []byte) (any, error) {
	group, err := palworld.DecodeGroup(data)
	if err != nil {
		return nil, err
	}
	handles := make([]any, 0, len(group.Handles))
	for _, handle := range group.Handles {
		entry := map[string]any{"instanceId": handle.InstanceID.String()}
		if !handle.PlayerUID.IsZero() {
			entry["playerUId"] = handle.PlayerUID.String()
		}
		handles = append(handles, entry)
	}
	decoded := map[string]any{
		"kind":             palworld.RawDataGroup.String(),
		"groupId":          group.ID.String(),
		"organizationType": group.OrganizationType,
		"handles":          handles,
		"baseIds":          guidStrings(group.BaseIDs),
	}
	if group.Name != "" {
		decoded["name"] = group.Name
	}
	if group.Unknown != 0 {
		decoded["unknown"] = group.Unknown
	}
	guild, guildErr := palworld.DecodeGuild(group.Remainder)
	if guildErr != nil {
		// Four zero bytes on every fixture organization, so it is reported only
		// when it holds something this cannot read.
		if remainder := trimZero(group.Remainder); len(remainder) != 0 {
			decoded["remainder"] = base64.StdEncoding.EncodeToString(group.Remainder)
			decoded["guildDecodeError"] = guildErr.Error()
		}
		return decoded, nil
	}
	members := make([]any, 0, len(guild.Members))
	for _, member := range guild.Members {
		members = append(members, map[string]any{
			"playerUId":       member.PlayerUID.String(),
			"name":            member.Name,
			"lastOnlineTicks": member.LastOnlineTicks,
			"role":            member.Role,
		})
	}
	decoded["guild"] = map[string]any{
		"name":          guild.Name,
		"admin":         guild.Admin.String(),
		"namedBy":       guild.NamedBy.String(),
		"baseCampLevel": guild.BaseCampLevel,
		"basePoints":    guidStrings(guild.BasePoints),
		"members":       members,
		"reserved":      base64.StdEncoding.EncodeToString(guild.Reserved),
		"trailer":       base64.StdEncoding.EncodeToString(guild.Trailer),
	}
	return decoded, nil
}

// baseCampNode renders a guild's base camp: where it is and who owns it.
func baseCampNode(data []byte) (any, error) {
	camp, err := palworld.DecodeBaseCamp(data)
	if err != nil {
		return nil, err
	}
	decoded := map[string]any{
		"kind":                     palworld.RawDataBaseCamp.String(),
		"id":                       camp.ID.String(),
		"name":                     camp.Name,
		"state":                    camp.State,
		"areaRange":                camp.AreaRange,
		"groupId":                  camp.GroupID.String(),
		"ownerMapObjectInstanceId": camp.OwnerMapObjectID.String(),
		"transform":                transformNode(camp.Transform),
		"fastTravelLocalTransform": transformNode(camp.FastTravel),
	}
	if trailer := trimZero(camp.Trailer); len(trailer) != 0 {
		decoded["trailer"] = base64.StdEncoding.EncodeToString(camp.Trailer)
	}
	return decoded, nil
}

// workerDirectorNode renders a base camp's worker director, whose one useful
// field is the container holding the camp's workers.
func workerDirectorNode(data []byte) (any, error) {
	director, err := palworld.DecodeWorkerDirector(data)
	if err != nil {
		return nil, err
	}
	decoded := map[string]any{
		"kind":        palworld.RawDataWorkerDirector.String(),
		"id":          director.ID.String(),
		"containerId": director.ContainerID.String(),
		"transform":   transformNode(director.Transform),
	}
	if reserved := trimZero(director.Reserved); len(reserved) != 0 {
		decoded["reserved"] = base64.StdEncoding.EncodeToString(director.Reserved)
	}
	if trailer := trimZero(director.Trailer); len(trailer) != 0 {
		decoded["trailer"] = base64.StdEncoding.EncodeToString(director.Trailer)
	}
	return decoded, nil
}

// transformNode renders an FTransform the same way the rest of the dump renders
// the Vector and Quat structs gvas decodes, so a transform out of a RawData blob
// and one out of a property look alike.
func transformNode(transform palworld.Transform) any {
	return map[string]any{
		"rotation":    transform.Rotation,
		"translation": transform.Translation,
		"scale":       transform.Scale,
	}
}

func guidStrings(ids []gvas.GUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
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

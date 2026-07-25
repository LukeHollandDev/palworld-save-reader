// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"errors"
	"fmt"
	"sort"

	"github.com/LukeHollandDev/palworld-save-reader/internal/palsav"
)

// Options controls projection resolution.
type Options struct {
	AllowPartial bool
	Explain      bool
}

// Diagnostic is one machine-readable matching decision or problem.
type Diagnostic struct {
	Severity   string   `json:"severity"`
	Kind       string   `json:"kind"`
	OutputPath string   `json:"outputPath,omitempty"`
	SourcePath string   `json:"sourcePath,omitempty"`
	Message    string   `json:"message"`
	Candidates []string `json:"candidates,omitempty"`
	Score      int      `json:"score,omitempty"`
}

// ResolutionError reports that no unique, complete projection could be
// resolved.
type ResolutionError struct {
	Kind    string
	Path    string
	Message string
}

func (err *ResolutionError) Error() string {
	if err.Path == "" {
		return "projection " + err.Kind + ": " + err.Message
	}
	return fmt.Sprintf("projection %s at %s: %s", err.Kind, err.Path, err.Message)
}

// IsResolutionError reports whether err came from projection matching.
func IsResolutionError(err error) bool {
	var target *ResolutionError
	return errors.As(err, &target)
}

type evaluation struct {
	value      *Value
	score      int
	evidence   int
	issues     []Diagnostic
	selections []Diagnostic
}

type candidate struct {
	node       *dataNode
	evaluation evaluation
}

// Apply resolves document against one decoded save and returns ordered output.
func Apply(properties palsav.Properties, document *Document, options Options) (*Value, []Diagnostic, error) {
	if document == nil || document.Shape == nil {
		return nil, nil, invalid("$", "nil document or shape")
	}
	root, err := normalizeProperties(properties)
	if err != nil {
		return nil, nil, err
	}
	var nodes []*dataNode
	collectCandidates(root, document.Shape.Kind, &nodes)
	if len(nodes) == 0 {
		diagnostic := Diagnostic{
			Severity:   "error",
			Kind:       "missing",
			OutputPath: "$",
			Message:    "no decoded value has the requested top-level kind " + document.Shape.Kind.String(),
		}
		return nil, []Diagnostic{diagnostic}, &ResolutionError{
			Kind: "unresolved", Path: "$", Message: diagnostic.Message,
		}
	}

	candidates := make([]candidate, 0, len(nodes))
	for _, node := range nodes {
		result := evaluate(document.Shape, node, "$")
		if result.evidence == 0 {
			continue
		}
		candidates = append(candidates, candidate{node: node, evaluation: result})
	}
	if len(candidates) == 0 {
		diagnostic := Diagnostic{
			Severity:   "error",
			Kind:       "missing",
			OutputPath: "$",
			Message:    "no candidate contains an exact requested field name",
		}
		return nil, []Diagnostic{diagnostic}, &ResolutionError{
			Kind: "unresolved", Path: "$", Message: diagnostic.Message,
		}
	}

	eligible := candidates[:0]
	for _, item := range candidates {
		if options.AllowPartial || len(item.evaluation.issues) == 0 {
			eligible = append(eligible, item)
		}
	}
	if len(eligible) == 0 {
		best := bestCandidates(candidates)
		diagnostics := append([]Diagnostic(nil), best[0].evaluation.issues...)
		if len(best) > 1 {
			paths := make([]string, 0, len(best))
			for _, item := range best {
				paths = append(paths, item.node.path)
			}
			sort.Strings(paths)
			diagnostics = append(diagnostics, Diagnostic{
				Severity:   "error",
				Kind:       "ambiguous",
				OutputPath: "$",
				Message:    "multiple incomplete candidates have the same highest compatibility score",
				Candidates: paths,
				Score:      best[0].evaluation.score,
			})
		}
		if options.Explain {
			diagnostics = append(diagnostics, candidateDiagnostics(candidates, best[0].node.path)...)
		}
		return nil, diagnostics, &ResolutionError{
			Kind: "unresolved",
			Path: "$",
			Message: fmt.Sprintf(
				"best candidate %s has %d unresolved or incompatible field(s)",
				best[0].node.path,
				len(best[0].evaluation.issues),
			),
		}
	}

	best := bestCandidates(eligible)
	if len(best) != 1 {
		paths := make([]string, 0, len(best))
		for _, item := range best {
			paths = append(paths, item.node.path)
		}
		sort.Strings(paths)
		diagnostic := Diagnostic{
			Severity:   "error",
			Kind:       "ambiguous",
			OutputPath: "$",
			Message:    "multiple candidates have the same highest compatibility score",
			Candidates: paths,
			Score:      best[0].evaluation.score,
		}
		return nil, []Diagnostic{diagnostic}, &ResolutionError{
			Kind: "ambiguous", Path: "$", Message: diagnostic.Message,
		}
	}

	selected := best[0]
	diagnostics := append([]Diagnostic(nil), selected.evaluation.issues...)
	if options.AllowPartial {
		for index := range diagnostics {
			diagnostics[index].Severity = "warning"
		}
	}
	if options.Explain {
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   "info",
			Kind:       "root",
			OutputPath: "$",
			SourcePath: selected.node.path,
			Message:    "selected unique highest-scoring candidate",
			Score:      selected.evaluation.score,
		})
		diagnostics = append(diagnostics, selected.evaluation.selections...)
		diagnostics = append(diagnostics, candidateDiagnostics(candidates, selected.node.path)...)
	}
	return selected.evaluation.value, diagnostics, nil
}

func evaluate(shape *Shape, data *dataNode, outputPath string) evaluation {
	if shape.Kind == KindNull {
		if data.kind == kindUnsupported {
			return typeIssue(shape, data, outputPath)
		}
		return evaluation{
			value:    copyDataValue(data),
			score:    1,
			evidence: 0,
		}
	}
	if shape.Kind != data.kind {
		return typeIssue(shape, data, outputPath)
	}
	switch shape.Kind {
	case KindString, KindNumber, KindBoolean:
		return evaluation{
			value: &Value{kind: data.kind, scalar: data.value},
			score: 2,
		}
	case KindObject:
		return evaluateObject(shape, data, outputPath)
	case KindArray:
		return evaluateArray(shape, data, outputPath)
	default:
		return typeIssue(shape, data, outputPath)
	}
}

func evaluateObject(shape *Shape, data *dataNode, outputPath string) evaluation {
	result := evaluation{
		value: &Value{kind: KindObject, fields: make([]valueField, 0, len(shape.Fields))},
	}
	for _, requested := range shape.Fields {
		childOutputPath := propertyPath(outputPath, requested.Name)
		var matches []*dataNode
		for _, field := range data.fields {
			if field.name == requested.Name {
				matches = append(matches, field.node)
			}
		}
		switch len(matches) {
		case 0:
			result.value.fields = append(result.value.fields, valueField{name: requested.Name, value: nullValue()})
			result.issues = append(result.issues, Diagnostic{
				Severity:   "error",
				Kind:       "missing",
				OutputPath: childOutputPath,
				SourcePath: data.path,
				Message:    fmt.Sprintf("field %q is not present", requested.Name),
			})
		case 1:
			result.evidence++
			child := evaluate(requested.Shape, matches[0], childOutputPath)
			result.value.fields = append(result.value.fields, valueField{name: requested.Name, value: child.value})
			result.score += 100 + child.score
			result.evidence += child.evidence
			result.issues = append(result.issues, child.issues...)
			result.selections = append(result.selections, Diagnostic{
				Severity:   "info",
				Kind:       "selected",
				OutputPath: childOutputPath,
				SourcePath: matches[0].path,
				Message:    "matched exact serialized field name",
			})
			result.selections = append(result.selections, child.selections...)
		default:
			result.evidence++
			result.value.fields = append(result.value.fields, valueField{name: requested.Name, value: nullValue()})
			paths := make([]string, 0, len(matches))
			for _, match := range matches {
				paths = append(paths, match.path)
			}
			result.issues = append(result.issues, Diagnostic{
				Severity:   "error",
				Kind:       "ambiguous",
				OutputPath: childOutputPath,
				SourcePath: data.path,
				Message:    fmt.Sprintf("field %q occurs more than once in the candidate object", requested.Name),
				Candidates: paths,
			})
		}
	}
	return result
}

func evaluateArray(shape *Shape, data *dataNode, outputPath string) evaluation {
	result := evaluation{
		value: &Value{kind: KindArray, items: make([]*Value, 0, len(data.items))},
		score: 10,
	}
	minScore := -1
	minEvidence := -1
	for index, item := range data.items {
		childPath := indexedPath(outputPath, index)
		child := evaluate(shape.Element, item, childPath)
		result.value.items = append(result.value.items, child.value)
		result.issues = append(result.issues, child.issues...)
		result.selections = append(result.selections, child.selections...)
		if minScore == -1 || child.score < minScore {
			minScore = child.score
		}
		if minEvidence == -1 || child.evidence < minEvidence {
			minEvidence = child.evidence
		}
	}
	if minScore >= 0 {
		result.score += minScore
		result.evidence += minEvidence
	}
	return result
}

func typeIssue(shape *Shape, data *dataNode, outputPath string) evaluation {
	actual := data.kind.String()
	if data.kind == kindUnsupported {
		actual = "unsupported"
	}
	return evaluation{
		value: nullValue(),
		issues: []Diagnostic{{
			Severity:   "error",
			Kind:       "incompatible",
			OutputPath: outputPath,
			SourcePath: data.path,
			Message:    fmt.Sprintf("requested %s but decoded value is %s", shape.Kind.String(), actual),
		}},
	}
}

func copyDataValue(data *dataNode) *Value {
	switch data.kind {
	case KindObject:
		value := &Value{kind: KindObject, fields: make([]valueField, 0, len(data.fields))}
		for _, field := range data.fields {
			value.fields = append(value.fields, valueField{name: field.name, value: copyDataValue(field.node)})
		}
		return value
	case KindArray:
		value := &Value{kind: KindArray, items: make([]*Value, 0, len(data.items))}
		for _, item := range data.items {
			value.items = append(value.items, copyDataValue(item))
		}
		return value
	case kindUnsupported:
		return nullValue()
	default:
		return &Value{kind: data.kind, scalar: data.value}
	}
}

func collectCandidates(node *dataNode, kind Kind, output *[]*dataNode) {
	if node.kind == kind {
		*output = append(*output, node)
	}
	for _, field := range node.fields {
		collectCandidates(field.node, kind, output)
	}
	for _, item := range node.items {
		collectCandidates(item, kind, output)
	}
}

func bestCandidates(candidates []candidate) []candidate {
	bestScore := -1
	bestIssues := int(^uint(0) >> 1)
	var best []candidate
	for _, item := range candidates {
		issueCount := len(item.evaluation.issues)
		if item.evaluation.score > bestScore ||
			(item.evaluation.score == bestScore && issueCount < bestIssues) {
			bestScore = item.evaluation.score
			bestIssues = issueCount
			best = []candidate{item}
		} else if item.evaluation.score == bestScore && issueCount == bestIssues {
			best = append(best, item)
		}
	}
	return best
}

func candidateDiagnostics(candidates []candidate, selectedPath string) []Diagnostic {
	diagnostics := make([]Diagnostic, 0, len(candidates)-1)
	for _, item := range candidates {
		if item.node.path == selectedPath {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   "info",
			Kind:       "rejected",
			SourcePath: item.node.path,
			Message: fmt.Sprintf(
				"candidate scored lower or had more problems (%d issue(s))",
				len(item.evaluation.issues),
			),
			Score: item.evaluation.score,
		})
	}
	return diagnostics
}

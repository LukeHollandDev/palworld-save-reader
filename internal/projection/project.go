// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package projection

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/LukeHollandDev/palworld-save-reader/internal/palsav"
)

// ApplyOptions controls projection resolution.
type ApplyOptions struct {
	AllowPartial bool
	Explain      bool
}

// Severity ranks a Diagnostic for a consumer deciding whether to fail.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// DiagnosticKind names what a Diagnostic reports. The set is closed, so a
// consumer can switch on it exhaustively. It is spelled DiagnosticKind rather
// than Kind because Kind already names the shape kinds.
type DiagnosticKind string

const (
	// DiagnosticMissing reports a requested field with no match.
	DiagnosticMissing DiagnosticKind = "missing"
	// DiagnosticAmbiguous reports more than one equally good match.
	DiagnosticAmbiguous DiagnosticKind = "ambiguous"
	// DiagnosticIncompatible reports a match whose kind differs from the shape.
	DiagnosticIncompatible DiagnosticKind = "incompatible"
	// DiagnosticSelected records a match that was taken.
	DiagnosticSelected DiagnosticKind = "selected"
	// DiagnosticRejected records a candidate that lost to the selected one.
	DiagnosticRejected DiagnosticKind = "rejected"
	// DiagnosticRoot records which candidate became the projection root.
	DiagnosticRoot DiagnosticKind = "root"
)

// Diagnostic is one machine-readable matching decision or problem.
type Diagnostic struct {
	Severity   Severity       `json:"severity"`
	Kind       DiagnosticKind `json:"kind"`
	OutputPath string         `json:"outputPath,omitempty"`
	SourcePath string         `json:"sourcePath,omitempty"`
	Message    string         `json:"message"`
	Candidates []string       `json:"candidates,omitempty"`
	Score      int            `json:"score,omitempty"`
}

// missingDiagnostic and ambiguousDiagnostic cover the two kinds raised from
// more than one place. The remaining kinds are built inline at their single
// call site.
func missingDiagnostic(outputPath, sourcePath, message string) Diagnostic {
	return Diagnostic{
		Severity:   SeverityError,
		Kind:       DiagnosticMissing,
		OutputPath: outputPath,
		SourcePath: sourcePath,
		Message:    message,
	}
}

func ambiguousDiagnostic(
	outputPath, sourcePath, message string,
	candidates []string,
	score int,
) Diagnostic {
	return Diagnostic{
		Severity:   SeverityError,
		Kind:       DiagnosticAmbiguous,
		OutputPath: outputPath,
		SourcePath: sourcePath,
		Message:    message,
		Candidates: candidates,
		Score:      score,
	}
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
	value      *Output
	score      int
	evidence   int
	issues     []Diagnostic
	selections []Diagnostic
}

type candidate struct {
	node       *sourceNode
	evaluation evaluation
}

// Apply resolves document against one decoded save and returns ordered output.
func Apply(properties palsav.Properties, document *Document, options ApplyOptions) (*Output, []Diagnostic, error) {
	if document == nil || document.Shape == nil {
		return nil, nil, invalid("$", "nil document or shape")
	}
	root, err := normalizeSource(properties)
	if err != nil {
		return nil, nil, err
	}
	var nodes []*sourceNode
	collectCandidates(root, document.Shape.Kind, &nodes)
	if len(nodes) == 0 {
		diagnostic := missingDiagnostic(
			"$", "",
			"no decoded value has the requested top-level kind "+document.Shape.Kind.String(),
		)
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
		diagnostic := missingDiagnostic("$", "", "no candidate contains an exact requested field name")
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
			diagnostics = append(diagnostics, ambiguousDiagnostic(
				"$", "",
				"multiple incomplete candidates have the same highest compatibility score",
				paths,
				best[0].evaluation.score,
			))
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
		diagnostic := ambiguousDiagnostic(
			"$", "",
			"multiple candidates have the same highest compatibility score",
			paths,
			best[0].evaluation.score,
		)
		return nil, []Diagnostic{diagnostic}, &ResolutionError{
			Kind: "ambiguous", Path: "$", Message: diagnostic.Message,
		}
	}

	selected := best[0]
	diagnostics := append([]Diagnostic(nil), selected.evaluation.issues...)
	if options.AllowPartial {
		for index := range diagnostics {
			diagnostics[index].Severity = SeverityWarning
		}
	}
	if options.Explain {
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   SeverityInfo,
			Kind:       DiagnosticRoot,
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

func evaluate(shape *Shape, data *sourceNode, outputPath string) evaluation {
	if shape.Kind == KindNull {
		if data.kind == kindUnsupported {
			return typeIssue(shape, data, outputPath)
		}
		return evaluation{
			value:    copySourceNode(data),
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
			value: &Output{kind: data.kind, scalar: data.value},
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

func evaluateObject(shape *Shape, data *sourceNode, outputPath string) evaluation {
	result := evaluation{
		value: &Output{kind: KindObject, fields: make([]outputField, 0, len(shape.Fields))},
	}
	for _, requested := range shape.Fields {
		childOutputPath := propertyPath(outputPath, requested.Name)
		var matches []*sourceNode
		for _, field := range data.fields {
			if field.name == requested.Name {
				matches = append(matches, field.node)
			}
		}
		switch len(matches) {
		case 0:
			result.value.fields = append(result.value.fields, outputField{name: requested.Name, value: nullOutput()})
			result.issues = append(result.issues, missingDiagnostic(
				childOutputPath,
				data.path,
				fmt.Sprintf("field %q is not present", requested.Name),
			))
		case 1:
			result.evidence++
			child := evaluate(requested.Shape, matches[0], childOutputPath)
			result.value.fields = append(result.value.fields, outputField{name: requested.Name, value: child.value})
			result.score += 100 + child.score
			result.evidence += child.evidence
			result.issues = append(result.issues, child.issues...)
			result.selections = append(result.selections, Diagnostic{
				Severity:   SeverityInfo,
				Kind:       DiagnosticSelected,
				OutputPath: childOutputPath,
				SourcePath: matches[0].path,
				Message:    "matched exact serialized field name",
			})
			result.selections = append(result.selections, child.selections...)
		default:
			result.evidence++
			result.value.fields = append(result.value.fields, outputField{name: requested.Name, value: nullOutput()})
			paths := make([]string, 0, len(matches))
			for _, match := range matches {
				paths = append(paths, match.path)
			}
			result.issues = append(result.issues, ambiguousDiagnostic(
				childOutputPath,
				data.path,
				fmt.Sprintf("field %q occurs more than once in the candidate object", requested.Name),
				paths,
				0,
			))
		}
	}
	return result
}

func evaluateArray(shape *Shape, data *sourceNode, outputPath string) evaluation {
	result := evaluation{
		value: &Output{kind: KindArray, items: make([]*Output, 0, len(data.items))},
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

func typeIssue(shape *Shape, data *sourceNode, outputPath string) evaluation {
	actual := data.kind.String()
	if data.kind == kindUnsupported {
		actual = "unsupported"
	}
	return evaluation{
		value: nullOutput(),
		issues: []Diagnostic{{
			Severity:   SeverityError,
			Kind:       DiagnosticIncompatible,
			OutputPath: outputPath,
			SourcePath: data.path,
			Message:    fmt.Sprintf("requested %s but decoded value is %s", shape.Kind.String(), actual),
		}},
	}
}

func copySourceNode(data *sourceNode) *Output {
	switch data.kind {
	case KindObject:
		value := &Output{kind: KindObject, fields: make([]outputField, 0, len(data.fields))}
		for _, field := range data.fields {
			value.fields = append(value.fields, outputField{name: field.name, value: copySourceNode(field.node)})
		}
		return value
	case KindArray:
		value := &Output{kind: KindArray, items: make([]*Output, 0, len(data.items))}
		for _, item := range data.items {
			value.items = append(value.items, copySourceNode(item))
		}
		return value
	case kindUnsupported:
		return nullOutput()
	default:
		return &Output{kind: data.kind, scalar: data.value}
	}
}

func collectCandidates(node *sourceNode, kind Kind, output *[]*sourceNode) {
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
	bestIssues := math.MaxInt
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
			Severity:   SeverityInfo,
			Kind:       DiagnosticRejected,
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

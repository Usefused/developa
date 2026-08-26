package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"

	goparser "developa/internal/indexer/golang"
)

const flowDescriptionBytes = 360

func reviewedFlowDescription(item SymbolDetail) (string, string) {
	description, source := flowDescription(item.Symbol)
	if source == "signature" && item.Review != nil && !item.Review.InsufficientEvidence {
		return boundedFlowDescription(item.Review.Summary), "llm_review"
	}
	return description, source
}

// Descriptions retain source prose when available. A missing comment must not
// turn a suggestive function name into an unsupported claim about behavior.
func flowDescription(symbol goparser.Symbol) (string, string) {
	if symbol.Documentation != nil && symbol.Documentation.Summary != "" {
		return boundedFlowDescription(symbol.Documentation.Summary), "source_comments"
	}
	for _, comment := range []string{symbol.Doc, symbol.Comment} {
		if paragraph := firstFlowParagraph(comment); paragraph != "" {
			return boundedFlowDescription(paragraph), "source_comment"
		}
	}
	return boundedFlowDescription(flowSignatureDescription(symbol)), "signature"
}

func firstFlowParagraph(comment string) string {
	comment = strings.ReplaceAll(comment, "\r\n", "\n")
	comment = strings.ReplaceAll(comment, "\r", "\n")
	var paragraph strings.Builder
	for line := range strings.SplitSeq(comment, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if paragraph.Len() > 0 {
				break
			}
			continue
		}
		if paragraph.Len() > 0 {
			paragraph.WriteByte(' ')
		}
		paragraph.WriteString(line)
		if paragraph.Len() > flowDescriptionBytes {
			break
		}
	}
	return paragraph.String()
}

func boundedFlowDescription(description string) string {
	description = strings.Join(strings.Fields(strings.ToValidUTF8(description, "�")), " ")
	if len(description) <= flowDescriptionBytes {
		return description
	}
	const ellipsis = "…"
	end := flowDescriptionBytes - len(ellipsis)
	for !utf8.RuneStart(description[end]) {
		end--
	}
	return strings.TrimSpace(description[:end]) + ellipsis
}

func flowSignatureDescription(symbol goparser.Symbol) string {
	switch symbol.Kind {
	case goparser.Function, goparser.Method, goparser.Closure, goparser.InterfaceMethod:
		return flowCallableDescription(symbol)
	default:
		return flowDeclarationDescription(symbol)
	}
}

func flowCallableDescription(symbol goparser.Symbol) string {
	if len(symbol.Parameters) == 0 && len(symbol.Results) == 0 {
		return "No parameters or return values."
	}
	parameters, results := "No parameters.", "No return values."
	if len(symbol.Parameters) > 0 {
		parameters = "Accepts " + flowParameterList(symbol.Parameters) + "."
	}
	if len(symbol.Results) > 0 {
		results = "Returns " + flowParameterList(symbol.Results) + "."
	}
	return parameters + " " + results
}

func flowParameterList(parameters []goparser.Parameter) string {
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		parts = append(parts, flowParameterDescription(parameter))
	}
	return strings.Join(parts, ", ")
}

func flowParameterDescription(parameter goparser.Parameter) string {
	typeName := strings.Join(strings.Fields(parameter.Type), " ")
	if typeName == "" {
		typeName = "unspecified type"
	}
	if parameter.Variadic && !strings.HasPrefix(typeName, "...") {
		typeName = "..." + typeName
	}
	if parameter.Name == "" {
		return typeName
	}
	return parameter.Name + " (" + typeName + ")"
}

func flowDeclarationDescription(symbol goparser.Symbol) string {
	switch symbol.Kind {
	case goparser.Struct:
		return flowStructDescription(len(symbol.Fields))
	case goparser.Interface:
		// Interface fields contain embeddings, while named methods are separate
		// symbols. Their count cannot establish how many methods exist.
		return "Interface declaration; method signatures are separate member records."
	case goparser.Alias:
		return flowTypeDescription("Type alias", symbol.Type)
	case goparser.NamedType:
		return flowTypeDescription("Named type", symbol.Type)
	case goparser.Field:
		return flowTypeDescription("Field", symbol.Type)
	case goparser.Constant:
		return flowTypeDescription("Constant", symbol.Type)
	case goparser.Variable:
		return flowTypeDescription("Variable", symbol.Type)
	default:
		return "Declaration details are unavailable."
	}
}

func flowStructDescription(count int) string {
	if count == 1 {
		return "Struct with 1 declared field."
	}
	return fmt.Sprintf("Struct with %d declared fields.", count)
}

func flowTypeDescription(kind, typeName string) string {
	if strings.TrimSpace(typeName) == "" {
		return kind + " declaration; type not explicitly declared."
	}
	return kind + " of type " + typeName + "."
}

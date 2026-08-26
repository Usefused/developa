// Package golang extracts source facts from captured bytes without executing code.
package golang

// SourceFile is a captured file from one source snapshot. Path is repository-relative.
type SourceFile struct {
	Path    string
	Content []byte
}

// Position uses one-based lines and UTF-8 byte columns, plus a zero-based byte offset.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset"`
}

// Span is a half-open source interval; End points just after the final byte.
type Span struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Completeness string

const (
	Complete Completeness = "complete"
	Partial  Completeness = "partial"
)

type Kind string

const (
	Function        Kind = "function"
	Method          Kind = "method"
	Struct          Kind = "struct"
	Interface       Kind = "interface"
	InterfaceMethod Kind = "interface_method"
	Alias           Kind = "alias"
	NamedType       Kind = "named_type"
	Field           Kind = "field"
	Constant        Kind = "constant"
	Variable        Kind = "variable"
	Closure         Kind = "closure"
)

// Parameter preserves grouping while expanding each named position in source order.
// An empty Name is an unnamed parameter/result; Type is syntax, not inferred typing.
type Parameter struct {
	Position int    `json:"position"`
	Group    int    `json:"group"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Variadic bool   `json:"variadic,omitempty"`
	Span     Span   `json:"span"`
}

// FieldInfo also represents embedded interface types and constraint terms.
type FieldInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Tag        string `json:"tag,omitempty"`
	TagLiteral string `json:"tag_literal,omitempty"`
	Embedded   bool   `json:"embedded"`
	Doc        string `json:"doc,omitempty"`
	Comment    string `json:"comment,omitempty"`
	Span       Span   `json:"span"`
}

// Symbol IDs are logical within the repository path/package, not cross-rename IDs.
// SourceID identifies the declaration occurrence and changes when its source changes.
type Symbol struct {
	ID              string         `json:"id"`
	SourceID        string         `json:"source_id"`
	ContentHash     string         `json:"content_hash"`
	ParentID        string         `json:"parent_id,omitempty"`
	Kind            Kind           `json:"kind"`
	Name            string         `json:"name"`
	Receiver        string         `json:"receiver,omitempty"`
	ReceiverName    string         `json:"receiver_name,omitempty"`
	Signature       string         `json:"signature"`
	Type            string         `json:"type,omitempty"`
	Values          []string       `json:"values,omitempty"`
	Visibility      string         `json:"visibility"`
	Parameters      []Parameter    `json:"parameters,omitempty"`
	Results         []Parameter    `json:"results,omitempty"`
	TypeParameters  []Parameter    `json:"type_parameters,omitempty"`
	Fields          []FieldInfo    `json:"fields,omitempty"`
	Doc             string         `json:"doc,omitempty"`
	Comment         string         `json:"comment,omitempty"`
	Documentation   *Documentation `json:"documentation,omitempty"`
	Span            Span           `json:"span"`
	Source          string         `json:"source"`
	SourceTruncated bool           `json:"source_truncated"`
}

// Documentation compiles source prose without treating comments as verified behavior.
type Documentation struct {
	Summary   string          `json:"summary"`
	Comments  []SourceComment `json:"comments"`
	Origin    string          `json:"origin"`
	Truncated bool            `json:"truncated"`
}

type SourceComment struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	Span *Span  `json:"span,omitempty"`
}

type Import struct {
	Path  string `json:"path"`
	Alias string `json:"alias,omitempty"`
	Span  Span   `json:"span"`
}

type FileBlock struct {
	Path         string       `json:"path"`
	ContentHash  string       `json:"content_hash"`
	Source       []byte       `json:"-"`
	Package      string       `json:"package"`
	Doc          string       `json:"doc,omitempty"`
	Overview     string       `json:"overview"`
	Imports      []Import     `json:"imports"`
	Symbols      []Symbol     `json:"symbols"`
	Completeness Completeness `json:"completeness"`
}

// Diagnostic messages are returned to the caller, never copied into telemetry.
type Diagnostic struct {
	Path     string   `json:"path"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Position Position `json:"position"`
}

type SkippedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Complete refers only to syntax extraction of the supplied Go files. It does
// not assert package/build completeness, resolved types, or a complete call graph.
type Result struct {
	Analysis               string                 `json:"analysis"`
	Completeness           Completeness           `json:"completeness"`
	Files                  []FileBlock            `json:"files"`
	Diagnostics            []Diagnostic           `json:"diagnostics"`
	Skipped                []SkippedFile          `json:"skipped"`
	Limitations            []string               `json:"limitations"`
	Calls                  []Call                 `json:"calls"`
	CallAnalysis           CallAnalysis           `json:"call_analysis"`
	Implementations        []Implementation       `json:"implementations"`
	ImplementationAnalysis ImplementationAnalysis `json:"implementation_analysis"`
}

// SymbolReference links evidence to a declaration in the same source snapshot.
type SymbolReference struct {
	SymbolID string `json:"symbol_id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Span     Span   `json:"span"`
}

// Implementation records a candidate declaration, never a resolved runtime call.
// Evidence distinguishes Go method-set proof from a conditional signature match.
type Implementation struct {
	Interface SymbolReference `json:"interface"`
	Method    SymbolReference `json:"method"`
	Receiver  SymbolReference `json:"receiver"`
	Target    SymbolReference `json:"target"`
	Pointer   bool            `json:"pointer"`
	Evidence  string          `json:"evidence"`
}

type ImplementationAnalysis struct {
	Status      string   `json:"status"`
	Limitations []string `json:"limitations"`
}

// Call records a source callsite. Only resolved calls have a local TargetID;
// unresolved/external/builtin records retain evidence without inventing targets.
type Call struct {
	ID              string           `json:"id"`
	CallerID        string           `json:"caller_id"`
	CallerName      string           `json:"caller_name"`
	TargetID        string           `json:"target_id"`
	TargetName      string           `json:"target_name"`
	Path            string           `json:"path"`
	Span            Span             `json:"span"`
	Resolution      string           `json:"resolution"`
	Reason          string           `json:"reason,omitempty"`
	Target          *SymbolReference `json:"target,omitempty"`
	Interface       *SymbolReference `json:"interface,omitempty"`
	InterfaceMethod *SymbolReference `json:"interface_method,omitempty"`
}

type CallAnalysis struct {
	Status      string       `json:"status"`
	Resolved    int          `json:"resolved"`
	Unresolved  int          `json:"unresolved"`
	Limitations []string     `json:"limitations"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

package codegraph

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// Parser handles AST parsing and symbol extraction
type Parser struct {
	// We could cache parsers here if needed
}

type Reference struct {
	Symbol string `json:"symbol"`
	Kind   string `json:"kind"`
}

func NewParser() *Parser {
	return &Parser{}
}

// ParseFile parses the content of a file and returns the root node of the AST
func (p *Parser) ParseFile(ctx context.Context, content []byte, langConfig *LanguageConfig) (*sitter.Node, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(langConfig.Grammar)
	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, err
	}
	return tree.RootNode(), nil
}

// ExtractReferences finds symbols used in the code (function calls, type references)
func (p *Parser) ExtractReferences(root *sitter.Node, content []byte, langName string) ([]string, error) {
	referenceDetails, err := p.ExtractReferenceDetails(root, content, langName)
	if err != nil {
		return nil, err
	}

	references := make([]string, 0, len(referenceDetails))
	for _, ref := range referenceDetails {
		references = append(references, ref.Symbol)
	}
	return uniqueStrings(references), nil
}

// ExtractReferenceDetails returns symbol references with semantic usage kind.
func (p *Parser) ExtractReferenceDetails(root *sitter.Node, content []byte, langName string) ([]Reference, error) {
	type referenceQuery struct {
		Kind  string
		Query string
	}

	var queries []referenceQuery
	switch langName {
	case "go":
		queries = []referenceQuery{
			{Kind: "call", Query: `(call_expression function: (identifier) @ref)`},
			{Kind: "method_call", Query: `(call_expression function: (selector_expression field: (field_identifier) @ref))`},
			{Kind: "type_ref", Query: `(type_identifier) @ref`},
		}
	case "python":
		queries = []referenceQuery{
			{Kind: "call", Query: `(call function: (identifier) @ref)`},
			{Kind: "method_call", Query: `(call function: (attribute attribute: (identifier) @ref))`},
			{Kind: "type_ref", Query: `(type (identifier) @ref)`},
		}
	case "javascript":
		queries = []referenceQuery{
			{Kind: "call", Query: `(call_expression function: (identifier) @ref)`},
			{Kind: "method_call", Query: `(call_expression function: (member_expression property: (property_identifier) @ref))`},
		}
	case "typescript":
		queries = []referenceQuery{
			{Kind: "call", Query: `(call_expression function: (identifier) @ref)`},
			{Kind: "method_call", Query: `(call_expression function: (member_expression property: (property_identifier) @ref))`},
			{Kind: "type_ref", Query: `(type_identifier) @ref`},
		}
	case "java":
		queries = []referenceQuery{
			{Kind: "method_call", Query: `(method_invocation name: (identifier) @ref)`},
			{Kind: "constructor_call", Query: `(object_creation_expression type: (type_identifier) @ref)`},
			{Kind: "type_ref", Query: `(type_identifier) @ref`},
		}
	default:
		return nil, fmt.Errorf("unsupported language for references: %s", langName)
	}

	langConfig, ok := SupportedLanguages[langName]
	if !ok {
		return nil, fmt.Errorf("unknown language: %s", langName)
	}

	var references []Reference
	seen := make(map[string]struct{})

	for _, refQuery := range queries {
		q, err := sitter.NewQuery([]byte(refQuery.Query), langConfig.Grammar)
		if err != nil {
			return nil, fmt.Errorf("invalid query for %s (%s): %v", langName, refQuery.Kind, err)
		}

		qc := sitter.NewQueryCursor()
		qc.Exec(q, root)

		for {
			m, ok := qc.NextMatch()
			if !ok {
				break
			}
			for _, capture := range m.Captures {
				symbol := capture.Node.Content(content)
				if symbol == "" {
					continue
				}

				key := refQuery.Kind + "\x00" + symbol
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				references = append(references, Reference{Symbol: symbol, Kind: refQuery.Kind})
			}
		}

		qc.Close()
		q.Close()
	}

	return references, nil
}

// ExtractDefinitions finds symbols defined in the code (functions, classes, types)
func (p *Parser) ExtractDefinitions(root *sitter.Node, content []byte, langName string) ([]string, error) {
	var definitions []string
	var queryStr string

	switch langName {
	case "go":
		queryStr = `
			(function_declaration name: (identifier) @func_def)
			(method_declaration name: (field_identifier) @method_def)
			(type_spec name: (type_identifier) @type_def)
		`
	case "python":
		queryStr = `
			(function_definition name: (identifier) @func_def)
			(class_definition name: (identifier) @class_def)
		`
	case "javascript":
		queryStr = `
			(function_declaration name: (identifier) @func_def)
			(class_declaration name: (identifier) @class_def)
			(method_definition name: (property_identifier) @method_def)
			(variable_declarator name: (identifier) @var_def)
		`
	case "typescript":
		queryStr = `
			(function_declaration name: (identifier) @func_def)
			(class_declaration name: (type_identifier) @class_def)
			(method_definition name: (property_identifier) @method_def)
			(variable_declarator name: (identifier) @var_def)
		`
	case "java":
		queryStr = `
			(method_declaration name: (identifier) @method_def)
			(class_declaration name: (identifier) @class_def)
			(interface_declaration name: (identifier) @interface_def)
		`
	default:
		return nil, fmt.Errorf("unsupported language for definitions: %s", langName)
	}

	langConfig, ok := SupportedLanguages[langName]
	if !ok {
		return nil, fmt.Errorf("unknown language: %s", langName)
	}

	q, err := sitter.NewQuery([]byte(queryStr), langConfig.Grammar)
	if err != nil {
		return nil, fmt.Errorf("invalid query for %s: %v", langName, err)
	}
	defer q.Close()

	qc := sitter.NewQueryCursor()
	defer qc.Close()

	qc.Exec(q, root)

	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, capture := range m.Captures {
			node := capture.Node
			symbol := node.Content(content)
			if symbol != "" {
				definitions = append(definitions, symbol)
			}
		}
	}

	return uniqueStrings(definitions), nil
}

// ExtractDefinitionEntries returns definitions with line numbers and kinds for graph persistence.
func (p *Parser) ExtractDefinitionEntries(root *sitter.Node, content []byte, langName string) ([]Definition, error) {
	type defQuery struct {
		Kind  string // "function", "method", "type", "class", "interface"
		Query string
	}

	var queries []defQuery
	switch langName {
	case "go":
		queries = []defQuery{
			{Kind: "function", Query: `(function_declaration name: (identifier) @def)`},
			{Kind: "method", Query: `(method_declaration name: (field_identifier) @def)`},
			{Kind: "type", Query: `(type_spec name: (type_identifier) @def)`},
		}
	case "python":
		queries = []defQuery{
			{Kind: "function", Query: `(function_definition name: (identifier) @def)`},
			{Kind: "class", Query: `(class_definition name: (identifier) @def)`},
		}
	case "javascript":
		queries = []defQuery{
			{Kind: "function", Query: `(function_declaration name: (identifier) @def)`},
			{Kind: "class", Query: `(class_declaration name: (identifier) @def)`},
			{Kind: "method", Query: `(method_definition name: (property_identifier) @def)`},
			{Kind: "function", Query: `(variable_declarator name: (identifier) @def)`},
		}
	case "typescript":
		queries = []defQuery{
			{Kind: "function", Query: `(function_declaration name: (identifier) @def)`},
			{Kind: "class", Query: `(class_declaration name: (type_identifier) @def)`},
			{Kind: "method", Query: `(method_definition name: (property_identifier) @def)`},
			{Kind: "function", Query: `(variable_declarator name: (identifier) @def)`},
		}
	case "java":
		queries = []defQuery{
			{Kind: "method", Query: `(method_declaration name: (identifier) @def)`},
			{Kind: "class", Query: `(class_declaration name: (identifier) @def)`},
			{Kind: "interface", Query: `(interface_declaration name: (identifier) @def)`},
		}
	default:
		return nil, fmt.Errorf("unsupported language for definitions: %s", langName)
	}

	langConfig, ok := SupportedLanguages[langName]
	if !ok {
		return nil, fmt.Errorf("unknown language: %s", langName)
	}

	var defs []Definition
	seen := make(map[string]struct{})

	for _, dq := range queries {
		q, err := sitter.NewQuery([]byte(dq.Query), langConfig.Grammar)
		if err != nil {
			return nil, fmt.Errorf("invalid query for %s (%s): %v", langName, dq.Kind, err)
		}

		qc := sitter.NewQueryCursor()
		qc.Exec(q, root)

		for {
			m, ok := qc.NextMatch()
			if !ok {
				break
			}
			for _, capture := range m.Captures {
				node := capture.Node
				symbol := node.Content(content)
				if symbol == "" {
					continue
				}
				if _, exists := seen[symbol]; exists {
					continue
				}
				seen[symbol] = struct{}{}

				// Get the parent node to compute the end line of the full definition
				parent := node.Parent()
				if parent == nil {
					parent = node
				}
				if langName == "go" && parent.Type() == "type_spec" {
					grandparent := parent.Parent()
					if grandparent != nil && grandparent.Type() == "type_declaration" {
						parent = grandparent
					}
				}

				endLine := int(parent.EndPoint().Row) + 1 // tree-sitter is 0-indexed

				defs = append(defs, Definition{
					Symbol:  symbol,
					Kind:    dq.Kind,
					Line:    int(node.StartPoint().Row) + 1,
					EndLine: endLine,
				})
			}
		}

		qc.Close()
		q.Close()
	}

	return defs, nil
}

// ExtractDefinitionSnippet finds the definition of a specific symbol and returns
// only the source text of that definition (function, type, class, etc.) — not the entire file.
func (p *Parser) ExtractDefinitionSnippet(root *sitter.Node, content []byte, langName string, targetSymbol string) (string, error) {
	var queryStr string

	switch langName {
	case "go":
		queryStr = `
			(function_declaration name: (identifier) @func_def)
			(method_declaration name: (field_identifier) @method_def)
			(type_spec name: (type_identifier) @type_def)
		`
	case "python":
		queryStr = `
			(function_definition name: (identifier) @func_def)
			(class_definition name: (identifier) @class_def)
		`
	case "javascript":
		queryStr = `
			(function_declaration name: (identifier) @func_def)
			(class_declaration name: (identifier) @class_def)
			(method_definition name: (property_identifier) @method_def)
			(variable_declarator name: (identifier) @var_def)
		`
	case "typescript":
		queryStr = `
			(function_declaration name: (identifier) @func_def)
			(class_declaration name: (type_identifier) @class_def)
			(method_definition name: (property_identifier) @method_def)
			(variable_declarator name: (identifier) @var_def)
		`
	case "java":
		queryStr = `
			(method_declaration name: (identifier) @method_def)
			(class_declaration name: (identifier) @class_def)
			(interface_declaration name: (identifier) @interface_def)
		`
	default:
		return "", fmt.Errorf("unsupported language: %s", langName)
	}

	langConfig, ok := SupportedLanguages[langName]
	if !ok {
		return "", fmt.Errorf("unknown language: %s", langName)
	}

	q, err := sitter.NewQuery([]byte(queryStr), langConfig.Grammar)
	if err != nil {
		return "", fmt.Errorf("invalid query for %s: %v", langName, err)
	}
	defer q.Close()

	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, root)

	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, capture := range m.Captures {
			node := capture.Node
			symbol := node.Content(content)
			if symbol == targetSymbol {
				// Found the definition — extract the PARENT node's text
				// The parent is the actual declaration (function_declaration, type_spec, etc.)
				parent := node.Parent()
				if parent == nil {
					parent = node
				}

				// For Go type_spec, go up one more level to get the full type declaration
				if langName == "go" && parent.Type() == "type_spec" {
					grandparent := parent.Parent()
					if grandparent != nil && grandparent.Type() == "type_declaration" {
						parent = grandparent
					}
				}

				snippet := parent.Content(content)
				// Cap individual snippet to 3000 chars to prevent huge functions
				if len(snippet) > 3000 {
					snippet = snippet[:3000] + "\n// ... (truncated)"
				}
				return snippet, nil
			}
		}
	}

	return "", nil // Not found
}

// ExtractImports extracts import paths from a source file's AST.
// Returns a list of import path strings (e.g., "fmt", "os/exec", "code-review/backend/internal/llm").
func (p *Parser) ExtractImports(root *sitter.Node, content []byte, langName string) []string {
	var queryStr string
	switch langName {
	case "go":
		queryStr = `(import_spec path: (interpreted_string_literal) @import_path)`
	case "python":
		queryStr = `(import_from_statement module_name: (dotted_name) @import_path)`
	case "javascript", "typescript":
		queryStr = `(import_statement source: (string) @import_path)`
	case "java":
		queryStr = `(import_declaration (scoped_identifier) @import_path)`
	default:
		return nil
	}

	langConfig, ok := SupportedLanguages[langName]
	if !ok {
		return nil
	}

	q, err := sitter.NewQuery([]byte(queryStr), langConfig.Grammar)
	if err != nil {
		return nil
	}
	defer q.Close()

	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, root)

	var imports []string
	seen := make(map[string]struct{})
	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, capture := range m.Captures {
			importPath := capture.Node.Content(content)
			// Strip quotes from Go/JS/TS imports: "fmt" -> fmt
			importPath = strings.Trim(importPath, "\"'`")
			if importPath == "" {
				continue
			}
			if _, exists := seen[importPath]; exists {
				continue
			}
			seen[importPath] = struct{}{}
			imports = append(imports, importPath)
		}
	}
	return imports
}

func uniqueStrings(slice []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range slice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

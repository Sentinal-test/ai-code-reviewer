package codegraph

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

// Parser handles AST parsing and symbol extraction
type Parser struct {
	// We could cache parsers here if needed
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
	var references []string
	// query will be language specific
	var queryStr string

	switch langName {
	case "go":
		queryStr = `
			(call_expression function: (identifier) @call)
			(call_expression function: (selector_expression field: (field_identifier) @method_call))
			(type_identifier) @type_ref
		`
	case "python":
		queryStr = `
			(call function: (identifier) @call)
			(call function: (attribute attribute: (identifier) @method_call))
			(type (identifier) @type_ref)
		`
	case "javascript":
		queryStr = `
			(call_expression function: (identifier) @call)
			(call_expression function: (member_expression property: (property_identifier) @method_call))
		`
	case "typescript":
		queryStr = `
			(call_expression function: (identifier) @call)
			(call_expression function: (member_expression property: (property_identifier) @method_call))
			(type_identifier) @type_ref
		`
	case "java":
		queryStr = `
			(method_invocation name: (identifier) @method_call)
			(object_creation_expression type: (type_identifier) @constructor_call)
			(type_identifier) @type_ref
		`
	default:
		return nil, fmt.Errorf("unsupported language for references: %s", langName)
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
			// Extract the text of the captured node
			node := capture.Node
			symbol := node.Content(content)
			if symbol != "" {
				references = append(references, symbol)
			}
		}
	}

	return uniqueStrings(references), nil
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

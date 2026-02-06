package analysis

import (
	"context"
	"fmt"
	"os"

	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

type SymbolType string

const (
	SymbolFunction  SymbolType = "function"
	SymbolStruct    SymbolType = "struct"
	SymbolInterface SymbolType = "interface"
	SymbolTypeAlias SymbolType = "type"
	SymbolClass     SymbolType = "class"
)

type Symbol struct {
	Name      string
	Type      SymbolType
	File      string
	LineStart int
	LineEnd   int
	Signature string
	Body      string
}

type Extractor struct {
	parser *sitter.Parser
}

func NewExtractor() *Extractor {
	p := sitter.NewParser()
	return &Extractor{parser: p}
}

func (e *Extractor) ExtractSymbols(ctx context.Context, filePath string) ([]Symbol, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	var lang *sitter.Language
	var queryStr string

	switch ext {
	case ".go":
		lang = golang.GetLanguage()
		queryStr = `
			(function_declaration name: (identifier) @name) @func
			(method_declaration name: (field_identifier) @name) @method
			(type_declaration (type_spec name: (type_identifier) @name type: (struct_type))) @struct
			(type_declaration (type_spec name: (type_identifier) @name type: (interface_type))) @interface
		`
	case ".py":
		lang = python.GetLanguage()
		queryStr = `
			(function_definition name: (identifier) @name) @func
			(class_definition name: (identifier) @name) @class
		`
	case ".js", ".jsx", ".ts", ".tsx":
		if ext == ".ts" || ext == ".tsx" {
			lang = typescript.GetLanguage()
		} else {
			// fallback to TS grammar which is usually compatible or use JS
			lang = tsx.GetLanguage()
		}
		queryStr = `
			(function_declaration name: (identifier) @name) @func
			(class_declaration name: (identifier) @name) @class
			(method_definition name: (property_identifier) @name) @method
			(interface_declaration name: (type_identifier) @name) @interface
		`
	default:
		return nil, nil // Unsupported language
	}

	e.parser.SetLanguage(lang)
	tree, err := e.parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	n := tree.RootNode()
	symbols := []Symbol{}

	q, err := sitter.NewQuery([]byte(queryStr), lang)
	if err != nil {
		return nil, fmt.Errorf("query error: %v", err)
	}
	defer q.Close()

	qc := sitter.NewQueryCursor()
	defer qc.Close()

	qc.Exec(q, n)

	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}

		var sym Symbol
		sym.File = filePath

		for _, cap := range m.Captures {
			nodeName := q.CaptureNameForId(cap.Index)
			node := cap.Node

			switch nodeName {
			case "name":
				sym.Name = node.Content(content)
			case "func", "method":
				sym.Type = SymbolFunction
				sym.Signature = getSignature(node, content)
				sym.Body = node.Content(content)
				sym.LineStart = int(node.StartPoint().Row) + 1
				sym.LineEnd = int(node.EndPoint().Row) + 1
			case "struct":
				sym.Type = SymbolStruct
				sym.Signature = node.Content(content)
				sym.Body = node.Content(content)
				sym.LineStart = int(node.StartPoint().Row) + 1
				sym.LineEnd = int(node.EndPoint().Row) + 1
			case "class":
				sym.Type = SymbolClass
				sym.Signature = getSignature(node, content)
				sym.Body = node.Content(content)
				sym.LineStart = int(node.StartPoint().Row) + 1
				sym.LineEnd = int(node.EndPoint().Row) + 1
			case "interface":
				sym.Type = SymbolInterface
				sym.Signature = node.Content(content)
				sym.Body = node.Content(content)
				sym.LineStart = int(node.StartPoint().Row) + 1
				sym.LineEnd = int(node.EndPoint().Row) + 1
			}
		}

		if sym.Name != "" {
			symbols = append(symbols, sym)
		}
	}

	return symbols, nil
}

// getSignature extracts just the signature part (header) of a function/method
func getSignature(n *sitter.Node, content []byte) string {
	// Simple heuristic: take everything until the first "{"
	full := n.Content(content)
	if idx := findFirstBrace(full); idx != -1 {
		return full[:idx]
	}
	return full
}

func findFirstBrace(s string) int {
	for i, r := range s {
		if r == '{' {
			return i
		}
	}
	return -1
}

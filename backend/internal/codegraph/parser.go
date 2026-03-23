package codegraph

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

var (
	goImportRe        = regexp.MustCompile(`(?m)^\s*(?:import\s+)?(?:(\w+)\s+)?"([^"]+)"`)
	pyImportRe        = regexp.MustCompile(`(?m)^\s*import\s+([A-Za-z0-9_.,\s]+)$`)
	pyFromImportRe    = regexp.MustCompile(`(?m)^\s*from\s+([A-Za-z0-9_\.]+)\s+import\s+([A-Za-z0-9_,\s\*()]+)`)
	jsNsImportRe      = regexp.MustCompile(`(?m)^\s*import\s+\*\s+as\s+([A-Za-z0-9_$]+)\s+from\s+['"]([^'"]+)['"]`)
	jsDefaultImportRe = regexp.MustCompile(`(?m)^\s*import\s+([A-Za-z0-9_$]+)\s*(?:,\s*\{[^}]+\})?\s+from\s+['"]([^'"]+)['"]`)
	jsNamedImportRe   = regexp.MustCompile(`(?m)^\s*import\s+(?:[A-Za-z0-9_$]+\s*,\s*)?\{([^}]+)\}\s+from\s+['"]([^'"]+)['"]`)
	javaImportRe      = regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([A-Za-z0-9_.]+)\s*;`)
	goOwnerRe         = regexp.MustCompile(`func\s*\(\s*[^)]*\*?([A-Za-z_][A-Za-z0-9_]*)\s*\)\s+[A-Za-z_][A-Za-z0-9_]*`)
)

// Parser handles AST parsing and symbol extraction
type Parser struct {
	// We could cache parsers here if needed
}

type Reference struct {
	Symbol            string `json:"symbol"`
	Kind              string `json:"kind"`
	Line              int    `json:"line,omitempty"`
	Qualifier         string `json:"qualifier,omitempty"`
	ScopeSymbol       string `json:"scope_symbol,omitempty"`
	ScopeQualified    string `json:"scope_qualified,omitempty"`
	ResolvedSymbol    string `json:"resolved_symbol,omitempty"`
	ResolvedQualified string `json:"resolved_qualified,omitempty"`
	ResolvedFile      string `json:"resolved_file,omitempty"`
}

type ImportSpec struct {
	Path  string `json:"path"`
	Alias string `json:"alias"`
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
	return p.extractReferenceDetails(root, content, langName, nil)
}

// ExtractReferenceDetailsWithDefinitions annotates references with their enclosing symbol.
func (p *Parser) ExtractReferenceDetailsWithDefinitions(root *sitter.Node, content []byte, langName string, defs []Definition) ([]Reference, error) {
	return p.extractReferenceDetails(root, content, langName, defs)
}

func (p *Parser) extractReferenceDetails(root *sitter.Node, content []byte, langName string, defs []Definition) ([]Reference, error) {
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
				node := capture.Node
				symbol := node.Content(content)
				if symbol == "" {
					continue
				}

				line := int(node.StartPoint().Row) + 1
				qualifier := extractQualifier(node, content, langName)
				scope := findEnclosingDefinition(defs, line)

				key := strings.Join([]string{
					refQuery.Kind,
					symbol,
					qualifier,
					fmt.Sprintf("%d", line),
					scope.QualifiedSymbol,
				}, "\x00")
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				references = append(references, Reference{
					Symbol:         symbol,
					Kind:           refQuery.Kind,
					Line:           line,
					Qualifier:      qualifier,
					ScopeSymbol:    scope.Symbol,
					ScopeQualified: scope.QualifiedSymbol,
				})
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
func (p *Parser) ExtractDefinitionEntries(root *sitter.Node, content []byte, langName string, filePath string) ([]Definition, error) {
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
	namespace := p.ExtractNamespace(root, content, langName, filePath)

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
				owner := extractDefinitionOwner(node, content, langName, dq.Kind)
				qualified := buildQualifiedSymbol(namespace, owner, symbol)

				defs = append(defs, Definition{
					Symbol:          symbol,
					QualifiedSymbol: qualified,
					Package:         namespace,
					Owner:           owner,
					Kind:            dq.Kind,
					Line:            int(node.StartPoint().Row) + 1,
					EndLine:         endLine,
				})
			}
		}

		qc.Close()
		q.Close()
	}

	return defs, nil
}

// ExtractNamespace returns the package/module namespace for the file.
func (p *Parser) ExtractNamespace(root *sitter.Node, content []byte, langName, filePath string) string {
	switch langName {
	case "go":
		if pkg := querySingleCapture(root, content, SupportedLanguages["go"].Grammar, `(package_clause (package_identifier) @pkg)`); pkg != "" {
			return pkg
		}
	case "java":
		if pkg := querySingleCapture(root, content, SupportedLanguages["java"].Grammar, `(package_declaration (scoped_identifier) @pkg)`); pkg != "" {
			return pkg
		}
	}
	return namespaceFromPath(filePath)
}

// ExtractImportSpecs returns imports with their local aliases/bindings.
func (p *Parser) ExtractImportSpecs(content []byte, langName string) []ImportSpec {
	text := string(content)
	var specs []ImportSpec
	seen := make(map[string]struct{})
	add := func(path, alias string) {
		path = normalizeImportPath(langName, path)
		alias = strings.TrimSpace(alias)
		if path == "" {
			return
		}
		if alias == "" {
			alias = lastSegment(path)
		}
		key := path + "\x00" + alias
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		specs = append(specs, ImportSpec{Path: path, Alias: alias})
	}

	switch langName {
	case "go":
		matches := goImportRe.FindAllStringSubmatch(text, -1)
		for _, m := range matches {
			add(m[2], m[1])
		}
	case "python":
		for _, m := range pyImportRe.FindAllStringSubmatch(text, -1) {
			parts := strings.Split(m[1], ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				chunks := strings.Split(part, " as ")
				path := strings.TrimSpace(chunks[0])
				alias := ""
				if len(chunks) > 1 {
					alias = strings.TrimSpace(chunks[1])
				}
				add(path, alias)
			}
		}
		for _, m := range pyFromImportRe.FindAllStringSubmatch(text, -1) {
			modulePath := strings.TrimSpace(m[1])
			// Support multi-line parenthesized imports by cleaning up m[2]
			importContent := m[2]
			importContent = strings.ReplaceAll(importContent, "(", "")
			importContent = strings.ReplaceAll(importContent, ")", "")
			importContent = strings.ReplaceAll(importContent, "\n", " ")

			parts := strings.Split(importContent, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" || part == "*" {
					continue
				}
				chunks := strings.Split(part, " as ")
				symbol := strings.TrimSpace(chunks[0])
				alias := symbol
				if len(chunks) > 1 {
					alias = strings.TrimSpace(chunks[1])
				}
				add(modulePath+"."+symbol, alias)
			}
		}
	case "javascript", "typescript":
		for _, m := range jsNsImportRe.FindAllStringSubmatch(text, -1) {
			add(m[2], m[1])
		}
		for _, m := range jsDefaultImportRe.FindAllStringSubmatch(text, -1) {
			add(m[2], m[1])
		}
		for _, m := range jsNamedImportRe.FindAllStringSubmatch(text, -1) {
			for _, part := range strings.Split(m[1], ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				chunks := strings.Split(part, " as ")
				alias := strings.TrimSpace(chunks[0])
				if len(chunks) > 1 {
					alias = strings.TrimSpace(chunks[1])
				}
				add(m[2], alias)
			}
		}
	case "java":
		for _, m := range javaImportRe.FindAllStringSubmatch(text, -1) {
			add(m[1], lastSegment(m[1]))
		}
	}

	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Alias == specs[j].Alias {
			return specs[i].Path < specs[j].Path
		}
		return specs[i].Alias < specs[j].Alias
	})
	return specs
}

func querySingleCapture(root *sitter.Node, content []byte, grammar *sitter.Language, query string) string {
	if root == nil || grammar == nil {
		return ""
	}
	q, err := sitter.NewQuery([]byte(query), grammar)
	if err != nil {
		return ""
	}
	defer q.Close()
	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, root)
	if m, ok := qc.NextMatch(); ok {
		for _, capture := range m.Captures {
			value := strings.TrimSpace(capture.Node.Content(content))
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func buildQualifiedSymbol(namespace, owner, symbol string) string {
	base := strings.Trim(strings.Join([]string{namespace, owner}, "."), ".")
	if base == "" {
		return symbol
	}
	if symbol == "" {
		return base
	}
	return base + "." + symbol
}

func extractDefinitionOwner(node *sitter.Node, content []byte, langName, kind string) string {
	if node == nil {
		return ""
	}

	switch langName {
	case "go":
		if kind == "method" {
			parent := node.Parent()
			if parent != nil {
				methodText := parent.Content(content)
				if m := goOwnerRe.FindStringSubmatch(methodText); len(m) == 2 {
					return m[1]
				}
			}
		}
	case "python", "javascript", "typescript", "java":
		for current := node.Parent(); current != nil; current = current.Parent() {
			switch current.Type() {
			case "class_definition", "class_declaration", "interface_declaration":
				for i := 0; i < int(current.NamedChildCount()); i++ {
					child := current.NamedChild(i)
					switch child.Type() {
					case "identifier", "type_identifier":
						return child.Content(content)
					}
				}
			}
		}
	}
	return ""
}

func extractQualifier(node *sitter.Node, content []byte, langName string) string {
	if node == nil {
		return ""
	}
	parent := node.Parent()
	if parent == nil {
		return ""
	}

	switch langName {
	case "go":
		if parent.Type() == "selector_expression" {
			if operand := parent.ChildByFieldName("operand"); operand != nil {
				return operand.Content(content)
			}
		}
	case "python":
		if parent.Type() == "attribute" {
			if value := parent.ChildByFieldName("object"); value != nil {
				return value.Content(content)
			}
			if value := parent.ChildByFieldName("value"); value != nil {
				return value.Content(content)
			}
		}
	case "javascript", "typescript":
		if parent.Type() == "member_expression" {
			if value := parent.ChildByFieldName("object"); value != nil {
				return value.Content(content)
			}
		}
	case "java":
		if parent.Type() == "method_invocation" {
			if value := parent.ChildByFieldName("object"); value != nil {
				return value.Content(content)
			}
		}
	}
	return ""
}

func findEnclosingDefinition(defs []Definition, line int) Definition {
	var best Definition
	bestSpan := 0
	for _, def := range defs {
		if line < def.Line || (def.EndLine > 0 && line > def.EndLine) {
			continue
		}
		span := def.EndLine - def.Line
		if best.QualifiedSymbol == "" || span < bestSpan {
			best = def
			bestSpan = span
		}
	}
	return best
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
	var imports []string
	for _, spec := range p.ExtractImportSpecs(content, langName) {
		imports = append(imports, spec.Path)
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

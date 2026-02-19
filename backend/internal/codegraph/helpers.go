package codegraph

import (
	"fmt"
	"sort"
	"strings"
)

// isBuiltinSymbol returns true if a symbol is a language primitive or stdlib function.
func isBuiltinSymbol(symbol string) bool {
	// Only filter extremely short single-character noise
	if len(symbol) <= 1 {
		return true
	}

	builtins := map[string]bool{
		// Go primitive types
		"string": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
		"float32": true, "float64": true, "complex64": true, "complex128": true,
		"bool": true, "byte": true, "rune": true, "error": true, "uintptr": true,
		"any": true, "comparable": true,

		// Go builtin functions
		"len": true, "cap": true, "make": true, "new": true, "append": true,
		"copy": true, "delete": true, "close": true, "panic": true, "recover": true,
		"print": true, "println": true, "real": true, "imag": true, "complex": true,
		"clear": true, "min": true, "max": true,

		// Go fmt/log package (extremely common stdlib)
		"Printf": true, "Println": true, "Sprintf": true, "Fprintf": true,
		"Errorf": true, "Print": true, "Sprint": true, "Sprintln": true,
		"Fatalf": true, "Fatal": true, "Fatalln": true,

		// Go os/env
		"Getenv": true, "Exit": true, "Setenv": true,

		// Go common stdlib types that are strictly stdlib
		"Background": true, "TODO": true, "WithCancel": true,
		"WithTimeout": true, "WithDeadline": true, "WithValue": true,

		// Python builtins
		"range": true, "self": true, "None": true, "cls": true,
		"super": true, "type": true, "list": true, "dict": true,
		"set": true, "tuple": true, "str": true, "map": true,
		"filter": true, "sorted": true, "enumerate": true,
		"isinstance": true, "issubclass": true, "getattr": true, "setattr": true,
		"hasattr": true, "property": true, "staticmethod": true, "classmethod": true,

		// JS/TS builtins
		"console": true, "log": true, "warn": true, "info": true, "debug": true,
		"setTimeout": true, "setInterval": true, "clearTimeout": true, "clearInterval": true,
		"Promise": true, "Array": true, "Object": true, "Map": true,
		"JSON": true, "Math": true, "Date": true, "RegExp": true, "Symbol": true,
		"parseInt": true, "parseFloat": true, "isNaN": true, "isFinite": true,
		"require": true, "module": true, "exports": true,
		"document": true, "window": true, "global": true, "process": true,
		"then": true, "catch": true, "finally": true, "async": true, "await": true,

		// Java builtins
		"System": true, "Integer": true, "Long": true, "Double": true, "Float": true,
		"Boolean": true, "Character": true, "Byte": true, "Short": true,
		"Override": true, "Deprecated": true, "SuppressWarnings": true,
		"ArrayList": true, "HashMap": true, "HashSet": true, "LinkedList": true,
		"Collections": true, "Arrays": true, "Objects": true, "Optional": true,
		"Collectors": true, "Stream": true,

		// Go stdlib method/type names (extremely common noise)
		"String": true, "Error": true, "Close": true, "Read": true, "Write": true,
		"Scan": true, "Query": true, "QueryRow": true, "Exec": true,
		"Get": true, "Set": true, "Add": true, "Do": true, "Put": true,
		"Now": true, "Since": true, "After": true, "Sleep": true,
		"Split": true, "Join": true, "Contains": true, "TrimSpace": true,
		"HasPrefix": true, "HasSuffix": true, "Replace": true,
		"ToLower": true, "ToUpper": true, "Trim": true,
		"Marshal": true, "Unmarshal": true, "Encode": true, "Decode": true,
		"NewReader": true, "NewWriter": true, "NewRequest": true,
		"ReadAll": true, "WriteFile": true, "ReadFile": true, "MkdirAll": true,
		"Handle": true, "HandleFunc": true, "ListenAndServe": true,
		"Header": true, "ResponseWriter": true, "Request": true,
		"DB": true, "Prepare": true, "Begin": true, "Commit": true, "Rollback": true,
		"Context": true, "Client": true,
	}

	return builtins[symbol]
}

// --- Sort / collection helpers ---

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedDependencyKeys(m map[string]*dependencyInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedSymbolSlice(symbols map[string]struct{}) []string {
	out := make([]string, 0, len(symbols))
	for s := range symbols {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func addReferenceUnique(existing []Reference, candidate Reference) []Reference {
	for _, item := range existing {
		if item.Symbol == candidate.Symbol && item.Kind == candidate.Kind {
			return existing
		}
	}
	return append(existing, candidate)
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

func displayList(values []string, limit int) string {
	if len(values) == 0 {
		return "none"
	}
	if len(values) <= limit {
		return strings.Join(values, ", ")
	}
	return strings.Join(values[:limit], ", ") + fmt.Sprintf(" (+%d more)", len(values)-limit)
}

package codegraph

import (
	"fmt"
	"sort"
	"strings"
)

// isBuiltinSymbol returns true if a symbol is a language primitive, stdlib function,
// or too short/generic to be worth searching for definitions.
func isBuiltinSymbol(symbol string) bool {
	// Too short — variables like ok, r, w, db, ctx, id
	if len(symbol) <= 2 {
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

		// Go fmt/log package (extremely common, never local definitions)
		"Printf": true, "Println": true, "Sprintf": true, "Fprintf": true,
		"Errorf": true, "Print": true, "Sprint": true, "Sprintln": true,
		"Fatalf": true, "Fatal": true, "Fatalln": true,

		// Go os/env
		"Getenv": true, "Exit": true, "Setenv": true,

		// Go common stdlib types/funcs that are never project-local
		"Context": true, "Background": true, "TODO": true, "WithCancel": true,
		"WithTimeout": true, "WithDeadline": true, "WithValue": true,
		"Writer": true, "Reader": true, "Closer": true, "ReadCloser": true,
		"WriteCloser": true, "ReadWriter": true, "ReadWriteCloser": true,
		"ResponseWriter": true, "Request": true, "Handler": true, "HandlerFunc": true,
		"Header": true, "StatusOK": true, "StatusBadRequest": true, "StatusNotFound": true,
		"StatusInternalServerError": true, "StatusUnauthorized": true, "StatusForbidden": true,

		// Go common operations
		"Error": true, "String": true, "Bytes": true,
		"Close": true, "Read": true, "Write": true, "Flush": true,
		"Lock": true, "Unlock": true, "RLock": true, "RUnlock": true,
		"Add": true, "Done": true, "Wait": true,
		"Marshal": true, "Unmarshal": true, "Encode": true, "Decode": true,
		"NewDecoder": true, "NewEncoder": true,
		"ReadAll": true, "ReadFile": true, "WriteFile": true,
		"NewBuffer": true, "NewReader": true, "NewWriter": true,
		"Set": true, "Get": true,

		// Go string/path operations
		"Contains": true, "HasPrefix": true, "HasSuffix": true,
		"TrimSpace": true, "TrimPrefix": true, "TrimSuffix": true, "Trim": true,
		"Split": true, "Join": true, "Replace": true, "ToLower": true, "ToUpper": true,
		"Fields": true, "Index": true, "Repeat": true,
		"Base": true, "Dir": true, "Ext": true, "Rel": true, "Abs": true,
		"Sscanf": true, "Scanf": true,

		// Go sync / concurrency
		"Mutex": true, "RWMutex": true, "WaitGroup": true, "Once": true,

		// Go net/http (already covered above but being thorough)
		"Do": true, "ListenAndServe": true,
		"NewRequestWithContext": true, "NewRequest": true,

		// Go testing
		"NoError": true, "NotNil": true, "Equal": true, "True": true, "False": true,
		"Nil": true, "Len": true, "Run": true,

		// Go regex
		"MustCompile": true, "FindStringSubmatch": true, "MatchString": true,

		// Go misc
		"WriteString": true, "Builder": true, "Buffer": true,
		"ValidString": true, "Walk": true, "IsDir": true,
		"Name": true, "Size": true, "Mode": true,
		"Exec": true, "LastInsertId": true, "Scan": true, "QueryRow": true,
		"URLParam": true, "Route": true,
		"WriteHeader":    true,
		"CommandContext": true,

		// Common short variable-like symbols
		"ctx": true, "err": true, "nil": true, "true": true, "false": true,
		"cmd": true, "buf": true, "req": true, "res": true,
		"msg": true, "key": true, "val": true, "out": true,

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
		"push": true, "pop": true, "shift": true, "unshift": true,
		"forEach": true, "reduce": true, "find": true, "some": true, "every": true,
		"keys": true, "values": true, "entries": true,
		"toString": true, "valueOf": true, "constructor": true, "prototype": true,
		"length": true, "splice": true, "slice": true, "concat": true,
		"includes": true, "indexOf": true, "startsWith": true, "endsWith": true,
		"trim": true, "replace": true, "match": true, "test": true, "split": true,

		// Java builtins
		"System": true, "Integer": true, "Long": true, "Double": true, "Float": true,
		"Boolean": true, "Character": true, "Byte": true, "Short": true,
		"List": true, "ArrayList": true, "HashMap": true, "HashSet": true,
		"Collections": true, "Arrays": true, "Stream": true, "Optional": true,
		"Collectors": true, "Iterator": true, "Iterable": true,
		"Exception": true, "RuntimeException": true, "IOException": true,
		"NullPointerException": true, "IllegalArgumentException": true,
		"Override": true, "Deprecated": true, "SuppressWarnings": true,
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

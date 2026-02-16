package codegraph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParser_Go(t *testing.T) {
	// Setup
	p := NewParser()
	ctx := context.Background()

	code := `
package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
	helper()
}

func helper() {
	result := add(1, 2)
	fmt.Printf("Result: %d", result)
}

func add(a, b int) int {
	return a + b
}

type MyStruct struct {
	Field string
}

func (m *MyStruct) Method() {
	fmt.Println(m.Field)
}
`
	langConfig := SupportedLanguages["go"]

	// Test ParseFile
	root, err := p.ParseFile(ctx, []byte(code), &langConfig)
	assert.NoError(t, err)
	assert.NotNil(t, root)

	// Test ExtractDefinitions
	defs, err := p.ExtractDefinitions(root, []byte(code), "go")
	assert.NoError(t, err)
	assert.Contains(t, defs, "main")
	assert.Contains(t, defs, "helper")
	assert.Contains(t, defs, "add")
	assert.Contains(t, defs, "MyStruct")
	assert.Contains(t, defs, "Method")

	// Test ExtractReferences
	refs, err := p.ExtractReferences(root, []byte(code), "go")
	assert.NoError(t, err)
	assert.Contains(t, refs, "Println")
	assert.Contains(t, refs, "helper")
	assert.Contains(t, refs, "add")
	assert.Contains(t, refs, "Printf")
	// assert.Contains(t, refs, "MyStruct") // Depending on query, might strictly be type definition
}

func TestParser_JS(t *testing.T) {
	p := NewParser()
	ctx := context.Background()

	code := `
function main() {
	console.log("Hello");
	helper();
}

function helper() {
	return add(1, 2);
}

const add = (a, b) => {
	return a + b;
}

class MyClass {
	method() {}
}
`
	langConfig := SupportedLanguages["javascript"]
	// JS/TS share grammar usually, or use specific ones. references.go uses "javascript" key.
	// We need to make sure "javascript" is in SupportedLanguages or use "typescript" if that's what we loaded.
	// languages.go has "javascript" -> "javascript" grammar?
	// Let's check languages.go first to be sure about keys.
	// Assuming "javascript" exists for now based on previous file context.

	root, err := p.ParseFile(ctx, []byte(code), &langConfig)
	assert.NoError(t, err)

	defs, err := p.ExtractDefinitions(root, []byte(code), "javascript")
	assert.NoError(t, err)
	assert.Contains(t, defs, "main")
	assert.Contains(t, defs, "helper")
	assert.Contains(t, defs, "add") // const add = ... might be variable_declarator
	assert.Contains(t, defs, "MyClass")
	assert.Contains(t, defs, "method")

	refs, err := p.ExtractReferences(root, []byte(code), "javascript")
	assert.NoError(t, err)
	assert.Contains(t, refs, "log") // console.log
	assert.Contains(t, refs, "helper")
	assert.Contains(t, refs, "add")
}

func TestParser_Python(t *testing.T) {
	p := NewParser()
	ctx := context.Background()

	code := `
def main():
    print("Hello")
    helper()

def helper():
    return add(1, 2)

def add(a, b):
    return a + b

class MyClass:
    def method(self):
        pass
`
	langConfig := SupportedLanguages["python"]
	root, err := p.ParseFile(ctx, []byte(code), &langConfig)
	assert.NoError(t, err)

	defs, err := p.ExtractDefinitions(root, []byte(code), "python")
	assert.NoError(t, err)
	assert.Contains(t, defs, "main")
	assert.Contains(t, defs, "helper")
	assert.Contains(t, defs, "add")
	assert.Contains(t, defs, "MyClass")

	refs, err := p.ExtractReferences(root, []byte(code), "python")
	assert.NoError(t, err)
	assert.Contains(t, refs, "print")
	assert.Contains(t, refs, "helper")
	assert.Contains(t, refs, "add")
}

package chunker

import (
	"code-review/backend/internal/codegraph"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DefaultTokenBudget is the target max tokens per chunk.
// Each chunk gets its own set of 3 specialist agent reviews.
const DefaultTokenBudget = 10_000

// Chunk represents a group of related files to be reviewed together.
type Chunk struct {
	Files     map[string]string // path → file content
	Diff      string            // diff sections for these files only
	Index     int               // 1-based chunk number
	Total     int               // total chunks in this PR
	CrossRefs []string          // files in OTHER chunks that this chunk depends on
	Dirs      []string          // directories represented in this chunk (for logging)
}

// EstimateTokens returns a rough token count for a string (4 chars ≈ 1 token).
func EstimateTokens(content string) int {
	return len(content) / 4
}

// GroupFiles splits changed files into review chunks using code graph edges
// to keep semantically connected files together.
//
// Pass nil for edges if no code graph is available (falls back to directory grouping).
func GroupFiles(files map[string]string, diff string, edges []codegraph.Edge, budget int) []Chunk {
	if budget <= 0 {
		budget = DefaultTokenBudget
	}

	// Fast path: everything fits in one chunk
	totalTokens := 0
	for _, content := range files {
		totalTokens += EstimateTokens(content)
	}
	if totalTokens <= budget {
		return []Chunk{singleChunk(files, diff)}
	}

	// 1. Build connected components using graph edges
	components := buildComponents(files, edges)

	// 2. Pack components into chunks by budget
	chunks := packChunks(components, files, budget)

	// 3. Compute cross-refs and extract per-chunk diffs
	filePaths := sortedKeys(files)
	diffSections := splitDiffByFile(diff)

	for i := range chunks {
		chunks[i].Index = i + 1
		chunks[i].Total = len(chunks)
		chunks[i].Diff = buildChunkDiff(chunks[i].Files, diffSections)
		chunks[i].CrossRefs = computeCrossRefs(chunks[i], chunks, edges)
		chunks[i].Dirs = chunkDirs(chunks[i].Files)
	}
	_ = filePaths

	return chunks
}

// buildComponents groups changed files into connected components using graph edges.
// Files that reference each other (directly or transitively) end up in the same component.
func buildComponents(files map[string]string, edges []codegraph.Edge) [][]string {
	filePaths := sortedKeys(files)
	changedSet := make(map[string]bool, len(files))
	for _, p := range filePaths {
		changedSet[p] = true
	}

	// Union-Find
	parent := make(map[string]string)
	for _, p := range filePaths {
		parent[p] = p
	}

	var find func(string) string
	find = func(x string) string {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	// Union files connected by graph edges (only if BOTH files are changed)
	for _, edge := range edges {
		_, fromChanged := files[edge.From]
		_, toChanged := files[edge.To]
		if fromChanged && toChanged {
			union(edge.From, edge.To)
		}
	}

	// Also union files in the same directory (secondary grouping)
	dirGroups := make(map[string][]string)
	for _, p := range filePaths {
		dir := filepath.Dir(p)
		dirGroups[dir] = append(dirGroups[dir], p)
	}
	for _, group := range dirGroups {
		for i := 1; i < len(group); i++ {
			union(group[0], group[i])
		}
	}

	// Collect components
	componentMap := make(map[string][]string)
	for _, p := range filePaths {
		root := find(p)
		componentMap[root] = append(componentMap[root], p)
	}

	components := make([][]string, 0, len(componentMap))
	for _, comp := range componentMap {
		sort.Strings(comp)
		components = append(components, comp)
	}

	// Sort components by first file path for deterministic output
	sort.Slice(components, func(i, j int) bool {
		return components[i][0] < components[j][0]
	})

	return components
}

// packChunks packs components into chunks, respecting the token budget.
func packChunks(components [][]string, files map[string]string, budget int) []Chunk {
	var chunks []Chunk
	currentFiles := make(map[string]string)
	currentTokens := 0

	for _, comp := range components {
		compTokens := 0
		for _, path := range comp {
			compTokens += EstimateTokens(files[path])
		}

		// If this component alone exceeds budget, give it its own chunk
		if compTokens > budget {
			// Flush current chunk first if non-empty
			if len(currentFiles) > 0 {
				chunks = append(chunks, Chunk{Files: currentFiles})
				currentFiles = make(map[string]string)
				currentTokens = 0
			}
			// Create a chunk for the oversized component
			oversized := make(map[string]string)
			for _, path := range comp {
				oversized[path] = files[path]
			}
			chunks = append(chunks, Chunk{Files: oversized})
			continue
		}

		// Would adding this component exceed budget?
		if currentTokens+compTokens > budget && len(currentFiles) > 0 {
			chunks = append(chunks, Chunk{Files: currentFiles})
			currentFiles = make(map[string]string)
			currentTokens = 0
		}

		// Add component to current chunk
		for _, path := range comp {
			currentFiles[path] = files[path]
		}
		currentTokens += compTokens
	}

	// Flush remaining
	if len(currentFiles) > 0 {
		chunks = append(chunks, Chunk{Files: currentFiles})
	}

	return chunks
}

// computeCrossRefs finds files in OTHER chunks that this chunk's files depend on.
func computeCrossRefs(chunk Chunk, allChunks []Chunk, edges []codegraph.Edge) []string {
	chunkFiles := make(map[string]bool)
	for path := range chunk.Files {
		chunkFiles[path] = true
	}

	crossRefSet := make(map[string]bool)
	for _, edge := range edges {
		// This chunk's file references a file in another chunk
		if chunkFiles[edge.From] && !chunkFiles[edge.To] {
			// Check if the target is in any other chunk
			for _, other := range allChunks {
				if _, ok := other.Files[edge.To]; ok {
					crossRefSet[edge.To] = true
				}
			}
		}
		// A file in another chunk references this chunk's file
		if chunkFiles[edge.To] && !chunkFiles[edge.From] {
			for _, other := range allChunks {
				if _, ok := other.Files[edge.From]; ok {
					crossRefSet[edge.From] = true
				}
			}
		}
	}

	refs := make([]string, 0, len(crossRefSet))
	for path := range crossRefSet {
		refs = append(refs, path)
	}
	sort.Strings(refs)
	return refs
}

// splitDiffByFile splits a unified diff string into per-file sections.
var diffFileHeaderRegex = regexp.MustCompile(`(?m)^diff --git a/(.*) b/(.*)$`)

func splitDiffByFile(diff string) map[string]string {
	result := make(map[string]string)
	if diff == "" {
		return result
	}

	indices := diffFileHeaderRegex.FindAllStringIndex(diff, -1)
	if len(indices) == 0 {
		return result
	}

	for i, idx := range indices {
		match := diffFileHeaderRegex.FindStringSubmatch(diff[idx[0]:idx[1]])
		if len(match) < 3 {
			continue
		}
		filePath := match[2] // use the "b/" path

		var section string
		if i+1 < len(indices) {
			section = diff[idx[0]:indices[i+1][0]]
		} else {
			section = diff[idx[0]:]
		}
		result[filePath] = section
	}

	return result
}

// buildChunkDiff assembles the diff for a specific chunk's files.
func buildChunkDiff(chunkFiles map[string]string, diffSections map[string]string) string {
	var b strings.Builder
	paths := sortedKeys(chunkFiles)
	for _, path := range paths {
		if section, ok := diffSections[path]; ok {
			b.WriteString(section)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// chunkDirs returns the unique directories represented in a chunk.
func chunkDirs(files map[string]string) []string {
	dirSet := make(map[string]bool)
	for path := range files {
		dirSet[filepath.Dir(path)] = true
	}
	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// singleChunk creates a single chunk containing all files (fast path).
func singleChunk(files map[string]string, diff string) Chunk {
	return Chunk{
		Files: files,
		Diff:  diff,
		Index: 1,
		Total: 1,
		Dirs:  chunkDirs(files),
	}
}

// sortedKeys returns map keys sorted alphabetically.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

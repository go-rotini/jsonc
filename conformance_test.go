package jsonc_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-rotini/jsonc"
)

// ---------------------------------------------------------------------------
// JSONC edge case corpus (always present in the repo)
// ---------------------------------------------------------------------------

// TestJSONCEdgeCases exercises the curated edge case corpus in
// testdata/jsonc/. Filename convention controls the expected outcome:
//
//	reject_*.jsonc  — must fail to parse (asserts the rejection path)
//	accept_*.jsonc  — must parse cleanly and survive ToJSON + strict-reparse
//	(unprefixed)    — treated as accept_*
func TestJSONCEdgeCases(t *testing.T) {
	dir := "testdata/jsonc"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("edge case dir unavailable: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (!strings.HasSuffix(name, ".jsonc") && !strings.HasSuffix(name, ".json")) {
			continue
		}
		path := filepath.Join(dir, name)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			runEdgeCaseChecks(t, data, name)
		})
	}
}

// runEdgeCaseChecks runs a single fixture through the public API. For
// reject_* fixtures it asserts every parse path consistently fails. For
// accept_* it exercises Parse → AST round-trip → Format/Minimize/ToJSON →
// re-parse → Marshal → Encoder/Decoder, plus Path/Patch/Walk/Filter
// where applicable. The breadth is deliberate: this single harness drives
// most of the conformance-target coverage.
func runEdgeCaseChecks(t *testing.T, data []byte, name string) {
	t.Helper()

	if strings.HasPrefix(name, "reject_") {
		// Each entry point must consistently refuse the input.
		if _, err := jsonc.Parse(data); err == nil {
			t.Fatalf("Parse: expected error, got nil")
		}
		if jsonc.Valid(data) {
			t.Errorf("Valid: returned true for reject_ fixture")
		}
		var v any
		if err := jsonc.Unmarshal(data, &v); err == nil {
			t.Errorf("Unmarshal: expected error, got nil")
		}
		if _, err := jsonc.ToJSON(data); err == nil {
			t.Errorf("ToJSON: expected error, got nil")
		}
		if _, err := jsonc.Format(data); err == nil {
			t.Errorf("Format: expected error, got nil")
		}
		if _, err := jsonc.Minimize(data); err == nil {
			t.Errorf("Minimize: expected error, got nil")
		}
		return
	}

	// Accept path.
	f, err := jsonc.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !jsonc.Valid(data) {
		t.Errorf("Valid returned false")
	}

	// ToJSON → strict-reparse.
	jsonOut, err := jsonc.ToJSON(data)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if _, err := jsonc.Parse(jsonOut, jsonc.WithStrictJSON()); err != nil {
		t.Errorf("ToJSON output not strict-JSON-valid: %v", err)
	}

	// StripComments alias matches ToJSON.
	stripped, err := jsonc.StripComments(data)
	if err != nil {
		t.Fatalf("StripComments: %v", err)
	}
	if !bytes.Equal(stripped, jsonOut) {
		t.Error("StripComments differs from ToJSON")
	}

	// Format / Minimize round-trips.
	formatted, err := jsonc.Format(data)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if _, err := jsonc.Parse(formatted); err != nil {
		t.Errorf("Format output not re-parseable: %v", err)
	}
	minimized, err := jsonc.Minimize(data)
	if err != nil {
		t.Fatalf("Minimize: %v", err)
	}
	if _, err := jsonc.Parse(minimized); err != nil {
		t.Errorf("Minimize output not re-parseable: %v", err)
	}

	// Decode → re-encode through the reflection encoder.
	var v any
	if err := jsonc.Unmarshal(data, &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	encoded, err := jsonc.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal of decoded value: %v", err)
	}
	if _, err := jsonc.Parse(encoded); err != nil {
		t.Errorf("re-Marshal output not parseable: %v", err)
	}
	if _, err := jsonc.MarshalIndent(v, "  "); err != nil {
		t.Errorf("MarshalIndent: %v", err)
	}

	// AST round-trip (NodeToBytes / NodeToBytesWithOptions / Walk / Filter
	// / Validate).
	astBytes, err := jsonc.NodeToBytes(f.Root)
	if err != nil {
		t.Fatalf("NodeToBytes: %v", err)
	}
	if _, err := jsonc.Parse(astBytes); err != nil {
		t.Errorf("NodeToBytes output not re-parseable: %v", err)
	}
	if _, err := jsonc.NodeToBytesWithOptions(f.Root, jsonc.WithIndent("  ")); err != nil {
		t.Errorf("NodeToBytesWithOptions: %v", err)
	}
	if err := f.Root.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	visited := 0
	jsonc.Walk(f.Root, func(_ *jsonc.Node) bool { visited++; return true })
	if visited == 0 {
		t.Errorf("Walk visited 0 nodes")
	}
	_ = jsonc.Filter(f.Root, func(n *jsonc.Node) bool {
		return n.Kind == jsonc.StringNode || n.Kind == jsonc.NumberNode
	})

	// Streaming Encoder over the decoded value.
	var encBuf bytes.Buffer
	enc := jsonc.NewEncoder(&encBuf)
	if err := enc.Encode(v); err != nil {
		t.Errorf("Encoder.Encode: %v", err)
	}

	// Streaming Decoder over the JSON output.
	dec := jsonc.NewDecoder(bytes.NewReader(jsonOut))
	var dvOut any
	if err := dec.Decode(&dvOut); err != nil {
		t.Errorf("Decoder.Decode: %v", err)
	}

	// RawValue lifecycle.
	rv := jsonc.RawValue(data)
	if _, err := rv.Standardize(); err != nil {
		t.Errorf("RawValue.Standardize: %v", err)
	}
	if _, err := rv.MarshalJSONC(); err != nil {
		t.Errorf("RawValue.MarshalJSONC: %v", err)
	}

	// Reflection round-trip with WithUseNumber for objects/arrays containing
	// numbers (preserves precision).
	var numV any
	if err := jsonc.UnmarshalWithOptions(data, &numV, jsonc.WithUseNumber()); err != nil {
		t.Errorf("WithUseNumber: %v", err)
	}

	// WithOrderedMap if the root is an object.
	if f.Root.Kind == jsonc.ObjectNode {
		var ordered any
		if err := jsonc.UnmarshalWithOptions(data, &ordered, jsonc.WithOrderedMap()); err != nil {
			t.Errorf("WithOrderedMap: %v", err)
		}
		if ms, ok := ordered.(jsonc.MapSlice); ok {
			if _, err := jsonc.Marshal(ms); err != nil {
				t.Errorf("Marshal MapSlice: %v", err)
			}
		}
	}
}

// TestJSONCEdgeCasesPathPatchExercise runs Path and Patch operations
// against the accept fixtures so the conformance target hits those code
// paths too.
func TestJSONCEdgeCasesPathPatchExercise(t *testing.T) {
	// Pick a representative accept fixture that's an object.
	data, err := os.ReadFile("testdata/jsonc/comments_everywhere.jsonc")
	if err != nil {
		t.Skip(err)
	}
	f, err := jsonc.Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	// Paths
	pAll, _ := jsonc.PathString("$..a")
	_ = pAll.Read(f.Root)

	pPointer, _ := jsonc.PathPointer("/a")
	if v, err := pPointer.ReadString(f.Root); err == nil {
		t.Logf("/a = %q", v)
	}

	// Walk + Filter
	jsonc.Walk(f.Root, func(_ *jsonc.Node) bool { return true })
	_ = jsonc.Filter(f.Root, func(n *jsonc.Node) bool { return n.Kind == jsonc.NumberNode })

	// Patch
	patch, err := jsonc.ParsePatch([]byte(`[
		{"op": "test", "path": "/a", "value": 1}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := patch.Apply(f.Root); err != nil {
		t.Logf("patch test failed (acceptable for arbitrary fixture): %v", err)
	}
}

// TestStrictJSONViolations verifies that our strict_json_violations.jsonc
// fixture is rejected when WithStrictJSON is enabled.
func TestStrictJSONViolations(t *testing.T) {
	data, err := os.ReadFile("testdata/jsonc/strict_json_violations.jsonc")
	if err != nil {
		t.Skip(err)
	}
	// Default JSONC mode: accepts.
	if _, err := jsonc.Parse(data); err != nil {
		t.Errorf("default mode should accept: %v", err)
	}
	// Strict mode: rejects.
	_, err = jsonc.Parse(data, jsonc.WithStrictJSON())
	var sje *jsonc.StrictJSONError
	if !errors.As(err, &sje) {
		t.Errorf("strict mode should reject with *StrictJSONError, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// JSONTestSuite (gated by repo presence)
// ---------------------------------------------------------------------------

// TestJSONTestSuite runs the package against nst/JSONTestSuite when the
// testdata/JSONTestSuite directory is populated (cloned by `make
// clone-test-suites`). Each file's prefix dictates the expected outcome:
//
//	y_*  must be accepted as RFC 8259 JSON
//	n_*  must be rejected
//	i_*  implementation-defined
func TestJSONTestSuite(t *testing.T) {
	dir := "testdata/JSONTestSuite/test_parsing"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("JSONTestSuite not cloned: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Allow duplicate keys: RFC 8259 leaves their handling
			// unspecified, JSONTestSuite considers them "must accept", and
			// encoding/json accepts them (last-wins). Without this we'd fail
			// y_object_duplicated_key and y_object_duplicated_key_and_value.
			_, parseErr := jsonc.Parse(data,
				jsonc.WithStrictJSON(),
				jsonc.WithAllowDuplicateKeys(),
			)
			switch {
			case strings.HasPrefix(name, "y_"):
				if parseErr != nil {
					t.Errorf("expected accept, got %v", parseErr)
				}
			case strings.HasPrefix(name, "n_"):
				if parseErr == nil {
					t.Errorf("expected reject, got success")
				}
			case strings.HasPrefix(name, "i_"):
				// Implementation-defined — record outcome but don't fail.
				t.Logf("i_ outcome: err=%v", parseErr)
			}
		})
	}
}

// TestJWCCTestSuite runs the package against tailscale/hujson testdata
// when cloned. hujson's test corpus generally targets JWCC (JSON with
// commas and comments), the same dialect we accept by default.
func TestJWCCTestSuite(t *testing.T) {
	// hujson's testdata layout uses *.jwcc files for valid inputs and
	// occasional *.json fixtures.
	dir := "testdata/hujson-testdata"
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("hujson testdata not cloned: %v", err)
	}
	count := 0
	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".jwcc" && ext != ".jsonc" && ext != ".json" {
			return nil
		}
		count++
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// We accept JWCC by default; just ensure we don't panic and that
			// well-formed inputs parse. We don't assert reject/accept here
			// because hujson's corpus mixes valid and intentionally invalid
			// fixtures without filename conventions we can rely on.
			_, _ = jsonc.Parse(data)
		})
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if count == 0 {
		t.Skip("no hujson fixtures found")
	}
}

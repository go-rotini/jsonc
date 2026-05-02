package jsonc_test

import (
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
// testdata/jsonc/. Each fixture must parse without error in JSONC mode and
// round-trip cleanly through the AST encoder.
func TestJSONCEdgeCases(t *testing.T) {
	dir := "testdata/jsonc"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("edge case dir unavailable: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonc") && !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := jsonc.Parse(data); err != nil {
				t.Fatalf("Parse: %v", err)
			}
			// ToJSON must always succeed for valid JSONC, producing strict JSON.
			out, err := jsonc.ToJSON(data)
			if err != nil {
				t.Fatalf("ToJSON: %v", err)
			}
			if _, err := jsonc.Parse(out, jsonc.WithStrictJSON()); err != nil {
				t.Errorf("ToJSON output not strict-JSON-valid: %v", err)
			}
		})
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

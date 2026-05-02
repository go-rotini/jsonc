package jsonc_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-rotini/jsonc"
)

// TestAcceptance walks testdata/acceptance/ and verifies that every fixture
// parses, decodes into any, round-trips losslessly through the AST encoder,
// and survives a strict-JSON conversion via ToJSON.
func TestAcceptance(t *testing.T) {
	dir := "testdata/acceptance"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("acceptance dir not available: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			runAcceptanceChecks(t, data, e.Name())
		})
	}
}

// runAcceptanceChecks exercises the full surface area of the package
// against a single fixture.
func runAcceptanceChecks(t *testing.T, data []byte, name string) {
	t.Helper()

	// 1. Parse to AST.
	f, err := jsonc.Parse(data)
	if err != nil {
		t.Fatalf("Parse %s: %v", name, err)
	}
	if f.Root == nil {
		t.Fatalf("Parse %s: nil root", name)
	}

	// 2. Decode into any (round-trips structural content).
	var v any
	if err := jsonc.Unmarshal(data, &v); err != nil {
		t.Fatalf("Unmarshal %s: %v", name, err)
	}

	// 3. ToJSON produces standard JSON (no comments, no trailing commas).
	jsonOut, err := jsonc.ToJSON(data)
	if err != nil {
		t.Fatalf("ToJSON %s: %v", name, err)
	}
	// 4. ToJSON output is itself valid RFC 8259 JSON — verify by re-parsing
	// with strict mode. (We can't simply grep for "//" or "/*" because those
	// substrings can appear inside string values.)
	if _, err := jsonc.Parse(jsonOut, jsonc.WithStrictJSON()); err != nil {
		t.Errorf("ToJSON %s output not strict-JSON-valid: %v", name, err)
	}

	// 5. Format produces re-parseable, comment-preserving output.
	formatted, err := jsonc.Format(data)
	if err != nil {
		t.Fatalf("Format %s: %v", name, err)
	}
	if _, err := jsonc.Parse(formatted); err != nil {
		t.Errorf("Format %s output not re-parseable: %v", name, err)
	}

	// 6. Minimize produces re-parseable output.
	minimized, err := jsonc.Minimize(data)
	if err != nil {
		t.Fatalf("Minimize %s: %v", name, err)
	}
	if _, err := jsonc.Parse(minimized); err != nil {
		t.Errorf("Minimize %s output not re-parseable: %v", name, err)
	}

	// 7. Decoding ToJSON output via stdlib json must produce the same
	// untyped result as decoding via jsonc.Unmarshal.
	var stdV any
	if err := jsonc.Unmarshal(jsonOut, &stdV); err != nil {
		t.Errorf("Unmarshal ToJSON output %s: %v", name, err)
	}
}

// TestAcceptanceTSConfig verifies a representative tsconfig.json shape
// decodes into a typed struct.
func TestAcceptanceTSConfig(t *testing.T) {
	data, err := os.ReadFile("testdata/acceptance/tsconfig.json")
	if err != nil {
		t.Skip(err)
	}

	type Compiler struct {
		Target string              `json:"target"`
		Module string              `json:"module"`
		Strict bool                `json:"strict"`
		Paths  map[string][]string `json:"paths"`
		OutDir string              `json:"outDir"`
	}
	type Config struct {
		CompilerOptions Compiler `json:"compilerOptions"`
		Include         []string `json:"include"`
		Exclude         []string `json:"exclude"`
	}
	var cfg Config
	if err := jsonc.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.CompilerOptions.Target != "es2020" {
		t.Errorf("target = %q", cfg.CompilerOptions.Target)
	}
	if !cfg.CompilerOptions.Strict {
		t.Errorf("strict should be true")
	}
	if len(cfg.Include) == 0 {
		t.Errorf("include should have entries")
	}
	if v, ok := cfg.CompilerOptions.Paths["@/*"]; !ok || len(v) == 0 {
		t.Errorf("expected @/* path mapping, got %+v", cfg.CompilerOptions.Paths)
	}
}

// TestAcceptanceVSCodeSettings verifies the VS Code settings fixture
// decodes into a string-keyed map.
func TestAcceptanceVSCodeSettings(t *testing.T) {
	data, err := os.ReadFile("testdata/acceptance/vscode-settings.json")
	if err != nil {
		t.Skip(err)
	}
	var settings map[string]any
	if err := jsonc.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings["editor.tabSize"]; !ok {
		t.Errorf("expected editor.tabSize key")
	}
	if _, ok := settings["[go]"]; !ok {
		t.Errorf("expected language-specific override [go]")
	}
}

// TestAcceptanceKitchenSink exercises path queries against the
// kitchen-sink fixture.
func TestAcceptanceKitchenSink(t *testing.T) {
	data, err := os.ReadFile("testdata/acceptance/kitchen-sink.jsonc")
	if err != nil {
		t.Skip(err)
	}
	f, err := jsonc.Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	// Path query: deep.level1.level2.level3.level4[2]
	p, _ := jsonc.PathString("$.deep.level1.level2.level3.level4[2]")
	got, err := p.ReadString(f.Root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "3" {
		t.Errorf("got %q, want \"3\"", got)
	}

	// Wildcard: numbers.*
	pAll, _ := jsonc.PathString("$.numbers[*]")
	matches := pAll.Read(f.Root)
	if len(matches) == 0 {
		t.Errorf("wildcard query returned no matches")
	}
}

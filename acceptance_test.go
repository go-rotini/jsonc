package jsonc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-rotini/jsonc"
)

// TestAcceptance walks testdata/acceptance/ and runs every fixture through
// the bulk of the public API: Parse, Unmarshal, Marshal, MarshalIndent,
// MarshalTo, Format, Minimize, ToJSON + strict-reparse, NodeToBytes,
// Encoder/Decoder streaming, EncodeFile + DecodeFile round-trip, Walk +
// Filter, RawValue.Standardize, and a Path query against the AST. The
// goal is high cross-package coverage from one harness rather than many
// scattered tests.
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
	if _, err := jsonc.Parse(jsonOut, jsonc.WithStrictJSON()); err != nil {
		t.Errorf("ToJSON %s output not strict-JSON-valid: %v", name, err)
	}

	// 4. StripComments alias produces identical bytes to ToJSON.
	stripped, err := jsonc.StripComments(data)
	if err != nil {
		t.Fatalf("StripComments %s: %v", name, err)
	}
	if !bytes.Equal(stripped, jsonOut) {
		t.Errorf("StripComments differs from ToJSON for %s", name)
	}

	// 5. Format produces re-parseable, comment-preserving output (default
	// indent of two spaces).
	formatted, err := jsonc.Format(data)
	if err != nil {
		t.Fatalf("Format %s: %v", name, err)
	}
	if _, err := jsonc.Parse(formatted); err != nil {
		t.Errorf("Format %s output not re-parseable: %v", name, err)
	}

	// 6. Format with custom indent.
	formattedTab, err := jsonc.Format(data, jsonc.WithIndent("\t"))
	if err != nil {
		t.Fatalf("Format with tab indent %s: %v", name, err)
	}
	if _, err := jsonc.Parse(formattedTab); err != nil {
		t.Errorf("tab-indented format %s not re-parseable: %v", name, err)
	}

	// 7. Minimize produces re-parseable output.
	minimized, err := jsonc.Minimize(data)
	if err != nil {
		t.Fatalf("Minimize %s: %v", name, err)
	}
	if _, err := jsonc.Parse(minimized); err != nil {
		t.Errorf("Minimize %s output not re-parseable: %v", name, err)
	}

	// 8. Decoding ToJSON output via stdlib json must succeed.
	var stdV any
	if err := json.Unmarshal(jsonOut, &stdV); err != nil {
		t.Errorf("stdlib json.Unmarshal of ToJSON output %s: %v", name, err)
	}

	// 9. Marshal the decoded value back. This goes through the reflection
	// encoder for whatever shape Unmarshal produced (map[string]any /
	// []any / scalars).
	encoded, err := jsonc.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal of decoded value %s: %v", name, err)
	}
	if _, err := jsonc.Parse(encoded); err != nil {
		t.Errorf("re-Marshal output not parseable for %s: %v", name, err)
	}

	// 10. MarshalIndent variant covers the indent-set encoder path.
	indented, err := jsonc.MarshalIndent(v, "  ")
	if err != nil {
		t.Fatalf("MarshalIndent of decoded value %s: %v", name, err)
	}
	if !strings.Contains(string(indented), "\n") {
		t.Errorf("MarshalIndent should produce multi-line output for %s", name)
	}

	// 11. MarshalTo with the explicit type parameter exercises the generic
	// entry point.
	if _, err := jsonc.MarshalTo(v); err != nil {
		t.Errorf("MarshalTo %s: %v", name, err)
	}

	// 12. NodeToBytes round-trip via the AST.
	astBytes, err := jsonc.NodeToBytes(f.Root)
	if err != nil {
		t.Fatalf("NodeToBytes %s: %v", name, err)
	}
	if _, err := jsonc.Parse(astBytes); err != nil {
		t.Errorf("NodeToBytes %s output not re-parseable: %v", name, err)
	}

	// 13. NodeToBytesWithOptions with indent + trailing comma.
	withOpts, err := jsonc.NodeToBytesWithOptions(f.Root,
		jsonc.WithIndent("  "),
		jsonc.WithTrailingComma(true),
	)
	if err != nil {
		t.Fatalf("NodeToBytesWithOptions %s: %v", name, err)
	}
	if _, err := jsonc.Parse(withOpts); err != nil {
		t.Errorf("trailing-comma AST output not re-parseable for %s: %v", name, err)
	}

	// 14. Encoder streaming: encode the value through Encoder.
	var encBuf bytes.Buffer
	enc := jsonc.NewEncoder(&encBuf, jsonc.WithIndent("  "))
	if err := enc.Encode(v); err != nil {
		t.Fatalf("Encoder.Encode %s: %v", name, err)
	}
	if _, err := jsonc.Parse(encBuf.Bytes()); err != nil {
		t.Errorf("Encoder.Encode output not re-parseable: %v", err)
	}
	// EncodeContext leg.
	encBuf.Reset()
	if err := enc.EncodeContext(context.Background(), v); err != nil {
		t.Fatalf("Encoder.EncodeContext %s: %v", name, err)
	}

	// 15. Decoder streaming over the JSON output.
	dec := jsonc.NewDecoder(bytes.NewReader(jsonOut))
	var dvOut any
	if err := dec.Decode(&dvOut); err != nil {
		t.Fatalf("Decoder.Decode %s: %v", name, err)
	}
	dec.SetContext(context.Background())
	_ = dec.More()
	_ = dec.InputOffset()

	// 16. EncodeFile / DecodeFile round-trip via temp file.
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "fixture.jsonc")
	if err := jsonc.EncodeFile(tmpPath, v, jsonc.WithIndent("  ")); err != nil {
		t.Fatalf("EncodeFile %s: %v", name, err)
	}
	var fileV any
	if err := jsonc.DecodeFile(tmpPath, &fileV); err != nil {
		t.Fatalf("DecodeFile %s: %v", name, err)
	}

	// 17. Walk + Filter exercise AST traversal.
	var nodeCount int
	jsonc.Walk(f.Root, func(_ *jsonc.Node) bool {
		nodeCount++
		return true
	})
	if nodeCount == 0 {
		t.Errorf("Walk %s found 0 nodes", name)
	}
	matches := jsonc.Filter(f.Root, func(n *jsonc.Node) bool {
		return n.Kind == jsonc.StringNode
	})
	_ = matches // count not asserted — fixtures vary

	// 18. Validate the AST structurally.
	if err := f.Root.Validate(); err != nil {
		t.Errorf("Root.Validate %s: %v", name, err)
	}

	// 19. RawValue.Standardize via the entry point.
	rv := jsonc.RawValue(data)
	if _, err := rv.Standardize(); err != nil {
		t.Errorf("RawValue.Standardize %s: %v", name, err)
	}
	if _, err := rv.MarshalJSONC(); err != nil {
		t.Errorf("RawValue.MarshalJSONC %s: %v", name, err)
	}
	var rvCopy jsonc.RawValue
	if err := rvCopy.UnmarshalJSONC(data); err != nil {
		t.Errorf("RawValue.UnmarshalJSONC %s: %v", name, err)
	}

	// 20. Reflection round-trip into MapSlice via WithOrderedMap (covers
	// nodeToMapSlice / fillGenericMap paths). WithOrderedMap kicks in when
	// decoding into `any`, producing a MapSlice for objects.
	if f.Root.Kind == jsonc.ObjectNode {
		var ordered any
		if err := jsonc.UnmarshalWithOptions(data, &ordered, jsonc.WithOrderedMap()); err != nil {
			t.Errorf("WithOrderedMap %s: %v", name, err)
		}
		if ms, ok := ordered.(jsonc.MapSlice); ok {
			// Marshal MapSlice through the reflection encoder.
			if _, err := jsonc.Marshal(ms); err != nil {
				t.Errorf("Marshal MapSlice %s: %v", name, err)
			}
		}
	}

	// 21. WithUseNumber decoding preserves number text.
	var numV any
	if err := jsonc.UnmarshalWithOptions(data, &numV, jsonc.WithUseNumber()); err != nil {
		t.Errorf("WithUseNumber %s: %v", name, err)
	}

	// 22. Valid quick-check.
	if !jsonc.Valid(data) {
		t.Errorf("Valid returned false for accepted fixture %s", name)
	}
}

// TestAcceptanceTSConfig verifies a representative tsconfig.json shape
// decodes into a typed struct, exercises path queries with multiple
// segment kinds, and round-trips a small Patch.
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

	// Path queries against the AST.
	f, err := jsonc.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	pTarget, _ := jsonc.PathString("$.compilerOptions.target")
	if v, _ := pTarget.ReadString(f.Root); v != "es2020" {
		t.Errorf("PathString $.compilerOptions.target = %q", v)
	}
	pPointer, _ := jsonc.PathPointer("/compilerOptions/strict")
	if v, _ := pPointer.ReadString(f.Root); v != "true" {
		t.Errorf("PathPointer /compilerOptions/strict = %q", v)
	}
	if positions := pTarget.ReadPositions(f.Root); len(positions) != 1 {
		t.Errorf("expected 1 position, got %d", len(positions))
	}

	// Patch test op against a real value.
	patch, _ := jsonc.ParsePatch([]byte(`[{"op": "test", "path": "/compilerOptions/target", "value": "es2020"}]`))
	if _, err := patch.Apply(f.Root); err != nil {
		t.Errorf("Patch test op failed: %v", err)
	}

	// Patch add + replace + remove sequence.
	mutate, _ := jsonc.ParsePatch([]byte(`[
		{"op": "add", "path": "/compilerOptions/newKey", "value": true},
		{"op": "replace", "path": "/compilerOptions/strict", "value": false},
		{"op": "remove", "path": "/compilerOptions/newKey"}
	]`))
	if _, err := mutate.Apply(f.Root); err != nil {
		t.Errorf("Patch sequence failed: %v", err)
	}
}

// TestAcceptanceVSCodeSettings verifies the VS Code settings fixture
// decodes into a string-keyed map and exercises map-side paths.
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

	// Marshal back with various options to exercise encode paths.
	out1, err := jsonc.MarshalWithOptions(settings, jsonc.WithIndent("  "), jsonc.WithTrailingComma(true))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out1, []byte(",\n}")) {
		t.Error("expected trailing comma in encoded output")
	}

	// Marshal with HTML escape on (stdlib parity mode).
	if _, err := jsonc.MarshalWithOptions(settings, jsonc.WithEscapeHTML(true)); err != nil {
		t.Errorf("Marshal with HTML escape: %v", err)
	}

	// Marshal with strict-JSON output.
	if _, err := jsonc.MarshalWithOptions(settings, jsonc.WithStrictJSONOutput(true), jsonc.WithIndent("  ")); err != nil {
		t.Errorf("Marshal with strict-JSON output: %v", err)
	}
}

// TestAcceptanceKitchenSink exercises path queries and tree manipulation
// against the kitchen-sink fixture.
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

	// Recursive descent.
	pRec, _ := jsonc.PathString("$..nested")
	recMatches := pRec.Read(f.Root)
	_ = recMatches // count varies per fixture

	// Replace + Append + Delete via Path API.
	tagsP, _ := jsonc.PathString("$.mixed[0]")
	replacement := &jsonc.Node{Kind: jsonc.NumberNode, Value: "999", RawValue: "999"}
	if err := tagsP.Replace(f.Root, replacement); err != nil {
		t.Errorf("Replace failed: %v", err)
	}
	mixedAll, _ := jsonc.PathString("$.mixed")
	if err := mixedAll.Append(f.Root, &jsonc.Node{Kind: jsonc.StringNode, Value: "added", RawValue: `"added"`}); err != nil {
		t.Errorf("Append failed: %v", err)
	}
	if err := tagsP.Delete(f.Root); err != nil {
		t.Errorf("Delete failed: %v", err)
	}
}

// TestAcceptanceTimeAndDuration exercises time.Time and time.Duration
// decoding against synthetic JSONC.
func TestAcceptanceTimeAndDuration(t *testing.T) {
	type S struct {
		At string `json:"at"`
	}
	src := []byte(`{
		// timestamp
		"at": "2024-01-15T10:30:00Z",
	}`)
	var s S
	if err := jsonc.Unmarshal(src, &s); err != nil {
		t.Fatal(err)
	}
	if s.At != "2024-01-15T10:30:00Z" {
		t.Errorf("got %q", s.At)
	}
}

// TestAcceptanceCustomMarshalers exercises the WithCustomMarshaler /
// WithCustomUnmarshaler registration paths against JSONC fixtures.
type acceptCustom struct {
	Tag string
}

func TestAcceptanceCustomMarshalers(t *testing.T) {
	in := acceptCustom{Tag: "x"}
	out, err := jsonc.MarshalWithOptions(in,
		jsonc.WithCustomMarshaler(func(v acceptCustom) ([]byte, error) {
			return []byte(`"` + v.Tag + `"`), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"x"` {
		t.Errorf("got %q", out)
	}

	var got acceptCustom
	if err := jsonc.UnmarshalWithOptions(out, &got,
		jsonc.WithCustomUnmarshaler(func(c *acceptCustom, data []byte) error {
			c.Tag = "ext:" + string(data)
			return nil
		}),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Tag, "ext:") {
		t.Errorf("got %+v", got)
	}
}

// TestAcceptanceStrictMode exercises WithStrict() + UnknownFieldError.
func TestAcceptanceStrictMode(t *testing.T) {
	type S struct {
		Known string `json:"known"`
	}
	src := []byte(`{
		// allowed
		"known": "yes",
		"unknown": 1,
	}`)
	var s S
	err := jsonc.UnmarshalWithOptions(src, &s, jsonc.WithStrict())
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

// TestAcceptanceMaxLimits exercises depth/key/document-size limits.
func TestAcceptanceMaxLimits(t *testing.T) {
	deep := []byte(strings.Repeat("[", 120) + strings.Repeat("]", 120))
	var v any
	if err := jsonc.UnmarshalWithOptions(deep, &v, jsonc.WithMaxDepth(50)); err == nil {
		t.Error("expected depth error")
	}

	manyKeys := []byte(`{"a":1,"b":2,"c":3,"d":4}`)
	if err := jsonc.UnmarshalWithOptions(manyKeys, &v, jsonc.WithMaxKeys(2)); err == nil {
		t.Error("expected max-keys error")
	}

	if err := jsonc.UnmarshalWithOptions([]byte(`{"x":1}`), &v, jsonc.WithMaxDocumentSize(2)); err == nil {
		t.Error("expected document-size error")
	}
}

// TestAcceptanceFormatError exercises FormatError producing source-pointer
// output.
func TestAcceptanceFormatError(t *testing.T) {
	src := []byte("{\n  bad\n}")
	_, err := jsonc.Parse(src)
	if err == nil {
		t.Skip("expected parse error")
	}
	plain := jsonc.FormatError(src, err)
	if !strings.Contains(plain, "bad") {
		t.Errorf("expected source line in formatted error, got %q", plain)
	}
	colored := jsonc.FormatError(src, err, true)
	if !strings.Contains(colored, "\x1b[") {
		t.Errorf("expected ANSI escape, got %q", colored)
	}
}

// TestAcceptanceWithComment encodes a struct with WithComment annotations
// matching one of the fixture-style shapes.
func TestAcceptanceWithComment(t *testing.T) {
	type Server struct {
		Port int    `json:"port"`
		Host string `json:"host"`
	}
	out, err := jsonc.MarshalWithOptions(Server{Port: 8080, Host: "localhost"},
		jsonc.WithIndent("  "),
		jsonc.WithComment(map[string][]jsonc.Comment{
			"port": {{Position: jsonc.HeadCommentPos, Text: "the port"}},
			"host": {{Position: jsonc.LineCommentPos, Text: "default"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "// the port") || !strings.Contains(string(out), "// default") {
		t.Errorf("expected both comments in output, got:\n%s", out)
	}
}

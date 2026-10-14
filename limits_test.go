package jsonc

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// allocatedBytes returns the bytes allocated while fn runs.
func allocatedBytes(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestLimitsBoundMemory(t *testing.T) {
	const size = 1 << 20
	nested := bytes.Repeat([]byte("["), size)
	wide := append([]byte("["), bytes.Repeat([]byte("1,"), size/2)...)
	blank := append(bytes.Repeat([]byte("\n"), size), '1')

	tests := []struct {
		name    string
		data    []byte
		opts    []DecodeOption
		wantErr bool
	}{
		{"default depth limit", nested, nil, true},
		{"node limit", wide, []DecodeOption{WithMaxNodes(10)}, true},
		{"blank lines", blank, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			n := allocatedBytes(func() { _, err = Parse(tt.data, tt.opts...) })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			// The input is 1 MB; the work a limit allows is far smaller.
			if n > 256<<10 {
				t.Errorf("allocated %d bytes for %d bytes of input", n, len(tt.data))
			}
		})
	}
}

func TestLimitStopsBeforeLaterSyntaxError(t *testing.T) {
	src := strings.Repeat("[", 200) + "@"
	_, err := Parse([]byte(src))
	var se *SyntaxError
	if !errors.As(err, &se) || !strings.Contains(se.Message, "depth") {
		t.Fatalf("err = %v, want the depth limit", err)
	}
}

// wideObject returns a flat object with n members named k0..k(n-1),
// followed by extra.
func wideObject(n int, extra string) string {
	var sb strings.Builder
	sb.WriteByte('{')
	for i := range n {
		fmt.Fprintf(&sb, `"k%d":%d,`, i, i)
	}
	sb.WriteString(extra)
	sb.WriteByte('}')
	return sb.String()
}

func TestDuplicateKeyInWideObject(t *testing.T) {
	src := wideObject(100, `"k3":"again"`)

	_, err := Parse([]byte(src))
	var dup *DuplicateKeyError
	if !errors.As(err, &dup) || dup.Key != "k3" {
		t.Fatalf("err = %v, want DuplicateKeyError for k3", err)
	}

	f, err := Parse([]byte(src), WithAllowDuplicateKeys())
	if err != nil {
		t.Fatal(err)
	}
	members := f.Root.Children
	if len(members) != 100 {
		t.Fatalf("len = %d, want 100", len(members))
	}
	last := members[len(members)-1]
	if last.Key != "k3" || last.Children[0].Value != "again" {
		t.Errorf("last member = %s: %s, want k3: again", last.Key, last.Children[0].Value)
	}
	if members[3].Key != "k4" {
		t.Errorf("members[3] = %s, want k4 (the replaced k3 is removed)", members[3].Key)
	}
	if len(f.Warnings) != 1 {
		t.Errorf("warnings = %v, want one", f.Warnings)
	}
}

func TestDuplicateKeyRepeatedReplacement(t *testing.T) {
	src := wideObject(20, `"k1":"x","k1":"y","k19":"z"`)
	var m map[string]any
	if err := UnmarshalWithOptions([]byte(src), &m, WithAllowDuplicateKeys()); err != nil {
		t.Fatal(err)
	}
	if len(m) != 20 || m["k1"] != "y" || m["k19"] != "z" {
		t.Errorf("got %v", m)
	}
}

func TestDuplicateKeyScalingLinear(t *testing.T) {
	src := []byte(wideObject(100000, `"last":0`))
	start := time.Now()
	if !Valid(src) {
		t.Fatal("not valid")
	}
	// A linear check takes well under a second; a quadratic one takes
	// minutes.
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("parsing 100000 keys took %v", d)
	}
}

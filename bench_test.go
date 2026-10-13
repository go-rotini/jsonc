package jsonc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
)

type benchConfig struct {
	Name    string            `json:"name"`
	Port    int               `json:"port"`
	Tags    []string          `json:"tags"`
	Labels  map[string]string `json:"labels"`
	Enabled bool              `json:"enabled"`
	Ratio   float64           `json:"ratio"`
	Nested  []benchConfig     `json:"nested,omitempty"`
}

func makeBenchConfig(depth int) benchConfig {
	c := benchConfig{
		Name:    "service-alpha",
		Port:    8080,
		Tags:    []string{"a", "b", "c"},
		Labels:  map[string]string{"env": "prod", "team": "core"},
		Enabled: true,
		Ratio:   0.75,
	}
	if depth > 0 {
		for range 5 {
			c.Nested = append(c.Nested, makeBenchConfig(depth-1))
		}
	}
	return c
}

// benchConfigData is a realistic nested config of about 50 KB.
var benchConfigData = func() []byte {
	data, err := json.MarshalIndent(makeBenchConfig(3), "", "  ")
	if err != nil {
		panic(err)
	}
	return data
}()

// benchWideObject returns a flat object with n members.
func benchWideObject(n int) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"key%07d":%d`, i, i)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// benchStream returns n small objects, one per line.
func benchStream(n int) []byte {
	var b bytes.Buffer
	for i := range n {
		fmt.Fprintf(&b, "{\"i\":%d}\n", i)
	}
	return b.Bytes()
}

func BenchmarkUnmarshalConfig(b *testing.B) {
	b.SetBytes(int64(len(benchConfigData)))
	b.ReportAllocs()
	for b.Loop() {
		var c benchConfig
		if err := Unmarshal(benchConfigData, &c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalConfigAny(b *testing.B) {
	b.SetBytes(int64(len(benchConfigData)))
	b.ReportAllocs()
	for b.Loop() {
		var v any
		if err := Unmarshal(benchConfigData, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalConfig(b *testing.B) {
	c := makeBenchConfig(3)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := MarshalIndent(c, "  "); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFormatConfig(b *testing.B) {
	b.SetBytes(int64(len(benchConfigData)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Format(benchConfigData); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseConfig(b *testing.B) {
	b.SetBytes(int64(len(benchConfigData)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(benchConfigData); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidConfig(b *testing.B) {
	b.SetBytes(int64(len(benchConfigData)))
	b.ReportAllocs()
	for b.Loop() {
		if !Valid(benchConfigData) {
			b.Fatal("not valid")
		}
	}
}

func BenchmarkUnmarshalWideObject(b *testing.B) {
	for _, n := range []int{100, 5000, 40000} {
		data := benchWideObject(n)
		b.Run(fmt.Sprintf("keys=%d", n), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				var v map[string]int
				if err := Unmarshal(data, &v); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecoderStream(b *testing.B) {
	for _, n := range []int{100, 2000} {
		data := benchStream(n)
		b.Run(fmt.Sprintf("values=%d", n), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				dec := NewDecoder(bytes.NewReader(data))
				for {
					var v map[string]int
					err := dec.Decode(&v)
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

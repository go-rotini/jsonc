package jsonc_test

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-rotini/jsonc"
)

func ExampleUnmarshal() {
	src := []byte(`{
		// Server configuration
		"port": 8080,
		"host": "localhost",
	}`)

	type Config struct {
		Port int    `json:"port"`
		Host string `json:"host"`
	}
	var cfg Config
	if err := jsonc.Unmarshal(src, &cfg); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("%s:%d\n", cfg.Host, cfg.Port)
	// Output: localhost:8080
}

func ExampleUnmarshalTo() {
	src := []byte(`{"name": "alice", "age": 30}`)
	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	p, err := jsonc.UnmarshalTo[Person](src)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(p.Name, p.Age)
	// Output: alice 30
}

func ExampleMarshal() {
	type Config struct {
		Port int    `json:"port"`
		Host string `json:"host"`
	}
	out, err := jsonc.Marshal(Config{Port: 8080, Host: "localhost"})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(out))
	// Output: {"port": 8080, "host": "localhost"}
}

func ExampleMarshalIndent() {
	out, err := jsonc.MarshalIndent(map[string]int{"a": 1, "b": 2}, "  ")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(out))
	// Output:
	// {
	//   "a": 1,
	//   "b": 2
	// }
}

func ExampleToJSON() {
	src := []byte(`{
		// kept as standard JSON output
		"port": 8080,
		"hosts": ["a", "b",], // trailing comma
	}`)
	out, err := jsonc.ToJSON(src)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	// Output is valid RFC 8259 JSON.
	fmt.Println(strings.Contains(string(out), "//"))
	fmt.Println(strings.Contains(string(out), ",]"))
	// Output:
	// false
	// false
}

func ExampleFormat() {
	src := []byte(`{"a":1,"b":[1,2,3]}`)
	out, err := jsonc.Format(src)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(out))
	// Output:
	// {
	//   "a": 1,
	//   "b": [
	//     1,
	//     2,
	//     3
	//   ]
	// }
}

func ExampleMinimize() {
	src := []byte(`{
		"a": 1,
		"b": 2
	}`)
	out, err := jsonc.Minimize(src)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(out))
	// Output: {"a": 1, "b": 2}
}

func ExamplePathString() {
	src := []byte(`{
		"server": {
			"port": 8080,
			"hosts": ["a.example", "b.example"]
		}
	}`)
	f, err := jsonc.Parse(src)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	p, _ := jsonc.PathString("$.server.hosts[1]")
	host, err := p.ReadString(f.Root)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(host)
	// Output: b.example
}

func ExamplePathPointer() {
	src := []byte(`{"users": [{"name": "alice"}, {"name": "bob"}]}`)
	f, _ := jsonc.Parse(src)

	p, _ := jsonc.PathPointer("/users/1/name")
	name, _ := p.ReadString(f.Root)
	fmt.Println(name)
	// Output: bob
}

func ExamplePatch_Apply() {
	doc := []byte(`{"a": 1, "b": 2}`)
	patch := []byte(`[
		{"op": "replace", "path": "/a", "value": 100},
		{"op": "add", "path": "/c", "value": 3}
	]`)

	f, _ := jsonc.Parse(doc)
	p, _ := jsonc.ParsePatch(patch)
	out, err := p.Apply(f.Root)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	bytes, _ := jsonc.NodeToBytes(out)
	fmt.Println(string(bytes))
	// Output: {"a": 100, "b": 2, "c": 3}
}

func ExampleEncoder() {
	var buf bytes.Buffer
	enc := jsonc.NewEncoder(&buf, jsonc.WithIndent("  "))
	for _, v := range []map[string]int{{"a": 1}, {"b": 2}} {
		if err := enc.Encode(v); err != nil {
			fmt.Println("error:", err)
			return
		}
	}
	fmt.Print(buf.String())
	// Output:
	// {
	//   "a": 1
	// }
	// {
	//   "b": 2
	// }
}

func ExampleDecoder() {
	src := strings.NewReader(`1 2 3`)
	dec := jsonc.NewDecoder(src)
	var sum int
	for dec.More() {
		var n int
		if err := dec.Decode(&n); err != nil {
			fmt.Println("error:", err)
			return
		}
		sum += n
	}
	fmt.Println(sum)
	// Output: 6
}

func ExampleRawValue() {
	type Wrapper struct {
		Meta jsonc.RawValue `json:"meta"`
		Name string         `json:"name"`
	}
	src := []byte(`{"meta": {"k": 1, /* preserved */ "v": [1,2,3]}, "name": "x"}`)
	var w Wrapper
	if err := jsonc.Unmarshal(src, &w); err != nil {
		fmt.Println("error:", err)
		return
	}
	// RawValue keeps the source bytes verbatim, including comments.
	fmt.Println(strings.Contains(string(w.Meta), "preserved"))
	// Output: true
}

func ExampleStripComments() {
	src := []byte(`{"a": 1 /* drop me */, "b": 2 // and me
	}`)
	out, _ := jsonc.StripComments(src)
	fmt.Println(strings.Contains(string(out), "/*"))
	fmt.Println(strings.Contains(string(out), "//"))
	// Output:
	// false
	// false
}

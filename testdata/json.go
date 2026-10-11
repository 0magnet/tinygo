package main

import (
	"encoding/json"
)

func main() {
	println("int:", encode(3))
	println("float64:", encode(3.14))
	println("string:", encode("foo"))
	println("slice of strings:", encode([]string{"foo", "bar"}))
	println("map of strings:", encode(map[string]int{"b": 2, "a": 1}))
	println("map of ints:", encode(map[int]string{2: "b", 1: "a"}))

	var m map[string]int
	if err := json.Unmarshal([]byte(`{"a":1,"b":2}`), &m); err != nil {
		panic("failed to JSON decode: " + err.Error())
	}
	println("decoded map:", len(m), m["a"], m["b"])
}

func encode(itf interface{}) string {
	buf, err := json.Marshal(itf)
	if err != nil {
		panic("failed to JSON encode: " + err.Error())
	}
	return string(buf)
}

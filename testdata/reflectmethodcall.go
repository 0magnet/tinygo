package main

import (
	"os"
	"reflect"
	"strconv"
	"text/template"
)

type Base struct{ ID int }

func (b Base) Describe() string { return "base " + strconv.Itoa(b.ID) }

type Big struct {
	A, B, C, D int64
	S          string
}

type Person struct {
	Base
	First string
	Age   int
}

func (p Person) Name() string                  { return p.First }
func (p Person) Greet(who string) string       { return "hi " + who + " from " + p.First }
func (p Person) Add(a, b int) (int, bool)      { return a + b, a+b > 10 }
func (p Person) Big(n int64) Big               { return Big{n, n + 1, n + 2, n + 3, p.First} }
func (p *Person) Birthday()                    { p.Age++ }
func (p *Person) Rename(first string) string   { old := p.First; p.First = first; return old }
func (p Person) unexported() string            { return "hidden" }
func (p Person) Pair(a string, b int) []string { return []string{a, strconv.Itoa(b)} }

type Counter int

func (c Counter) Double() int { return int(c) * 2 }

type Namer interface {
	Name() string
	Greet(string) string
}

type Holder struct {
	N Namer
}

func call(m reflect.Value, args ...interface{}) []reflect.Value {
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		in[i] = reflect.ValueOf(a)
	}
	return m.Call(in)
}

func main() {
	p := Person{Base: Base{ID: 7}, First: "Ann", Age: 30}
	_ = p.unexported()

	v := reflect.ValueOf(p)
	println("NumMethod value:", v.NumMethod(), v.Type().NumMethod())
	for i := 0; i < v.NumMethod(); i++ {
		m := v.Method(i)
		println("method", i, m.Kind().String(), m.Type().String())
	}
	println("Name:", call(v.MethodByName("Name"))[0].String())
	println("Greet:", call(v.MethodByName("Greet"), "Bob")[0].String())
	out := call(v.MethodByName("Add"), 4, 9)
	println("Add:", out[0].Int(), out[1].Bool())
	big := call(v.MethodByName("Big"), int64(5))[0].Interface().(Big)
	println("Big:", big.A, big.B, big.C, big.D, big.S)
	println("Pair:", len(call(v.MethodByName("Pair"), "x", 3)[0].Interface().([]string)))
	println("Describe (promoted):", call(v.MethodByName("Describe"))[0].String())
	println("miss valid:", v.MethodByName("Missing").IsValid())
	println("unexported valid:", v.MethodByName("unexported").IsValid())
	println("pointer method on value valid:", v.MethodByName("Birthday").IsValid())

	pv := reflect.ValueOf(&p)
	println("NumMethod pointer:", pv.NumMethod())
	call(pv.MethodByName("Birthday"))
	println("Age after Birthday:", p.Age)
	println("Rename old:", call(pv.MethodByName("Rename"), "Cat")[0].String(), "new:", p.First)
	println("pointer Name:", call(pv.MethodByName("Name"))[0].String())

	// The receiver is bound when the method value is made.
	m := reflect.ValueOf(p).MethodByName("Name")
	p.First = "Dan"
	println("bound copy:", call(m)[0].String())
	f := m.Interface().(func() string)
	println("Interface func:", f())

	c := reflect.ValueOf(Counter(21))
	println("Counter Double:", call(c.Method(0))[0].Int())

	h := Holder{N: p}
	iv := reflect.ValueOf(h).Field(0)
	println("interface kind:", iv.Kind().String())
	println("interface Greet:", call(iv.MethodByName("Greet"), "Eve")[0].String())
	println("interface Method(1):", call(iv.Method(1))[0].String())
	println("interface Describe valid:", iv.MethodByName("Describe").IsValid())

	tmpl := template.Must(template.New("t").Parse("{{.Name}} says {{.Greet \"x\"}} at {{.Age}}\n"))
	if err := tmpl.Execute(os.Stdout, p); err != nil {
		println("template error:", err.Error())
	}
	tmpl = template.Must(template.New("t").Parse("{{.Describe}} / {{.N.Name}}\n"))
	if err := tmpl.Execute(os.Stdout, struct {
		Person
		N Namer
	}{p, p}); err != nil {
		println("template error:", err.Error())
	}
}

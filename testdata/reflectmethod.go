package main

import "reflect"

type msg struct {
	Field string
}

func (m *msg) Reset()         {}
func (m *msg) String() string { return m.Field }

func main() {
	t := reflect.TypeOf(msg{})
	_, ok := reflect.PointerTo(t).MethodByName("XXX_OneofFuncs")
	println("MethodByName found:", ok)

	pt := reflect.PointerTo(t)
	println("NumMethod:", pt.NumMethod())
	for i := 0; i < pt.NumMethod(); i++ {
		println("Method index:", pt.Method(i).Index)
	}

	v := reflect.ValueOf(&msg{Field: "x"})
	println("Value.MethodByName valid:", v.MethodByName("String").IsValid())
	println("Value.MethodByName call:", v.MethodByName("String").Call(nil)[0].String())
	for i := 0; i < v.NumMethod(); i++ {
		println("Value.Method valid:", v.Method(i).IsValid())
	}
	println("Field by name:", v.Elem().FieldByName("Field").String())

	ft := reflect.TypeOf(func(int) error { return nil })
	println("func NumIn NumOut IsVariadic:", ft.NumIn(), ft.NumOut(), ft.IsVariadic())
	println("func String:", ft.String())
	add := func(a, b int) int { return a + b }
	println("func Call:", reflect.ValueOf(add).Call([]reflect.Value{reflect.ValueOf(2), reflect.ValueOf(3)})[0].Int())
}

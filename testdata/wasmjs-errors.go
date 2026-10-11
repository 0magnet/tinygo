package main

import "syscall/js"

func try(name string, f func()) {
	defer func() {
		if err, ok := recover().(js.Error); ok {
			println(name+":", err.Error())
		} else {
			println(name + ": no js.Error")
		}
	}()
	f()
}

func main() {
	// Property access that throws panics with a js.Error.
	throwing := js.Global().Get("throwing")
	try("Get", func() { throwing.Get("x") })
	try("Set", func() { throwing.Set("x", 1) })
	try("Delete", func() { throwing.Delete("x") })
	try("Index", func() { throwing.Index(0) })
	try("SetIndex", func() { throwing.SetIndex(0, 1) })
	println("done")
}

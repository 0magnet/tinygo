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
	g := js.Global()

	// Property access that throws panics with a js.Error.
	throwing := g.Get("throwing")
	try("Get", func() { throwing.Get("x") })
	try("Set", func() { throwing.Set("x", 1) })
	try("Delete", func() { throwing.Delete("x") })
	try("Index", func() { throwing.Index(0) })
	try("SetIndex", func() { throwing.SetIndex(0, 1) })

	// The second call starts while the event for the first still waits to
	// be dispatched. Each callback must get its own event.
	g.Set("first", js.FuncOf(func(js.Value, []js.Value) any { return "first" }))
	g.Set("second", js.FuncOf(func(js.Value, []js.Value) any { return "second" }))
	done := make(chan bool)
	go func() {
		println("first ->", g.Call("first").String())
		done <- true
	}()
	go func() {
		println("second ->", g.Call("second").String())
		done <- true
	}()
	<-done
	<-done

	// An exception from an unchecked import ends the program.
	g.Set("ping", js.FuncOf(func(js.Value, []js.Value) any { return "pong" }))
	g.Set("crash", js.FuncOf(func(js.Value, []js.Value) any {
		return g.Get("throwingLength").Length()
	}))
	g.Call("ready")
	select {}
}

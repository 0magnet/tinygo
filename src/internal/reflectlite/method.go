package reflectlite

// This file has no string literals or new interface types, so that a program
// which does not use Method compiles to the same binary as before.

import (
	"internal/gclayout"
	"unsafe"
)

// methodTable lists the exported methods of a type sorted by name, or all
// methods of an interface type. See compiler/reflectmethods.go.
type methodTable struct {
	typecode *RawType
	len      uintptr
	first    [0]methodEntry
}

// methodEntry is one method. fn is called as a func value of type typ with
// the receiver's interface data word as its context.
type methodEntry struct {
	name string
	typ  *RawType
	fn   unsafe.Pointer
}

// methodTableList is the list transform.LowerReflectMethods defines. It holds
// the tables of the types a program can reach, if the program uses Method.
type methodTableList struct {
	len    uintptr
	tables [0]*methodTable
}

// methodTables is a function rather than a variable so that a program without
// Method keeps no debug info for it.
//
//go:linkname methodTables reflect/types.methodtables
func methodTables() *methodTableList

func (t *methodTable) entries() []methodEntry {
	return unsafe.Slice((*methodEntry)(unsafe.Pointer(&t.first)), t.len)
}

// find returns the index of the named method, or -1. A binary search would add a
// runtime.stringLess reference, which changes the link of every program.
func (t *methodTable) find(name string) int {
	for i, m := range t.entries() {
		if m.name == name {
			return i
		}
	}
	return -1
}

// typeMethods returns the method table of t, or nil if it has none.
func typeMethods(t *RawType) *methodTable {
	list := methodTables()
	tables := unsafe.Slice((**methodTable)(unsafe.Pointer(&list.tables)), list.len)
	for _, table := range tables {
		if table.typecode == t {
			return table
		}
	}
	return nil
}

// valueMethod implements Value.Method. It panics with missing if the program
// kept no method table for the type of v.
func valueMethod(v Value, i int, missing any) Value {
	if v.Kind() == Interface {
		table := interfaceMethods(v)
		if table == nil {
			panic(missing)
		}
		return v.Elem().MethodByName(table.entries()[i].name)
	}
	var entries []methodEntry
	if table := typeMethods(v.typecode); table != nil {
		entries = table.entries()
	}
	if len(entries) != v.NumMethod() {
		panic(missing)
	}
	return bindMethod(v, &entries[i], missing)
}

// valueMethodByName implements Value.MethodByName.
func valueMethodByName(v Value, name string, missing any) Value {
	if v.Kind() == Interface {
		if table := interfaceMethods(v); table != nil && table.find(name) < 0 {
			return Value{}
		}
		return v.Elem().MethodByName(name)
	}
	table := typeMethods(v.typecode)
	if table == nil {
		return Value{}
	}
	i := table.find(name)
	if i < 0 {
		return Value{}
	}
	return bindMethod(v, &table.entries()[i], missing)
}

func interfaceMethods(v Value) *methodTable {
	if table := typeMethods(v.typecode); table != nil {
		return table
	}
	return typeMethods(v.typecode.underlying())
}

// bindMethod returns a func value that calls m with v as the receiver. It
// panics with missing if m cannot be called through reflection.
func bindMethod(v Value, m *methodEntry, missing any) Value {
	if m.fn == nil {
		panic(missing)
	}
	// The receiver is passed as an interface data word, which holds a value
	// that fits in a pointer and points at a copy of a larger one.
	var recv unsafe.Pointer
	size := v.typecode.Size()
	switch {
	case size > unsafe.Sizeof(uintptr(0)):
		recv = alloc(size, v.typecode.gcLayout())
		memcpy(recv, v.value, size)
	case v.isIndirect():
		recv = loadSmallValue(v.value, size)
	default:
		recv = v.value
	}
	fn := (*funcHeader)(alloc(unsafe.Sizeof(funcHeader{}), gclayout.Pointer.AsPtr()))
	fn.Context = recv
	fn.Code = m.fn
	return Value{
		typecode: m.typ,
		value:    unsafe.Pointer(fn),
		flags:    v.flags & (valueFlagExported | valueFlagRO),
	}
}

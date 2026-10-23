package reflectlite

import (
	"internal/gclayout"
	"unsafe"
)

// Call calls the function v with the input arguments in. As in Go, an extra
// argument of a variadic function goes in its final slice parameter.
func (v Value) Call(in []Value) []Value {
	return v.call("Call", in, false)
}

// CallSlice calls the variadic function v with the input arguments in,
// passing the last one as the variadic slice itself.
func (v Value) CallSlice(in []Value) []Value {
	return v.call("CallSlice", in, true)
}

func (v Value) call(op string, in []Value, isSlice bool) []Value {
	if v.Kind() != Func {
		panic(&ValueError{Method: "reflect.Value." + op, Kind: v.Kind()})
	}
	t := v.typecode
	ft := t.funcType(op)
	// A func value is two words, so v.value points at it.
	fn := *(*funcHeader)(v.value)
	if fn.Code == nil {
		panic("reflect: call of nil function")
	}

	numIn := t.NumIn()
	if isSlice {
		if !t.IsVariadic() {
			panic("reflect: CallSlice of non-variadic function")
		}
		if len(in) != numIn {
			panic("reflect: CallSlice with wrong argument count")
		}
	} else if t.IsVariadic() {
		fixed := numIn - 1
		if len(in) < fixed {
			panic("reflect: Call with too few input arguments")
		}
		st := t.In(fixed)
		extra := MakeSlice(st, len(in)-fixed, len(in)-fixed)
		for i := range len(in) - fixed {
			extra.Index(i).Set(in[fixed+i])
		}
		in = append(in[:fixed:fixed], extra)
	} else if len(in) != numIn {
		if len(in) < numIn {
			panic("reflect: Call with too few input arguments")
		}
		panic("reflect: Call with too many input arguments")
	}

	argOffsets, argSize := tupleLayout(numIn, t.In)
	args := allocWords(argSize)
	for i, x := range in {
		pt := t.In(i)
		if !x.typecode.AssignableTo(pt) {
			panic("reflect: " + op + " using " + x.typecode.String() + " as type " + pt.String())
		}
		Value{typecode: pt, value: unsafe.Add(args, argOffsets[i]), flags: valueFlagIndirect | valueFlagExported}.Set(x)
	}

	numOut := t.NumOut()
	resultOffsets, resultSize := tupleLayout(numOut, t.Out)
	results := allocWords(resultSize)

	// The call stub is a plain function; called as a func value it gets a
	// nil context as its last argument, which it ignores.
	stub := funcHeader{Code: ft.call}
	callStub := *(*func(code, context, args, results unsafe.Pointer))(unsafe.Pointer(&stub))
	callStub(fn.Code, fn.Context, args, results)

	out := make([]Value, numOut)
	for i := range out {
		rt := t.Out(i)
		p := unsafe.Add(results, resultOffsets[i])
		// Results are not addressable: a small one is held in the Value
		// itself, a larger one by a pointer without the indirect flag.
		if size := rt.Size(); size <= unsafe.Sizeof(uintptr(0)) {
			out[i] = Value{typecode: rt, value: unsafe.Pointer(loadValue(p, size)), flags: valueFlagExported}
		} else {
			out[i] = Value{typecode: rt, value: p, flags: valueFlagExported}
		}
	}
	return out
}

// allocWords allocates size bytes that the GC scans as pointers, since a list
// of parameters or results has no single layout of its own.
func allocWords(size uintptr) unsafe.Pointer {
	word := unsafe.Sizeof(uintptr(0))
	return alloc((size+word-1)&^(word-1), gclayout.Pointer.AsPtr())
}

// tupleLayout lays out n values of the types at returns as the fields of a
// struct, matching the call stubs in compiler/reflectcall.go.
func tupleLayout(n int, at func(int) *RawType) (offsets []uintptr, size uintptr) {
	offsets = make([]uintptr, n)
	for i := range n {
		t := at(i)
		align := uintptr(t.Align())
		size = (size + align - 1) &^ (align - 1)
		offsets[i] = size
		size += t.Size()
	}
	return offsets, size
}

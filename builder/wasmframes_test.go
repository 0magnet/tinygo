package builder

import (
	"bytes"
	"testing"
)

func wasmSection(id byte, content []byte) []byte {
	return append(appendLEB([]byte{id}, uint64(len(content))), content...)
}

func wasmBody(code []byte) []byte {
	body := append([]byte{1, 1, 0x7f}, code...) // one i32 local
	return append(appendLEB(nil, uint64(len(body))), body...)
}

const framePrologue = 9 // global.get, i32.const 32, i32.sub, local.tee 0, global.set

// frameCode allocates a 32 byte frame in local 0, runs call, and frees the
// frame again.
func frameCode(call ...byte) []byte {
	code := []byte{0x23, 0x00, 0x41, 0x20, 0x6b, 0x22, 0x00, 0x24, 0x00}
	code = append(code, call...)
	return append(code, 0x20, 0x00, 0x41, 0x20, 0x6a, 0x24, 0x00, 0x0b)
}

func TestZeroWasmFrames(t *testing.T) {
	bodies := [][]byte{
		frameCode(0x10, 0x00),                               // calls the unwind import
		frameCode(),                                         // leaf
		frameCode(0x41, 0x00, 0x11, 0x00, 0x00),             // indirect call that may reach function 1
		frameCode(0x41, 0x00, 0x41, 0x00, 0x11, 0x01, 0x00), // indirect call with no pausing target
	}
	pausing := []bool{true, false, true, false}

	build := func(bodies [][]byte) []byte {
		var m []byte
		m = append(m, "\x00asm\x01\x00\x00\x00"...)
		m = append(m, wasmSection(1, []byte{2, 0x60, 0, 0, 0x60, 1, 0x7f, 0})...)
		imp := []byte{1, 8}
		imp = append(imp, "asyncify"...)
		imp = append(imp, 12)
		imp = append(imp, "start_unwind"...)
		imp = append(imp, 0, 0)
		m = append(m, wasmSection(2, imp)...)
		m = append(m, wasmSection(3, []byte{4, 0, 0, 0, 0})...)
		m = append(m, wasmSection(4, []byte{1, 0x70, 0, 1})...)
		m = append(m, wasmSection(6, []byte{1, 0x7f, 1, 0x41, 0x80, 0x80, 0x04, 0x0b})...)
		m = append(m, wasmSection(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 1})...) // table[0] = function 1
		code := []byte{byte(len(bodies))}
		for _, b := range bodies {
			code = append(code, wasmBody(b)...)
		}
		return append(m, wasmSection(10, code)...)
	}

	out, err := zeroWasmFramesBytes(build(bodies), false)
	if err != nil {
		t.Fatal(err)
	}
	clr := appendFrameClear(nil, 0, 32, false)
	if len(clr) != 4*7 {
		t.Errorf("clear code is %d bytes, want four i64 stores", len(clr))
	}
	var want [][]byte
	for i, b := range bodies {
		if pausing[i] {
			b = append(b[:framePrologue:framePrologue], append(clr, b[framePrologue:]...)...)
		}
		want = append(want, b)
	}
	if !bytes.Equal(out, build(want)) {
		t.Errorf("rewritten module differs\n got: %x\nwant: %x", out, build(want))
	}
}

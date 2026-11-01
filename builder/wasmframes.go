package builder

import (
	"bytes"
	"errors"
	"os"
)

// zeroWasmFrames makes each function that may pause clear its new stack frame,
// so words left there by earlier deeper calls do not pin objects while paused.
func zeroWasmFrames(path string, bulkMemory bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := zeroWasmFramesBytes(data, bulkMemory)
	if err != nil || out == nil {
		return err
	}
	return os.WriteFile(path, out, 0o666)
}

var errWasmDecode = errors.New("wasm: cannot decode")

type wasmReader struct {
	b   []byte
	p   int
	err error
}

func (r *wasmReader) byte() byte {
	if r.p >= len(r.b) {
		r.err = errWasmDecode
		return 0
	}
	c := r.b[r.p]
	r.p++
	return c
}

// leb reads an unsigned LEB128 number of up to 64 bits.
func (r *wasmReader) leb() uint64 {
	var v uint64
	for shift := 0; shift < 70; shift += 7 {
		c := r.byte()
		v |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return v
		}
	}
	r.err = errWasmDecode
	return 0
}

// sleb reads a signed LEB128 number of up to 64 bits.
func (r *wasmReader) sleb() int64 {
	var v int64
	shift := 0
	for {
		c := r.byte()
		v |= int64(c&0x7f) << shift
		shift += 7
		if c&0x80 == 0 {
			if shift < 64 && c&0x40 != 0 {
				v |= -1 << shift
			}
			return v
		}
		if shift >= 70 {
			r.err = errWasmDecode
			return 0
		}
	}
}

func (r *wasmReader) bytes(n int) []byte {
	if n < 0 || r.p+n > len(r.b) {
		r.err = errWasmDecode
		return nil
	}
	b := r.b[r.p : r.p+n]
	r.p += n
	return b
}

func appendLEB(b []byte, v uint64) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b = append(b, c|0x80)
			continue
		}
		return append(b, c)
	}
}

func appendSLEB(b []byte, v int64) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && c&0x40 == 0) || (v == -1 && c&0x40 != 0) {
			return append(b, c)
		}
		b = append(b, c|0x80)
	}
}

type wasmFrame struct {
	at    int // offset in the body just after the stack pointer store
	local uint64
	size  int64
}

type wasmFunc struct {
	body     []byte // locals and code, without the size prefix
	callees  []uint64
	indirect []uint64 // type indices of its indirect calls
	ok       bool
	frames   []wasmFrame
}

// decodeWasmFunc records the calls of a function body and the places where it
// allocates a constant sized stack frame.
func decodeWasmFunc(body []byte, sp uint64) wasmFunc {
	f := wasmFunc{body: body}
	r := &wasmReader{b: body}
	for n := r.leb(); n > 0 && r.err == nil; n-- {
		r.leb()
		r.byte()
	}
	type inst struct {
		op  byte
		imm int64
	}
	var prev [3]inst
	push := func(op byte, imm int64) {
		prev[0], prev[1], prev[2] = prev[1], prev[2], inst{op, imm}
	}
	for r.p < len(body) && r.err == nil {
		op := r.byte()
		var imm int64
		switch {
		case op == 0x00 || op == 0x01 || op == 0x05 || op == 0x0b || op == 0x0f || op == 0x1a || op == 0x1b || op == 0xd1:
		case op >= 0x45 && op <= 0xc4:
		case op == 0x02 || op == 0x03 || op == 0x04:
			if c := r.byte(); c != 0x40 && (c < 0x6f || c > 0x7f) {
				r.p--
				r.sleb()
			}
		case op == 0x0c || op == 0x0d || op == 0x25 || op == 0x26 || op == 0xd2:
			r.leb()
		case op == 0x0e:
			for n := r.leb(); n > 0 && r.err == nil; n-- {
				r.leb()
			}
			r.leb()
		case op == 0x10 || op == 0x12:
			f.callees = append(f.callees, r.leb())
		case op == 0x11 || op == 0x13:
			f.indirect = append(f.indirect, r.leb())
			r.leb()
		case op == 0x1c:
			for n := r.leb(); n > 0 && r.err == nil; n-- {
				r.byte()
			}
		case op >= 0x20 && op <= 0x24:
			imm = int64(r.leb())
			// A frame allocation is i32.const N, i32.sub, local.tee L, global.set SP.
			if op == 0x24 && uint64(imm) == sp && prev[0].op == 0x41 && prev[0].imm > 0 && prev[0].imm%4 == 0 && prev[1].op == 0x6b && prev[2].op == 0x22 {
				f.frames = append(f.frames, wasmFrame{at: r.p, local: uint64(prev[2].imm), size: prev[0].imm})
			}
		case op >= 0x28 && op <= 0x3e:
			if align := r.leb(); align&0x40 != 0 {
				r.leb()
			}
			r.leb()
		case op == 0x3f || op == 0x40:
			r.leb()
		case op == 0x41 || op == 0x42:
			imm = r.sleb()
		case op == 0x43:
			r.bytes(4)
		case op == 0x44:
			r.bytes(8)
		case op == 0xd0:
			r.sleb()
		case op == 0xfc:
			switch sub := r.leb(); {
			case sub <= 7:
			case sub == 9 || sub == 11 || sub == 13 || sub == 15 || sub == 16 || sub == 17:
				r.leb()
			case sub == 8 || sub == 10 || sub == 12 || sub == 14:
				r.leb()
				r.leb()
			default:
				return wasmFunc{}
			}
		default:
			// SIMD, atomics, exceptions and the like are not expected here.
			return wasmFunc{}
		}
		push(op, imm)
	}
	f.ok = r.err == nil
	return f
}

// zeroWasmFramesBytes returns the rewritten module, or nil when there is
// nothing to change.
func zeroWasmFramesBytes(data []byte, bulkMemory bool) ([]byte, error) {
	if len(data) < 8 || !bytes.Equal(data[:4], []byte("\x00asm")) {
		return nil, errWasmDecode
	}
	type section struct {
		id         byte
		start, end int // content
		hdr        int // start of the section id byte
	}
	var sections []section
	r := &wasmReader{b: data, p: 8}
	for r.p < len(data) && r.err == nil {
		hdr := r.p
		id := r.byte()
		size := int(r.leb())
		start := r.p
		r.bytes(size)
		sections = append(sections, section{id, start, r.p, hdr})
	}
	if r.err != nil {
		return nil, r.err
	}

	var numFuncImports, numGlobalImports uint64
	var sigs []string       // signature of each type
	var funcTypes []uint64  // type of each defined function
	var tableFuncs []uint64 // functions placed in tables
	preciseIndirect := true
	unwindImport := ^uint64(0)
	sp := ^uint64(0)
	codeSection := -1
	for i, s := range sections {
		r := &wasmReader{b: data[:s.end], p: s.start}
		switch s.id {
		case 1: // types
			for n := r.leb(); n > 0 && r.err == nil; n-- {
				if r.byte() != 0x60 {
					preciseIndirect = false
					break
				}
				start := r.p
				r.bytes(int(r.leb()))
				r.bytes(int(r.leb()))
				sigs = append(sigs, string(data[start:r.p]))
			}
		case 3: // functions
			for n := r.leb(); n > 0 && r.err == nil; n-- {
				funcTypes = append(funcTypes, r.leb())
			}
		case 9: // elements
			for n := r.leb(); n > 0 && r.err == nil; n-- {
				flags := r.leb()
				if flags > 3 {
					preciseIndirect = false
					break
				}
				if flags == 2 {
					r.leb()
				}
				if flags&1 == 0 {
					// The offset is a constant expression.
					for c := r.byte(); c != 0x0b && r.err == nil; c = r.byte() {
						if c == 0x41 || c == 0x42 {
							r.sleb()
						} else {
							r.leb()
						}
					}
				}
				if flags != 0 {
					r.byte()
				}
				for m := r.leb(); m > 0 && r.err == nil; m-- {
					tableFuncs = append(tableFuncs, r.leb())
				}
			}
		case 2: // imports
			for n := r.leb(); n > 0 && r.err == nil; n-- {
				module := string(r.bytes(int(r.leb())))
				name := string(r.bytes(int(r.leb())))
				switch r.byte() {
				case 0:
					if module == "asyncify" && name == "start_unwind" {
						unwindImport = numFuncImports
					}
					r.leb()
					numFuncImports++
				case 1:
					r.byte()
					flags := r.leb()
					r.leb()
					if flags&1 != 0 {
						r.leb()
					}
				case 2:
					flags := r.leb()
					r.leb()
					if flags&1 != 0 {
						r.leb()
					}
				case 3:
					r.byte()
					r.byte()
					numGlobalImports++
				default:
					return nil, errWasmDecode
				}
			}
		case 6: // globals
			// LLVM defines __stack_pointer as the first mutable i32 global.
			if r.leb() > 0 && r.byte() == 0x7f && r.byte() == 1 && numGlobalImports == 0 {
				sp = 0
			}
		case 10:
			codeSection = i
		}
		if r.err != nil {
			return nil, r.err
		}
	}
	if sp == ^uint64(0) || unwindImport == ^uint64(0) || codeSection < 0 {
		return nil, nil
	}

	// Decode every function.
	cs := sections[codeSection]
	r = &wasmReader{b: data[:cs.end], p: cs.start}
	count := r.leb()
	funcs := make([]wasmFunc, 0, count)
	for i := uint64(0); i < count && r.err == nil; i++ {
		size := int(r.leb())
		funcs = append(funcs, decodeWasmFunc(r.bytes(size), sp))
	}
	if r.err != nil {
		return nil, r.err
	}

	// A function may pause if it reaches the unwind import or cannot be decoded.
	// An indirect call may reach any function in a table with its signature.
	if len(funcTypes) != len(funcs) {
		preciseIndirect = false
	}
	for _, f := range funcs {
		for _, typ := range f.indirect {
			if typ >= uint64(len(sigs)) {
				preciseIndirect = false
			}
		}
	}
	for _, typ := range funcTypes {
		if typ >= uint64(len(sigs)) {
			preciseIndirect = false
		}
	}
	sig := func(typ uint64) string {
		if typ < uint64(len(sigs)) {
			return sigs[typ]
		}
		return ""
	}
	callers := make([][]int, len(funcs))
	indirectCallers := map[string][]int{}
	mayPause := make([]bool, len(funcs))
	var worklist []int
	mark := func(i int) {
		if !mayPause[i] {
			mayPause[i] = true
			worklist = append(worklist, i)
		}
	}
	for i, f := range funcs {
		if !f.ok {
			mark(i)
		}
		for _, typ := range f.indirect {
			k := sig(typ)
			indirectCallers[k] = append(indirectCallers[k], i)
		}
		for _, callee := range f.callees {
			if callee == unwindImport {
				mark(i)
			}
			if callee >= numFuncImports && callee-numFuncImports < uint64(len(funcs)) {
				c := int(callee - numFuncImports)
				callers[c] = append(callers[c], i)
			}
		}
	}
	inTable := make([]bool, len(funcs))
	for _, fn := range tableFuncs {
		if fn >= numFuncImports && fn-numFuncImports < uint64(len(funcs)) {
			inTable[fn-numFuncImports] = true
		}
	}
	if !preciseIndirect {
		for i, f := range funcs {
			if len(f.indirect) != 0 {
				mark(i)
			}
		}
	}
	sigPauses := map[string]bool{}
	for len(worklist) != 0 {
		f := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]
		for _, caller := range callers[f] {
			mark(caller)
		}
		if inTable[f] && preciseIndirect {
			if k := sig(funcTypes[f]); !sigPauses[k] {
				sigPauses[k] = true
				for _, caller := range indirectCallers[k] {
					mark(caller)
				}
			}
		}
	}

	// Rewrite the code section.
	var code []byte
	code = appendLEB(code, count)
	changed := false
	for i, f := range funcs {
		body := f.body
		if f.ok && mayPause[i] && len(f.frames) != 0 {
			var nb []byte
			last := 0
			for _, fr := range f.frames {
				nb = append(nb, body[last:fr.at]...)
				nb = appendFrameClear(nb, fr.local, fr.size, bulkMemory)
				last = fr.at
			}
			body = append(nb, body[last:]...)
			changed = true
		}
		code = appendLEB(code, uint64(len(body)))
		code = append(code, body...)
	}
	if !changed {
		return nil, nil
	}
	out := make([]byte, 0, len(data)+len(code)-(cs.end-cs.start))
	out = append(out, data[:cs.hdr]...)
	out = append(out, 10)
	out = appendLEB(out, uint64(len(code)))
	out = append(out, code...)
	out = append(out, data[cs.end:]...)
	return out, nil
}

// appendFrameClear appends code that zeroes size bytes at the address in the
// given local.
func appendFrameClear(b []byte, local uint64, size int64, bulkMemory bool) []byte {
	if bulkMemory && size > 64 {
		b = append(b, 0x20)
		b = appendLEB(b, local)
		b = append(b, 0x41, 0x00, 0x41)
		b = appendSLEB(b, size)
		return append(b, 0xfc, 0x0b, 0x00) // memory.fill 0
	}
	for off := int64(0); off < size; {
		b = append(b, 0x20)
		b = appendLEB(b, local)
		if size-off >= 8 {
			b = append(b, 0x42, 0x00, 0x37, 0x03) // i64.const 0, i64.store align=8
			b = appendLEB(b, uint64(off))
			off += 8
		} else {
			b = append(b, 0x41, 0x00, 0x36, 0x02) // i32.const 0, i32.store align=4
			b = appendLEB(b, uint64(off))
			off += 4
		}
	}
	return b
}

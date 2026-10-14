//go:build llvm23

package interp

import "tinygo.org/x/go-llvm"

// opBr is the opcode interp uses for every branch instruction, conditional or
// not. LLVM 23 has two opcodes for them, UncondBr and CondBr; interp keeps
// one, and tells the two forms apart by their number of successors.
const opBr = llvm.UncondBr

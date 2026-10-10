//go:build !llvm23

package interp

import "tinygo.org/x/go-llvm"

// opBr is the opcode interp uses for every branch instruction, conditional or
// not. The two forms are told apart by their number of operands.
const opBr = llvm.Br

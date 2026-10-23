package compiler

// This file creates the call stubs reflect.Value.Call uses to call a func
// value whose signature is only known at run time.

import (
	"go/token"
	"go/types"
	"strconv"

	"tinygo.org/x/go-llvm"
)

// getReflectCallStub returns the call stub for func values of signature sig,
// stored in the call field of its type descriptor. Go code calls it as a
//
//	func(code, context, args, results unsafe.Pointer)
//
// where args points to the parameters and results to room for the results,
// each laid out as the fields of a struct. The stub loads the parameters,
// calls code with context the way a call of the func value would, and stores
// the results.
func (c *compilerContext) getReflectCallStub(sig *types.Signature) llvm.Value {
	typeCodeName, isLocal := c.getTypeCodeName(sig)
	name := "reflect/types.call:" + typeCodeName
	if !isLocal {
		if fn := c.mod.NamedFunction(name); !fn.IsNil() {
			return fn
		}
	}

	ptr := c.dataPtrType
	stubType := llvm.FunctionType(c.ctx.VoidType(), []llvm.Type{ptr, ptr, ptr, ptr, ptr}, false)
	stub := llvm.AddFunction(c.mod, name, stubType)
	c.addStandardAttributes(stub)
	if isLocal {
		// A type local to a function is described once, so its stub is made
		// once; another function's type of the same name gets its own.
		stub.SetLinkage(llvm.InternalLinkage)
	} else {
		stub.SetLinkage(llvm.LinkOnceODRLinkage)
	}
	stub.SetUnnamedAddr(true)

	b := &builder{
		compilerContext: c,
		Builder:         c.ctx.NewBuilder(),
	}
	defer b.Dispose()
	b.SetInsertPointAtEnd(c.ctx.AddBasicBlock(stub, "entry"))
	code, context, args, results := stub.Param(0), stub.Param(1), stub.Param(2), stub.Param(3)

	abi := c.getFunctionABI(sig, false)
	argsType := c.getLLVMType(tupleStruct(sig.Params()))
	var params []llvm.Value
	for i := 0; i < sig.Params().Len(); i++ {
		field := b.CreateStructGEP(argsType, args, i, "")
		if abi.params[i].indirect {
			// The callee may treat the memory as its own copy, which the
			// args buffer is.
			params = append(params, field)
			continue
		}
		params = append(params, b.CreateLoad(c.getLLVMType(sig.Params().At(i).Type()), field, ""))
	}

	fnType := c.getLLVMFunctionType(sig)
	if abi.indirectResult {
		// The results buffer has the layout of the result struct.
		params = append([]llvm.Value{results}, params...)
		params = append(params, context)
		b.createCall(fnType, code, params, "")
		b.CreateRetVoid()
		return stub
	}
	params = append(params, context)
	result := b.createCall(fnType, code, params, "")
	switch sig.Results().Len() {
	case 0:
	case 1:
		b.CreateStore(result, results)
	default:
		resultsType := c.getLLVMType(tupleStruct(sig.Results()))
		for i := 0; i < sig.Results().Len(); i++ {
			field := b.CreateStructGEP(resultsType, results, i, "")
			b.CreateStore(b.CreateExtractValue(result, i, ""), field)
		}
	}
	b.CreateRetVoid()
	return stub
}

// tupleStruct returns a struct with the types of t as its fields, the layout
// reflect gives a list of parameters or results.
func tupleStruct(t *types.Tuple) *types.Struct {
	fields := make([]*types.Var, t.Len())
	for i := range fields {
		fields[i] = types.NewVar(token.NoPos, nil, "f"+strconv.Itoa(i), t.At(i).Type())
	}
	return types.NewStruct(fields, nil)
}

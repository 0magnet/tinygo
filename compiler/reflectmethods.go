package compiler

// This file creates the method tables reflect.Value.Method reads. The
// transform package decides which of them stay, see LowerReflectMethods.

import (
	"go/types"
	"sort"

	"github.com/tinygo-org/tinygo/compiler/llvmutil"
	"tinygo.org/x/go-llvm"
)

// createReflectMethodTables emits {typecode, len, [len]{name, func type, func}}
// for each type in c.reflectTypes, as methodTable in internal/reflectlite reads.
func (c *compilerContext) createReflectMethodTables() {
	var tables []llvm.Value
	for i := 0; i < len(c.reflectTypes); i++ {
		if table := c.createReflectMethodTable(c.reflectTypes[i]); !table.IsNil() {
			tables = append(tables, table)
		}
	}
	if len(tables) == 0 {
		return
	}
	// Keep the tables until transform.LowerReflectMethods decides on them.
	llvmutil.AppendToGlobal(c.mod, "llvm.used", tables...)
	llvmutil.AppendToGlobal(c.mod, "tinygo.reflectmethods", tables...)
}

func (c *compilerContext) createReflectMethodTable(typ types.Type) llvm.Value {
	typeCodeName, isLocal := c.getTypeCodeName(typ)
	_, isInterface := typ.Underlying().(*types.Interface)
	var methods []*types.Selection
	for method := range c.program.MethodSets.MethodSet(typ).Methods() {
		fn := method.Obj().(*types.Func)
		if isGenericMethod(fn) || !isInterface && !fn.Exported() {
			continue
		}
		methods = append(methods, method)
	}
	if len(methods) == 0 {
		return llvm.Value{}
	}
	sort.Slice(methods, func(i, j int) bool {
		return methods[i].Obj().Name() < methods[j].Obj().Name()
	})

	typeCode := c.getTypeCode(typ)
	entryType := c.ctx.StructType([]llvm.Type{c.dataPtrType, c.uintptrType, c.dataPtrType, c.dataPtrType}, false)
	entries := make([]llvm.Value, len(methods))
	for i, method := range methods {
		name := method.Obj().Name()
		sig := method.Obj().Type().(*types.Signature)
		funcType := types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
		bound := llvm.ConstNull(c.dataPtrType)
		if !isInterface {
			if fn := c.getReflectMethodFunc(method, funcType, isLocal); !fn.IsNil() {
				bound = fn
			}
		}
		entries[i] = llvm.ConstNamedStruct(entryType, []llvm.Value{
			c.getReflectMethodName(name),
			llvm.ConstInt(c.uintptrType, uint64(len(name)), false),
			c.getTypeCode(funcType),
			bound,
		})
	}
	value := c.ctx.ConstStruct([]llvm.Value{
		typeCode,
		llvm.ConstInt(c.uintptrType, uint64(len(entries)), false),
		llvm.ConstArray(entryType, entries),
	}, false)
	global := llvm.AddGlobal(c.mod, value.Type(), typeCodeName+"$reflectmethods")
	global.SetInitializer(value)
	global.SetGlobalConstant(true)
	global.SetAlignment(c.targetData.ABITypeAlignment(c.dataPtrType))
	if isLocal {
		global.SetLinkage(llvm.InternalLinkage)
	} else {
		global.SetLinkage(llvm.LinkOnceODRLinkage)
	}
	return global
}

// getReflectMethodName returns a pointer to the bytes of a method name.
func (c *compilerContext) getReflectMethodName(name string) llvm.Value {
	globalName := "reflect/types.methodname:" + name
	global := c.mod.NamedGlobal(globalName)
	if global.IsNil() {
		value := c.ctx.ConstString(name, false)
		global = llvm.AddGlobal(c.mod, value.Type(), globalName)
		global.SetInitializer(value)
		global.SetAlignment(1)
		global.SetUnnamedAddr(true)
		global.SetLinkage(llvm.LinkOnceODRLinkage)
		global.SetGlobalConstant(true)
	}
	return global
}

// getReflectMethodFunc returns a func of type funcType that calls the invoke
// wrapper of method with its context as the receiver, or nil if it cannot.
func (c *compilerContext) getReflectMethodFunc(method *types.Selection, funcType *types.Signature, isLocal bool) llvm.Value {
	fn := c.program.MethodValue(method)
	if fn == nil {
		return llvm.Value{}
	}
	llvmFnType, llvmFn := c.getFunction(fn)
	wrapper := c.getInterfaceInvokeWrapper(fn, llvmFnType, llvmFn)
	if !wrapper.GetStringAttributeAtIndex(-1, "tinygo-interface-abi-error").IsNil() {
		return llvm.Value{}
	}
	name := wrapper.Name() + "$reflect"
	if f := c.mod.NamedFunction(name); !f.IsNil() {
		return f
	}

	// The wrapper takes the receiver after the result pointer, if any. Skip
	// a method whose other parameters do not line up with funcType.
	fnType := c.getLLVMFunctionType(funcType)
	wrapperType := wrapper.GlobalValueType()
	resultOffset := 0
	if c.getFunctionABI(funcType, false).indirectResult {
		resultOffset = 1
	}
	params := fnType.ParamTypes()
	wrapperParams := wrapperType.ParamTypes()
	if len(wrapperParams) != len(params)+1 || wrapperType.ReturnType() != fnType.ReturnType() {
		return llvm.Value{}
	}
	for i, t := range params {
		j := i
		if i >= resultOffset {
			j++
		}
		if wrapperParams[j] != t {
			return llvm.Value{}
		}
	}

	f := llvm.AddFunction(c.mod, name, fnType)
	c.addStandardAttributes(f)
	if isLocal {
		f.SetLinkage(llvm.InternalLinkage)
	} else {
		f.SetLinkage(llvm.LinkOnceODRLinkage)
	}
	f.SetUnnamedAddr(true)
	b := &builder{
		compilerContext: c,
		Builder:         c.ctx.NewBuilder(),
	}
	defer b.Dispose()
	if c.Debug {
		pos := c.program.Fset.Position(fn.Pos())
		difunc := c.attachDebugInfoRaw(fn, f, "$reflect", pos.Filename, pos.Line)
		b.SetCurrentDebugLocation(uint(pos.Line), uint(pos.Column), difunc, llvm.Metadata{})
	}
	b.SetInsertPointAtEnd(c.ctx.AddBasicBlock(f, "entry"))
	in := f.Params()
	context := in[len(in)-1]
	args := append([]llvm.Value{}, in[:resultOffset]...)
	args = append(args, context)
	args = append(args, in[resultOffset:len(in)-1]...)
	args = append(args, llvm.ConstNull(c.dataPtrType))
	if wrapperType.ReturnType().TypeKind() == llvm.VoidTypeKind {
		b.CreateCall(wrapperType, wrapper, args, "")
		b.CreateRetVoid()
	} else {
		b.CreateRet(b.CreateCall(wrapperType, wrapper, args, ""))
	}
	return f
}

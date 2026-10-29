package transform

// This file decides which method tables from compiler/reflectmethods.go stay
// in the program, and lists them in reflect/types.methodtables.

import (
	"strings"

	"github.com/tinygo-org/tinygo/compiler/llvmutil"
	"tinygo.org/x/go-llvm"
)

// LowerReflectMethods keeps the method tables of reachable types if Value.Method
// is reachable without them, as the Go linker does, and drops all others.
func LowerReflectMethods(mod llvm.Module) {
	var tables []llvm.Value
	if marker := mod.NamedGlobal("tinygo.reflectmethods"); !marker.IsNil() {
		seen := make(map[llvm.Value]bool)
		init := marker.Initializer()
		for i := 0; i < init.OperandsCount(); i++ {
			table := stripConstantExprs(init.Operand(i))
			if !seen[table] {
				seen[table] = true
				tables = append(tables, table)
			}
		}
		llvmutil.RemoveGlobalReferences(mod, "llvm.used", "tinygo.reflectmethods")
	}

	fn := mod.NamedFunction("reflect/types.methodtables")
	var kept []llvm.Value
	if len(tables) != 0 && !fn.IsNil() {
		m := newReachability(mod)
		if m.marked[fn] {
			// A kept table can make more type codes reachable.
			for changed := true; changed; {
				changed = false
				for _, table := range tables {
					typecode := stripConstantExprs(table.Initializer().Operand(0))
					if m.marked[typecode] && !m.marked[table] {
						m.mark(table)
						changed = true
					}
				}
			}
			for _, table := range tables {
				if m.marked[table] {
					kept = append(kept, table)
				}
			}
		}
	}

	if !fn.IsNil() && fn.IsDeclaration() {
		ctx := mod.Context()
		ptrType := llvm.PointerType(ctx.Int8Type(), 0)
		targetData := llvm.NewTargetData(mod.DataLayout())
		defer targetData.Dispose()
		uintptrType := ctx.IntType(targetData.PointerSize() * 8)
		value := ctx.ConstStruct([]llvm.Value{
			llvm.ConstInt(uintptrType, uint64(len(kept)), false),
			llvm.ConstArray(ptrType, kept),
		}, false)
		global := llvm.AddGlobal(mod, value.Type(), "reflect/types.methodtables.list")
		global.SetInitializer(value)
		global.SetGlobalConstant(true)
		global.SetLinkage(llvm.InternalLinkage)
		global.SetAlignment(targetData.ABITypeAlignment(ptrType))

		fn.SetLinkage(llvm.InternalLinkage)
		builder := ctx.NewBuilder()
		defer builder.Dispose()
		builder.SetInsertPointAtEnd(ctx.AddBasicBlock(fn, "entry"))
		builder.CreateRet(global)
	}

	isKept := make(map[llvm.Value]bool, len(kept))
	for _, table := range kept {
		isKept[table] = true
	}
	for _, table := range tables {
		if !isKept[table] {
			table.EraseFromParentAsGlobal()
		}
	}
}

// stripConstantExprs returns the global a constant expression is based on.
func stripConstantExprs(value llvm.Value) llvm.Value {
	for !value.IsAConstantExpr().IsNil() {
		value = value.Operand(0)
	}
	return value
}

// reachability marks what the roots of a module reach. As interface lowering has
// not run yet, it follows a $methodset method only once its signature is invoked.
type reachability struct {
	marked    map[llvm.Value]bool
	constants map[llvm.Value]bool
	invoked   map[string]bool         // signatures of reachable interface calls
	pending   map[string][]llvm.Value // methods waiting for such a call
	work      []llvm.Value
}

func newReachability(mod llvm.Module) *reachability {
	m := &reachability{
		marked:    make(map[llvm.Value]bool),
		constants: make(map[llvm.Value]bool),
		invoked:   make(map[string]bool),
		pending:   make(map[string][]llvm.Value),
	}
	for fn := mod.FirstFunction(); !fn.IsNil(); fn = llvm.NextFunction(fn) {
		if !isDiscardable(fn.Linkage()) && !fn.IsDeclaration() {
			m.mark(fn)
		}
	}
	for global := mod.FirstGlobal(); !global.IsNil(); global = llvm.NextGlobal(global) {
		if !isDiscardable(global.Linkage()) && !global.IsDeclaration() {
			m.mark(global)
		}
	}
	return m
}

func isDiscardable(linkage llvm.Linkage) bool {
	switch linkage {
	case llvm.LinkOnceAnyLinkage, llvm.LinkOnceODRLinkage, llvm.InternalLinkage, llvm.PrivateLinkage, llvm.AvailableExternallyLinkage:
		return true
	}
	return false
}

// mark marks a global value and everything it refers to.
func (m *reachability) mark(root llvm.Value) {
	m.add(root)
	for len(m.work) != 0 {
		v := m.work[len(m.work)-1]
		m.work = m.work[:len(m.work)-1]
		switch {
		case !v.IsAFunction().IsNil():
			if attr := v.GetStringAttributeAtIndex(-1, "tinygo-invoke"); !attr.IsNil() {
				m.invoke(attr.GetStringValue())
			}
			for bb := v.FirstBasicBlock(); !bb.IsNil(); bb = llvm.NextBasicBlock(bb) {
				for inst := bb.FirstInstruction(); !inst.IsNil(); inst = llvm.NextInstruction(inst) {
					for i := 0; i < inst.OperandsCount(); i++ {
						m.visit(inst.Operand(i))
					}
				}
			}
		case !v.IsAGlobalVariable().IsNil() && strings.HasSuffix(v.Name(), "$methodset"):
			// {length, [n]signature, {n methods}}, see getTypeMethodSet.
			init := v.Initializer()
			signatures, methods := init.Operand(1), init.Operand(2)
			for i := 0; i < signatures.OperandsCount(); i++ {
				signature := stripConstantExprs(signatures.Operand(i)).Name()
				method := stripConstantExprs(methods.Operand(i))
				if m.invoked[signature] {
					m.add(method)
				} else {
					m.pending[signature] = append(m.pending[signature], method)
				}
			}
		case !v.IsAGlobalVariable().IsNil():
			if init := v.Initializer(); !init.IsNil() {
				m.visit(init)
			}
		}
	}
}

func (m *reachability) add(v llvm.Value) {
	if !m.marked[v] {
		m.marked[v] = true
		m.work = append(m.work, v)
	}
}

// invoke marks the methods an interface call of the given signature can reach.
func (m *reachability) invoke(signature string) {
	if m.invoked[signature] {
		return
	}
	m.invoked[signature] = true
	for _, method := range m.pending[signature] {
		m.add(method)
	}
	delete(m.pending, signature)
}

// visit queues the global values a constant refers to.
func (m *reachability) visit(v llvm.Value) {
	if v.IsAConstant().IsNil() {
		return
	}
	if !v.IsAGlobalValue().IsNil() {
		m.add(v)
		return
	}
	if m.constants[v] {
		return
	}
	m.constants[v] = true
	for i := 0; i < v.OperandsCount(); i++ {
		m.visit(v.Operand(i))
	}
}

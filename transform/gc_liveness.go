package transform

import (
	"slices"

	"tinygo.org/x/go-llvm"
)

// clearDeadStackSlots stores nil to each slot where its pointer, and every value
// derived from it that has no slot of its own, can no longer be used.
func clearDeadStackSlots(builder llvm.Builder, fn, stackObject llvm.Value, stackObjectType llvm.Type, pointers, slotStores []llvm.Value) {
	if len(pointers) == 0 {
		return
	}
	rooted := make(map[llvm.Value]struct{}, len(pointers))
	for _, ptr := range pointers {
		rooted[ptr] = struct{}{}
	}

	preds := map[llvm.BasicBlock][]llvm.BasicBlock{}
	blockIndex := map[llvm.BasicBlock]int{}
	for bb := fn.FirstBasicBlock(); !bb.IsNil(); bb = llvm.NextBasicBlock(bb) {
		blockIndex[bb] = len(blockIndex)
		term := bb.LastInstruction()
		if term.IsNil() {
			continue
		}
		for i := 0; i < term.SuccessorsCount(); i++ {
			succ := term.Successor(i)
			preds[succ] = append(preds[succ], bb)
		}
	}

	// Instruction positions within their block, filled in on demand.
	positions := map[llvm.BasicBlock]map[llvm.Value]int{}
	position := func(inst llvm.Value) int {
		bb := inst.InstructionParent()
		m, ok := positions[bb]
		if !ok {
			m = map[llvm.Value]int{}
			n := 0
			for i := bb.FirstInstruction(); !i.IsNil(); i = llvm.NextInstruction(i) {
				m[i] = n
				n++
			}
			positions[bb] = m
		}
		return m[inst]
	}

	ctx := fn.Type().Context()
	for i, ptr := range pointers {
		nullValue := llvm.ConstNull(ptr.Type())
		defBlock := ptr.InstructionParent()
		defPos := position(ptr)

		// Collect the uses of ptr and of the values derived from it.
		var uses []llvm.Value          // non-phi uses
		var phiPreds []llvm.BasicBlock // blocks whose end uses ptr through a phi
		// Start from the value ptr was extracted or cast from as well, so a
		// sibling taken from the same aggregate keeps the slot alive too.
		src := ptr
		for !src.IsAInstruction().IsNil() {
			op := src.InstructionOpcode()
			if op != llvm.ExtractValue && op != llvm.BitCast && op != llvm.GetElementPtr {
				break
			}
			next := src.Operand(0)
			if next.IsAInstruction().IsNil() && next.IsAArgument().IsNil() {
				break
			}
			src = next
		}
		seen := map[llvm.Value]struct{}{ptr: {}, src: {}}
		worklist := []llvm.Value{ptr}
		if src != ptr {
			worklist = append(worklist, src)
		}
		// Memory ptr may have been written to. The optimizer can later replace
		// a load from it with ptr itself, so such loads count as derived values.
		tainted := map[llvm.Value]struct{}{}
		derive := func(inst llvm.Value) {
			if _, ok := seen[inst]; ok {
				return
			}
			seen[inst] = struct{}{}
			if _, ok := rooted[inst]; !ok && derivesPointer(inst) {
				worklist = append(worklist, inst)
			}
		}
		var taint func(addr llvm.Value)
		taint = func(addr llvm.Value) {
			base := stripPointerCasts(addr)
			if base == stackObject || !base.IsAGlobalValue().IsNil() || !base.IsAConstant().IsNil() {
				return
			}
			if _, ok := tainted[base]; ok {
				return
			}
			tainted[base] = struct{}{}
			ptrs := []llvm.Value{base}
			for k := 0; k < len(ptrs); k++ {
				for _, user := range getUses(ptrs[k]) {
					if user.IsAInstruction().IsNil() {
						continue
					}
					switch user.InstructionOpcode() {
					case llvm.GetElementPtr, llvm.BitCast:
						ptrs = append(ptrs, user)
					case llvm.Load:
						uses = append(uses, user)
						derive(user)
					case llvm.Call:
						uses = append(uses, user)
						derive(user)
						for a := 0; a < user.OperandsCount()-1; a++ {
							if arg := user.Operand(a); arg.Type().TypeKind() == llvm.PointerTypeKind {
								taint(arg)
							}
						}
					}
				}
			}
		}
		for len(worklist) != 0 {
			val := worklist[len(worklist)-1]
			worklist = worklist[:len(worklist)-1]
			for _, user := range getUses(val) {
				if user.IsAInstruction().IsNil() {
					continue
				}
				switch user.InstructionOpcode() {
				case llvm.Store:
					if user.Operand(0) == val {
						taint(user.Operand(1))
					}
				case llvm.Call:
					for a := 0; a < user.OperandsCount()-1; a++ {
						if arg := user.Operand(a); arg != val && arg.Type().TypeKind() == llvm.PointerTypeKind {
							taint(arg)
						}
					}
				}
				if !user.IsAPHINode().IsNil() {
					for j := 0; j < user.IncomingCount(); j++ {
						if user.IncomingValue(j) == val {
							phiPreds = append(phiPreds, user.IncomingBlock(j))
						}
					}
				} else {
					uses = append(uses, user)
				}
				if _, ok := seen[user]; ok {
					continue
				}
				seen[user] = struct{}{}
				if _, ok := rooted[user]; ok {
					continue
				}
				if derivesPointer(user) {
					worklist = append(worklist, user)
				}
			}
		}

		// Compute the blocks where ptr is live on entry and on exit.
		liveIn := map[llvm.BasicBlock]bool{}
		liveOut := map[llvm.BasicBlock]bool{}
		var markLiveIn func(bb llvm.BasicBlock)
		markLiveIn = func(bb llvm.BasicBlock) {
			if liveIn[bb] {
				return
			}
			liveIn[bb] = true
			for _, pred := range preds[bb] {
				liveOut[pred] = true
				if pred != defBlock {
					markLiveIn(pred)
				}
			}
		}
		lastUse := map[llvm.BasicBlock]llvm.Value{}
		for _, use := range uses {
			bb := use.InstructionParent()
			pos := position(use)
			if bb == defBlock && pos < defPos {
				// A use of the value from the previous trip round a loop.
				markLiveIn(bb)
				continue
			}
			if bb != defBlock {
				markLiveIn(bb)
			}
			if last, ok := lastUse[bb]; !ok || position(last) < pos {
				lastUse[bb] = use
			}
		}
		for _, pred := range phiPreds {
			liveOut[pred] = true
			if pred != defBlock {
				markLiveIn(pred)
			}
		}

		store := func(insertBefore llvm.Value) {
			builder.SetInsertPointBefore(insertBefore)
			gep := builder.CreateGEP(stackObjectType, stackObject, []llvm.Value{
				llvm.ConstInt(ctx.Int32Type(), 0, false),
				llvm.ConstInt(ctx.Int32Type(), uint64(2+i), false),
			}, "")
			builder.CreateStore(nullValue, gep)
		}

		// Visit blocks in function order, so the output is deterministic.
		inOrder := func(set map[llvm.BasicBlock]bool) []llvm.BasicBlock {
			list := make([]llvm.BasicBlock, 0, len(set))
			for bb := range set {
				list = append(list, bb)
			}
			slices.SortFunc(list, func(a, b llvm.BasicBlock) int { return blockIndex[a] - blockIndex[b] })
			return list
		}
		touched := map[llvm.BasicBlock]bool{defBlock: true}
		for bb := range liveIn {
			touched[bb] = true
		}

		// Clear the slot after the last use in a block the value does not
		// leave alive.
		for _, bb := range inOrder(touched) {
			if liveOut[bb] {
				continue
			}
			last, ok := lastUse[bb]
			if !ok {
				if bb == defBlock {
					store(llvm.NextInstruction(slotStores[i]))
				}
				continue
			}
			if last == bb.LastInstruction() {
				continue
			}
			if next := llvm.NextInstruction(last); !next.IsAReturnInst().IsNil() || !next.IsAUnreachableInst().IsNil() {
				// The frame ends here, and a tail call must stay next to its return.
				continue
			}
			store(llvm.NextInstruction(last))
		}

		// Clear the slot on entry to a block that the value reaches dead.
		cleared := map[llvm.BasicBlock]bool{}
		for _, bb := range inOrder(liveOut) {
			term := bb.LastInstruction()
			for j := 0; j < term.SuccessorsCount(); j++ {
				succ := term.Successor(j)
				if succ == defBlock || liveIn[succ] || cleared[succ] {
					continue
				}
				cleared[succ] = true
				inst := succ.FirstInstruction()
				for !inst.IsAPHINode().IsNil() {
					inst = llvm.NextInstruction(inst)
				}
				store(inst)
			}
		}
	}
}

// derivesPointer returns whether inst produces a value that may hold, or be
// computed from, a pointer it takes as operand.
func derivesPointer(inst llvm.Value) bool {
	typ := inst.Type()
	if typ.TypeKind() == llvm.VoidTypeKind {
		return false
	}
	if typeHasPointers(typ) {
		return true
	}
	switch typ.TypeKind() {
	case llvm.IntegerTypeKind:
		// An integer of pointer size may hold a pointer that LLVM loaded or
		// copied as an integer.
		return typ.IntTypeWidth() >= 32
	case llvm.StructTypeKind, llvm.ArrayTypeKind, llvm.VectorTypeKind:
		return true
	}
	return false
}

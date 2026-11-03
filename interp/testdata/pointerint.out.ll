; ModuleID = 'testdata/pointerint.ll'
source_filename = "testdata/pointerint.ll"
target datalayout = "e-m:e-i64:64-f80:128-n8:16:32:64-S128"
target triple = "x86_64--linux"

@gep.global = global i8 0
@udiv.global = global i8 0
@udiv.result = local_unnamed_addr global i64 0
@add.global = global i8 0
@add.result = local_unnamed_addr global i64 ptrtoint (ptr getelementptr inbounds nuw (i8, ptr @add.global, i64 2) to i64)
@sub.global = global i8 0
@sub.result = local_unnamed_addr global i64 0
@xor.global = global i8 0
@xor.result = local_unnamed_addr global i64 0

declare void @externalCall(ptr) local_unnamed_addr

define void @runtime.initAll() unnamed_addr {
entry:
  call fastcc void @gep.init(ptr undef)
  call fastcc void @udiv.init(ptr undef)
  call fastcc void @"sub.init#interp"(ptr undef)
  call fastcc void @"xor.init#interp"(ptr undef)
  ret void
}

define internal fastcc void @gep.init(ptr %context) unnamed_addr {
  %base = inttoptr i64 4096 to ptr
  %offset = ptrtoint ptr @gep.global to i64
  %addr = getelementptr i8, ptr %base, i64 %offset
  call void @externalCall(ptr %addr)
  ret void
}

define internal fastcc void @udiv.init(ptr %context) unnamed_addr {
  %n = ptrtoint ptr @udiv.global to i64
  %q = udiv i64 4096, %n
  store i64 %q, ptr @udiv.result, align 8
  ret void
}

define internal fastcc void @"sub.init#interp"(ptr %0) unnamed_addr {
entry:
  store i64 sub (i64 ptrtoint (ptr @sub.global to i64), i64 2), ptr @sub.result, align 8
  ret void
}

define internal fastcc void @"xor.init#interp"(ptr %0) unnamed_addr {
entry:
  store i64 xor (i64 ptrtoint (ptr @xor.global to i64), i64 2), ptr @xor.result, align 8
  ret void
}

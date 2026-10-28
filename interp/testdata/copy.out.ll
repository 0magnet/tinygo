; ModuleID = 'testdata/copy.ll'
source_filename = "testdata/copy.ll"
target datalayout = "e-m:e-i64:64-f80:128-n8:16:32:64-S128"
target triple = "x86_64--linux"

@moveDst = local_unnamed_addr global [3 x i8] c"foo"
@copyDst = local_unnamed_addr global [3 x i8] c"foo"
@externalSrc = external local_unnamed_addr global [2 x i8]
@moveExternalDst = local_unnamed_addr global [2 x i8] zeroinitializer
@moveEscapedSrc = global [4 x i8] c"abcd"
@moveEscapedDst = local_unnamed_addr global [4 x i8] zeroinitializer
@volatileSrc = global [2 x i8] c"xy"
@volatileDst = global [2 x i8] zeroinitializer
@fillDst = local_unnamed_addr global [4 x i8] c"A\7F\7FD"
@fillEscapedDst = global [4 x i8] c"ABCD"
@fillVolatileDst = global [4 x i8] c"ABCD"

declare void @use(ptr) local_unnamed_addr

define void @runtime.initAll() local_unnamed_addr {
  call fastcc void @"main.init#interp"()
  ret void
}

; Function Attrs: nocallback nofree nosync nounwind willreturn memory(argmem: readwrite)
declare void @llvm.memmove.p0.p0.i64(ptr writeonly captures(none), ptr readonly captures(none), i64, i1 immarg) #0

; Function Attrs: nocallback nofree nosync nounwind willreturn memory(argmem: readwrite)
declare void @llvm.memcpy.p0.p0.i64(ptr noalias writeonly captures(none), ptr noalias readonly captures(none), i64, i1 immarg) #0

; Function Attrs: nocallback nofree nosync nounwind willreturn memory(argmem: write)
declare void @llvm.memset.p0.i64(ptr writeonly captures(none), i8, i64, i1 immarg) #1

define internal fastcc void @"main.init#interp"() unnamed_addr {
entry:
  call void @llvm.memmove.p0.p0.i64(ptr @moveExternalDst, ptr @externalSrc, i64 2, i1 false)
  call void @use(ptr @moveEscapedSrc)
  call void @llvm.memmove.p0.p0.i64(ptr @moveEscapedDst, ptr @moveEscapedSrc, i64 4, i1 false)
  call void @llvm.memcpy.p0.p0.i64(ptr @volatileDst, ptr @volatileSrc, i64 2, i1 true)
  call void @use(ptr @fillEscapedDst)
  call void @llvm.memset.p0.i64(ptr getelementptr inbounds (i8, ptr @fillEscapedDst, i32 1), i8 127, i64 2, i1 false)
  call void @llvm.memset.p0.i64(ptr getelementptr inbounds (i8, ptr @fillVolatileDst, i32 1), i8 127, i64 2, i1 true)
  ret void
}

attributes #0 = { nocallback nofree nosync nounwind willreturn memory(argmem: readwrite) }
attributes #1 = { nocallback nofree nosync nounwind willreturn memory(argmem: write) }

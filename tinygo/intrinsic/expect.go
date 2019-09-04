package intrinsic

// Assume tells LLVM that the given condition should always be true.
// If the condition is false, this results in undefined behavior.
//go:export llvm.assume
func Assume(condition bool)

// ExpectBool provides a hint to LLVM that some value is typically equal to some other value.
// The return value is the input value.
//go:export llvm.expect.i1
func ExpectBool(val bool, expected bool) bool

// ExpectInt8 provides a hint to LLVM that some value is typically equal to some other value.
// The return value is the input value.
//go:export llvm.expect.i8
func ExpectInt8(val int8, expected int8) int8

// ExpectUint8 provides a hint to LLVM that some value is typically equal to some other value.
// The return value is the input value.
//go:export llvm.expect.i32
func ExpectUint8(val uint8, expected uint8) uint8

// ExpectInt16 provides a hint to LLVM that some value is typically equal to some other value.
// The return value is the input value.
//go:export llvm.expect.i16
func ExpectInt16(val int16, expected int16) int16

// ExpectUint16 provides a hint to LLVM that some value is typically equal to some other value.
// The return value is the input value.
//go:export llvm.expect.i16
func ExpectUint16(val uint16, expected uint16) uint16

// ExpectInt32 provides a hint to LLVM that some value is typically equal to some other value.
// The return value is the input value.
//go:export llvm.expect.i32
func ExpectInt32(val int32, expected int32) int32

// ExpectUint32 provides a hint to LLVM that some value is typically equal to some other value.
// The return value is the input value.
//go:export llvm.expect.i32
func ExpectUint32(val uint32, expected uint32) uint32

// ExpectInt64 provides a hint to LLVM that some value is typically equal to some other value.
// The return value is the input value.
//go:export llvm.expect.i64
func ExpectInt64(val int64, expected int64) int64

// ExpectUint64 provides a hint to LLVM that some value is typically equal to some other value.
// The return value is the input value.
//go:export llvm.expect.i64
func ExpectUint64(val uint64, expected uint64) uint64

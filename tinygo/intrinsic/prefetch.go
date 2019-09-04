package intrinsic

import "unsafe"

// PrefetchMode is a type specifying the kind of prefetch to use.
type PrefetchMode uint32

const (
	// PrefetchRead prefetches a chunk of memory for reading.
	PrefetchRead PrefetchMode = iota
	// PrefetchWrite prefetches a chunk of memory for writing.
	PrefetchWrite
)

// PrefetchLocality is a type specifying the level of locality to apply when prefetching a chunk of memory.
// Locality levels are integers in [0, 3], where 0 is no locality and 3 is the maximum possible locality.
// If locality is 3, the memory chunk will be held in the cache as much as possible.
type PrefetchLocality uint32

const (
	// MaximumPrefetchLocality is the maximum value for PrefetchLocality, and ensures the memory chunk will be held in cache as much as possible.
	MaximumPrefetchLocality PrefetchLocality = 3

	// MinimumPrefetchLocality is the minimum value for PrefetchLocality, and does not encourage storing the memory chunk in cache.
	MinimumPrefetchLocality PrefetchLocality = 0
)

// PrefetchCacheType indicates which kind of cache to hold the memory chunk in.
type PrefetchCacheType uint32

const (
	// PrefetchInstruction indicates that the memory chunk should be loaded into the instruction cache.
	PrefetchInstruction PrefetchCacheType = iota
	// PrefetchData indicates that the memory chunk should be loaded into the data cache.
	PrefetchData
)

// Prefetch informs the CPU that a chunk of memory will be used soon.
//go:export llvm.prefetch
func Prefetch(address unsafe.Pointer, mode PrefetchMode, locality PrefetchLocality, cache PrefetchCacheType)

package maps

import (
	"fmt"

	_ "unsafe"
)

type Map interface {
	// Each invokes a function with every key-value pair.
	// It inherits the same semantics as a map range loop.
	Each(func(key string, value interface{}))

	// Get checks if the key is present.
	// If it is not present, the second return is false.
	Get(key string) (interface{}, bool)

	// Put a key-value pair in the map.
	// If the key is already present in the map, the value is updated.
	Put(key string, value interface{})

	// Remove the key from the map.
	// If it is not present, nothing happens.
	Delete(key string)

	// Info spits out miscellaneous statistics for debugging purposes.
	Info() string
}

// Go is Go's implementation of a map.
type Go map[string]interface{}

func (m Go) Each(fn func(key string, value interface{})) {
	for k, v := range m {
		fn(k, v)
	}
}

func (m Go) Get(key string) (interface{}, bool) {
	v, ok := m[key]
	return v, ok
}

func (m Go) Put(key string, value interface{}) {
	m[key] = value
}

func (m Go) Delete(key string) {
	delete(m, key)
}

func (m Go) Info() string {
	return fmt.Sprintf("len=%d", len(m))
}

//go:linkname runtime_stringHash runtime.stringHash
//go:noescape
func runtime_stringHash(str string, seed uintptr) uintptr

func strhash(str string) uint64 {
	return uint64(runtime_stringHash(str, 0x3a753e5aea42b0e7))
}

// Use this if not running on the standard Go toolchain: (TODO: build tags)
/*
func strhash(str string) uint64 {
	// FNV1a reversed
	hash := uint64(0xcbf29ce484222325)
	for _, b := range []byte(str) {
		hash ^= uint64(b)
		hash *= 0x100000001b3
	}

	return bits.Reverse64(hash)
}
*/

package maps

import (
	"fmt"
	"math/bits"
)

// inverseFreeRatio is the inverse of the target ratio of empty slots.
// When the ratio is exceeded, the map will be grown.
// Increasing this value will increase the average CPU time spent in freeSlot, but will also increase memory density somewhat.
const inverseFreeRatio = 8

// MakeScatterChain makes a ScatterChain with capacity for the specified number of elements.
func MakeScatterChain(size uint) (res ScatterChain) {
	if size != 0 {
		size += (size / inverseFreeRatio) + 1

		logSize := bits.Len(size - 1)
		res.slots = make([]scatterChainSlot, 1<<logSize)
		res.shift = 64 - uint(logSize)
	}

	return
}

// ScatterChain is a map implementation using a chained scatter table with Brent's invariant (based off of the system used by Lua).
// The zero value is a ready-to-use empty map.
// This is somewhat nice in that it uses an exactly-predictable amount of memory for a given maximum capacity.
// The constant memory overhead is somewhat lower than Go's maps, but the minimum proportional memory overhead is significantly higher.
// It is more memory-efficient for tiny maps, and less memory-efficient for large maps.
// This implementation requires an ordered comparator to be defined over the key type.
type ScatterChain struct {
	// slots are where the actual data is stored.
	// An empty slot is represented by the zero value of scatterChainSlot.
	// The hash of a key is used to map it to a primary slot in this array.
	// If there are any keys with that primary index, the slot will be marked as a "head", and will contain a linked list of key-value pairs.
	// Colliding pairs will be stored in otherwise-unused slots.
	// Brent's invariant differs from a traditional chained scatter in how an insert behaves when the primary index for a new key is previously used by a seperate collision list.
	// In a traditional scatter table, the new pair would be appended to the other collision list, effectively concatenating them.
	// In a scatter table with Brent's invariant, the contents of the old slot are instead migrated elsewhere.
	// This avoids an edge case where all slots form one giant linked list, but complicates iteration a bit when interleaved with insertion or deletion.
	// In order to provide Go-style iteration semantics, this implementation sorts the collision chains in hash order, followed by key order in case of a full collision.
	slots []scatterChainSlot

	// n is the number of key-value pairs currently stored in the map.
	n uint

	// shift is the downward shift of a hash required to produce a slot index.
	// This is 64-bits.Len64(len(slots)-1).
	shift uint
}

type scatterChainSlot struct {
	// key is the key of the pair if present.
	key string

	// value is the currently assigned value corresponding to the key.
	value interface{}

	// tag contains all other metadata for the slot.
	// If the slot is empty, this will be scatterChainEmpty.
	tag scatterChainTag
}

// scatterChainTag stores metadata for a slot.
// It tracks whether a slot is a head, and stores the index of the next slot in the chain (if present).
type scatterChainTag uintptr

const (
	// scatterChainTagEmpty is the zero value of a tag.
	// It indicates that the slot is empty.
	scatterChainTagEmpty scatterChainTag = 0

	// scatterChainTagHead is a bit flag that indicates that the slot is the head of a collision chain.
	scatterChainTagHead scatterChainTag = 1

	// scatterChainTagHasNext is a bit flag that indicates that there is another slot in the collision chain.
	// If set, the index of the next slot is stored in the bits above this.
	scatterChainTagHasNext scatterChainTag = 2
)

// next returns the index of the next slot in the chain, if present.
// If there is no following slot in the chain, this returns false.
func (t scatterChainTag) next() (uint, bool) {
	return uint(t >> 2), t&scatterChainTagHasNext != 0
}

// isHead checks if this slot is a head.
func (t scatterChainTag) isHead() bool {
	return t&scatterChainTagHead != 0
}

// setNext links a slot following this in the chain.
func (t *scatterChainTag) setNext(idx uint) {
	*t = (*t & scatterChainTagHead) | scatterChainTagHasNext | scatterChainTag(idx<<2)
}

// behead downgrades a non-empty slot tag from a head to a regular slot.
func (t scatterChainTag) behead() scatterChainTag {
	t &^= scatterChainTagHead
	if t == scatterChainTagEmpty {
		t = ^(scatterChainTagHasNext | scatterChainTagHead)
	}

	return t
}

func (t scatterChainTag) String() string {
	next, hasNext := t.next()
	isHead := t.isHead()
	switch {
	case t == scatterChainTagEmpty:
		return "empty"
	case isHead && !hasNext:
		return "singlet"
	case isHead && hasNext:
		return fmt.Sprintf("head (next: %d)", next)
	case hasNext:
		return fmt.Sprintf("middle (next: %d)", next)
	default:
		return "tail"
	}
}

func (m *ScatterChain) Info() string {
	var heads uint
	for i := range m.slots {
		if m.slots[i].tag.isHead() {
			heads++
		}
	}

	return fmt.Sprintf("len=%d cap=%d heads=%d (%0.2f%% collision rate)", m.n, len(m.slots), heads, 100*(float64(m.n-heads)/float64(m.n)))
}

func (m *ScatterChain) dump() {
	fmt.Println("table:")
	for _, slot := range m.slots {
		if slot.tag == scatterChainTagEmpty {
			fmt.Println("\tempty")
			continue
		}

		fmt.Printf("\tkey=%s value=%v tag=%s\n", slot.key, slot.value, slot.tag.String())
	}
}

func (m *ScatterChain) Each(fn func(key string, value interface{})) {
	if m == nil {
		return
	}

	// A naive approach for iterating over a chained scatter table would be to simply loop forwards by index.
	// Normally this works, but the Go spec defines strict behavior requirements when modifying a map during iteration.
	// When inserting a new key into a chained scatter table with Brent's variation, existing key-value pairs may be moved (likely causing them to be lost).
	// When inserting or deleting pairs, the entire table may also resize.

	// This function implements a traversal of the map by iterating in hash order, followed by key comparison order in case of a full collision.
	// This guarantees that all elements initially in the map are hit unless they are deleted.
	// Pairs inserted during iteration may not be hit, but this is allowed by the Go spec.

	// Find the first element.
	var lastKey string
	var lastHash uint64
	{
		i := 0
		for {
			if i >= len(m.slots) {
				// The map is empty.
				return
			}

			if m.slots[i].tag.isHead() {
				// This is the first list head, and thus the first value.
				// We may pass filled slots that are not heads - we will hit them later.
				// A simpler implementation would just loop by index, but that doesn't work here because Go allows the map to be modified during iteration.
				// For a normal scatter chain that would work anyway, Brent's variation requires data to be moved when inserting a new key.
				lastKey = m.slots[i].key
				lastHash = strhash(lastKey)
				fn(m.slots[i].key, m.slots[i].value)
				break
			}

			i++
		}
	}

	// Start at the slot corresponding to the first element's hash.
	i := uint(lastHash >> m.shift)
	for !m.slots[i].tag.isHead() {
		// This slot is not a head, so move to the next slot.
		i++
		if i >= uint(len(m.slots)) {
			return
		}
	}

	for {
		for {
			keyHash := strhash(m.slots[i].key)
			if keyHash > lastHash || (keyHash == lastHash && m.slots[i].key > lastKey) {
				// This key has not been processed yet.
				lastKey = m.slots[i].key
				lastHash = keyHash
				fn(m.slots[i].key, m.slots[i].value)
				if i >= uint(len(m.slots)) || m.slots[i].tag == scatterChainTagEmpty || m.slots[i].key != lastKey {
					// The table was modified, so rescan the chain.
					i = uint(lastHash >> m.shift)
					break
				}
			}

			// Move to the next key in the chain.
			next, ok := m.slots[i].tag.next()
			if !ok {
				// There are no more keys in this chain.
				// Move to the next chain.
				i = uint(lastHash>>m.shift) + 1
				break
			}

			i = next
		}

		for i < uint(len(m.slots)) && !m.slots[i].tag.isHead() {
			// This slot is not a head, so move to the next slot.
			i++
		}
		if i >= uint(len(m.slots)) {
			return
		}
	}
}

func (m *ScatterChain) Get(key string) (interface{}, bool) {
	if m == nil || len(m.slots) == 0 {
		return nil, false
	}

	hash := strhash(key)

	idx := uint(hash >> uint64(m.shift))
	if !m.slots[idx].tag.isHead() {
		return nil, false
	}

	for {
		if m.slots[idx].key == key {
			return m.slots[idx].value, true
		}

		next, ok := m.slots[idx].tag.next()
		if !ok {
			return nil, false
		}

		idx = next
	}
}

func (m *ScatterChain) Put(key string, value interface{}) {
	if m.n == uint(len(m.slots)) || uint(len(m.slots))-m.n < uint(len(m.slots))/inverseFreeRatio {
		// Ensure that at least one slot is available for insert, even if we might not use it.
		// Additionally, apply a constant upper bound to the load factor such that freeSlot does not get extremely slow.
		// It might be possible to pack a free list by using the space otherwise occupied by key-value pairs (and thus allow for a higher load factor), but that seems a bit complicated.
		m.grow()
	}

	m.doPut(key, value)
}

func (m *ScatterChain) grow() {
	if len(m.slots) == 0 {
		// Handle a fresh map seperately.
		m.slots = make([]scatterChainSlot, 4)
		m.shift = 62
		return
	}

	// Create a larger temporary map.
	var tmp ScatterChain
	tmp.shift = m.shift - 1
	tmp.slots = make([]scatterChainSlot, 2*len(m.slots))

	// Copy the pairs into the new map.
	for i := range m.slots {
		if m.slots[i].tag == scatterChainTagEmpty {
			continue
		}

		tmp.doPut(m.slots[i].key, m.slots[i].value)
	}

	// Overwrite the old map with the new map.
	*m = tmp

	// There is a fancier way to do this which skips reallocating indices, but it appears to be slightly slower.
}

// doPut inserts or updates a key-value pair.
// This will panic if there is not sufficient available space.
func (m *ScatterChain) doPut(key string, value interface{}) {
	hash := strhash(key)
	idx := uint(hash >> m.shift)
	switch {
	case m.slots[idx].tag == scatterChainTagEmpty:
		// Configure the slot as a fresh head.
		m.slots[idx].tag = scatterChainTagHead

	case !m.slots[idx].tag.isHead():
		// This slot is currently used by a different chain.
		// Find somewhere to move the previous pair.
		dst := m.freeSlot(idx)

		// Find the parent of the pair.
		parent := uint(strhash(m.slots[idx].key) >> m.shift)
		for {
			next, _ := m.slots[parent].tag.next()
			if next == idx {
				break
			}

			parent = next
		}

		// Move the pair.
		m.slots[dst] = m.slots[idx]

		// Update the parent's reference.
		m.slots[parent].tag.setNext(dst)

		// Configure the slot as a fresh head.
		m.slots[idx].tag = scatterChainTagHead

	case m.slots[idx].key == key:
		// Update the pair in-place.
		m.slots[idx].value = value
		return

	default:
		if keyHash := strhash(m.slots[idx].key); keyHash > hash || (keyHash == hash && m.slots[idx].key > key) {
			// In order to insert to the head of a chain, we must move the former-head's pair.
			dst := m.freeSlot(idx)
			m.slots[dst] = m.slots[idx]
			m.slots[dst].tag = m.slots[dst].tag.behead()

			// Reconfigure the head slot.
			m.slots[idx].key = key
			m.slots[idx].tag.setNext(dst)
			break
		}

		// Traverse the chain, looking for the insertion point.
		for {
			next, ok := m.slots[idx].tag.next()
			if !ok {
				// That was the end of the chain.
				// Insert after the last pair.
				break
			}

			if keyHash := strhash(m.slots[next].key); keyHash > hash || (keyHash == hash && m.slots[next].key > key) {
				// The next key is beyond the key we want to insert.
				// Insert after idx.
				break
			}

			if m.slots[next].key == key {
				// Update the pair in-place.
				m.slots[next].value = value
				return
			}

			idx = next
		}

		// Reserve a slot for the new pair.
		dst := m.freeSlot(idx)

		// Insert the slot into the chain.
		m.slots[dst].tag = m.slots[idx].tag.behead()
		m.slots[idx].tag.setNext(dst)

		idx = dst
	}

	// Populate the slot with the pair.
	m.slots[idx].key, m.slots[idx].value = key, value
	m.n++
}

// freeSlot finds the nearest free slot.
// If there are no free slots, this will panic.
func (m *ScatterChain) freeSlot(near uint) uint {
	for i, j := int(near), near+1; i >= 0 || j < uint(len(m.slots)); {
		if i >= 0 {
			if m.slots[i].tag == scatterChainTagEmpty {
				return uint(i)
			}
			i--
		}
		if j < uint(len(m.slots)) {
			if m.slots[j].tag == scatterChainTagEmpty {
				return j
			}
			j++
		}
	}

	panic("no free slot")
}

func (m *ScatterChain) Delete(key string) {
	if m == nil || len(m.slots) == 0 {
		return
	}

	hash := strhash(key)
	idx := uint(hash >> uint64(m.shift))
	if !m.slots[idx].tag.isHead() {
		// This hash-bucket is empty.
		return
	}

	if m.slots[idx].key == key {
		// The key is at the head of the chain.
		m.n--
		if next, ok := m.slots[idx].tag.next(); ok {
			// Move the next pair to the chain head.
			m.slots[idx] = m.slots[next]
			m.slots[next] = scatterChainSlot{}
			m.slots[idx].tag |= scatterChainTagHead
			return
		}

		// The key is also the only value in the chain.
		// Clear the slot.
		m.slots[idx] = scatterChainSlot{}
		return
	}

	// Search for the key in the chain.
	var prev uint
	for {
		next, ok := m.slots[idx].tag.next()
		if !ok {
			// The key is not in the map.
			return
		}

		idx, prev = next, idx
		if m.slots[idx].key == key {
			break
		}
	}

	// Replace the reference to this key's slot.
	m.slots[prev].tag = (m.slots[prev].tag & scatterChainTagHead) | m.slots[idx].tag

	// Clear the slot.
	m.slots[idx] = scatterChainSlot{}

	m.n--
}

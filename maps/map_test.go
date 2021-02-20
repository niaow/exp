package maps

import (
	"reflect"
	"sort"
	"strconv"
	"testing"
	"unsafe"

	"golang.org/x/exp/rand"
)

func TestMaps(t *testing.T) {
	t.Parallel()

	impls := []struct {
		name   string
		create func() Map
	}{
		{"Go", func() Map { return make(Go) }},
		{"ScatterChain", func() Map { return &ScatterChain{} }},
	}

	for _, impl := range impls {
		impl := impl
		t.Run(impl.name, func(t *testing.T) {
			t.Parallel()

			t.Run("PutAndGet", testPutAndGet(impl.create))
			t.Run("Update", testUpdate(impl.create))
			t.Run("Each", testEach(impl.create))
			t.Run("Clear", testClear(impl.create))
		})
	}
}

func testPutAndGet(create func() Map) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		// Generate a bunch of string keys.
		keys := make([]string, 10000)
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}

		// Put the keys in random order.
		rand.New(rand.NewSource(4)).Shuffle(len(keys), func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
		})

		m := create()
		for i, k := range keys {
			m.Put(k, &keys[i])
			for j, k := range keys[:i+1] {
				got, ok := m.Get(k)
				if !ok {
					t.Errorf("%q is missing", k)
					continue
				}
				if got != &keys[j] {
					t.Errorf("wrong value for key %q", k)
				}
			}
		}
	}
}

func testUpdate(create func() Map) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		// Generate a bunch of string keys.
		keys := make([]string, 100)
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}

		// Put the keys in random order.
		rand := rand.New(rand.NewSource(4))
		rand.Shuffle(len(keys), func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
		})

		// Insert indices into a map.
		m := create()
		for i, k := range keys {
			m.Put(k, i)
		}

		// Shuffle again, updating the indices in the map.
		rand.Shuffle(len(keys), func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
			m.Put(keys[i], i)
			m.Put(keys[j], j)
		})

		// Check that the indices are still correct.
		for i, k := range keys {
			v, ok := m.Get(k)
			if !ok {
				t.Errorf("lost key %q", k)
				continue
			}
			if v != i {
				t.Errorf("expected %d at key %q but got %v", i, k, v)
			}
		}
	}
}

func testEach(create func() Map) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		// Generate a bunch of string keys.
		keys := make([]string, 10000)
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}

		// Put the keys in random order.
		rand.New(rand.NewSource(5)).Shuffle(len(keys), func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
		})

		// Insert keys into the map.
		m := create()
		for i, k := range keys {
			m.Put(k, &keys[i])
		}

		// Read back the pairs.
		found := make([]string, 0, len(keys))
		m.Each(func(key string, value interface{}) {
			found = append(found, key)
			if ptr, ok := value.(*string); !ok || *ptr != key {
				t.Errorf("wrong value for key %q", key)
			}
		})

		sort.Strings(keys)
		sort.Strings(found)
		if !reflect.DeepEqual(keys, found) {
			t.Errorf("inserted %s but found %s", keys, found)
		}
	}
}

func testClear(create func() Map) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		// Generate a bunch of string keys.
		keys := make([]string, 10000)
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}

		// Put the keys in random order.
		rand.New(rand.NewSource(5)).Shuffle(len(keys), func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
		})

		// Insert keys into the map.
		m := create()
		for i, k := range keys {
			m.Put(k, &keys[i])
		}

		// Read back the pairs.
		found := make([]string, 0, len(keys))
		m.Each(func(key string, value interface{}) {
			found = append(found, key)
			if ptr, ok := value.(*string); !ok || *ptr != key {
				t.Errorf("wrong value for key %q", key)
			}
			m.Delete(key)
			if _, ok := m.Get(key); ok {
				t.Errorf("key %q still exists", key)
			}
		})

		sort.Strings(keys)
		sort.Strings(found)
		if !reflect.DeepEqual(keys, found) {
			t.Errorf("inserted %s but found %s", keys, found)
		}
	}
}

func BenchmarkMap(b *testing.B) {
	impls := []struct {
		name   string
		create func(uint) Map
	}{
		{"Go", func(cap uint) Map { return make(Go, cap) }},
		{"ScatterChain", func(cap uint) Map {
			chain := MakeScatterChain(cap)
			return &chain
		}},
	}

	for _, impl := range impls {
		impl := impl
		b.Run(impl.name, func(b *testing.B) {
			b.Run("CreateAndInsertSmall", benchCreateAndInsertSmall(impl.create))
			b.Run("CreateAndInsertSmallDynamic", benchCreateAndInsertSmallDynamic(impl.create))
			b.Run("RandomReadHit", benchRandomReadHit(impl.create))
		})
	}
}

func benchCreateAndInsertSmall(create func(uint) Map) func(b *testing.B) {
	sizes := []struct {
		name string
		val  int
	}{
		{"1", 1},
		{"16", 16},
		{"64", 64},
		{"256", 256},
		{"1K", 1 << 10},
		{"64K", 1 << 16},
		{"1M", 1 << 20},
	}

	return func(b *testing.B) {
		// Generate a bunch of string keys.
		keys := make([]string, sizes[len(sizes)-1].val)
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}

		// Put the keys in random order.
		rand.New(rand.NewSource(9)).Shuffle(len(keys), func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
		})

		for _, size := range sizes {
			in := keys[:size.val]
			b.Run(size.name, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					m := create(uint(len(in)))
					for j, k := range in {
						m.Put(k, &in[j])
					}
				}

				// This isnt great but whatever.
				b.SetBytes(int64(len(in)) * int64(unsafe.Sizeof("")+unsafe.Sizeof(interface{}(nil))))
			})
		}
	}
}

func benchCreateAndInsertSmallDynamic(create func(uint) Map) func(b *testing.B) {
	sizes := []struct {
		name string
		val  int
	}{
		{"1", 1},
		{"16", 16},
		{"64", 64},
		{"256", 256},
		{"1K", 1 << 10},
		{"64K", 1 << 16},
		{"1M", 1 << 20},
	}

	return func(b *testing.B) {
		// Generate a bunch of string keys.
		keys := make([]string, sizes[len(sizes)-1].val)
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}

		// Put the keys in random order.
		rand.New(rand.NewSource(9)).Shuffle(len(keys), func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
		})

		for _, size := range sizes {
			in := keys[:size.val]
			b.Run(size.name, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					m := create(0)
					for j, k := range in {
						m.Put(k, &in[j])
					}
				}

				// This isnt great but whatever.
				b.SetBytes(int64(len(in)) * int64(unsafe.Sizeof("")+unsafe.Sizeof(interface{}(nil))))
			})
		}
	}
}

func benchRandomReadHit(create func(uint) Map) func(b *testing.B) {
	sizes := []struct {
		name string
		val  int
	}{
		{"1", 1},
		{"16", 16},
		{"64", 64},
		{"256", 256},
		{"1K", 1 << 10},
		{"64K", 1 << 16},
		{"1M", 1 << 20},
	}

	return func(b *testing.B) {
		// Generate a bunch of string keys.
		keys := make([]string, sizes[len(sizes)-1].val)
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}

		// Put the keys in random order.
		rand.New(rand.NewSource(9)).Shuffle(len(keys), func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
		})

		for _, size := range sizes {
			in := keys[:size.val]
			m := create(uint(len(in)))
			for j, k := range in {
				m.Put(k, &in[j])
			}

			b.Run(size.name, func(b *testing.B) {
				_ = in[0]

				j := 0
				for i := 0; i < b.N; i++ {
					m.Get(in[j])
					j++
					if j >= len(in) {
						j = 0
					}
				}
			})
		}
	}
}

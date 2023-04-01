package bimap

import "sync"

// BiMap is a bi-directional hashmap that is thread safe and supports immutability.
type BiMap[K comparable, V comparable] struct {
	s         sync.RWMutex
	immutable bool
	forward   map[K]V
	inverse   map[V]K
}

// NewBiMap returns a an empty, mutable, biMap.
func NewBiMap[K comparable, V comparable]() *BiMap[K, V] {
	return &BiMap[K, V]{forward: make(map[K]V), inverse: make(map[V]K), immutable: false}
}

// NewBiMapFrom returns a new BiMap from a map[K, V].
func NewBiMapFromMap[K comparable, V comparable](forwardMap map[K]V) *BiMap[K, V] {
	biMap := NewBiMap[K, V]()
	for k, v := range forwardMap {
		biMap.Insert(k, v)
	}
	return biMap
}

// Insert puts a key and value into the BiMap, provided its mutable.
// Also creates the reverse mapping from value to key.
func (b *BiMap[K, V]) Insert(k K, v V) {
	b.s.RLock()
	if b.immutable {
		panic("Cannot modify immutable map")
	}
	b.s.RUnlock()

	b.s.Lock()
	defer b.s.Unlock()
	b.forward[k] = v
	b.inverse[v] = k
}

// Exists checks whether or not a key exists in the BiMap.
func (b *BiMap[K, V]) Exists(k K) bool {
	b.s.RLock()
	defer b.s.RUnlock()
	_, ok := b.forward[k]
	return ok
}

// ExistsInverse checks whether or not a value exists in the BiMap.
func (b *BiMap[K, V]) ExistsInverse(k V) bool {
	b.s.RLock()
	defer b.s.RUnlock()

	_, ok := b.inverse[k]
	return ok
}

// Get returns the value for a given key in the BiMap,
// and whether or not the element was present.
func (b *BiMap[K, V]) Get(k K) (V, bool) {
	if !b.Exists(k) {
		return *new(V), false
	}
	b.s.RLock()
	defer b.s.RUnlock()
	return b.forward[k], true
}

// GetInverse returns the key for a given value in the BiMap,
// and whether or not the element was present.
func (b *BiMap[K, V]) GetInverse(v V) (K, bool) {
	if !b.ExistsInverse(v) {
		return *new(K), false
	}
	b.s.RLock()
	defer b.s.RUnlock()
	return b.inverse[v], true

}

// Delete removes a key-value pair from the BiMap for a given key.
// Returns if the key doesn't exist.
func (b *BiMap[K, V]) Delete(k K) {
	b.s.RLock()
	if b.immutable {
		panic("Cannot modify immutable map")
	}
	b.s.RUnlock()

	if !b.Exists(k) {
		return
	}
	val, _ := b.Get(k)
	b.s.Lock()
	defer b.s.Unlock()
	delete(b.forward, k)
	delete(b.inverse, val)
}

// DeleteInverse emoves a key-value pair from the BiMap for a given value.
// Returns if the value doesn't exist.
func (b *BiMap[K, V]) DeleteInverse(v V) {
	b.s.RLock()
	if b.immutable {
		panic("Cannot modify immutable map")
	}
	b.s.RUnlock()

	if !b.ExistsInverse(v) {
		return
	}

	key, _ := b.GetInverse(v)
	b.s.Lock()
	defer b.s.Unlock()
	delete(b.inverse, v)
	delete(b.forward, key)

}

// Size returns the number of elements in the bimap.
func (b *BiMap[K, V]) Size() int {
	b.s.RLock()
	defer b.s.RUnlock()
	return len(b.forward)
}

// MakeImmutable freezes the BiMap preventing any further
// write actions from taking place.
func (b *BiMap[K, V]) MakeImmutable() {
	b.s.Lock()
	defer b.s.Unlock()
	b.immutable = true
}

// GetInverseMap returns a regular go map mapping from the BiMap's values to its keys.
func (b *BiMap[K, V]) GetInverseMap() map[V]K {
	return b.inverse
}

// GetForwardMap returns a regular go map mapping from the BiMap's keys to its values.
func (b *BiMap[K, V]) GetForwardMap() map[K]V {
	return b.forward
}

// Lock manually locks the BiMap's mutex.
func (b *BiMap[K, V]) Lock() {
	b.s.Lock()
}

// Unlock manually unlocks the BiMap's mutex.
func (b *BiMap[K, V]) Unlock() {
	b.s.Unlock()
}

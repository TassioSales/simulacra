package sim

// Storage is a node's simulated disk. Writes land in a page cache and only
// become durable on Sync; a crash throws the page cache away.
type Storage struct {
	durable map[string][]byte
	pending map[string][]byte
}

func newStorage() *Storage {
	return &Storage{durable: map[string][]byte{}, pending: map[string][]byte{}}
}

// Put writes a value to the page cache.
func (s *Storage) Put(key string, val []byte) {
	s.pending[key] = append([]byte(nil), val...)
}

// Get reads the latest value, durable or not.
func (s *Storage) Get(key string) ([]byte, bool) {
	if v, ok := s.pending[key]; ok {
		return v, true
	}
	v, ok := s.durable[key]
	return v, ok
}

// Sync makes all pending writes durable (fsync).
func (s *Storage) Sync() {
	for k, v := range s.pending {
		s.durable[k] = v
	}
	clear(s.pending)
}

func (s *Storage) crash() { clear(s.pending) }

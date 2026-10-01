// Package check verifies client histories against a sequential model.
//
// The linearizability checker is the Wing & Gong search with Lowe's
// memoization (the same approach as Knossos and Porcupine): walk the history
// in real-time order, try to linearize each pending call, and backtrack when
// a return is reached whose call could not be placed. The history is split by
// key first (P-compositionality), which keeps the search small.
package check

import (
	"cmp"
	"fmt"
	"hash/maphash"
	"math"
	"slices"

	"simulacra/internal/sim"
)

// KVInput is a client request against a key-value register.
type KVInput struct {
	Kind     string `json:"kind"` // get, put, cas
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"`
	Expected string `json:"expected,omitempty"`
}

func (in KVInput) String() string {
	switch in.Kind {
	case "put":
		return fmt.Sprintf("put %s=%s", in.Key, in.Value)
	case "cas":
		return fmt.Sprintf("cas %s %s→%s", in.Key, orNil(in.Expected), in.Value)
	}
	return "get " + in.Key
}

// KVOutput is the server's answer.
type KVOutput struct {
	Ok    bool   `json:"ok"`
	Value string `json:"value,omitempty"`
}

func orNil(s string) string {
	if s == "" {
		return "∅"
	}
	return s
}

// LinReport explains the outcome for the first key that failed.
type LinReport struct {
	Ok      bool   `json:"ok"`
	Unknown bool   `json:"unknown"` // search budget exhausted
	Key     string `json:"key,omitempty"`
	// Ops are the op IDs of the failing key; Linearized is the longest
	// prefix the search managed to order; Stuck is the op that could not be
	// placed after it.
	Ops        []int `json:"ops,omitempty"`
	Linearized []int `json:"linearized,omitempty"`
	Stuck      []int `json:"stuck,omitempty"`
	States     int   `json:"states"`
}

// Budget caps the number of search states per key.
const Budget = 2_000_000

// Linearizable checks a KV history. Reads that never returned carry no
// information and are dropped; writes that never returned may have happened
// at any point after their call, so they get an infinite return time.
func Linearizable(history []sim.Op) LinReport {
	byKey := map[string][]sim.Op{}
	var keys []string
	for _, op := range history {
		in, ok := op.Input.(KVInput)
		if !ok {
			continue
		}
		if op.Pending && in.Kind == "get" {
			continue
		}
		if _, seen := byKey[in.Key]; !seen {
			keys = append(keys, in.Key)
		}
		byKey[in.Key] = append(byKey[in.Key], op)
	}
	slices.Sort(keys)
	total := LinReport{Ok: true}
	for _, k := range keys {
		r := checkKey(byKey[k])
		total.States += r.States
		if !r.Ok {
			r.Key = k
			r.States = total.States
			return r
		}
		if r.Unknown {
			total.Unknown = true
		}
	}
	return total
}

type entry struct {
	op     *sim.Op
	idx    int // position of op inside the key's slice (bit in the set)
	call   bool
	match  *entry
	prev   *entry
	next   *entry
	sortAt sim.Time
}

func step(state string, op *sim.Op) (string, bool) {
	in := op.Input.(KVInput)
	var out KVOutput
	if !op.Pending {
		out = op.Output.(KVOutput)
	}
	switch in.Kind {
	case "get":
		return state, out.Value == state
	case "put":
		return in.Value, true
	case "cas":
		success := state == in.Expected
		if op.Pending { // unknown outcome: a failed CAS is a no-op anyway
			if success {
				return in.Value, true
			}
			return state, true
		}
		if success != out.Ok {
			return state, false
		}
		if success {
			return in.Value, true
		}
		return state, true
	}
	return state, false
}

type bitset []uint64

func newBitset(n int) bitset         { return make(bitset, (n+63)/64) }
func (b bitset) set(i int)           { b[i/64] |= 1 << (i % 64) }
func (b bitset) clear(i int)         { b[i/64] &^= 1 << (i % 64) }
func (b bitset) clone() bitset       { return append(bitset(nil), b...) }
func (b bitset) equal(o bitset) bool { return slices.Equal(b, o) }

type cacheEntry struct {
	set   bitset
	state string
}

var seed = maphash.MakeSeed()

func (b bitset) hash(state string) uint64 {
	var h maphash.Hash
	h.SetSeed(seed)
	for _, w := range b {
		var buf [8]byte
		for i := range 8 {
			buf[i] = byte(w >> (8 * i))
		}
		h.Write(buf[:])
	}
	h.WriteString(state)
	return h.Sum64()
}

func checkKey(ops []sim.Op) LinReport {
	n := len(ops)
	// Build the event list: calls and returns sorted by time. Calls sort
	// before returns at equal time, which makes touching ops concurrent.
	events := make([]*entry, 0, 2*n)
	for i := range ops {
		op := &ops[i]
		ret := op.Return
		if op.Pending {
			ret = math.MaxInt64
		}
		c := &entry{op: op, idx: i, call: true, sortAt: op.Call}
		r := &entry{op: op, idx: i, sortAt: ret}
		c.match = r
		events = append(events, c, r)
	}
	slices.SortStableFunc(events, func(a, b *entry) int {
		if c := cmp.Compare(a.sortAt, b.sortAt); c != 0 {
			return c
		}
		if a.call != b.call {
			if a.call {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.op.ID, b.op.ID)
	})
	head := &entry{}
	prev := head
	for _, e := range events {
		prev.next = e
		e.prev = prev
		prev = e
	}

	lift := func(e *entry) {
		e.prev.next = e.next
		if e.next != nil {
			e.next.prev = e.prev
		}
		m := e.match
		m.prev.next = m.next
		if m.next != nil {
			m.next.prev = m.prev
		}
	}
	unlift := func(e *entry) {
		m := e.match
		m.prev.next = m
		if m.next != nil {
			m.next.prev = m
		}
		e.prev.next = e
		if e.next != nil {
			e.next.prev = e
		}
	}

	type frame struct {
		e     *entry
		state string
	}
	var stack []frame
	lin := newBitset(n)
	cache := map[uint64][]cacheEntry{}
	state := ""
	states := 0
	var best []int
	var bestStuck []int

	e := head.next
	for head.next != nil {
		states++
		if states > Budget {
			return LinReport{Ok: true, Unknown: true, States: states}
		}
		if e.call {
			if next, ok := step(state, e.op); ok {
				nl := lin.clone()
				nl.set(e.idx)
				h := nl.hash(next)
				seen := false
				for _, c := range cache[h] {
					if c.state == next && c.set.equal(nl) {
						seen = true
						break
					}
				}
				if !seen {
					cache[h] = append(cache[h], cacheEntry{set: nl, state: next})
					stack = append(stack, frame{e: e, state: state})
					state = next
					lin = nl.clone() // the cache keeps nl; lin is mutated on backtrack
					lift(e)
					e = head.next
					continue
				}
			}
			e = e.next
			continue
		}
		// A return whose call is still in the list: this path is dead.
		if len(stack) >= len(best) {
			best = best[:0]
			for _, f := range stack {
				best = append(best, f.e.op.ID)
			}
			bestStuck = bestStuck[:0]
			bestStuck = append(bestStuck, e.op.ID)
		}
		if len(stack) == 0 {
			ids := make([]int, n)
			for i := range ops {
				ids[i] = ops[i].ID
			}
			return LinReport{Ok: false, Ops: ids, Linearized: best, Stuck: bestStuck, States: states}
		}
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		state = top.state
		lin.clear(top.e.idx)
		unlift(top.e)
		e = top.e.next
	}
	return LinReport{Ok: true, States: states}
}

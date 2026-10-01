package sim

import (
	"container/heap"
	"fmt"
	"hash"
	"hash/fnv"
	"math/rand/v2"
	"reflect"
	"sync"
)

// Config describes one simulation run.
type Config struct {
	Seed     uint64
	Servers  int
	Clients  int
	Duration Time
	Network  NetworkConfig
	Faults   []Fault      // nemesis schedule, see PlanFaults
	Disabled map[int]bool // fault IDs to skip (used when shrinking)
	Record   bool         // keep the full trace (slower, used for replay)
	MaxSteps int          // safety net against livelock; 0 means 5M

	NewServer func(id NodeID, servers []NodeID) Node
	NewClient func(id NodeID, servers []NodeID) Node
}

// Result is what a run produced.
type Result struct {
	Seed         uint64        `json:"seed"`
	Hash         string        `json:"hash"`
	Steps        int           `json:"steps"`
	End          Time          `json:"end"`
	Messages     int           `json:"messages"`
	Dropped      int           `json:"dropped"`
	Aborted      bool          `json:"aborted"`
	History      []Op          `json:"history"`
	Observations []Observation `json:"observations"`
	Trace        []TraceEvent  `json:"trace,omitempty"`
	Faults       []Fault       `json:"faults"`
}

type evKind uint8

const (
	evDeliver evKind = iota
	evTimer
	evFaultStart
	evFaultEnd
)

type event struct {
	at    Time
	seq   uint64
	kind  evKind
	from  NodeID
	to    NodeID
	inc   int // target incarnation, for timers
	msgID int
	msg   any
	fault *Fault
}

type eventQueue []*event

func (q eventQueue) Len() int { return len(q) }
func (q eventQueue) Less(i, j int) bool {
	if q[i].at != q[j].at {
		return q[i].at < q[j].at
	}
	return q[i].seq < q[j].seq
}
func (q eventQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *eventQueue) Push(x any)   { *q = append(*q, x.(*event)) }
func (q *eventQueue) Pop() any {
	old := *q
	e := old[len(old)-1]
	*q = old[:len(old)-1]
	return e
}

type slot struct {
	id       NodeID
	client   bool
	node     Node
	up       bool
	inc      int
	downBy   int // fault that crashed it
	storage  *Storage
	env      *Env
	newNode  func() Node
	openedOp map[int]bool
}

// World is a single simulation. It is not safe for concurrent use; run many
// worlds in parallel instead.
type World struct {
	cfg     Config
	now     Time
	seq     uint64
	queue   eventQueue
	slots   []*slot
	servers []NodeID
	cut     [][]int // cut[a][b] > 0 means servers a and b can't talk
	netRng  *rand.Rand
	hasher  hash.Hash64
	buf     [8]byte
	msgSeq  int
	res     *Result
}

// Run executes a simulation to completion and returns its result.
func Run(cfg Config) *Result {
	if cfg.MaxSteps == 0 {
		cfg.MaxSteps = 5_000_000
	}
	w := &World{
		cfg:    cfg,
		netRng: rand.New(rand.NewPCG(cfg.Seed, 0x6e6574776f726b)),
		hasher: fnv.New64a(),
		res:    &Result{Seed: cfg.Seed, Faults: []Fault{}, History: []Op{}, Observations: []Observation{}},
	}
	n := cfg.Servers
	w.cut = make([][]int, n)
	for i := range w.cut {
		w.cut[i] = make([]int, n)
	}
	for i := range n {
		w.servers = append(w.servers, NodeID(i))
	}
	total := cfg.Servers + cfg.Clients
	for i := range total {
		id := NodeID(i)
		s := &slot{id: id, client: i >= n, storage: newStorage(), downBy: -1, openedOp: map[int]bool{}}
		servers := append([]NodeID(nil), w.servers...)
		if s.client {
			s.newNode = func() Node { return cfg.NewClient(id, servers) }
		} else {
			s.newNode = func() Node { return cfg.NewServer(id, servers) }
		}
		w.slots = append(w.slots, s)
	}
	for i := range cfg.Faults {
		f := &cfg.Faults[i]
		if cfg.Disabled[f.ID] {
			continue
		}
		w.res.Faults = append(w.res.Faults, *f)
		w.push(&event{at: f.At, kind: evFaultStart, fault: f, from: None, to: None})
		w.push(&event{at: f.Until, kind: evFaultEnd, fault: f, from: None, to: None})
	}
	for _, s := range w.slots {
		w.boot(s)
	}

	steps := 0
	for w.queue.Len() > 0 {
		e := heap.Pop(&w.queue).(*event)
		if e.at > cfg.Duration {
			break
		}
		w.now = e.at
		steps++
		if steps > cfg.MaxSteps {
			w.res.Aborted = true
			break
		}
		w.dispatch(e)
	}
	w.res.Steps = steps
	w.res.End = w.now
	for i := range w.res.History {
		op := &w.res.History[i]
		w.hashStr(fmt.Sprintf("%d|%v|%v", op.ID, op.Input, op.Output))
	}
	w.res.Hash = fmt.Sprintf("%016x", w.hasher.Sum64())
	return w.res
}

func (w *World) push(e *event) {
	w.seq++
	e.seq = w.seq
	heap.Push(&w.queue, e)
}

func (w *World) boot(s *slot) {
	s.up = true
	s.inc++
	s.env = &Env{
		w:    w,
		slot: s,
		rng:  rand.New(rand.NewPCG(w.cfg.Seed^uint64(s.id+1)*0x9e3779b97f4a7c15, uint64(s.inc))),
	}
	s.node = s.newNode()
	s.node.Start(s.env)
}

func (w *World) dispatch(e *event) {
	switch e.kind {
	case evDeliver:
		s := w.slots[e.to]
		typ := typeName(e.msg)
		w.hashEvent(1, e.from, e.to, e.msgID, typ)
		switch {
		case !s.up:
			w.res.Dropped++
			w.trace(TraceEvent{Kind: "drop", From: e.from, To: e.to, MsgID: e.msgID, Type: typ, Note: "destino fora do ar"})
		case w.blocked(e.from, e.to):
			w.res.Dropped++
			w.trace(TraceEvent{Kind: "drop", From: e.from, To: e.to, MsgID: e.msgID, Type: typ, Note: "partição"})
		default:
			w.trace(TraceEvent{Kind: "deliver", From: e.from, To: e.to, MsgID: e.msgID, Type: typ})
			s.node.Receive(s.env, e.from, e.msg)
		}
	case evTimer:
		s := w.slots[e.to]
		if !s.up || s.inc != e.inc {
			return
		}
		typ := typeName(e.msg)
		w.hashEvent(2, e.to, e.to, 0, typ)
		w.trace(TraceEvent{Kind: "timer", From: e.to, To: e.to, Type: typ, Data: w.data(e.msg)})
		s.node.Timer(s.env, e.msg)
	case evFaultStart:
		w.faultStart(e.fault)
	case evFaultEnd:
		w.faultEnd(e.fault)
	}
}

func (w *World) faultStart(f *Fault) {
	w.hashEvent(3, f.Node, None, f.ID, f.Kind)
	switch f.Kind {
	case "crash":
		s := w.slots[f.Node]
		if !s.up {
			return
		}
		s.up = false
		s.downBy = f.ID
		s.storage.crash()
		s.node = nil
		w.trace(TraceEvent{Kind: "crash", From: f.Node, To: None, Data: f.ID, Note: "processo derrubado; escritas sem fsync perdidas"})
	case "partition":
		w.applyCut(f.Groups, 1)
		w.trace(TraceEvent{Kind: "partition", From: None, To: None, Data: f.Groups, Note: fmt.Sprintf("falha #%d", f.ID)})
	}
}

func (w *World) faultEnd(f *Fault) {
	w.hashEvent(4, f.Node, None, f.ID, f.Kind)
	switch f.Kind {
	case "crash":
		s := w.slots[f.Node]
		if s.up || s.downBy != f.ID {
			return
		}
		s.downBy = -1
		w.trace(TraceEvent{Kind: "restart", From: f.Node, To: None, Data: f.ID})
		w.boot(s)
	case "partition":
		w.applyCut(f.Groups, -1)
		w.trace(TraceEvent{Kind: "heal", From: None, To: None, Data: f.Groups, Note: fmt.Sprintf("falha #%d", f.ID)})
	}
}

func (w *World) applyCut(groups [][]NodeID, delta int) {
	for i, ga := range groups {
		for j, gb := range groups {
			if i == j {
				continue
			}
			for _, a := range ga {
				for _, b := range gb {
					w.cut[a][b] += delta
				}
			}
		}
	}
}

func (w *World) blocked(a, b NodeID) bool {
	n := NodeID(w.cfg.Servers)
	if a >= n || b >= n || a < 0 || b < 0 {
		return false // clients reach every server
	}
	return w.cut[a][b] > 0
}

func (w *World) send(from, to NodeID, msg any) {
	w.msgSeq++
	id := w.msgSeq
	w.res.Messages++
	typ := typeName(msg)
	w.hashEvent(5, from, to, id, typ)
	w.trace(TraceEvent{Kind: "send", From: from, To: to, MsgID: id, Type: typ, Data: w.data(msg)})
	if to < 0 || int(to) >= len(w.slots) {
		return
	}
	if w.blocked(from, to) {
		w.res.Dropped++
		w.trace(TraceEvent{Kind: "drop", From: from, To: to, MsgID: id, Type: typ, Note: "partição"})
		return
	}
	net := w.cfg.Network
	if w.netRng.Float64() < net.DropRate {
		w.res.Dropped++
		w.trace(TraceEvent{Kind: "drop", From: from, To: to, MsgID: id, Type: typ, Note: "perda na rede"})
		return
	}
	copies := 1
	if w.netRng.Float64() < net.DupRate {
		copies = 2
	}
	for range copies {
		lat := net.MinLatency
		if span := net.MaxLatency - net.MinLatency; span > 0 {
			lat += Time(w.netRng.Int64N(int64(span) + 1))
		}
		if w.netRng.Float64() < net.SlowRate {
			lat *= 10
		}
		w.push(&event{at: w.now + lat, kind: evDeliver, from: from, to: to, msgID: id, msg: msg})
	}
}

func (w *World) trace(e TraceEvent) {
	if !w.cfg.Record {
		return
	}
	e.T = w.now
	w.res.Trace = append(w.res.Trace, e)
}

func (w *World) data(v any) any {
	if !w.cfg.Record {
		return nil
	}
	return v
}

func (w *World) hashEvent(kind byte, a, b NodeID, id int, typ string) {
	h := w.hasher
	put := func(v uint64) {
		for i := range 8 {
			w.buf[i] = byte(v >> (8 * i))
		}
		h.Write(w.buf[:])
	}
	put(uint64(w.now))
	put(uint64(kind))
	put(uint64(int64(a)))
	put(uint64(int64(b)))
	put(uint64(id))
	h.Write([]byte(typ))
}

func (w *World) hashStr(s string) { w.hasher.Write([]byte(s)) }

// typeNames caches message type names; worlds run in parallel, hence sync.Map.
var typeNames sync.Map

func typeName(v any) string {
	t := reflect.TypeOf(v)
	if t == nil {
		return "nil"
	}
	if n, ok := typeNames.Load(t); ok {
		return n.(string)
	}
	n := t.Name()
	if n == "" {
		n = t.String()
	}
	typeNames.Store(t, n)
	return n
}

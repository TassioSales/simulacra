// Package sim is a deterministic discrete-event simulator for distributed
// systems. Every source of nondeterminism — the clock, the network, the order
// in which events fire, crashes and partitions — is derived from a single
// seed, so any run can be replayed bit for bit.
//
// Systems under test are written as event-driven actors (Node) that talk to
// the outside world only through an Env. The simulator owns the only thread.
package sim

import "fmt"

// NodeID identifies an actor. Servers are numbered 0..Servers-1 and clients
// follow them.
type NodeID int

// None is used where a node reference is absent.
const None NodeID = -1

// Time is virtual time in microseconds.
type Time int64

const (
	Microsecond Time = 1
	Millisecond      = 1000 * Microsecond
	Second           = 1000 * Millisecond
)

// Ms returns t in milliseconds.
func (t Time) Ms() float64 { return float64(t) / float64(Millisecond) }

func (t Time) String() string { return fmt.Sprintf("%.3fms", t.Ms()) }

// Node is an actor in the simulation. All callbacks run on the simulator's
// single thread; a Node must not start goroutines or read the real clock.
//
// Messages passed to Env.Send must not be mutated after sending: copy any
// slices that the node keeps using.
type Node interface {
	// Start is called when the node boots and again after every restart.
	// Volatile state must be rebuilt from Env.Storage.
	Start(env *Env)
	Receive(env *Env, from NodeID, msg any)
	Timer(env *Env, tag any)
}

// NetworkConfig controls the simulated network.
type NetworkConfig struct {
	DropRate   float64 // probability a message is lost
	DupRate    float64 // probability a message is delivered twice
	MinLatency Time
	MaxLatency Time
	SlowRate   float64 // probability a message takes 10x longer (reordering)
}

// Fault is one entry of the nemesis schedule.
type Fault struct {
	ID     int        `json:"id"`
	Kind   string     `json:"kind"` // "crash" or "partition"
	At     Time       `json:"at"`
	Until  Time       `json:"until"`
	Node   NodeID     `json:"node"`
	Groups [][]NodeID `json:"groups,omitempty"` // links between groups are cut
	Bridge NodeID     `json:"bridge"`           // server outside both groups that reaches everyone
}

func (f Fault) String() string {
	if f.Kind == "crash" {
		return fmt.Sprintf("crash S%d em %.0fms por %.0fms", f.Node, f.At.Ms(), (f.Until - f.At).Ms())
	}
	if f.Bridge != None {
		return fmt.Sprintf("ponte %v ↔ %v via S%d em %.0fms por %.0fms", f.Groups[0], f.Groups[1], f.Bridge, f.At.Ms(), (f.Until - f.At).Ms())
	}
	return fmt.Sprintf("partição %v | %v em %.0fms por %.0fms", f.Groups[0], f.Groups[1], f.At.Ms(), (f.Until - f.At).Ms())
}

// Op is one client operation in the history, used by checkers.
type Op struct {
	ID      int    `json:"id"`
	Client  NodeID `json:"client"`
	Call    Time   `json:"call"`
	Return  Time   `json:"return"` // -1 when the op never completed
	Input   any    `json:"input"`
	Output  any    `json:"output,omitempty"`
	Pending bool   `json:"pending"`
}

// Observation is a fact a node reports about itself (e.g. "I became leader
// of term 4"). Checkers use observations to verify invariants.
type Observation struct {
	T    Time   `json:"t"`
	Node NodeID `json:"node"`
	Kind string `json:"kind"`
	Data any    `json:"data"`
}

// TraceEvent is one line of the recorded trace. From/To are -1 when unused.
type TraceEvent struct {
	T     Time   `json:"t"`
	Kind  string `json:"kind"`
	From  NodeID `json:"from"`
	To    NodeID `json:"to"`
	MsgID int    `json:"msg,omitempty"`
	Type  string `json:"type,omitempty"`
	Data  any    `json:"data,omitempty"`
	Note  string `json:"note,omitempty"`
}

// Violation is a safety property that a run broke.
type Violation struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

package sim

import (
	"fmt"
	"math/rand/v2"
)

// Env is a node's only window to the world. It is valid for one incarnation
// of the node; after a crash the node gets a fresh Env.
type Env struct {
	w    *World
	slot *slot
	rng  *rand.Rand
}

// ID returns the node's identity.
func (e *Env) ID() NodeID { return e.slot.id }

// Now returns the current virtual time.
func (e *Env) Now() Time { return e.w.now }

// Rand returns a deterministic random source private to this incarnation.
func (e *Env) Rand() *rand.Rand { return e.rng }

// Storage returns the node's disk. It survives crashes; unsynced writes don't.
func (e *Env) Storage() *Storage { return e.slot.storage }

// Send puts a message on the simulated network.
func (e *Env) Send(to NodeID, msg any) { e.w.send(e.slot.id, to, msg) }

// SetTimer schedules Node.Timer(tag) after d. Timers die with the incarnation.
func (e *Env) SetTimer(d Time, tag any) {
	e.w.push(&event{at: e.w.now + d, kind: evTimer, from: e.slot.id, to: e.slot.id, inc: e.slot.inc, msg: tag})
}

// Observe reports a fact for invariant checkers.
func (e *Env) Observe(kind string, data any) {
	w := e.w
	w.res.Observations = append(w.res.Observations, Observation{T: w.now, Node: e.slot.id, Kind: kind, Data: data})
	w.hashStr(kind)
	w.trace(TraceEvent{Kind: "observe", From: e.slot.id, To: None, Type: kind, Data: data})
}

// Logf adds a free-form note to the trace (only kept when recording).
func (e *Env) Logf(format string, args ...any) {
	if !e.w.cfg.Record {
		return
	}
	e.w.trace(TraceEvent{Kind: "log", From: e.slot.id, To: None, Note: fmt.Sprintf(format, args...)})
}

// Invoke records the start of a client operation and returns its ID.
func (e *Env) Invoke(input any) int {
	w := e.w
	id := len(w.res.History)
	w.res.History = append(w.res.History, Op{ID: id, Client: e.slot.id, Call: w.now, Return: -1, Input: input, Pending: true})
	e.slot.openedOp[id] = true
	w.trace(TraceEvent{Kind: "invoke", From: e.slot.id, To: None, MsgID: id, Data: input})
	return id
}

// Complete records the response of a client operation.
func (e *Env) Complete(id int, output any) {
	w := e.w
	if !e.slot.openedOp[id] {
		panic(fmt.Sprintf("sim: client %d completed op %d it does not own", e.slot.id, id))
	}
	delete(e.slot.openedOp, id)
	op := &w.res.History[id]
	op.Return = w.now
	op.Output = output
	op.Pending = false
	w.trace(TraceEvent{Kind: "return", From: e.slot.id, To: None, MsgID: id, Data: output})
}

// Abandon gives up on an operation: its effect is unknown, so checkers treat
// it as possibly happening at any point after its call.
func (e *Env) Abandon(id int) {
	delete(e.slot.openedOp, id)
	e.w.trace(TraceEvent{Kind: "timeout", From: e.slot.id, To: None, MsgID: id})
}

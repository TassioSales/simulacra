// Package raftkv is a replicated key-value store built on Raft, written as
// event-driven actors for the simulator. It is a faithful implementation of
// the paper's safety rules plus client sessions for exactly-once semantics.
//
// Each Bug flag switches off one rule, reproducing a class of defect that
// has shipped in real consensus systems.
package raftkv

import "simulacra/internal/sim"

// Cmd is a state-machine command.
type Cmd struct {
	Kind     string     `json:"kind"` // noop, get, put, cas
	Key      string     `json:"key,omitempty"`
	Value    string     `json:"value,omitempty"`
	Expected string     `json:"expected,omitempty"`
	Client   sim.NodeID `json:"client"`
	Seq      int        `json:"seq"`
}

// Entry is a log entry.
type Entry struct {
	Term int `json:"term"`
	Cmd  Cmd `json:"cmd"`
}

type RequestVote struct {
	Term      int `json:"term"`
	LastIndex int `json:"lastIndex"`
	LastTerm  int `json:"lastTerm"`
}

type VoteReply struct {
	Term    int  `json:"term"`
	Granted bool `json:"granted"`
}

type AppendEntries struct {
	Term      int     `json:"term"`
	PrevIndex int     `json:"prevIndex"`
	PrevTerm  int     `json:"prevTerm"`
	Entries   []Entry `json:"entries,omitempty"`
	Commit    int     `json:"commit"`
}

type AppendReply struct {
	Term    int  `json:"term"`
	Success bool `json:"success"`
	Match   int  `json:"match"`
	Hint    int  `json:"hint"` // next index the follower wants
}

type ClientRequest struct {
	Cmd Cmd `json:"cmd"`
}

type ClientReply struct {
	Seq    int        `json:"seq"`
	Ok     bool       `json:"ok"`
	Value  string     `json:"value,omitempty"`
	Err    string     `json:"err,omitempty"` // "not-leader"
	Leader sim.NodeID `json:"leader"`
}

// Timer tags.
type electionTimeout struct{ Gen int }
type heartbeat struct{ Term int }
type voteRetry struct{ Term int }

// Observations used by the invariant checkers.
type LeaderObs struct {
	Term int `json:"term"`
}

type ApplyObs struct {
	Index  int        `json:"index"`
	Term   int        `json:"term"`
	Kind   string     `json:"kind"`
	Client sim.NodeID `json:"client"`
	Seq    int        `json:"seq"`
}

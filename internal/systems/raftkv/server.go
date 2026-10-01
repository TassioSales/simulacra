package raftkv

import (
	"encoding/json"
	"strconv"

	"simulacra/internal/sim"
)

const (
	electionMin   = 150 * sim.Millisecond
	electionRange = 150 * sim.Millisecond
	heartbeatIval = 50 * sim.Millisecond
	voteRetryIval = 40 * sim.Millisecond
	maxBatch      = 64
)

type role int

const (
	follower role = iota
	candidate
	leader
)

func (r role) String() string {
	return [...]string{"follower", "candidate", "leader"}[r]
}

type session struct {
	seq   int
	reply ClientReply
}

type waiter struct {
	client sim.NodeID
	seq    int
}

// Server is one Raft replica.
type Server struct {
	bugs  Bugs
	id    sim.NodeID
	peers []sim.NodeID // all servers, including self

	// Persistent state.
	term     int
	votedFor sim.NodeID
	log      []Entry // log[0] is a sentinel with term 0

	// Volatile state.
	role     role
	leader   sim.NodeID
	commit   int
	applied  int
	votes    map[sim.NodeID]bool
	next     map[sim.NodeID]int
	match    map[sim.NodeID]int
	kv       map[string]string
	sessions map[sim.NodeID]session
	waiting  map[int]waiter
	elecGen  int
}

// NewServer builds a replica with the given bugs enabled.
func NewServer(id sim.NodeID, peers []sim.NodeID, bugs Bugs) *Server {
	return &Server{id: id, peers: peers, bugs: bugs}
}

func (s *Server) Start(env *sim.Env) {
	s.load(env.Storage())
	s.role = follower
	s.leader = sim.None
	s.commit, s.applied = 0, 0
	s.kv = map[string]string{}
	s.sessions = map[sim.NodeID]session{}
	s.waiting = map[int]waiter{}
	s.resetElection(env)
}

// ---- persistence -------------------------------------------------------

func (s *Server) load(st *sim.Storage) {
	s.term = readInt(st, "term", 0)
	s.votedFor = sim.NodeID(readInt(st, "vote", int(sim.None)))
	n := readInt(st, "len", 0)
	s.log = make([]Entry, 1, n+1)
	for i := 1; i <= n; i++ {
		raw, _ := st.Get("e/" + strconv.Itoa(i))
		var e Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			panic("raftkv: corrupt log entry " + strconv.Itoa(i))
		}
		s.log = append(s.log, e)
	}
}

func readInt(st *sim.Storage, key string, def int) int {
	raw, ok := st.Get(key)
	if !ok {
		return def
	}
	v, _ := strconv.Atoi(string(raw))
	return v
}

// persistState writes term and vote and fsyncs.
func (s *Server) persistState(env *sim.Env) {
	st := env.Storage()
	st.Put("term", []byte(strconv.Itoa(s.term)))
	if !s.bugs[BugVoteNotPersisted] {
		st.Put("vote", []byte(strconv.Itoa(int(s.votedFor))))
	}
	st.Sync()
}

// persistLog writes entries from index `from` onwards and the log length.
func (s *Server) persistLog(env *sim.Env, from int, sync bool) {
	st := env.Storage()
	for i := from; i < len(s.log); i++ {
		raw, _ := json.Marshal(s.log[i])
		st.Put("e/"+strconv.Itoa(i), raw)
	}
	st.Put("len", []byte(strconv.Itoa(s.lastIndex())))
	if sync {
		st.Sync()
	}
}

// ---- helpers -----------------------------------------------------------

func (s *Server) lastIndex() int { return len(s.log) - 1 }
func (s *Server) lastTerm() int  { return s.log[len(s.log)-1].Term }
func (s *Server) majority() int  { return len(s.peers)/2 + 1 }

func (s *Server) resetElection(env *sim.Env) {
	s.elecGen++
	d := electionMin + sim.Time(env.Rand().Int64N(int64(electionRange)))
	env.SetTimer(d, electionTimeout{Gen: s.elecGen})
}

func (s *Server) stepDown(env *sim.Env, term int) {
	if term > s.term {
		s.term = term
		s.votedFor = sim.None
		s.persistState(env)
	}
	if s.role == leader {
		env.Logf("S%d deixa de ser líder (termo %d)", s.id, s.term)
	}
	s.role = follower
	clear(s.waiting)
}

// ---- event handlers ----------------------------------------------------

func (s *Server) Timer(env *sim.Env, tag any) {
	switch t := tag.(type) {
	case electionTimeout:
		if t.Gen != s.elecGen || s.role == leader {
			return
		}
		s.startElection(env)
	case heartbeat:
		if s.role != leader || t.Term != s.term {
			return
		}
		s.broadcastAppend(env)
		env.SetTimer(heartbeatIval, heartbeat{Term: s.term})
	case voteRetry:
		if s.role != candidate || t.Term != s.term {
			return
		}
		s.requestVotes(env)
	}
}

func (s *Server) Receive(env *sim.Env, from sim.NodeID, msg any) {
	switch m := msg.(type) {
	case RequestVote:
		s.onRequestVote(env, from, m)
	case VoteReply:
		s.onVoteReply(env, from, m)
	case AppendEntries:
		s.onAppend(env, from, m)
	case AppendReply:
		s.onAppendReply(env, from, m)
	case ClientRequest:
		s.onClient(env, from, m.Cmd)
	}
}

func (s *Server) startElection(env *sim.Env) {
	s.term++
	s.role = candidate
	s.votedFor = s.id
	s.persistState(env)
	s.votes = map[sim.NodeID]bool{s.id: true}
	env.Logf("S%d inicia eleição no termo %d", s.id, s.term)
	s.requestVotes(env)
	s.resetElection(env)
}

// requestVotes asks every peer that hasn't voted yet, and schedules a resend
// in case the request or the reply is lost.
func (s *Server) requestVotes(env *sim.Env) {
	for _, p := range s.peers {
		if p != s.id && !s.votes[p] {
			env.Send(p, RequestVote{Term: s.term, LastIndex: s.lastIndex(), LastTerm: s.lastTerm()})
		}
	}
	env.SetTimer(voteRetryIval, voteRetry{Term: s.term})
}

func (s *Server) onRequestVote(env *sim.Env, from sim.NodeID, m RequestVote) {
	if m.Term > s.term {
		s.stepDown(env, m.Term)
	}
	upToDate := m.LastTerm > s.lastTerm() || (m.LastTerm == s.lastTerm() && m.LastIndex >= s.lastIndex())
	granted := false
	if m.Term == s.term && (s.votedFor == sim.None || s.votedFor == from) && upToDate {
		s.votedFor = from
		s.persistState(env)
		granted = true
		s.resetElection(env)
	}
	env.Send(from, VoteReply{Term: s.term, Granted: granted})
}

func (s *Server) onVoteReply(env *sim.Env, from sim.NodeID, m VoteReply) {
	if m.Term > s.term {
		s.stepDown(env, m.Term)
		return
	}
	if s.role != candidate || m.Term != s.term || !m.Granted {
		return
	}
	s.votes[from] = true
	if len(s.votes) >= s.majority() {
		s.becomeLeader(env)
	}
}

func (s *Server) becomeLeader(env *sim.Env) {
	s.role = leader
	s.leader = s.id
	env.Observe("leader", LeaderObs{Term: s.term})
	s.next = map[sim.NodeID]int{}
	s.match = map[sim.NodeID]int{}
	for _, p := range s.peers {
		s.next[p] = s.lastIndex() + 1
		s.match[p] = 0
	}
	// A no-op in the new term lets the leader commit entries of older terms.
	s.log = append(s.log, Entry{Term: s.term, Cmd: Cmd{Kind: "noop", Client: sim.None}})
	s.persistLog(env, s.lastIndex(), true)
	s.match[s.id] = s.lastIndex()
	s.broadcastAppend(env)
	env.SetTimer(heartbeatIval, heartbeat{Term: s.term})
}

func (s *Server) broadcastAppend(env *sim.Env) {
	for _, p := range s.peers {
		if p != s.id {
			s.sendAppend(env, p)
		}
	}
}

func (s *Server) sendAppend(env *sim.Env, to sim.NodeID) {
	next := min(max(s.next[to], 1), s.lastIndex()+1)
	prev := next - 1
	end := min(len(s.log), next+maxBatch)
	entries := append([]Entry(nil), s.log[next:end]...) // copy: the log keeps changing
	env.Send(to, AppendEntries{Term: s.term, PrevIndex: prev, PrevTerm: s.log[prev].Term, Entries: entries, Commit: s.commit})
}

func (s *Server) onAppend(env *sim.Env, from sim.NodeID, m AppendEntries) {
	if m.Term < s.term && !s.bugs[BugAcceptStaleTerm] {
		env.Send(from, AppendReply{Term: s.term, Success: false, Hint: s.lastIndex() + 1})
		return
	}
	if m.Term > s.term {
		s.stepDown(env, m.Term)
	} else if s.role != follower && m.Term == s.term {
		s.stepDown(env, m.Term)
	}
	s.leader = from
	s.resetElection(env)

	if m.PrevIndex > s.lastIndex() {
		env.Send(from, AppendReply{Term: s.term, Hint: s.lastIndex() + 1})
		return
	}
	if s.log[m.PrevIndex].Term != m.PrevTerm {
		// Skip back over the whole conflicting term.
		t := s.log[m.PrevIndex].Term
		i := m.PrevIndex
		for i > 1 && s.log[i-1].Term == t {
			i--
		}
		env.Send(from, AppendReply{Term: s.term, Hint: i})
		return
	}
	dirty := -1
	for i, e := range m.Entries {
		idx := m.PrevIndex + 1 + i
		if idx <= s.lastIndex() {
			if s.log[idx].Term == e.Term {
				continue
			}
			if idx <= s.commit {
				env.Logf("S%d sobrescreve entrada já aplicada %d", s.id, idx)
			}
			s.log = s.log[:idx]
		}
		s.log = append(s.log, e)
		if dirty < 0 {
			dirty = idx
		}
	}
	if dirty >= 0 {
		s.persistLog(env, dirty, !s.bugs[BugUnsyncedLog])
	}
	last := m.PrevIndex + len(m.Entries)
	if m.Commit > s.commit {
		s.commit = min(m.Commit, last)
		s.apply(env)
	}
	env.Send(from, AppendReply{Term: s.term, Success: true, Match: last})
}

func (s *Server) onAppendReply(env *sim.Env, from sim.NodeID, m AppendReply) {
	if m.Term > s.term {
		s.stepDown(env, m.Term)
		return
	}
	if s.role != leader || m.Term != s.term {
		return
	}
	if !m.Success {
		s.next[from] = max(1, m.Hint)
		s.sendAppend(env, from)
		return
	}
	if m.Match > s.match[from] {
		s.match[from] = m.Match
	}
	s.next[from] = s.match[from] + 1
	s.advanceCommit(env)
	if s.next[from] <= s.lastIndex() {
		s.sendAppend(env, from)
	}
}

func (s *Server) advanceCommit(env *sim.Env) {
	for n := s.lastIndex(); n > s.commit; n-- {
		// Raft §5.4.2: only entries of the current term are committed by
		// counting replicas.
		if s.log[n].Term != s.term {
			break
		}
		count := 0
		for _, p := range s.peers {
			if s.match[p] >= n {
				count++
			}
		}
		if count >= s.majority() {
			s.commit = n
			s.apply(env)
			return
		}
	}
}

func (s *Server) apply(env *sim.Env) {
	for s.applied < s.commit {
		s.applied++
		e := s.log[s.applied]
		reply := s.execute(e.Cmd)
		env.Observe("apply", ApplyObs{Index: s.applied, Term: e.Term, Kind: e.Cmd.Kind, Client: e.Cmd.Client, Seq: e.Cmd.Seq})
		if w, ok := s.waiting[s.applied]; ok {
			delete(s.waiting, s.applied)
			if s.role == leader && w.client == e.Cmd.Client && w.seq == e.Cmd.Seq {
				env.Send(w.client, reply)
			}
		}
	}
}

// execute runs a command against the state machine, deduplicating retries.
func (s *Server) execute(c Cmd) ClientReply {
	if c.Kind == "noop" {
		return ClientReply{}
	}
	if !s.bugs[BugNoDedup] {
		if sess, ok := s.sessions[c.Client]; ok && c.Seq <= sess.seq {
			return sess.reply
		}
	}
	r := ClientReply{Seq: c.Seq, Leader: s.id}
	switch c.Kind {
	case "get":
		r.Value, r.Ok = s.kv[c.Key], true
	case "put":
		s.kv[c.Key] = c.Value
		r.Ok = true
	case "cas":
		if s.kv[c.Key] == c.Expected {
			s.kv[c.Key] = c.Value
			r.Ok = true
		}
	}
	s.sessions[c.Client] = session{seq: c.Seq, reply: r}
	return r
}

func (s *Server) onClient(env *sim.Env, from sim.NodeID, c Cmd) {
	if s.role != leader {
		env.Send(from, ClientReply{Seq: c.Seq, Err: "not-leader", Leader: s.leader})
		return
	}
	if !s.bugs[BugNoDedup] {
		if sess, ok := s.sessions[c.Client]; ok && c.Seq <= sess.seq {
			if c.Seq == sess.seq {
				env.Send(from, sess.reply)
			}
			return
		}
	}
	if c.Kind == "get" && s.bugs[BugStaleRead] {
		// Reads served from local state without confirming leadership.
		env.Send(from, ClientReply{Seq: c.Seq, Ok: true, Value: s.kv[c.Key], Leader: s.id})
		return
	}
	s.log = append(s.log, Entry{Term: s.term, Cmd: c})
	s.persistLog(env, s.lastIndex(), true)
	s.match[s.id] = s.lastIndex()
	s.waiting[s.lastIndex()] = waiter{client: c.Client, seq: c.Seq}
	s.broadcastAppend(env)
}

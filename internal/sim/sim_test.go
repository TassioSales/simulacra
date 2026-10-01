package sim

import "testing"

// pinger sends a numbered ping to every other server every 10ms and records
// what it received; it persists a counter without syncing on odd ticks.
type pinger struct {
	peers []NodeID
	got   map[NodeID]int
	tick  int
}

type ping struct{ N int }
type tickTag struct{}

func (p *pinger) Start(env *Env) {
	p.got = map[NodeID]int{}
	env.SetTimer(10*Millisecond, tickTag{})
}

func (p *pinger) Timer(env *Env, _ any) {
	p.tick++
	for _, q := range p.peers {
		if q != env.ID() {
			env.Send(q, ping{N: p.tick})
		}
	}
	env.Storage().Put("tick", []byte{byte(p.tick)})
	if p.tick%2 == 0 {
		env.Storage().Sync()
	}
	env.SetTimer(10*Millisecond, tickTag{})
}

func (p *pinger) Receive(env *Env, from NodeID, msg any) {
	p.got[from]++
	env.Observe("recv", from)
}

type idle struct{}

func (idle) Start(*Env)                {}
func (idle) Receive(*Env, NodeID, any) {}
func (idle) Timer(*Env, any)           {}

func run(t *testing.T, faults []Fault, net NetworkConfig) *Result {
	t.Helper()
	return Run(Config{
		Seed: 7, Servers: 3, Clients: 1, Duration: Second,
		Network:   net,
		Faults:    faults,
		Record:    true,
		NewServer: func(id NodeID, s []NodeID) Node { return &pinger{peers: s} },
		NewClient: func(NodeID, []NodeID) Node { return idle{} },
	})
}

func received(r *Result, from, to NodeID, lo, hi Time) int {
	n := 0
	for _, o := range r.Observations {
		if o.Kind == "recv" && o.Node == to && o.Data.(NodeID) == from && o.T >= lo && o.T < hi {
			n++
		}
	}
	return n
}

func TestPartitionBlocksAndHeals(t *testing.T) {
	net := NetworkConfig{MinLatency: Millisecond, MaxLatency: Millisecond}
	r := run(t, []Fault{{ID: 0, Kind: "partition", At: 200 * Millisecond, Until: 600 * Millisecond, Groups: [][]NodeID{{0}, {1, 2}}, Bridge: None, Node: None}}, net)
	if n := received(r, 0, 1, 250*Millisecond, 550*Millisecond); n != 0 {
		t.Fatalf("S1 heard S0 %d times during partition", n)
	}
	if n := received(r, 2, 1, 250*Millisecond, 550*Millisecond); n == 0 {
		t.Fatal("S1 and S2 should still talk")
	}
	if n := received(r, 0, 1, 650*Millisecond, Second); n == 0 {
		t.Fatal("partition did not heal")
	}
}

func TestBridgeKeepsMiddleConnected(t *testing.T) {
	net := NetworkConfig{MinLatency: Millisecond, MaxLatency: Millisecond}
	r := run(t, []Fault{{ID: 0, Kind: "partition", At: 0, Until: Second, Groups: [][]NodeID{{0}, {1}}, Bridge: 2, Node: None}}, net)
	if received(r, 0, 1, 0, Second) != 0 || received(r, 1, 0, 0, Second) != 0 {
		t.Fatal("sides of a bridge must not talk")
	}
	if received(r, 0, 2, 0, Second) == 0 || received(r, 2, 1, 0, Second) == 0 {
		t.Fatal("bridge node must reach both sides")
	}
}

func TestCrashLosesUnsyncedWrites(t *testing.T) {
	s := newStorage()
	s.Put("a", []byte("1"))
	s.Sync()
	s.Put("a", []byte("2"))
	s.Put("b", []byte("x"))
	if v, _ := s.Get("a"); string(v) != "2" {
		t.Fatal("reads must see pending writes")
	}
	s.crash()
	if v, _ := s.Get("a"); string(v) != "1" {
		t.Fatalf("after crash a=%s, want the synced 1", v)
	}
	if _, ok := s.Get("b"); ok {
		t.Fatal("unsynced key survived the crash")
	}
}

func TestCrashedNodeMissesMessagesAndRestarts(t *testing.T) {
	net := NetworkConfig{MinLatency: Millisecond, MaxLatency: Millisecond}
	r := run(t, []Fault{{ID: 0, Kind: "crash", Node: 1, At: 300 * Millisecond, Until: 500 * Millisecond, Bridge: None}}, net)
	if n := received(r, 0, 1, 310*Millisecond, 500*Millisecond); n != 0 {
		t.Fatalf("crashed node received %d messages", n)
	}
	if n := received(r, 0, 1, 520*Millisecond, Second); n == 0 {
		t.Fatal("node did not come back")
	}
	if r.Dropped == 0 {
		t.Fatal("messages to a crashed node must count as dropped")
	}
}

func TestSameSeedSameTrace(t *testing.T) {
	net := NetworkConfig{DropRate: 0.2, DupRate: 0.2, MinLatency: Millisecond, MaxLatency: 20 * Millisecond, SlowRate: 0.1}
	a, b := run(t, nil, net), run(t, nil, net)
	if a.Hash != b.Hash || len(a.Trace) != len(b.Trace) {
		t.Fatal("same seed, different run")
	}
}

func TestPlanFaultsRespectsMinority(t *testing.T) {
	for seed := uint64(1); seed < 200; seed++ {
		faults := PlanFaults(seed, FaultPlan{CrashRate: 5, PartitionRate: 5}, 5, 10*Second)
		for _, f := range faults {
			if f.Until > 10*Second*85/100 {
				t.Fatalf("seed %d: fault %d heals after the quiet period", seed, f.ID)
			}
			if f.Kind != "crash" {
				continue
			}
			down := 0
			for _, g := range faults {
				if g.Kind == "crash" && g.At <= f.At && f.At < g.Until {
					down++
				}
			}
			if down > 2 {
				t.Fatalf("seed %d: %d servers down at once out of 5", seed, down)
			}
		}
	}
}

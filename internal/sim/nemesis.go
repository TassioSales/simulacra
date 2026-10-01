package sim

import (
	"cmp"
	"math"
	"math/rand/v2"
	"slices"
)

// FaultPlan controls how aggressive the nemesis is.
type FaultPlan struct {
	CrashRate     float64 // crashes per virtual second
	PartitionRate float64 // partitions per virtual second
}

// PlanFaults derives the fault schedule from the seed. Faults only start in
// the first 70% of the run and all heal by 85%, leaving a quiet tail where
// the system should converge. At most a minority of servers is down at once.
func PlanFaults(seed uint64, plan FaultPlan, servers int, duration Time) []Fault {
	rng := rand.New(rand.NewPCG(seed, 0x6e656d65736973))
	startBy := duration * 70 / 100
	healBy := duration * 85 / 100
	var faults []Fault
	next := func(rate float64, t Time) Time {
		if rate <= 0 {
			return math.MaxInt64
		}
		return t + Time(rng.ExpFloat64()/rate*float64(Second))
	}
	uniform := func(lo, hi Time) Time { return lo + Time(rng.Int64N(int64(hi-lo)+1)) }

	// Crashes: keep a minority down at any moment.
	maxDown := (servers - 1) / 2
	for t := next(plan.CrashRate, 0); t < startBy; t = next(plan.CrashRate, t) {
		node := NodeID(rng.IntN(servers))
		// Half the crashes are quick supervisor restarts, half are long outages.
		downFor := uniform(10*Millisecond, 150*Millisecond)
		if rng.IntN(2) == 0 {
			downFor = uniform(150*Millisecond, 2*Second)
		}
		until := min(t+downFor, healBy)
		down, sameNode := 0, false
		for _, f := range faults {
			if f.Kind == "crash" && f.At < until && t < f.Until {
				down++
				sameNode = sameNode || f.Node == node
			}
		}
		if down >= maxDown || sameNode {
			continue
		}
		faults = append(faults, Fault{Kind: "crash", At: t, Until: until, Node: node, Bridge: None})
	}

	// Partitions: split the servers in two, isolate one of them, or build a
	// bridge — two sides that can't talk but both reach a node in the middle.
	for t := next(plan.PartitionRate, 0); t < startBy; t = next(plan.PartitionRate, t) {
		ids := make([]NodeID, servers)
		for i := range ids {
			ids[i] = NodeID(i)
		}
		rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		f := Fault{Kind: "partition", At: t, Node: None, Bridge: None}
		var a, b []NodeID
		switch kind := rng.IntN(3); {
		case kind == 2 && servers >= 3:
			f.Bridge = ids[0]
			k := 1 + rng.IntN(servers-2)
			a, b = ids[1:1+k], ids[1+k:]
		case kind == 1 && servers > 2:
			k := 1 + rng.IntN(servers-1)
			a, b = ids[:k], ids[k:]
		default:
			a, b = ids[:1], ids[1:]
		}
		a, b = slices.Clone(a), slices.Clone(b)
		slices.Sort(a)
		slices.Sort(b)
		f.Groups = [][]NodeID{a, b}
		f.Until = min(t+uniform(200*Millisecond, 3*Second), healBy)
		faults = append(faults, f)
	}

	slices.SortStableFunc(faults, func(a, b Fault) int { return cmp.Compare(a.At, b.At) })
	for i := range faults {
		faults[i].ID = i
	}
	return faults
}

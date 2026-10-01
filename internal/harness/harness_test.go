package harness

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func cfg(t *testing.T, bugs ...string) RunConfig {
	t.Helper()
	c := DefaultConfig()
	c.Bugs = bugs
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDeterminism(t *testing.T) {
	c := cfg(t)
	for seed := uint64(1); seed <= 5; seed++ {
		a, _ := Run(c, seed, Options{})
		b, res := Run(c, seed, Options{Record: true})
		if a.Hash != b.Hash {
			t.Fatalf("seed %d: hash differs between runs (%s vs %s)", seed, a.Hash, b.Hash)
		}
		if len(res.Trace) == 0 {
			t.Fatalf("seed %d: recording produced no trace", seed)
		}
	}
	x, _ := Run(c, 1, Options{})
	y, _ := Run(c, 2, Options{})
	if x.Hash == y.Hash {
		t.Fatal("different seeds produced the same run")
	}
}

func TestCorrectRaftSurvivesFaults(t *testing.T) {
	c := cfg(t)
	seeds := 150
	if testing.Short() {
		seeds = 30
	}
	ops := 0
	for seed := uint64(1); seed <= uint64(seeds); seed++ {
		o, _ := Run(c, seed, Options{})
		if o.Failed() {
			t.Fatalf("seed %d: correct implementation violated %s: %s", seed, o.Violations[0].Kind, o.Violations[0].Message)
		}
		if o.Aborted {
			t.Fatalf("seed %d: run aborted", seed)
		}
		ops += o.Ops
	}
	if ops/seeds < 50 {
		t.Fatalf("too little client traffic: %d ops per seed", ops/seeds)
	}
}

// findFailure searches seeds in parallel and returns the lowest failing one.
func findFailure(c RunConfig, maxSeeds int) (uint64, Outcome, bool) {
	var (
		mu    sync.Mutex
		best  uint64
		found Outcome
		next  atomic.Uint64
		wg    sync.WaitGroup
	)
	next.Store(1)
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				seed := next.Add(1) - 1
				mu.Lock()
				stop := seed > uint64(maxSeeds) || (best != 0 && seed > best)
				mu.Unlock()
				if stop {
					return
				}
				if o, _ := Run(c, seed, Options{}); o.Failed() {
					mu.Lock()
					if best == 0 || seed < best {
						best, found = seed, o
					}
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	return best, found, best != 0
}

func TestEveryBugIsFound(t *testing.T) {
	for _, info := range Systems["raftkv"].Bugs {
		t.Run(string(info.ID), func(t *testing.T) {
			c := cfg(t, string(info.ID))
			limit := 500
			if info.Hint != "" { // rare bug: hostile environment, many seeds
				if testing.Short() {
					t.Skip("bug raro; rode sem -short")
				}
				c.CrashRate, c.PartitionRate, limit = 1.5, 1.5, 30000
			}
			seed, o, ok := findFailure(c, limit)
			if !ok {
				t.Fatalf("bug not found in %d seeds", limit)
			}
			t.Logf("found at seed %d: %s", seed, o.Violations[0].Message)
		})
	}
}

func TestShrinkKeepsFailure(t *testing.T) {
	c := cfg(t, "stale-read")
	for seed := uint64(1); seed <= 400; seed++ {
		if o, _ := Run(c, seed, Options{}); !o.Failed() {
			continue
		}
		s := Shrink(c, seed)
		if !s.Reproduces || s.After > s.Before {
			t.Fatalf("bad shrink %+v", s)
		}
		disabled := map[int]bool{}
		for _, id := range s.Disabled {
			disabled[id] = true
		}
		o, _ := Run(c, seed, Options{Disabled: disabled})
		if !o.Failed() {
			t.Fatal("shrunk run no longer fails")
		}
		t.Logf("seed %d: %d → %d faults in %d runs", seed, s.Before, s.After, s.Runs)
		return
	}
	t.Fatal("no failing seed to shrink")
}

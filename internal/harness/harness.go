// Package harness wires systems under test to the simulator: it turns a
// user-facing RunConfig into a sim.Config, runs it, checks the result and
// can shrink a failing run to the smallest set of faults that still fails.
package harness

import (
	"errors"
	"fmt"
	"time"

	"simulacra/internal/sim"
	"simulacra/internal/systems/raftkv"
)

// RunConfig is the user-facing description of a scenario.
type RunConfig struct {
	System        string   `json:"system"`
	Bugs          []string `json:"bugs"`
	Servers       int      `json:"servers"`
	Clients       int      `json:"clients"`
	DurationMs    int      `json:"durationMs"`
	DropRate      float64  `json:"dropRate"`
	DupRate       float64  `json:"dupRate"`
	MinLatencyMs  float64  `json:"minLatencyMs"`
	MaxLatencyMs  float64  `json:"maxLatencyMs"`
	SlowRate      float64  `json:"slowRate"`
	CrashRate     float64  `json:"crashRate"`
	PartitionRate float64  `json:"partitionRate"`
}

// DefaultConfig is a moderately hostile environment.
func DefaultConfig() RunConfig {
	return RunConfig{
		System: "raftkv", Bugs: []string{}, Servers: 3, Clients: 3, DurationMs: 8000,
		DropRate: 0.02, DupRate: 0.01, MinLatencyMs: 1, MaxLatencyMs: 12, SlowRate: 0.01,
		CrashRate: 0.6, PartitionRate: 0.5,
	}
}

// Normalize fills defaults and validates ranges.
func (c *RunConfig) Normalize() error {
	d := DefaultConfig()
	if c.System == "" {
		c.System = d.System
	}
	if c.Bugs == nil {
		c.Bugs = []string{}
	}
	if c.Servers == 0 {
		c.Servers = d.Servers
	}
	if c.Clients == 0 {
		c.Clients = d.Clients
	}
	if c.DurationMs == 0 {
		c.DurationMs = d.DurationMs
	}
	if c.MaxLatencyMs == 0 {
		c.MinLatencyMs, c.MaxLatencyMs = d.MinLatencyMs, d.MaxLatencyMs
	}
	var errs []error
	if _, ok := Systems[c.System]; !ok {
		errs = append(errs, fmt.Errorf("sistema desconhecido %q", c.System))
	}
	if c.Servers < 1 || c.Servers > 7 {
		errs = append(errs, errors.New("servers deve estar entre 1 e 7"))
	}
	if c.Clients < 1 || c.Clients > 10 {
		errs = append(errs, errors.New("clients deve estar entre 1 e 10"))
	}
	if c.DurationMs < 500 || c.DurationMs > 60000 {
		errs = append(errs, errors.New("durationMs deve estar entre 500 e 60000"))
	}
	for name, p := range map[string]float64{"dropRate": c.DropRate, "dupRate": c.DupRate, "slowRate": c.SlowRate} {
		if p < 0 || p > 0.5 {
			errs = append(errs, fmt.Errorf("%s deve estar entre 0 e 0.5", name))
		}
	}
	if c.MinLatencyMs < 0 || c.MaxLatencyMs < c.MinLatencyMs || c.MaxLatencyMs > 1000 {
		errs = append(errs, errors.New("latência inválida"))
	}
	if c.CrashRate < 0 || c.CrashRate > 10 || c.PartitionRate < 0 || c.PartitionRate > 10 {
		errs = append(errs, errors.New("taxas de falha devem estar entre 0 e 10 por segundo"))
	}
	if sys, ok := Systems[c.System]; ok {
		if err := sys.ValidateBugs(c.Bugs); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// System is a system under test.
type System struct {
	Name         string                                                                       `json:"name"`
	Title        string                                                                       `json:"title"`
	Description  string                                                                       `json:"description"`
	Bugs         []raftkv.BugInfo                                                             `json:"bugs"`
	ValidateBugs func([]string) error                                                         `json:"-"`
	Build        func(bugs []string) (server, client func(sim.NodeID, []sim.NodeID) sim.Node) `json:"-"`
	Check        func(*sim.Result) []sim.Violation                                            `json:"-"`
}

// Systems is the registry of systems under test.
var Systems = map[string]*System{
	"raftkv": {
		Name:  "raftkv",
		Title: "Raft KV",
		Description: "Armazenamento chave-valor replicado com Raft: eleição de líder, replicação de log, " +
			"persistência com fsync e sessões de cliente para exatamente-uma-vez.",
		Bugs: raftkv.Catalog,
		ValidateBugs: func(ids []string) error {
			_, err := raftkv.ParseBugs(ids)
			return err
		},
		Build: func(ids []string) (func(sim.NodeID, []sim.NodeID) sim.Node, func(sim.NodeID, []sim.NodeID) sim.Node) {
			bugs, _ := raftkv.ParseBugs(ids)
			return func(id sim.NodeID, peers []sim.NodeID) sim.Node { return raftkv.NewServer(id, peers, bugs) },
				func(id sim.NodeID, peers []sim.NodeID) sim.Node { return raftkv.NewClient(id, peers) }
		},
		Check: raftkv.Check,
	},
}

// Outcome summarizes one seed.
type Outcome struct {
	Seed       uint64          `json:"seed"`
	Hash       string          `json:"hash"`
	Violations []sim.Violation `json:"violations"`
	Ops        int             `json:"ops"`
	Messages   int             `json:"messages"`
	Dropped    int             `json:"dropped"`
	Steps      int             `json:"steps"`
	Faults     int             `json:"faults"`
	Aborted    bool            `json:"aborted"`
	ElapsedMs  float64         `json:"elapsedMs"`
}

// Failed reports whether any property was violated.
func (o Outcome) Failed() bool { return len(o.Violations) > 0 }

// Options tweak a single run.
type Options struct {
	Record   bool
	Disabled map[int]bool
}

// Run simulates one seed. cfg must be normalized.
func Run(cfg RunConfig, seed uint64, opt Options) (Outcome, *sim.Result) {
	sys := Systems[cfg.System]
	server, client := sys.Build(cfg.Bugs)
	duration := sim.Time(cfg.DurationMs) * sim.Millisecond
	ms := func(v float64) sim.Time { return sim.Time(v * float64(sim.Millisecond)) }
	start := time.Now()
	res := sim.Run(sim.Config{
		Seed: seed, Servers: cfg.Servers, Clients: cfg.Clients, Duration: duration,
		Network: sim.NetworkConfig{
			DropRate: cfg.DropRate, DupRate: cfg.DupRate, SlowRate: cfg.SlowRate,
			MinLatency: ms(cfg.MinLatencyMs), MaxLatency: ms(cfg.MaxLatencyMs),
		},
		Faults:    sim.PlanFaults(seed, sim.FaultPlan{CrashRate: cfg.CrashRate, PartitionRate: cfg.PartitionRate}, cfg.Servers, duration),
		Disabled:  opt.Disabled,
		Record:    opt.Record,
		NewServer: server,
		NewClient: client,
	})
	v := sys.Check(res)
	if v == nil {
		v = []sim.Violation{}
	}
	return Outcome{
		Seed: seed, Hash: res.Hash, Violations: v, Ops: len(res.History),
		Messages: res.Messages, Dropped: res.Dropped, Steps: res.Steps, Faults: len(res.Faults),
		Aborted: res.Aborted, ElapsedMs: float64(time.Since(start).Microseconds()) / 1000,
	}, res
}

// ShrinkResult is the minimal fault set that still reproduces a failure.
type ShrinkResult struct {
	Seed       uint64 `json:"seed"`
	Before     int    `json:"before"`
	After      int    `json:"after"`
	Disabled   []int  `json:"disabled"`
	Kept       []int  `json:"kept"`
	Runs       int    `json:"runs"`
	Violation  string `json:"violation"`
	Reproduces bool   `json:"reproduces"`
}

// Shrink removes injected faults one at a time (greedy delta debugging) and
// keeps a removal whenever the same kind of violation still happens. Because
// the network and each node draw from their own random streams, disabling a
// fault perturbs the rest of the run as little as possible.
func Shrink(cfg RunConfig, seed uint64) ShrinkResult {
	base, res := Run(cfg, seed, Options{})
	out := ShrinkResult{Seed: seed, Before: len(res.Faults), Runs: 1}
	if !base.Failed() {
		return out
	}
	kind := base.Violations[0].Kind
	out.Violation = kind
	out.Reproduces = true
	disabled := map[int]bool{}
	fails := func() bool {
		o, _ := Run(cfg, seed, Options{Disabled: disabled})
		out.Runs++
		for _, v := range o.Violations {
			if v.Kind == kind {
				return true
			}
		}
		return false
	}
	// Two passes: removing a late fault can make an earlier one redundant.
	for range 2 {
		changed := false
		for _, f := range res.Faults {
			if disabled[f.ID] {
				continue
			}
			disabled[f.ID] = true
			if fails() {
				changed = true
			} else {
				delete(disabled, f.ID)
			}
		}
		if !changed {
			break
		}
	}
	out.Disabled = []int{}
	out.Kept = []int{}
	for _, f := range res.Faults {
		if disabled[f.ID] {
			out.Disabled = append(out.Disabled, f.ID)
		} else {
			out.Kept = append(out.Kept, f.ID)
		}
	}
	out.After = len(out.Kept)
	return out
}

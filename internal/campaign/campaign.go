// Package campaign runs batches of seeds in parallel and keeps their results.
//
// Only a compact summary of each seed is stored: since runs are
// deterministic, the full trace of any seed is recomputed on demand.
package campaign

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"simulacra/internal/harness"
)

const MaxSeeds = 100_000

// SeedResult is the compact outcome of one seed.
type SeedResult struct {
	Seed      uint64   `json:"seed"`
	Hash      string   `json:"hash"`
	Failed    bool     `json:"failed"`
	Kinds     []string `json:"kinds,omitempty"`
	Message   string   `json:"message,omitempty"`
	Ops       int      `json:"ops"`
	Messages  int      `json:"messages"`
	Faults    int      `json:"faults"`
	ElapsedMs float64  `json:"elapsedMs"`
}

// Summary is a campaign without its per-seed results.
type Summary struct {
	ID         string            `json:"id"`
	CreatedAt  time.Time         `json:"createdAt"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	Config     harness.RunConfig `json:"config"`
	StartSeed  uint64            `json:"startSeed"`
	Seeds      int               `json:"seeds"`
	Status     string            `json:"status"` // running, done, cancelled, interrupted
	Done       int               `json:"done"`
	Failed     int               `json:"failed"`
	ElapsedMs  float64           `json:"elapsedMs"`
	Kinds      map[string]int    `json:"kinds"`
	FirstFail  *uint64           `json:"firstFail,omitempty"`
}

// Campaign is a summary plus results in completion order.
type Campaign struct {
	Summary
	Results []SeedResult `json:"results"`
}

type entry struct {
	mu      sync.RWMutex
	c       Campaign
	started time.Time
	cancel  atomic.Bool
}

// Manager owns all campaigns and persists finished ones to disk.
type Manager struct {
	dir     string
	mu      sync.RWMutex
	entries map[string]*entry
	workers int
}

// NewManager loads campaigns saved in dir.
func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	m := &Manager{dir: dir, entries: map[string]*entry{}, workers: max(1, runtime.NumCPU()-1)}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var c Campaign
		if json.Unmarshal(raw, &c) != nil || c.ID == "" {
			continue
		}
		if c.Status == "running" {
			c.Status = "interrupted"
		}
		m.entries[c.ID] = &entry{c: c}
	}
	return m, nil
}

// Start validates cfg and launches a campaign in the background.
func (m *Manager) Start(cfg harness.RunConfig, seeds int, startSeed uint64) (Summary, error) {
	if err := cfg.Normalize(); err != nil {
		return Summary{}, err
	}
	if seeds < 1 || seeds > MaxSeeds {
		return Summary{}, fmt.Errorf("seeds deve estar entre 1 e %d", MaxSeeds)
	}
	if startSeed == 0 {
		startSeed = randomSeed()
	}
	e := &entry{started: time.Now()}
	e.c = Campaign{
		Summary: Summary{
			ID: newID(), CreatedAt: e.started, Config: cfg, StartSeed: startSeed, Seeds: seeds,
			Status: "running", Kinds: map[string]int{},
		},
		Results: make([]SeedResult, 0, seeds),
	}
	m.mu.Lock()
	m.entries[e.c.ID] = e
	m.mu.Unlock()
	go m.run(e)
	return e.snapshot(), nil
}

func (m *Manager) run(e *entry) {
	cfg, start, total := e.c.Config, e.c.StartSeed, e.c.Seeds
	var next atomic.Int64
	var wg sync.WaitGroup
	for range m.workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !e.cancel.Load() {
				i := next.Add(1) - 1
				if i >= int64(total) {
					return
				}
				seed := start + uint64(i)
				o, _ := harness.Run(cfg, seed, harness.Options{})
				r := SeedResult{Seed: seed, Hash: o.Hash, Failed: o.Failed(), Ops: o.Ops, Messages: o.Messages, Faults: o.Faults, ElapsedMs: o.ElapsedMs}
				for _, v := range o.Violations {
					if !slices.Contains(r.Kinds, v.Kind) {
						r.Kinds = append(r.Kinds, v.Kind)
					}
				}
				if r.Failed {
					r.Message = o.Violations[0].Message
				}
				e.add(r)
			}
		}()
	}
	wg.Wait()
	e.mu.Lock()
	now := time.Now()
	e.c.FinishedAt = &now
	e.c.ElapsedMs = float64(now.Sub(e.started).Microseconds()) / 1000
	if e.cancel.Load() && e.c.Done < total {
		e.c.Status = "cancelled"
	} else {
		e.c.Status = "done"
	}
	e.mu.Unlock()
	if err := m.save(e); err != nil {
		fmt.Fprintln(os.Stderr, "campaign: falha ao salvar:", err)
	}
}

func (e *entry) add(r SeedResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.c.Results = append(e.c.Results, r)
	e.c.Done++
	e.c.ElapsedMs = float64(time.Since(e.started).Microseconds()) / 1000
	if r.Failed {
		e.c.Failed++
		for _, k := range r.Kinds {
			e.c.Kinds[k]++
		}
		if e.c.FirstFail == nil || r.Seed < *e.c.FirstFail {
			s := r.Seed
			e.c.FirstFail = &s
		}
	}
}

func (e *entry) snapshot() Summary {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s := e.c.Summary
	s.Kinds = maps.Clone(e.c.Kinds)
	return s
}

func (m *Manager) save(e *entry) error {
	e.mu.RLock()
	raw, err := json.Marshal(e.c)
	id := e.c.ID
	e.mu.RUnlock()
	if err != nil {
		return err
	}
	tmp := filepath.Join(m.dir, id+".json.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(m.dir, id+".json"))
}

func (m *Manager) get(id string) (*entry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[id]
	return e, ok
}

// List returns every campaign, newest first.
func (m *Manager) List() []Summary {
	m.mu.RLock()
	out := make([]Summary, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, e.snapshot())
	}
	m.mu.RUnlock()
	slices.SortFunc(out, func(a, b Summary) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out
}

// Get returns a campaign with all its results.
func (m *Manager) Get(id string) (Campaign, bool) {
	e, ok := m.get(id)
	if !ok {
		return Campaign{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	c := e.c
	c.Kinds = maps.Clone(e.c.Kinds)
	c.Results = slices.Clone(e.c.Results)
	return c, true
}

// Progress returns the summary and the results completed after cursor.
func (m *Manager) Progress(id string, cursor int) (Summary, []SeedResult, bool) {
	e, ok := m.get(id)
	if !ok {
		return Summary{}, nil, false
	}
	s := e.snapshot()
	e.mu.RLock()
	defer e.mu.RUnlock()
	if cursor > len(e.c.Results) {
		cursor = len(e.c.Results)
	}
	return s, slices.Clone(e.c.Results[cursor:]), true
}

// Config returns the scenario of a campaign.
func (m *Manager) Config(id string) (harness.RunConfig, bool) {
	e, ok := m.get(id)
	if !ok {
		return harness.RunConfig{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.c.Config, true
}

// Seed returns the stored result of one seed, if the campaign ran it.
func (m *Manager) Seed(id string, seed uint64) (SeedResult, bool) {
	e, ok := m.get(id)
	if !ok {
		return SeedResult{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, r := range e.c.Results {
		if r.Seed == seed {
			return r, true
		}
	}
	return SeedResult{}, false
}

// Cancel stops a running campaign.
func (m *Manager) Cancel(id string) bool {
	e, ok := m.get(id)
	if ok {
		e.cancel.Store(true)
	}
	return ok
}

// Delete cancels and forgets a campaign.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	e, ok := m.entries[id]
	delete(m.entries, id)
	m.mu.Unlock()
	if !ok {
		return errors.New("campanha não encontrada")
	}
	e.cancel.Store(true)
	err := os.Remove(filepath.Join(m.dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ValidID guards file names built from user input.
func ValidID(id string) bool {
	return len(id) > 2 && len(id) < 40 && strings.HasPrefix(id, "c-") &&
		strings.Trim(id[2:], "0123456789abcdef") == ""
}

func newID() string {
	var b [6]byte
	rand.Read(b[:])
	return "c-" + hex.EncodeToString(b[:])
}

func randomSeed() uint64 {
	var b [4]byte
	rand.Read(b[:])
	return uint64(binary.LittleEndian.Uint32(b[:])%900_000_000) + 1
}

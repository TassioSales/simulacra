// Command simulacra runs deterministic simulation campaigns against
// distributed systems and serves the web UI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"simulacra/internal/campaign"
	"simulacra/internal/harness"
	"simulacra/internal/server"
)

const usage = `simulacra — testes por simulação determinística

uso:
  simulacra serve   [-addr 127.0.0.1:7777] [-data ./data]
  simulacra run     [flags de cenário] [-seeds 500] [-start 1] [-workers N]
  simulacra replay  [flags de cenário] -seed N [-out trace.json]
  simulacra shrink  [flags de cenário] -seed N
  simulacra systems

flags de cenário:
  -system raftkv -bugs stale-read,no-dedup -servers 3 -clients 3 -duration 8000
  -drop 0.02 -dup 0.01 -crash 0.6 -partition 0.5
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "run":
		err = runCmd(os.Args[2:])
	case "replay":
		err = replayCmd(os.Args[2:])
	case "shrink":
		err = shrinkCmd(os.Args[2:])
	case "systems":
		err = json.NewEncoder(os.Stdout).Encode(harness.Systems)
	default:
		fmt.Print(usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func scenarioFlags(fs *flag.FlagSet) func() (harness.RunConfig, error) {
	d := harness.DefaultConfig()
	system := fs.String("system", d.System, "sistema sob teste")
	bugs := fs.String("bugs", "", "bugs a ativar, separados por vírgula")
	servers := fs.Int("servers", d.Servers, "número de servidores")
	clients := fs.Int("clients", d.Clients, "número de clientes")
	duration := fs.Int("duration", d.DurationMs, "duração virtual em ms")
	drop := fs.Float64("drop", d.DropRate, "probabilidade de perda de mensagem")
	dup := fs.Float64("dup", d.DupRate, "probabilidade de duplicação")
	crash := fs.Float64("crash", d.CrashRate, "crashes por segundo virtual")
	partition := fs.Float64("partition", d.PartitionRate, "partições por segundo virtual")
	return func() (harness.RunConfig, error) {
		c := d
		c.System, c.Servers, c.Clients, c.DurationMs = *system, *servers, *clients, *duration
		c.DropRate, c.DupRate, c.CrashRate, c.PartitionRate = *drop, *dup, *crash, *partition
		c.Bugs = []string{}
		for _, b := range strings.Split(*bugs, ",") {
			if b = strings.TrimSpace(b); b != "" {
				c.Bugs = append(c.Bugs, b)
			}
		}
		return c, c.Normalize()
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7777", "endereço HTTP")
	data := fs.String("data", "data", "diretório das campanhas")
	fs.Parse(args)
	mgr, err := campaign.NewManager(*data)
	if err != nil {
		return err
	}
	return server.ListenAndServe(*addr, mgr)
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	scenario := scenarioFlags(fs)
	seeds := fs.Int("seeds", 500, "quantidade de seeds")
	start := fs.Uint64("start", 1, "primeira seed")
	workers := fs.Int("workers", runtime.NumCPU(), "execuções em paralelo")
	fs.Parse(args)
	cfg, err := scenario()
	if err != nil {
		return err
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	var cancelled atomic.Bool
	go func() { <-stop; cancelled.Store(true) }()

	fmt.Printf("%s  bugs=%v  %d seeds a partir de %d  (%d workers)\n", cfg.System, cfg.Bugs, *seeds, *start, *workers)
	began := time.Now()
	var next atomic.Uint64
	next.Store(*start)
	var mu sync.Mutex
	var done, failed int
	firsts := map[string]uint64{}
	var wg sync.WaitGroup
	for range *workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !cancelled.Load() {
				seed := next.Add(1) - 1
				if seed >= *start+uint64(*seeds) {
					return
				}
				o, _ := harness.Run(cfg, seed, harness.Options{})
				mu.Lock()
				done++
				if o.Failed() {
					failed++
					for _, v := range o.Violations {
						if f, ok := firsts[v.Kind]; !ok || seed < f {
							firsts[v.Kind] = seed
						}
					}
					fmt.Printf("  seed %-8d FALHOU  %s\n", seed, o.Violations[0].Message)
				}
				if done%100 == 0 {
					fmt.Printf("  … %d/%d\n", done, *seeds)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	el := time.Since(began)
	fmt.Printf("\n%d seeds em %s (%.0f seeds/s) — %d falharam\n", done, el.Round(time.Millisecond), float64(done)/el.Seconds(), failed)
	for k, s := range firsts {
		fmt.Printf("  %-22s primeira seed: %d\n", k, s)
	}
	if failed > 0 {
		os.Exit(3)
	}
	return nil
}

func replayCmd(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	scenario := scenarioFlags(fs)
	seed := fs.Uint64("seed", 1, "seed a reproduzir")
	out := fs.String("out", "", "arquivo para gravar o trace em JSON")
	fs.Parse(args)
	cfg, err := scenario()
	if err != nil {
		return err
	}
	o, res := harness.Run(cfg, *seed, harness.Options{Record: true})
	fmt.Printf("seed %d  hash %s  %d eventos  %d ops  %d mensagens (%d perdidas)  %d falhas injetadas\n",
		o.Seed, o.Hash, len(res.Trace), o.Ops, o.Messages, o.Dropped, o.Faults)
	for _, f := range res.Faults {
		fmt.Println("  falha:", f)
	}
	for _, v := range o.Violations {
		fmt.Printf("  VIOLAÇÃO %s: %s\n", v.Kind, v.Message)
	}
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		return json.NewEncoder(f).Encode(map[string]any{"outcome": o, "result": res})
	}
	return nil
}

func shrinkCmd(args []string) error {
	fs := flag.NewFlagSet("shrink", flag.ExitOnError)
	scenario := scenarioFlags(fs)
	seed := fs.Uint64("seed", 1, "seed a minimizar")
	fs.Parse(args)
	cfg, err := scenario()
	if err != nil {
		return err
	}
	s := harness.Shrink(cfg, *seed)
	if !s.Reproduces {
		return fmt.Errorf("seed %d não falha nesse cenário", *seed)
	}
	fmt.Printf("seed %d (%s): %d → %d falhas em %d execuções; mantidas %v\n", s.Seed, s.Violation, s.Before, s.After, s.Runs, s.Kept)
	return nil
}

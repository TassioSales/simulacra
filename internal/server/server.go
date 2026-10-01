// Package server exposes campaigns over HTTP and serves the embedded UI.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"simulacra/internal/campaign"
	"simulacra/internal/harness"
	"simulacra/internal/sim"
	"simulacra/web"
)

type api struct {
	mgr *campaign.Manager
}

// Handler builds the HTTP routes.
func Handler(mgr *campaign.Manager) http.Handler {
	a := &api{mgr: mgr}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/systems", a.systems)
	mux.HandleFunc("GET /api/defaults", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, harness.DefaultConfig()) })
	mux.HandleFunc("GET /api/campaigns", a.list)
	mux.HandleFunc("POST /api/campaigns", a.create)
	mux.HandleFunc("GET /api/campaigns/{id}", a.get)
	mux.HandleFunc("DELETE /api/campaigns/{id}", a.remove)
	mux.HandleFunc("POST /api/campaigns/{id}/cancel", a.cancel)
	mux.HandleFunc("GET /api/campaigns/{id}/stream", a.stream)
	mux.HandleFunc("GET /api/campaigns/{id}/seeds/{seed}", a.replay)
	mux.HandleFunc("POST /api/campaigns/{id}/seeds/{seed}/shrink", a.shrink)
	mux.Handle("/", spa())
	return secure(mux)
}

// ListenAndServe runs the server until it fails.
func ListenAndServe(addr string, mgr *campaign.Manager) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           logRequests(Handler(mgr)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("simulacra ouvindo em http://%s", addr)
	return srv.ListenAndServe()
}

func (a *api) systems(w http.ResponseWriter, _ *http.Request) {
	out := make([]*harness.System, 0, len(harness.Systems))
	for _, s := range harness.Systems {
		out = append(out, s)
	}
	writeJSON(w, 200, out)
}

func (a *api) list(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, a.mgr.List())
}

type createReq struct {
	Config    harness.RunConfig `json:"config"`
	Seeds     int               `json:"seeds"`
	StartSeed uint64            `json:"startSeed"`
}

func (a *api) create(w http.ResponseWriter, r *http.Request) {
	// Start from the defaults so omitted fields keep sensible values (a zero
	// rate is meaningful, so Normalize can't tell "absent" from "0").
	req := createReq{Config: harness.DefaultConfig()}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, 400, "JSON inválido: "+err.Error())
		return
	}
	s, err := a.mgr.Start(req.Config, req.Seeds, req.StartSeed)
	if err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	writeJSON(w, 201, s)
}

func (a *api) id(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !campaign.ValidID(id) {
		writeErr(w, 404, "campanha não encontrada")
		return "", false
	}
	return id, true
}

func (a *api) get(w http.ResponseWriter, r *http.Request) {
	id, ok := a.id(w, r)
	if !ok {
		return
	}
	c, ok := a.mgr.Get(id)
	if !ok {
		writeErr(w, 404, "campanha não encontrada")
		return
	}
	writeJSON(w, 200, c)
}

func (a *api) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := a.id(w, r)
	if !ok {
		return
	}
	if err := a.mgr.Delete(id); err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	w.WriteHeader(204)
}

func (a *api) cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := a.id(w, r)
	if !ok {
		return
	}
	if !a.mgr.Cancel(id) {
		writeErr(w, 404, "campanha não encontrada")
		return
	}
	w.WriteHeader(202)
}

// stream sends progress as Server-Sent Events until the campaign ends.
func (a *api) stream(w http.ResponseWriter, r *http.Request) {
	id, ok := a.id(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming não suportado")
		return
	}
	cursor, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		s, results, ok := a.mgr.Progress(id, cursor)
		if !ok {
			fmt.Fprint(w, "event: error\ndata: {\"error\":\"campanha não encontrada\"}\n\n")
			flusher.Flush()
			return
		}
		cursor += len(results)
		raw, _ := json.Marshal(map[string]any{"summary": s, "results": results, "cursor": cursor})
		fmt.Fprintf(w, "event: progress\ndata: %s\n\n", raw)
		flusher.Flush()
		if s.Status != "running" {
			fmt.Fprint(w, "event: end\ndata: {}\n\n")
			flusher.Flush()
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}

func parseSeed(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	seed, err := strconv.ParseUint(r.PathValue("seed"), 10, 64)
	if err != nil {
		writeErr(w, 400, "seed inválida")
		return 0, false
	}
	return seed, true
}

type replayResp struct {
	Outcome      harness.Outcome   `json:"outcome"`
	Config       harness.RunConfig `json:"config"`
	Faults       []faultView       `json:"faults"`
	Trace        []sim.TraceEvent  `json:"trace"`
	History      []sim.Op          `json:"history"`
	ExpectedHash string            `json:"expectedHash,omitempty"`
	Disabled     []int             `json:"disabled"`
}

type faultView struct {
	sim.Fault
	Disabled bool   `json:"disabled"`
	Label    string `json:"label"`
}

// replay re-simulates a seed with full tracing. Nothing is stored: the seed
// is the recording.
func (a *api) replay(w http.ResponseWriter, r *http.Request) {
	id, ok := a.id(w, r)
	if !ok {
		return
	}
	seed, ok := parseSeed(w, r)
	if !ok {
		return
	}
	cfg, ok := a.mgr.Config(id)
	if !ok {
		writeErr(w, 404, "campanha não encontrada")
		return
	}
	disabled := map[int]bool{}
	var disabledList []int
	for _, part := range strings.Split(r.URL.Query().Get("disabled"), ",") {
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			writeErr(w, 400, "lista disabled inválida")
			return
		}
		if !disabled[n] {
			disabled[n] = true
			disabledList = append(disabledList, n)
		}
	}
	o, res := harness.Run(cfg, seed, harness.Options{Record: true, Disabled: disabled})
	planned := sim.PlanFaults(seed, sim.FaultPlan{CrashRate: cfg.CrashRate, PartitionRate: cfg.PartitionRate},
		cfg.Servers, sim.Time(cfg.DurationMs)*sim.Millisecond)
	faults := make([]faultView, 0, len(planned))
	for _, f := range planned {
		faults = append(faults, faultView{Fault: f, Disabled: disabled[f.ID], Label: f.String()})
	}
	resp := replayResp{Outcome: o, Config: cfg, Faults: faults, Trace: res.Trace, History: res.History, Disabled: disabledList}
	if resp.Disabled == nil {
		resp.Disabled = []int{}
	}
	if len(disabled) == 0 {
		if sr, ok := a.mgr.Seed(id, seed); ok {
			resp.ExpectedHash = sr.Hash
		}
	}
	writeJSON(w, 200, resp)
}

func (a *api) shrink(w http.ResponseWriter, r *http.Request) {
	id, ok := a.id(w, r)
	if !ok {
		return
	}
	seed, ok := parseSeed(w, r)
	if !ok {
		return
	}
	cfg, ok := a.mgr.Config(id)
	if !ok {
		writeErr(w, 404, "campanha não encontrada")
		return
	}
	s := harness.Shrink(cfg, seed)
	if !s.Reproduces {
		writeErr(w, 422, "essa seed não falha: não há o que minimizar")
		return
	}
	writeJSON(w, 200, s)
}

// spa serves the embedded frontend, falling back to index.html for client
// side routes.
func spa() http.Handler {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(dist, p); err == nil {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(p, "api/") {
				writeErr(w, 404, "rota não encontrada")
				return
			}
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "frontend não compilado", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}

func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:")
		// Mutating requests must come from the UI itself (basic CSRF guard for
		// a server that listens on localhost).
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
				writeErr(w, 403, "origem não permitida")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(origin, host string) bool {
	return origin == "http://"+host || origin == "https://"+host
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (s *statusWriter) WriteHeader(c int) { s.code = c; s.ResponseWriter.WriteHeader(c) }
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.code, time.Since(start).Round(time.Millisecond))
		}
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		log.Printf("encode: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

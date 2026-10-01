package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"simulacra/internal/campaign"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	mgr, err := campaign.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(mgr))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func TestCampaignLifecycle(t *testing.T) {
	srv := newServer(t)
	var created campaign.Summary
	code := do(t, "POST", srv.URL+"/api/campaigns", `{"config":{"system":"raftkv","bugs":["stale-read"]},"seeds":60,"startSeed":1}`, &created)
	if code != 201 {
		t.Fatalf("create: %d", code)
	}

	// Follow the SSE stream until the campaign ends.
	res, err := http.Get(srv.URL + "/api/campaigns/" + created.ID + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	seen := 0
	deadline := time.Now().Add(30 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		line := sc.Text()
		if line == "event: end" {
			break
		}
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			var p struct {
				Results []campaign.SeedResult `json:"results"`
			}
			json.Unmarshal([]byte(data), &p)
			seen += len(p.Results)
		}
	}
	res.Body.Close()
	if seen != 60 {
		t.Fatalf("stream delivered %d results, want 60", seen)
	}

	var c campaign.Campaign
	do(t, "GET", srv.URL+"/api/campaigns/"+created.ID, "", &c)
	if c.Status != "done" || c.Done != 60 || c.Failed == 0 || c.FirstFail == nil {
		t.Fatalf("unexpected campaign %+v", c.Summary)
	}

	// Replaying a seed must reproduce the exact run from the campaign.
	var replay replayResp
	seed := *c.FirstFail
	do(t, "GET", srv.URL+"/api/campaigns/"+created.ID+"/seeds/"+itoa(seed), "", &replay)
	if replay.ExpectedHash == "" || replay.Outcome.Hash != replay.ExpectedHash {
		t.Fatalf("replay hash %s != campaign hash %s", replay.Outcome.Hash, replay.ExpectedHash)
	}
	if len(replay.Trace) == 0 || len(replay.Outcome.Violations) == 0 {
		t.Fatal("replay lost the trace or the failure")
	}

	var shrink struct {
		Reproduces bool `json:"reproduces"`
		After      int  `json:"after"`
		Before     int  `json:"before"`
	}
	if code := do(t, "POST", srv.URL+"/api/campaigns/"+created.ID+"/seeds/"+itoa(seed)+"/shrink", "", &shrink); code != 200 || !shrink.Reproduces || shrink.After > shrink.Before {
		t.Fatalf("shrink: %d %+v", code, shrink)
	}

	if code := do(t, "DELETE", srv.URL+"/api/campaigns/"+created.ID, "", nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code := do(t, "GET", srv.URL+"/api/campaigns/"+created.ID, "", nil); code != 404 {
		t.Fatalf("deleted campaign still there: %d", code)
	}
}

func TestValidation(t *testing.T) {
	srv := newServer(t)
	cases := map[string]string{
		"unknown bug":    `{"config":{"bugs":["nope"]},"seeds":10}`,
		"too many seeds": `{"config":{},"seeds":999999999}`,
		"unknown field":  `{"config":{},"seeds":10,"evil":true}`,
		"bad rate":       `{"config":{"dropRate":2},"seeds":10}`,
	}
	for name, body := range cases {
		if code := do(t, "POST", srv.URL+"/api/campaigns", body, nil); code < 400 || code >= 500 {
			t.Errorf("%s: got %d, want 4xx", name, code)
		}
	}
	if code := do(t, "GET", srv.URL+"/api/campaigns/..%2F..%2Fetc", "", nil); code != 404 {
		t.Errorf("path traversal id: %d", code)
	}
}

func TestCrossOriginWriteRejected(t *testing.T) {
	srv := newServer(t)
	req, _ := http.NewRequest("POST", srv.URL+"/api/campaigns", strings.NewReader(`{"config":{},"seeds":1}`))
	req.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("cross-origin POST: %d, want 403", res.StatusCode)
	}
}

func TestSPAFallback(t *testing.T) {
	srv := newServer(t)
	res, err := http.Get(srv.URL + "/campaigns/c-abc")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("client route: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

func itoa(n uint64) string { return strconv.FormatUint(n, 10) }

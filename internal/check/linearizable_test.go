package check

import (
	"testing"

	"simulacra/internal/sim"
)

func op(id int, call, ret sim.Time, in KVInput, out KVOutput) sim.Op {
	return sim.Op{ID: id, Call: call, Return: ret, Input: in, Output: out}
}

func pending(id int, call sim.Time, in KVInput) sim.Op {
	return sim.Op{ID: id, Call: call, Return: -1, Input: in, Pending: true}
}

func put(k, v string) KVInput { return KVInput{Kind: "put", Key: k, Value: v} }
func get(k string) KVInput    { return KVInput{Kind: "get", Key: k} }
func cas(k, from, to string) KVInput {
	return KVInput{Kind: "cas", Key: k, Expected: from, Value: to}
}
func ok() KVOutput          { return KVOutput{Ok: true} }
func val(v string) KVOutput { return KVOutput{Ok: true, Value: v} }
func failed() KVOutput      { return KVOutput{Ok: false} }

func TestLinearizable(t *testing.T) {
	cases := []struct {
		name string
		h    []sim.Op
		want bool
	}{
		{"empty", nil, true},
		{"sequential", []sim.Op{
			op(0, 0, 10, put("x", "1"), ok()),
			op(1, 20, 30, get("x"), val("1")),
		}, true},
		{"stale read after write returned", []sim.Op{
			op(0, 0, 10, put("x", "1"), ok()),
			op(1, 20, 30, get("x"), val("")),
		}, false},
		{"concurrent read may see old or new", []sim.Op{
			op(0, 0, 10, put("x", "1"), ok()),
			op(1, 5, 30, get("x"), val("")),
			op(2, 6, 31, get("x"), val("1")),
		}, true},
		{"read goes back in time", []sim.Op{
			op(0, 0, 100, put("x", "1"), ok()),
			op(1, 10, 20, get("x"), val("1")),
			op(2, 30, 40, get("x"), val("")),
		}, false},
		{"cas succeeds once", []sim.Op{
			op(0, 0, 10, put("x", "a"), ok()),
			op(1, 20, 50, cas("x", "a", "b"), ok()),
			op(2, 20, 50, cas("x", "a", "c"), ok()),
		}, false},
		{"cas one wins one loses", []sim.Op{
			op(0, 0, 10, put("x", "a"), ok()),
			op(1, 20, 50, cas("x", "a", "b"), ok()),
			op(2, 20, 50, cas("x", "a", "c"), failed()),
			op(3, 60, 70, get("x"), val("b")),
		}, true},
		{"pending write may take effect late", []sim.Op{
			pending(0, 0, put("x", "1")),
			op(1, 10, 20, get("x"), val("")),
			op(2, 500, 510, get("x"), val("1")),
		}, true},
		{"pending write can't be undone", []sim.Op{
			pending(0, 0, put("x", "1")),
			op(1, 10, 20, get("x"), val("1")),
			op(2, 30, 40, get("x"), val("")),
		}, false},
		{"keys are independent", []sim.Op{
			op(0, 0, 10, put("x", "1"), ok()),
			op(1, 0, 10, put("y", "2"), ok()),
			op(2, 20, 30, get("y"), val("2")),
			op(3, 20, 30, get("x"), val("1")),
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Linearizable(c.h)
			if r.Ok != c.want {
				t.Fatalf("Linearizable = %v, want %v (report %+v)", r.Ok, c.want, r)
			}
		})
	}
}

func TestReportPointsAtFailingKey(t *testing.T) {
	r := Linearizable([]sim.Op{
		op(0, 0, 10, put("a", "1"), ok()),
		op(1, 20, 30, get("a"), val("1")),
		op(2, 0, 10, put("b", "1"), ok()),
		op(3, 20, 30, get("b"), val("2")),
	})
	if r.Ok || r.Key != "b" {
		t.Fatalf("expected failure on key b, got %+v", r)
	}
	if len(r.Stuck) != 1 || r.Stuck[0] != 3 {
		t.Fatalf("expected op 3 to be stuck, got %v", r.Stuck)
	}
}

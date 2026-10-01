package raftkv

import (
	"fmt"

	"simulacra/internal/check"
	"simulacra/internal/sim"
)

const (
	retryAfter = 150 * sim.Millisecond
	giveUpAt   = 1500 * sim.Millisecond
	thinkMax   = 40 * sim.Millisecond
)

var keys = []string{"x", "y"}

type nextOp struct{}
type retry struct{ Seq int }
type giveUp struct{ Seq int }

type inflight struct {
	cmd  Cmd
	opID int
}

// Client issues a random mix of get/put/cas, one at a time, retrying against
// other servers until it gets an answer or gives up.
type Client struct {
	id      sim.NodeID
	servers []sim.NodeID
	seq     int
	cur     *inflight
	target  sim.NodeID
	seen    map[string]string // last value observed per key, used for CAS
}

func NewClient(id sim.NodeID, servers []sim.NodeID) *Client {
	return &Client{id: id, servers: servers, seen: map[string]string{}}
}

func (c *Client) Start(env *sim.Env) {
	c.target = c.servers[env.Rand().IntN(len(c.servers))]
	env.SetTimer(sim.Time(env.Rand().Int64N(int64(thinkMax))), nextOp{})
}

func (c *Client) Timer(env *sim.Env, tag any) {
	switch t := tag.(type) {
	case nextOp:
		c.issue(env)
	case retry:
		if c.cur == nil || c.cur.cmd.Seq != t.Seq {
			return
		}
		c.target = c.servers[(int(c.target)+1)%len(c.servers)]
		c.send(env)
	case giveUp:
		if c.cur == nil || c.cur.cmd.Seq != t.Seq {
			return
		}
		env.Abandon(c.cur.opID)
		c.cur = nil
		c.schedule(env)
	}
}

func (c *Client) issue(env *sim.Env) {
	r := env.Rand()
	c.seq++
	key := keys[r.IntN(len(keys))]
	cmd := Cmd{Key: key, Client: c.id, Seq: c.seq}
	switch p := r.IntN(100); {
	case p < 45:
		cmd.Kind = "get"
	case p < 80:
		cmd.Kind = "put"
		cmd.Value = fmt.Sprintf("%d.%d", c.id, c.seq)
	default:
		cmd.Kind = "cas"
		cmd.Expected = c.seen[key]
		cmd.Value = fmt.Sprintf("%d.%d", c.id, c.seq)
	}
	id := env.Invoke(check.KVInput{Kind: cmd.Kind, Key: cmd.Key, Value: cmd.Value, Expected: cmd.Expected})
	c.cur = &inflight{cmd: cmd, opID: id}
	c.send(env)
	env.SetTimer(giveUpAt, giveUp{Seq: c.seq})
}

func (c *Client) send(env *sim.Env) {
	env.Send(c.target, ClientRequest{Cmd: c.cur.cmd})
	env.SetTimer(retryAfter, retry{Seq: c.cur.cmd.Seq})
}

func (c *Client) schedule(env *sim.Env) {
	env.SetTimer(1+sim.Time(env.Rand().Int64N(int64(thinkMax))), nextOp{})
}

func (c *Client) Receive(env *sim.Env, from sim.NodeID, msg any) {
	m, ok := msg.(ClientReply)
	if !ok || c.cur == nil || m.Seq != c.cur.cmd.Seq {
		return
	}
	if m.Err != "" {
		if m.Leader != sim.None && m.Leader != c.target {
			c.target = m.Leader
		} else {
			c.target = c.servers[(int(c.target)+1)%len(c.servers)]
		}
		return // the pending retry timer will resend
	}
	cmd := c.cur.cmd
	switch cmd.Kind {
	case "get":
		c.seen[cmd.Key] = m.Value
	case "put":
		c.seen[cmd.Key] = cmd.Value
	case "cas":
		if m.Ok {
			c.seen[cmd.Key] = cmd.Value
		}
	}
	env.Complete(c.cur.opID, check.KVOutput{Ok: m.Ok, Value: m.Value})
	c.cur = nil
	c.schedule(env)
}

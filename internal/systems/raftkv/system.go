package raftkv

import (
	"fmt"

	"simulacra/internal/check"
	"simulacra/internal/sim"
)

// Bug identifies a deliberately broken rule.
type Bug string

const (
	BugStaleRead        Bug = "stale-read"
	BugVoteNotPersisted Bug = "vote-not-persisted"
	BugAcceptStaleTerm  Bug = "accept-stale-term"
	BugUnsyncedLog      Bug = "unsynced-log"
	BugNoDedup          Bug = "no-dedup"
)

// Bugs is the set of enabled bugs.
type Bugs map[Bug]bool

// BugInfo documents a bug for the CLI and UI.
type BugInfo struct {
	ID          Bug    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Detects     string `json:"detects"`
	Hint        string `json:"hint,omitempty"`
}

// Catalog lists every bug the implementation can switch on.
var Catalog = []BugInfo{
	{
		ID:          BugStaleRead,
		Title:       "Leitura sem confirmar liderança",
		Description: "O líder responde get direto do estado local, sem passar pelo log nem confirmar com a maioria. Um líder antigo isolado por partição continua respondendo valores velhos.",
		Detects:     "linearizabilidade",
	},
	{
		ID:          BugVoteNotPersisted,
		Title:       "Voto não persistido",
		Description: "O termo é gravado em disco, mas o votedFor não. Depois de um crash o nó pode votar duas vezes no mesmo termo, elegendo dois líderes.",
		Detects:     "segurança de eleição",
		Hint:        "Raro: exige duas candidaturas no mesmo termo e um reinício rápido do eleitor. Use crashes e partições ≥ 1,5/s e ~20 mil seeds (≈1 falha a cada 5 mil).",
	},
	{
		ID:          BugAcceptStaleTerm,
		Title:       "Aceita AppendEntries de termo antigo",
		Description: "O seguidor não rejeita mensagens de um líder deposto, que pode truncar entradas já confirmadas por outro líder.",
		Detects:     "segurança da máquina de estados, linearizabilidade",
	},
	{
		ID:          BugUnsyncedLog,
		Title:       "Confirma sem fsync",
		Description: "O seguidor responde sucesso ao líder antes de sincronizar o log em disco. Um crash apaga entradas que a maioria já tinha confirmado.",
		Detects:     "segurança da máquina de estados, linearizabilidade",
	},
	{
		ID:          BugNoDedup,
		Title:       "Sem deduplicação de clientes",
		Description: "Retentativas e mensagens duplicadas pela rede são aplicadas mais de uma vez; um put antigo pode sobrescrever um mais novo.",
		Detects:     "linearizabilidade",
	},
}

// ParseBugs validates bug IDs.
func ParseBugs(ids []string) (Bugs, error) {
	b := Bugs{}
	for _, id := range ids {
		found := false
		for _, info := range Catalog {
			if string(info.ID) == id {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("bug desconhecido %q", id)
		}
		b[Bug(id)] = true
	}
	return b, nil
}

// Check runs every checker over a finished run.
func Check(r *sim.Result) []sim.Violation {
	var out []sim.Violation
	out = append(out, electionSafety(r.Observations)...)
	out = append(out, stateMachineSafety(r.Observations)...)
	if lin := check.Linearizable(r.History); !lin.Ok {
		out = append(out, sim.Violation{
			Kind:    "linearizability",
			Message: fmt.Sprintf("histórico da chave %q não é linearizável: nenhuma ordem sequencial explica as respostas", lin.Key),
			Details: lin,
		})
	}
	return out
}

// electionSafety: at most one leader per term (Raft §5.2).
func electionSafety(obs []sim.Observation) []sim.Violation {
	leaders := map[int]sim.NodeID{}
	for _, o := range obs {
		l, ok := o.Data.(LeaderObs)
		if !ok {
			continue
		}
		if prev, ok := leaders[l.Term]; ok && prev != o.Node {
			return []sim.Violation{{
				Kind:    "election-safety",
				Message: fmt.Sprintf("dois líderes no termo %d: S%d e S%d", l.Term, prev, o.Node),
				Details: map[string]any{"term": l.Term, "nodes": []sim.NodeID{prev, o.Node}, "t": o.T},
			}}
		}
		leaders[l.Term] = o.Node
	}
	return nil
}

// stateMachineSafety: no two nodes apply different entries at the same index
// (Raft §5.4.3). Restarted nodes re-apply their log, so this also catches a
// node that lost an entry it had applied before.
func stateMachineSafety(obs []sim.Observation) []sim.Violation {
	type applied struct {
		obs  ApplyObs
		node sim.NodeID
		t    sim.Time
	}
	first := map[int]applied{}
	for _, o := range obs {
		a, ok := o.Data.(ApplyObs)
		if !ok {
			continue
		}
		prev, ok := first[a.Index]
		if !ok {
			first[a.Index] = applied{a, o.Node, o.T}
			continue
		}
		if prev.obs != a {
			return []sim.Violation{{
				Kind: "state-machine-safety",
				Message: fmt.Sprintf("índice %d aplicado com entradas diferentes: S%d aplicou (termo %d, %s c%d#%d) e S%d aplicou (termo %d, %s c%d#%d)",
					a.Index, prev.node, prev.obs.Term, prev.obs.Kind, prev.obs.Client, prev.obs.Seq,
					o.Node, a.Term, a.Kind, a.Client, a.Seq),
				Details: map[string]any{"index": a.Index, "first": prev.obs, "firstNode": prev.node, "second": a, "secondNode": o.Node, "t": o.T},
			}}
		}
	}
	return nil
}

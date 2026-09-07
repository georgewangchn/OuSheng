package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"ousheng/internal/board"
	"ousheng/internal/card"
)

// --- Input / Output types (SDK infers JSON schema from struct tags) ---

type ReadBoardInput struct {
	Path   string `json:"path" jsonschema:"path to the board store (git repo)"`
	ID     string `json:"id,omitempty" jsonschema:"filter by card id"`
	Owner  string `json:"owner,omitempty" jsonschema:"filter by owner"`
	Status string `json:"status,omitempty" jsonschema:"filter by status: proposed|agreed|live|verified|deprecated"`
	Kind   string `json:"kind,omitempty" jsonschema:"filter by contract kind: http|cli|lib|event"`
}

type ReadBoardOutput struct {
	Cards []card.Summary `json:"cards"`
}

type WriteBoardInput struct {
	Path            string `json:"path" jsonschema:"path to the board store"`
	CardYAML        string `json:"card_yaml" jsonschema:"card content in YAML format (matches card schema)"`
	ExpectedVersion int    `json:"expected_version" jsonschema:"CAS expected version (0 for new card)"`
}

type WriteBoardOutput struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Status  string `json:"status"`
}

type ConvergeInput struct {
	Path       string `json:"path" jsonschema:"path to the board store"`
	StuckAfter string `json:"stuck_after,omitempty" jsonschema:"time-based stuck threshold (e.g. 24h, empty=disabled)"`
}

type ConvergeOutput struct {
	Status   string   `json:"status"`
	Blockers []string `json:"blockers,omitempty"`
	Cycle    []string `json:"cycle,omitempty"`
}

// --- Handlers ---

func readBoard(_ context.Context, _ *mcp.CallToolRequest, in ReadBoardInput) (*mcp.CallToolResult, ReadBoardOutput, error) {
	b := newBoard(in.Path)
	cards, err := b.ReadBoard(board.Scope{
		ID:     in.ID,
		Owner:  in.Owner,
		Status: in.Status,
		Kind:   in.Kind,
	})
	if err != nil {
		return nil, ReadBoardOutput{}, err
	}
	summaries := make([]card.Summary, len(cards))
	for i, c := range cards {
		summaries[i] = c.Summarize()
	}
	return nil, ReadBoardOutput{Cards: summaries}, nil
}

func writeBoard(_ context.Context, _ *mcp.CallToolRequest, in WriteBoardInput) (*mcp.CallToolResult, WriteBoardOutput, error) {
	c, err := card.Decode([]byte(in.CardYAML))
	if err != nil {
		return nil, WriteBoardOutput{}, err
	}
	b := newBoard(in.Path)
	written, err := b.WriteBoard(c, in.ExpectedVersion)
	if err != nil {
		return nil, WriteBoardOutput{}, err
	}
	return nil, WriteBoardOutput{
		ID:      written.ID,
		Version: written.Version,
		Status:  string(written.Status),
	}, nil
}

func converge(_ context.Context, _ *mcp.CallToolRequest, in ConvergeInput) (*mcp.CallToolResult, ConvergeOutput, error) {
	b := newBoard(in.Path)
	opts := board.ConvergeOptions{}
	if in.StuckAfter != "" {
		d, err := time.ParseDuration(in.StuckAfter)
		if err != nil {
			return nil, ConvergeOutput{}, fmt.Errorf("invalid stuck_after: %w", err)
		}
		opts.StuckAfter = d
	}
	res, err := b.ConvergeWithOpts(opts)
	if err != nil {
		return nil, ConvergeOutput{}, err
	}
	return nil, ConvergeOutput{
		Status:   string(res.Status),
		Blockers: res.Blockers,
		Cycle:    res.Cycle,
	}, nil
}

// --- Helpers ---

func newBoard(dir string) *board.Board { return board.New(dir) }

// --- Server wiring ---

func main() {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "ousheng-mcp",
			Version: "v0.1.0",
			Title:   "OuSheng Board",
		},
		nil,
	)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_board",
		Description: "Read cards from the OuSheng board. Returns card summaries. Use filters to narrow by id, owner, status, or contract kind.",
	}, readBoard)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_board",
		Description: "Write or update a card on the OuSheng board. Accepts card YAML + expected version (CAS). New cards pass expected_version=0.",
	}, writeBoard)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "converge",
		Description: "Run convergence check on the board. Returns CONVERGED (all verified), IN_PROGRESS (still working), or STUCK (cycle/broken dependency).",
	}, converge)

	registerV03Tools(server)

	log.Println("ousheng-mcp: serving over stdio")
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func registerV03Tools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_my_context",
		Description: "Minimal engineering context for an actor: identity, accountable human, roles, systems, target version, active work, blockers. " + timingHint,
	}, getMyContext)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_actor_context",
		Description: "Actor view: adds agents (for humans), responsible systems, accountable-for work, blocked work.",
	}, getActorContext)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_system_context",
		Description: "System view: responsible humans, executors, active work, open bugs, blockers.",
	}, getSystemContext)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_work_item",
		Description: "Full work item detail with dependency briefs (progressive disclosure layer 2).",
	}, getWorkItem)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_work_items",
		Description: "Query work items by system/status/assignee/version/type/open filters.",
	}, queryWorkItems)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_work_item",
		Description: "Create a work item (starts at backlog). Respect CAS: new item revision=1.",
	}, createWorkItem)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_work_item",
		Description: "Update a work item (status transition; CAS on revision). After finishing a task, update status then re-read context.",
	}, updateWorkItem)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "assign_work_item",
		Description: "Assign a work item to an actor with optional acting role.",
	}, assignWorkItem)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "report_bug",
		Description: "Report a bug work item (type forced to bug; supports detected_by).",
	}, reportBug)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "report_progress",
		Description: "Report progress (reported state, not fact): value 0..1, actor, basis.",
	}, reportProgress)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_evidence",
		Description: "Append typed evidence to a work item. git_commit evidence is verified against the repo.",
	}, addEvidence)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_evidence",
		Description: "List evidence for a work item (progressive disclosure layer 3).",
	}, getEvidence)
}

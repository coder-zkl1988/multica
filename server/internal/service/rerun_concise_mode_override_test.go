package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRerunIssueConciseModeOverride pins the tri-state contract of the rerun's
// concise-mode flag:
//
//   - nil inherits the source task's mode (legacy rerun behaviour);
//   - true/false forces the mode on the new row, deliberately diverging from
//     the source. A cross-mode rerun cannot resume the source session — the
//     daemon's rerunSourceMatchesTaskScope requires an exact mode match — so
//     the override is the "restart in concise mode" affordance, not a hot
//     switch of a running task.
func TestRerunIssueConciseModeOverride(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, issueID := seedAttributionFixture(t, pool)
	_ = workspaceID

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}

	seedSource := func(t *testing.T, concise bool) pgtype.UUID {
		t.Helper()
		var sourceID pgtype.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, concise_mode)
			VALUES ($1, $2, $3, 'failed', 0, $4)
			RETURNING id
		`, agentID, runtimeID, issueID, concise).Scan(&sourceID); err != nil {
			t.Fatalf("insert source task: %v", err)
		}
		t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, sourceID) })
		return sourceID
	}

	rerunMode := func(t *testing.T, sourceID pgtype.UUID, override *bool) bool {
		t.Helper()
		task, err := svc.RerunIssue(ctx, util.MustParseUUID(issueID), sourceID, pgtype.UUID{}, util.MustParseUUID(creatorID), nil, override)
		if err != nil {
			t.Fatalf("RerunIssue: %v", err)
		}
		t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID) })
		var concise bool
		if err := pool.QueryRow(ctx, `SELECT concise_mode FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&concise); err != nil {
			t.Fatalf("read rerun concise_mode: %v", err)
		}
		return concise
	}

	t.Run("nil inherits source mode", func(t *testing.T) {
		source := seedSource(t, true)
		if got := rerunMode(t, source, nil); !got {
			t.Fatal("nil override should inherit concise=true from source")
		}
	})

	t.Run("override to concise on standard source", func(t *testing.T) {
		source := seedSource(t, false)
		if got := rerunMode(t, source, &[]bool{true}[0]); !got {
			t.Fatal("override=true should force concise on the rerun row")
		}
	})

	t.Run("override to standard on concise source", func(t *testing.T) {
		source := seedSource(t, true)
		if got := rerunMode(t, source, &[]bool{false}[0]); got {
			t.Fatal("override=false should force standard mode on the rerun row")
		}
	})
}

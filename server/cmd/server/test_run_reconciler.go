package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/handler"
)

const (
	// testRunReconcileInterval paces the sweep for run cases whose agent task
	// ended outside the daemon's /fail hook. The handler already leaves each
	// ended task a minute of grace, so a faster tick buys nothing.
	testRunReconcileInterval = time.Minute
	// testRunReconcileBudget bounds one round below the interval so a slow
	// database delays the next round instead of stacking rounds.
	testRunReconcileBudget = 45 * time.Second
	// testRunReconcileBatchSize bounds the cases settled per round.
	testRunReconcileBatchSize = 100
)

// testRunCaseReconciler is the slice of the handler this sweeper drives.
type testRunCaseReconciler interface {
	ReconcileTestRunCasesWithEndedTasks(ctx context.Context, limit int32) (int, error)
}

var _ testRunCaseReconciler = (*handler.Handler)(nil)

// runTestRunReconciler settles test run cases whose agent task the runtime
// sweeper, orphan recovery or a cancellation ended directly. Those paths
// never run the /fail hook, so without this a round waits on such a case
// forever.
func runTestRunReconciler(ctx context.Context, reconciler testRunCaseReconciler) {
	runPeriodicSweep(ctx, testRunReconcileInterval, func() {
		reconcileTestRunCasesWithBudget(ctx, reconciler, testRunReconcileBudget)
	})
}

func reconcileTestRunCasesWithBudget(ctx context.Context, reconciler testRunCaseReconciler, budget time.Duration) {
	roundCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	settled, err := reconciler.ReconcileTestRunCasesWithEndedTasks(roundCtx, testRunReconcileBatchSize)
	if err != nil {
		slog.Warn("test run reconciler failed", "settled", settled, "error", err)
		return
	}
	if settled > 0 {
		slog.Info("test run reconciler settled cases whose task ended", "count", settled)
	}
}

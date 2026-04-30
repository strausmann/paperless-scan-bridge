// Package jobs owns the scan-job state machine and its embedded
// persistence layer.
//
// The state machine is fully specified in CONTAINER_SUITE.md sec. 4.7
// and has not been implemented yet — this file only declares the
// public types so other packages can compile against a stable
// signature. The real Store, the BoltDB-backed persistence, the
// cancellation paths, and the "jobs in dispatched/scanning at startup
// → failed (daemon restart)" recovery rule all land in Phase 1.4 once
// the dispatch subsystem is wired up.
package jobs

import (
	"context"
	"time"
)

// State is the position of a job in the state machine.
type State string

const (
	StateQueued     State = "queued"
	StateDispatched State = "dispatched"
	StateScanning   State = "scanning"
	StateProcessing State = "processing"
	StateCompleted  State = "completed"
	StateFailed     State = "failed"
	StateArchived   State = "archived"
	StateCancelled  State = "cancelled"
)

// Job is one scan request and everything we know about its execution.
type Job struct {
	ID          string            // ULID, sortable by creation time
	Profile     string            // profile name from the YAML registry
	State       State
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
	Metadata    map[string]string // free-form caller metadata
	OutputPath  string            // absolute path under the consume share
	FailReason  string            // populated only when State == StateFailed
}

// Store is the persistence contract the API and dispatch packages
// will program against. The current implementation is intentionally
// missing — see the package comment.
type Store interface {
	Create(ctx context.Context, j Job) error
	Get(ctx context.Context, id string) (Job, error)
	List(ctx context.Context, limit int) ([]Job, error)
	Transition(ctx context.Context, id string, to State) error
	Close() error
}

// TODO(phase 1.4): provide a BoltDB-backed Store implementation.
// TODO(phase 1.4): publish scan_bridge_queue_depth and
// scan_bridge_active_jobs gauges from the store on every transition.
// TODO(phase 1.4): on daemon start, scan the store for jobs in
// StateDispatched or StateScanning and transition them to StateFailed
// with FailReason "daemon restart during execution" — never resume.

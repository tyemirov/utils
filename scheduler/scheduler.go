// Package scheduler provides a small retry-aware job runner that can be embedded
// in services needing persisted scheduling semantics.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Repository exposes persistence hooks for fetching pending jobs and recording attempt results.
type Repository interface {
	PendingJobs(ctx context.Context, maxRetries int, now time.Time) ([]Job, error)
	ApplyAttemptResult(ctx context.Context, job Job, update AttemptUpdate) error
}

// Dispatcher performs the effectful work for a job (sending an email, firing an SMS, etc.).
type Dispatcher interface {
	Attempt(ctx context.Context, job Job) (DispatchResult, error)
}

// Job represents a scheduled unit of work alongside metadata the scheduler needs for backoff decisions.
type Job struct {
	ID              string
	ScheduledFor    *time.Time
	RetryCount      int
	LastAttemptedAt time.Time
	Payload         any
}

// DispatchResult carries dispatcher-supplied metadata, including status overrides and provider IDs.
type DispatchResult struct {
	Status            string
	ProviderMessageID string
}

// AttemptUpdate describes the mutation that must be persisted after a dispatch attempt.
type AttemptUpdate struct {
	Status            string
	ProviderMessageID string
	RetryCount        int
	LastAttemptedAt   time.Time
}

// Clock abstracts time acquisition for deterministic tests.
type Clock interface {
	Now() time.Time
}

// Config contains all inputs required to construct a Worker.
type Config struct {
	Repository    Repository
	Dispatcher    Dispatcher
	Logger        *slog.Logger
	Interval      time.Duration
	MaxRetries    int
	SuccessStatus string
	FailureStatus string
	Clock         Clock
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

// Worker orchestrates scheduled retries with exponential backoff and contextual logging.
type Worker struct {
	repository    Repository
	dispatcher    Dispatcher
	logger        *slog.Logger
	interval      time.Duration
	maxRetries    int
	successStatus string
	failureStatus string
	clock         Clock
}

const maxBackoffShift = 20

var errInvalidConfig = errors.New("invalid scheduler config")

// NewWorker validates the configuration and returns a ready-to-run Worker.
func NewWorker(cfg Config) (*Worker, error) {
	if cfg.Repository == nil || cfg.Dispatcher == nil || cfg.Logger == nil {
		return nil, fmt.Errorf("%w: repository, dispatcher, and logger are required", errInvalidConfig)
	}
	if cfg.Interval <= 0 {
		return nil, fmt.Errorf("%w: interval must be positive", errInvalidConfig)
	}
	if cfg.MaxRetries <= 0 {
		return nil, fmt.Errorf("%w: max retries must be positive", errInvalidConfig)
	}
	if cfg.SuccessStatus == "" || cfg.FailureStatus == "" {
		return nil, fmt.Errorf("%w: success and failure statuses are required", errInvalidConfig)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	return &Worker{
		repository:    cfg.Repository,
		dispatcher:    cfg.Dispatcher,
		logger:        cfg.Logger,
		interval:      cfg.Interval,
		maxRetries:    cfg.MaxRetries,
		successStatus: cfg.SuccessStatus,
		failureStatus: cfg.FailureStatus,
		clock:         clock,
	}, nil
}

// Run executes the retry loop until the provided context is canceled.
func (worker *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()

	worker.logger.Info("scheduler_worker_started", "interval", worker.interval, "max_retries", worker.maxRetries)
	for {
		select {
		case <-ctx.Done():
			worker.logger.Info("scheduler_worker_stopped")
			return
		case <-ticker.C:
			worker.runCycle(ctx)
		}
	}
}

// RunOnce executes a single scheduler cycle. This is primarily used in tests.
func (worker *Worker) RunOnce(ctx context.Context) {
	worker.runCycle(ctx)
}

func (worker *Worker) runCycle(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	now := worker.clock.Now()
	pendingJobs, pendingErr := worker.repository.PendingJobs(ctx, worker.maxRetries, now)
	if pendingErr != nil {
		worker.logger.Error("scheduler_pending_jobs_error", "error", pendingErr)
		return
	}

	for _, job := range pendingJobs {
		if ctx.Err() != nil {
			return
		}
		if !worker.shouldAttempt(job, now) {
			continue
		}
		worker.executeJob(ctx, job, now)
	}
}

func (worker *Worker) shouldAttempt(job Job, now time.Time) bool {
	if job.ScheduledFor != nil && now.Before(job.ScheduledFor.UTC()) {
		return false
	}
	if job.RetryCount <= 0 || job.LastAttemptedAt.IsZero() {
		return true
	}
	shift := job.RetryCount
	if shift > maxBackoffShift {
		shift = maxBackoffShift
	}
	backoff := worker.interval * time.Duration(1<<uint(shift))
	nextAttempt := job.LastAttemptedAt.UTC().Add(backoff)
	return !now.Before(nextAttempt)
}

func (worker *Worker) executeJob(ctx context.Context, job Job, now time.Time) {
	attemptedAt := now.UTC()
	result, dispatchErr := worker.dispatcher.Attempt(ctx, job)

	status := result.Status
	if status == "" {
		if dispatchErr != nil {
			status = worker.failureStatus
		} else {
			status = worker.successStatus
		}
	}
	if status == "" {
		status = worker.failureStatus
	}

	update := AttemptUpdate{
		Status:            status,
		ProviderMessageID: result.ProviderMessageID,
		RetryCount:        job.RetryCount + 1,
		LastAttemptedAt:   attemptedAt,
	}

	if applyErr := worker.repository.ApplyAttemptResult(ctx, job, update); applyErr != nil {
		worker.logger.Error("scheduler_apply_attempt_error", "job_id", job.ID, "error", applyErr)
	}

	if dispatchErr != nil {
		worker.logger.Error("scheduler_dispatch_error", "job_id", job.ID, "error", dispatchErr)
		return
	}

	worker.logger.Info("scheduler_dispatch_success", "job_id", job.ID, "status", status)
}

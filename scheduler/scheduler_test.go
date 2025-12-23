package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestWorkerExecutesDueJobs(t *testing.T) {
	t.Helper()

	now := time.Now().UTC()
	repo := &fakeRepository{
		jobs: []Job{
			{
				ID:         "job-email",
				RetryCount: 0,
			},
		},
	}
	dispatcher := &fakeDispatcher{
		results: []DispatchResult{
			{ProviderMessageID: "provider-1"},
		},
	}

	worker := newTestWorker(t, repo, dispatcher, now)
	worker.RunOnce(context.Background())

	if len(dispatcher.calls) != 1 {
		t.Fatalf("expected dispatcher to run once, got %d", len(dispatcher.calls))
	}
	if len(repo.updates) != 1 {
		t.Fatalf("expected repository update")
	}
	update := repo.updates[0]
	if update.Status != "sent" {
		t.Fatalf("expected success status, got %s", update.Status)
	}
	if update.RetryCount != 1 {
		t.Fatalf("expected retry count 1, got %d", update.RetryCount)
	}
	if update.ProviderMessageID != "provider-1" {
		t.Fatalf("expected provider id propagation")
	}
	if update.LastAttemptedAt.IsZero() {
		t.Fatalf("expected last attempted timestamp")
	}
}

func TestWorkerSkipsFutureJobs(t *testing.T) {
	t.Helper()

	now := time.Now().UTC()
	future := now.Add(2 * time.Minute)
	repo := &fakeRepository{
		jobs: []Job{
			{
				ID:           "job-future",
				ScheduledFor: &future,
			},
		},
	}
	dispatcher := &fakeDispatcher{}

	worker := newTestWorker(t, repo, dispatcher, now)
	worker.RunOnce(context.Background())

	if len(dispatcher.calls) != 0 {
		t.Fatalf("expected no dispatch calls")
	}
	if len(repo.updates) != 0 {
		t.Fatalf("expected no repository updates")
	}
}

func TestWorkerRespectsExponentialBackoff(t *testing.T) {
	t.Helper()

	baseInterval := time.Second
	now := time.Now().UTC()
	repo := &fakeRepository{
		jobs: []Job{
			{
				ID:              "job-backoff",
				RetryCount:      2,
				LastAttemptedAt: now.Add(-1 * time.Second),
			},
		},
	}
	dispatcher := &fakeDispatcher{}

	worker := newTestWorker(t, repo, dispatcher, now)
	worker.interval = baseInterval
	worker.RunOnce(context.Background())
	if len(dispatcher.calls) != 0 {
		t.Fatalf("expected no dispatch before backoff elapses")
	}

	repo.jobs[0].LastAttemptedAt = now.Add(-4 * time.Second)
	worker.RunOnce(context.Background())
	if len(dispatcher.calls) != 1 {
		t.Fatalf("expected dispatch after backoff, got %d", len(dispatcher.calls))
	}
}

func TestWorkerDefaultsFailureStatusOnError(t *testing.T) {
	t.Helper()

	now := time.Now().UTC()
	repo := &fakeRepository{
		jobs: []Job{
			{ID: "job-error"},
		},
	}
	dispatcher := &fakeDispatcher{
		errors: []error{
			assertionError("send failed"),
		},
	}

	worker := newTestWorker(t, repo, dispatcher, now)
	worker.RunOnce(context.Background())

	if len(repo.updates) != 1 {
		t.Fatalf("expected repository update on error")
	}
	if repo.updates[0].Status != "failed" {
		t.Fatalf("expected failure status, got %s", repo.updates[0].Status)
	}
}

// Helpers.

type fakeRepository struct {
	jobs        []Job
	updates     []AttemptUpdate
	appliedJobs []Job
}

func (repo *fakeRepository) PendingJobs(_ context.Context, _ int, _ time.Time) ([]Job, error) {
	cloned := make([]Job, len(repo.jobs))
	copy(cloned, repo.jobs)
	return cloned, nil
}

func (repo *fakeRepository) ApplyAttemptResult(_ context.Context, job Job, update AttemptUpdate) error {
	repo.appliedJobs = append(repo.appliedJobs, job)
	repo.updates = append(repo.updates, update)
	return nil
}

type fakeDispatcher struct {
	results []DispatchResult
	errors  []error
	calls   []Job
}

func (dispatcher *fakeDispatcher) Attempt(_ context.Context, job Job) (DispatchResult, error) {
	dispatcher.calls = append(dispatcher.calls, job)
	var result DispatchResult
	if len(dispatcher.results) > 0 {
		result = dispatcher.results[0]
		dispatcher.results = dispatcher.results[1:]
	}
	var err error
	if len(dispatcher.errors) > 0 {
		err = dispatcher.errors[0]
		dispatcher.errors = dispatcher.errors[1:]
	}
	return result, err
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

func newTestWorker(t *testing.T, repo Repository, dispatcher Dispatcher, now time.Time) *Worker {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	worker, err := NewWorker(Config{
		Repository:    repo,
		Dispatcher:    dispatcher,
		Logger:        logger,
		Interval:      time.Second,
		MaxRetries:    5,
		SuccessStatus: "sent",
		FailureStatus: "failed",
		Clock:         fixedClock{now: now},
	})
	if err != nil {
		t.Fatalf("new worker error: %v", err)
	}
	return worker
}

type assertionError string

func (err assertionError) Error() string {
	return string(err)
}

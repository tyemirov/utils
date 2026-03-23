package scheduler

import (
	"context"
	"errors"
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

func TestWorkerSkipsJobWhenClaimLost(t *testing.T) {
	t.Helper()

	now := time.Now().UTC()
	repo := &fakeClaimingRepository{
		fakeRepository: fakeRepository{
			jobs: []Job{
				{ID: "job-claim"},
			},
		},
		claimResults: []bool{false},
	}
	dispatcher := &fakeDispatcher{}

	worker := newTestWorker(t, repo, dispatcher, now)
	worker.RunOnce(context.Background())

	if len(repo.claimedJobs) != 1 {
		t.Fatalf("expected one claim attempt, got %d", len(repo.claimedJobs))
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("expected dispatcher not to run when claim is lost")
	}
	if len(repo.updates) != 0 {
		t.Fatalf("expected no attempt update when claim is lost")
	}
}

func TestWorkerSkipsJobWhenClaimFails(t *testing.T) {
	t.Helper()

	now := time.Now().UTC()
	repo := &fakeClaimingRepository{
		fakeRepository: fakeRepository{
			jobs: []Job{
				{ID: "job-claim-error"},
			},
		},
		claimErrors: []error{
			assertionError("claim failed"),
		},
	}
	dispatcher := &fakeDispatcher{}

	worker := newTestWorker(t, repo, dispatcher, now)
	worker.RunOnce(context.Background())

	if len(repo.claimedJobs) != 1 {
		t.Fatalf("expected one claim attempt, got %d", len(repo.claimedJobs))
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("expected dispatcher not to run when claim fails")
	}
	if len(repo.updates) != 0 {
		t.Fatalf("expected no attempt update when claim fails")
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

type fakeClaimingRepository struct {
	fakeRepository
	claimResults []bool
	claimErrors  []error
	claimedJobs  []Job
}

func (repo *fakeClaimingRepository) ClaimJobForAttempt(_ context.Context, job Job, _ time.Time) (bool, error) {
	repo.claimedJobs = append(repo.claimedJobs, job)

	claimed := true
	if len(repo.claimResults) > 0 {
		claimed = repo.claimResults[0]
		repo.claimResults = repo.claimResults[1:]
	}

	var err error
	if len(repo.claimErrors) > 0 {
		err = repo.claimErrors[0]
		repo.claimErrors = repo.claimErrors[1:]
	}

	return claimed, err
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

func TestSystemClockNow(t *testing.T) {
	clock := systemClock{}
	before := time.Now().UTC()
	got := clock.Now()
	after := time.Now().UTC()
	if got.Before(before) || got.After(after) {
		t.Fatalf("systemClock.Now() returned %v, expected between %v and %v", got, before, after)
	}
}

func TestNewWorkerValidation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	repo := &fakeRepository{}
	dispatcher := &fakeDispatcher{}

	cases := []struct {
		name string
		cfg  Config
	}{
		{"nil repository", Config{Repository: nil, Dispatcher: dispatcher, Logger: logger, Interval: time.Second, MaxRetries: 1, SuccessStatus: "s", FailureStatus: "f"}},
		{"nil dispatcher", Config{Repository: repo, Dispatcher: nil, Logger: logger, Interval: time.Second, MaxRetries: 1, SuccessStatus: "s", FailureStatus: "f"}},
		{"nil logger", Config{Repository: repo, Dispatcher: dispatcher, Logger: nil, Interval: time.Second, MaxRetries: 1, SuccessStatus: "s", FailureStatus: "f"}},
		{"zero interval", Config{Repository: repo, Dispatcher: dispatcher, Logger: logger, Interval: 0, MaxRetries: 1, SuccessStatus: "s", FailureStatus: "f"}},
		{"zero max retries", Config{Repository: repo, Dispatcher: dispatcher, Logger: logger, Interval: time.Second, MaxRetries: 0, SuccessStatus: "s", FailureStatus: "f"}},
		{"empty success status", Config{Repository: repo, Dispatcher: dispatcher, Logger: logger, Interval: time.Second, MaxRetries: 1, SuccessStatus: "", FailureStatus: "f"}},
		{"empty failure status", Config{Repository: repo, Dispatcher: dispatcher, Logger: logger, Interval: time.Second, MaxRetries: 1, SuccessStatus: "s", FailureStatus: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWorker(tc.cfg)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !errors.Is(err, errInvalidConfig) {
				t.Fatalf("expected errInvalidConfig, got %v", err)
			}
		})
	}
}

func TestNewWorkerDefaultClock(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	worker, err := NewWorker(Config{
		Repository:    &fakeRepository{},
		Dispatcher:    &fakeDispatcher{},
		Logger:        logger,
		Interval:      time.Second,
		MaxRetries:    1,
		SuccessStatus: "sent",
		FailureStatus: "failed",
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	if _, ok := worker.clock.(systemClock); !ok {
		t.Fatalf("expected default systemClock, got %T", worker.clock)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	repo := &fakeRepository{}
	dispatcher := &fakeDispatcher{}
	worker := newTestWorker(t, repo, dispatcher, time.Now().UTC())
	worker.interval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancel")
	}
}

func TestRunCycleCancelledContext(t *testing.T) {
	repo := &fakeRepository{
		jobs: []Job{{ID: "job-1"}},
	}
	dispatcher := &fakeDispatcher{}
	worker := newTestWorker(t, repo, dispatcher, time.Now().UTC())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.RunOnce(ctx)

	if len(dispatcher.calls) != 0 {
		t.Fatalf("expected no dispatch on cancelled context, got %d", len(dispatcher.calls))
	}
}

type errorRepository struct {
	fakeRepository
	pendingErr error
}

func (repo *errorRepository) PendingJobs(_ context.Context, _ int, _ time.Time) ([]Job, error) {
	return nil, repo.pendingErr
}

func TestRunCyclePendingJobsError(t *testing.T) {
	repo := &errorRepository{pendingErr: errors.New("db down")}
	dispatcher := &fakeDispatcher{}
	worker := newTestWorker(t, repo, dispatcher, time.Now().UTC())
	worker.RunOnce(context.Background())

	if len(dispatcher.calls) != 0 {
		t.Fatalf("expected no dispatch on pending error, got %d", len(dispatcher.calls))
	}
}

type cancellingFakeDispatcher struct {
	cancel context.CancelFunc
	calls  []Job
}

func (dispatcher *cancellingFakeDispatcher) Attempt(_ context.Context, job Job) (DispatchResult, error) {
	dispatcher.calls = append(dispatcher.calls, job)
	dispatcher.cancel()
	return DispatchResult{}, nil
}

func TestRunCycleContextCancelledMidLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := &fakeRepository{
		jobs: []Job{{ID: "job-1"}, {ID: "job-2"}},
	}
	dispatcher := &cancellingFakeDispatcher{cancel: cancel}
	worker := newTestWorker(t, repo, dispatcher, time.Now().UTC())
	worker.RunOnce(ctx)

	if len(dispatcher.calls) != 1 {
		t.Fatalf("expected only one dispatch before cancel, got %d", len(dispatcher.calls))
	}
}

func TestShouldAttemptHighBackoffShift(t *testing.T) {
	now := time.Now().UTC()
	worker := newTestWorker(t, &fakeRepository{}, &fakeDispatcher{}, now)
	job := Job{
		ID:              "job-high-retry",
		RetryCount:      100,
		LastAttemptedAt: now.Add(-2000000 * time.Second),
	}
	if !worker.shouldAttempt(job, now) {
		t.Fatal("expected shouldAttempt to return true for high retry count with enough elapsed time")
	}
}

func TestExecuteJobWithExplicitStatus(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepository{
		jobs: []Job{{ID: "job-explicit"}},
	}
	dispatcher := &fakeDispatcher{
		results: []DispatchResult{{Status: "custom-status", ProviderMessageID: "p1"}},
	}
	worker := newTestWorker(t, repo, dispatcher, now)
	worker.RunOnce(context.Background())

	if len(repo.updates) != 1 {
		t.Fatalf("expected one update, got %d", len(repo.updates))
	}
	if repo.updates[0].Status != "custom-status" {
		t.Fatalf("expected custom-status, got %s", repo.updates[0].Status)
	}
}

type applyErrorRepository struct {
	fakeRepository
	applyErr error
}

func (repo *applyErrorRepository) ApplyAttemptResult(_ context.Context, job Job, update AttemptUpdate) error {
	repo.fakeRepository.appliedJobs = append(repo.fakeRepository.appliedJobs, job)
	repo.fakeRepository.updates = append(repo.fakeRepository.updates, update)
	return repo.applyErr
}

func TestExecuteJobApplyError(t *testing.T) {
	now := time.Now().UTC()
	repo := &applyErrorRepository{
		fakeRepository: fakeRepository{
			jobs: []Job{{ID: "job-apply-err"}},
		},
		applyErr: errors.New("apply failed"),
	}
	dispatcher := &fakeDispatcher{
		results: []DispatchResult{{ProviderMessageID: "p1"}},
	}
	worker := newTestWorker(t, repo, dispatcher, now)
	worker.RunOnce(context.Background())

	if len(dispatcher.calls) != 1 {
		t.Fatalf("expected dispatch to occur, got %d calls", len(dispatcher.calls))
	}
}

func TestClaimJobSuccess(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeClaimingRepository{
		fakeRepository: fakeRepository{
			jobs: []Job{{ID: "job-claim-ok"}},
		},
		claimResults: []bool{true},
	}
	dispatcher := &fakeDispatcher{
		results: []DispatchResult{{ProviderMessageID: "p1"}},
	}
	worker := newTestWorker(t, repo, dispatcher, now)
	worker.RunOnce(context.Background())

	if len(repo.claimedJobs) != 1 {
		t.Fatalf("expected one claim, got %d", len(repo.claimedJobs))
	}
	if len(dispatcher.calls) != 1 {
		t.Fatalf("expected dispatch after successful claim, got %d", len(dispatcher.calls))
	}
}

func TestRunTickerFires(t *testing.T) {
	repo := &fakeRepository{
		jobs: []Job{{ID: "tick-job"}},
	}
	dispatcher := &fakeDispatcher{
		results: []DispatchResult{{ProviderMessageID: "p1"}},
	}
	worker := newTestWorker(t, repo, dispatcher, time.Now().UTC())
	worker.interval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()
	// Wait for at least one tick to fire
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if len(dispatcher.calls) == 0 {
		t.Fatal("expected at least one dispatch from ticker")
	}
}

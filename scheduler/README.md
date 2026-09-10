# Scheduler: Persistent Jobs and Retries

[Package catalog](../README.md#find-a-library)

Import: `github.com/tyemirov/utils/scheduler`

Use `scheduler` for periodic job scans, due-job dispatch, and exponential retry
delays. Applications supply persistent storage and the operation for each job.

## Start with the Executable Examples

The [package tests](scheduler_test.go) contain complete repository, dispatcher,
and clock implementations. Run the public worker scenarios from the repository root:

```sh
make test-unit UNIT_PACKAGES='./scheduler -run "TestWorker(ExecutesDueJobs|RespectsExponentialBackoff|SkipsJobWhenClaimLost)" -v'
```

`TestWorkerExecutesDueJobs` constructs a worker and processes a due job.
`TestWorkerRespectsExponentialBackoff` checks retry timing.
`TestWorkerSkipsJobWhenClaimLost` checks the ownership claim before dispatch.
The scenarios use local dependencies and need no external service.

## Connect an Application

1. Implement `Repository.PendingJobs` and `Repository.ApplyAttemptResult` for the application database.
2. Implement `Dispatcher.Attempt` for the job operation.
3. Supply a logger, scan interval, retry limit, and result statuses to `NewWorker`.
4. Call `Run` with the service context for periodic scans.
5. Use `RunOnce` for a single scan.

The application controls job storage, payloads, dispatch, and shutdown.
For concurrent workers, implement `ClaimingRepository` with an atomic database claim.
The worker skips dispatch when the repository reports a lost claim.

## Consumer Source Examples

PoodleScanner imports this package in `internal/catalogschedules/repository.go`
and `internal/catalogschedules/dispatcher.go`. LoopAware imports it in
`internal/api/traffic_report_schedule.go`. These source examples were checked
on 2026-09-10. They show source usage, with deployment verified separately.

## Validation

```sh
make test-unit UNIT_PACKAGES=./scheduler
```

See [source contracts](scheduler.go) for required configuration and repository methods.

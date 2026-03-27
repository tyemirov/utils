package billing

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSubscriptionStateRepositoryMigrateCreatesTable(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	testContext := context.Background()

	require.NoError(t, Migrate(testContext, database))
	require.True(t, database.WithContext(testContext).Migrator().HasTable(&billingSubscriptionStateRecord{}))
}

func TestSubscriptionStateRepositoryUpsertAndGet(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	testContext := context.Background()
	require.NoError(t, Migrate(testContext, database))

	repository := NewSubscriptionStateRepository(database)
	nextBillingAt := time.Date(2026, time.February, 25, 18, 0, 0, 0, time.UTC)
	eventOccurredAt := time.Date(2026, time.February, 19, 17, 30, 0, 0, time.UTC)
	upsertErr := repository.Upsert(testContext, SubscriptionStateUpsertInput{
		ProviderCode:      ProviderCodePaddle,
		UserEmail:         " USER@EXAMPLE.COM ",
		Status:            subscriptionStatusActive,
		ProviderStatus:    "ACTIVE",
		ActivePlan:        " PRO ",
		SubscriptionID:    "sub_123",
		NextBillingAt:     nextBillingAt,
		LastEventID:       "evt_123",
		LastEventType:     "transaction.completed",
		EventOccurredAt:   eventOccurredAt,
		LastTransactionID: "txn_123",
	})
	require.NoError(t, upsertErr)

	state, found, stateErr := repository.Get(testContext, ProviderCodePaddle, "user@example.com")
	require.NoError(t, stateErr)
	require.True(t, found)
	require.Equal(t, ProviderCodePaddle, state.ProviderCode)
	require.Equal(t, "user@example.com", state.UserEmail)
	require.Equal(t, subscriptionStatusActive, state.Status)
	require.Equal(t, "active", state.ProviderStatus)
	require.Equal(t, PlanCodePro, state.ActivePlan)
	require.Equal(t, "sub_123", state.SubscriptionID)
	require.Equal(t, nextBillingAt, state.NextBillingAt)
	require.Equal(t, eventOccurredAt, state.LastEventOccurredAt)
}

func TestSubscriptionStateRepositoryUpsertUpdatesExistingState(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	testContext := context.Background()
	require.NoError(t, Migrate(testContext, database))

	repository := NewSubscriptionStateRepository(database)
	require.NoError(t, repository.Upsert(testContext, SubscriptionStateUpsertInput{
		ProviderCode:      ProviderCodePaddle,
		UserEmail:         "user@example.com",
		Status:            subscriptionStatusActive,
		ProviderStatus:    "active",
		ActivePlan:        PlanCodePlus,
		SubscriptionID:    "sub_123",
		LastEventID:       "evt_123",
		LastEventType:     "subscription.created",
		LastTransactionID: "txn_123",
	}))
	require.NoError(t, repository.Upsert(testContext, SubscriptionStateUpsertInput{
		ProviderCode:      ProviderCodePaddle,
		UserEmail:         "user@example.com",
		Status:            subscriptionStatusInactive,
		ProviderStatus:    "canceled",
		SubscriptionID:    "sub_123",
		LastEventID:       "evt_124",
		LastEventType:     "subscription.canceled",
		LastTransactionID: "txn_124",
	}))

	state, found, stateErr := repository.Get(testContext, ProviderCodePaddle, "user@example.com")
	require.NoError(t, stateErr)
	require.True(t, found)
	require.Equal(t, subscriptionStatusInactive, state.Status)
	require.Equal(t, "canceled", state.ProviderStatus)
	require.Equal(t, "", state.ActivePlan)

	var recordsCount int64
	countErr := database.WithContext(testContext).Model(&billingSubscriptionStateRecord{}).Count(&recordsCount).Error
	require.NoError(t, countErr)
	require.EqualValues(t, 1, recordsCount)
}

func TestSubscriptionStateRepositoryGetBySubscriptionID(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	testContext := context.Background()
	require.NoError(t, Migrate(testContext, database))

	repository := NewSubscriptionStateRepository(database)
	require.NoError(t, repository.Upsert(testContext, SubscriptionStateUpsertInput{
		ProviderCode:      ProviderCodePaddle,
		UserEmail:         "state-owner@example.com",
		Status:            subscriptionStatusActive,
		ProviderStatus:    "active",
		ActivePlan:        PlanCodePlus,
		SubscriptionID:    "sub_match_1",
		LastEventID:       "evt_match_1",
		LastEventType:     "subscription.activated",
		LastTransactionID: "txn_match_1",
	}))

	state, found, stateErr := repository.GetBySubscriptionID(testContext, ProviderCodePaddle, "sub_match_1")
	require.NoError(t, stateErr)
	require.True(t, found)
	require.Equal(t, "state-owner@example.com", state.UserEmail)
	require.Equal(t, PlanCodePlus, state.ActivePlan)
	require.Equal(t, "sub_match_1", state.SubscriptionID)
}

func TestSubscriptionStateRepositoryGetBySubscriptionIDReturnsNotFound(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	testContext := context.Background()
	require.NoError(t, Migrate(testContext, database))

	repository := NewSubscriptionStateRepository(database)
	state, found, stateErr := repository.GetBySubscriptionID(testContext, ProviderCodePaddle, "sub_missing")
	require.NoError(t, stateErr)
	require.False(t, found)
	require.Equal(t, SubscriptionState{}, state)
}

func TestSubscriptionStateRepositoryUpsertRejectsStaleEvent(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	testContext := context.Background()
	require.NoError(t, Migrate(testContext, database))

	repository := NewSubscriptionStateRepository(database)

	newerTime := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)
	olderTime := time.Date(2026, 3, 27, 11, 0, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(testContext, SubscriptionStateUpsertInput{
		ProviderCode:      ProviderCodePaddle,
		UserEmail:         "user@example.com",
		Status:            subscriptionStatusActive,
		ProviderStatus:    "active",
		ActivePlan:        PlanCodePro,
		SubscriptionID:    "sub_001",
		LastEventID:       "evt_newer",
		LastEventType:     "subscription.activated",
		EventOccurredAt:   newerTime,
		LastTransactionID: "txn_newer",
	}))

	require.NoError(t, repository.Upsert(testContext, SubscriptionStateUpsertInput{
		ProviderCode:      ProviderCodePaddle,
		UserEmail:         "user@example.com",
		Status:            subscriptionStatusInactive,
		ProviderStatus:    "canceled",
		ActivePlan:        "",
		SubscriptionID:    "sub_001",
		LastEventID:       "evt_older",
		LastEventType:     "subscription.canceled",
		EventOccurredAt:   olderTime,
		LastTransactionID: "txn_older",
	}))

	state, found, stateErr := repository.Get(testContext, ProviderCodePaddle, "user@example.com")
	require.NoError(t, stateErr)
	require.True(t, found)
	require.Equal(t, subscriptionStatusActive, state.Status)
	require.Equal(t, "active", state.ProviderStatus)
	require.Equal(t, PlanCodePro, state.ActivePlan)
	require.Equal(t, "evt_newer", state.LastEventID)
}

func newBillingSubscriptionStateTestDatabase(testingContext *testing.T) *gorm.DB {
	testingContext.Helper()

	database, databaseErr := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(testingContext, databaseErr)
	return database
}

// Coverage gap tests for subscription_state_repository.go

func TestMigrateNilContext(t *testing.T) {
	var nilCtx context.Context
	err := Migrate(nilCtx, nil)
	require.ErrorIs(t, err, ErrBillingSubscriptionStateRepositoryUnavailable)
}

func TestMigrateNilDatabase(t *testing.T) {
	err := Migrate(context.Background(), nil)
	require.ErrorIs(t, err, ErrBillingSubscriptionStateRepositoryUnavailable)
}

func TestSubscriptionStateRepositoryUpsertNilRepository(t *testing.T) {
	var repo *subscriptionStateRepository
	err := repo.Upsert(context.Background(), SubscriptionStateUpsertInput{})
	require.ErrorIs(t, err, ErrBillingSubscriptionStateRepositoryUnavailable)
}

func TestSubscriptionStateRepositoryUpsertEmptyProvider(t *testing.T) {
	repo := &subscriptionStateRepository{database: nil}
	err := repo.Upsert(context.Background(), SubscriptionStateUpsertInput{ProviderCode: "paddle", UserEmail: "user@example.com", Status: "active"})
	require.ErrorIs(t, err, ErrBillingSubscriptionStateRepositoryUnavailable)
}

func TestSubscriptionStateRepositoryUpsertInvalidStatus(t *testing.T) {
	repo := &subscriptionStateRepository{database: nil}
	err := repo.Upsert(context.Background(), SubscriptionStateUpsertInput{})
	require.ErrorIs(t, err, ErrBillingSubscriptionStateRepositoryUnavailable)
}

func TestSubscriptionStateRepositoryGetNilRepository(t *testing.T) {
	var repo *subscriptionStateRepository
	_, _, err := repo.Get(context.Background(), "paddle", "user@example.com")
	require.ErrorIs(t, err, ErrBillingSubscriptionStateRepositoryUnavailable)
}

func TestSubscriptionStateRepositoryGetEmptyProvider(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	require.NoError(t, Migrate(context.Background(), database))
	repo := NewSubscriptionStateRepository(database)
	_, _, err := repo.Get(context.Background(), "", "user@example.com")
	require.ErrorIs(t, err, ErrBillingSubscriptionStateProviderInvalid)
}

func TestSubscriptionStateRepositoryGetEmptyEmail(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	require.NoError(t, Migrate(context.Background(), database))
	repo := NewSubscriptionStateRepository(database)
	_, _, err := repo.Get(context.Background(), "paddle", "")
	require.ErrorIs(t, err, ErrBillingSubscriptionStateUserEmailInvalid)
}

func TestSubscriptionStateRepositoryGetBySubscriptionIDNilRepository(t *testing.T) {
	var repo *subscriptionStateRepository
	_, _, err := repo.GetBySubscriptionID(context.Background(), "paddle", "sub_123")
	require.ErrorIs(t, err, ErrBillingSubscriptionStateRepositoryUnavailable)
}

func TestSubscriptionStateRepositoryGetBySubscriptionIDEmptyProvider(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	require.NoError(t, Migrate(context.Background(), database))
	repo := NewSubscriptionStateRepository(database)
	_, _, err := repo.GetBySubscriptionID(context.Background(), "", "sub_123")
	require.ErrorIs(t, err, ErrBillingSubscriptionStateProviderInvalid)
}

func TestSubscriptionStateRepositoryGetBySubscriptionIDEmptyID(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	require.NoError(t, Migrate(context.Background(), database))
	repo := NewSubscriptionStateRepository(database)
	_, _, err := repo.GetBySubscriptionID(context.Background(), "paddle", "")
	require.ErrorIs(t, err, ErrBillingSubscriptionStateSubscriptionIDInvalid)
}

func TestSubscriptionStateRepositoryUpsertValidation(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	require.NoError(t, Migrate(context.Background(), database))
	repo := NewSubscriptionStateRepository(database)

	// empty provider
	err := repo.Upsert(context.Background(), SubscriptionStateUpsertInput{
		UserEmail: "user@example.com",
		Status:    subscriptionStatusActive,
	})
	require.ErrorIs(t, err, ErrBillingSubscriptionStateProviderInvalid)

	// empty email
	err = repo.Upsert(context.Background(), SubscriptionStateUpsertInput{
		ProviderCode: "paddle",
		Status:       subscriptionStatusActive,
	})
	require.ErrorIs(t, err, ErrBillingSubscriptionStateUserEmailInvalid)

	// empty status
	err = repo.Upsert(context.Background(), SubscriptionStateUpsertInput{
		ProviderCode: "paddle",
		UserEmail:    "user@example.com",
	})
	require.ErrorIs(t, err, ErrBillingSubscriptionStateStatusInvalid)

	// invalid status value
	err = repo.Upsert(context.Background(), SubscriptionStateUpsertInput{
		ProviderCode: "paddle",
		UserEmail:    "user@example.com",
		Status:       "invalid_status",
	})
	require.ErrorIs(t, err, ErrBillingSubscriptionStateStatusInvalid)
}

func TestSubscriptionStateRepositoryGetNotFound(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	require.NoError(t, Migrate(context.Background(), database))
	repo := NewSubscriptionStateRepository(database)
	state, found, err := repo.Get(context.Background(), "paddle", "noone@example.com")
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, SubscriptionState{}, state)
}

func TestSubscriptionStateRepositoryGetQueryError(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	require.NoError(t, Migrate(context.Background(), database))
	repo := NewSubscriptionStateRepository(database)
	sqlDB, sqlDBErr := database.DB()
	require.NoError(t, sqlDBErr)
	require.NoError(t, sqlDB.Close())
	_, _, err := repo.Get(context.Background(), "paddle", "user@example.com")
	require.Error(t, err)
}

func TestSubscriptionStateRepositoryGetBySubscriptionIDQueryError(t *testing.T) {
	database := newBillingSubscriptionStateTestDatabase(t)
	require.NoError(t, Migrate(context.Background(), database))
	repo := NewSubscriptionStateRepository(database)
	sqlDB, sqlDBErr := database.DB()
	require.NoError(t, sqlDBErr)
	require.NoError(t, sqlDB.Close())
	_, _, err := repo.GetBySubscriptionID(context.Background(), "paddle", "sub_123")
	require.Error(t, err)
}

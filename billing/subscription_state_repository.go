package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	billingSubscriptionStateTableName = "billing_subscription_states"
)

var (
	ErrBillingSubscriptionStateRepositoryUnavailable = errors.New("billing.subscription_state.repository.unavailable")
	ErrBillingSubscriptionStateProviderInvalid       = errors.New("billing.subscription_state.provider.invalid")
	ErrBillingSubscriptionStateUserEmailInvalid      = errors.New("billing.subscription_state.user_email.invalid")
	ErrBillingSubscriptionStateSubscriptionIDInvalid = errors.New("billing.subscription_state.subscription_id.invalid")
	ErrBillingSubscriptionStateStatusInvalid         = errors.New("billing.subscription_state.status.invalid")
)

type billingSubscriptionStateRecord struct {
	ID                  uint   `gorm:"primaryKey"`
	ProviderCode        string `gorm:"size:32;index:idx_billing_subscription_user_provider,unique;not null"`
	UserEmail           string `gorm:"size:320;index:idx_billing_subscription_user_provider,unique;not null"`
	Status              string `gorm:"size:32;not null"`
	ProviderStatus      string `gorm:"size:64"`
	ActivePlan          string `gorm:"size:64"`
	SubscriptionID      string `gorm:"size:64;index:idx_billing_subscription_provider_subscription_id"`
	NextBillingAt       *time.Time
	LastEventID         string `gorm:"size:64"`
	LastEventType       string `gorm:"size:64"`
	LastEventOccurredAt *time.Time
	LastTransactionID   string `gorm:"size:64"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (billingSubscriptionStateRecord) TableName() string {
	return billingSubscriptionStateTableName
}

type SubscriptionState struct {
	ProviderCode        string
	UserEmail           string
	Status              string
	ProviderStatus      string
	ActivePlan          string
	SubscriptionID      string
	NextBillingAt       time.Time
	LastEventID         string
	LastEventType       string
	LastEventOccurredAt time.Time
	LastTransactionID   string
	UpdatedAt           time.Time
}

type SubscriptionStateUpsertInput struct {
	ProviderCode      string
	UserEmail         string
	Status            string
	ProviderStatus    string
	ActivePlan        string
	SubscriptionID    string
	NextBillingAt     time.Time
	LastEventID       string
	LastEventType     string
	EventOccurredAt   time.Time
	LastTransactionID string
}

type SubscriptionStateRepository interface {
	Upsert(context.Context, SubscriptionStateUpsertInput) error
	Get(context.Context, string, string) (SubscriptionState, bool, error)
	GetBySubscriptionID(context.Context, string, string) (SubscriptionState, bool, error)
}

type subscriptionStateRepository struct {
	database *gorm.DB
}

func NewSubscriptionStateRepository(database *gorm.DB) SubscriptionStateRepository {
	return &subscriptionStateRepository{
		database: database,
	}
}

func Migrate(ctx context.Context, database *gorm.DB) error {
	if ctx == nil || database == nil {
		return ErrBillingSubscriptionStateRepositoryUnavailable
	}
	return database.WithContext(ctx).AutoMigrate(&billingSubscriptionStateRecord{})
}

func (repository *subscriptionStateRepository) Upsert(
	ctx context.Context,
	input SubscriptionStateUpsertInput,
) error {
	if repository == nil || repository.database == nil {
		return ErrBillingSubscriptionStateRepositoryUnavailable
	}

	normalizedProviderCode := strings.ToLower(strings.TrimSpace(input.ProviderCode))
	if normalizedProviderCode == "" {
		return ErrBillingSubscriptionStateProviderInvalid
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(input.UserEmail))
	if normalizedUserEmail == "" {
		return ErrBillingSubscriptionStateUserEmailInvalid
	}
	normalizedStatus := strings.ToLower(strings.TrimSpace(input.Status))
	if normalizedStatus == "" {
		return ErrBillingSubscriptionStateStatusInvalid
	}
	if normalizedStatus != subscriptionStatusActive && normalizedStatus != subscriptionStatusInactive {
		return fmt.Errorf("%w: %s", ErrBillingSubscriptionStateStatusInvalid, normalizedStatus)
	}
	normalizedActivePlan := strings.ToLower(strings.TrimSpace(input.ActivePlan))
	if normalizedStatus == subscriptionStatusInactive {
		normalizedActivePlan = ""
	}
	normalizedProviderStatus := strings.ToLower(strings.TrimSpace(input.ProviderStatus))
	var nextBillingAt *time.Time
	if !input.NextBillingAt.IsZero() {
		resolvedNextBillingAt := input.NextBillingAt.UTC()
		nextBillingAt = &resolvedNextBillingAt
	}
	var lastEventOccurredAt *time.Time
	if !input.EventOccurredAt.IsZero() {
		resolvedLastEventOccurredAt := input.EventOccurredAt.UTC()
		lastEventOccurredAt = &resolvedLastEventOccurredAt
	}

	stateRecord := billingSubscriptionStateRecord{
		ProviderCode:        normalizedProviderCode,
		UserEmail:           normalizedUserEmail,
		Status:              normalizedStatus,
		ProviderStatus:      normalizedProviderStatus,
		ActivePlan:          normalizedActivePlan,
		SubscriptionID:      strings.TrimSpace(input.SubscriptionID),
		NextBillingAt:       nextBillingAt,
		LastEventID:         strings.TrimSpace(input.LastEventID),
		LastEventType:       strings.TrimSpace(input.LastEventType),
		LastEventOccurredAt: lastEventOccurredAt,
		LastTransactionID:   strings.TrimSpace(input.LastTransactionID),
	}

	return repository.database.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "provider_code"},
				{Name: "user_email"},
			},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"status":                 stateRecord.Status,
				"provider_status":        stateRecord.ProviderStatus,
				"active_plan":            stateRecord.ActivePlan,
				"subscription_id":        stateRecord.SubscriptionID,
				"next_billing_at":        stateRecord.NextBillingAt,
				"last_event_id":          stateRecord.LastEventID,
				"last_event_type":        stateRecord.LastEventType,
				"last_event_occurred_at": stateRecord.LastEventOccurredAt,
				"last_transaction_id":    stateRecord.LastTransactionID,
				"updated_at":             time.Now().UTC(),
			}),
		}).Create(&stateRecord).Error
}

func (repository *subscriptionStateRepository) Get(
	ctx context.Context,
	providerCode string,
	userEmail string,
) (SubscriptionState, bool, error) {
	if repository == nil || repository.database == nil {
		return SubscriptionState{}, false, ErrBillingSubscriptionStateRepositoryUnavailable
	}

	normalizedProviderCode := strings.ToLower(strings.TrimSpace(providerCode))
	if normalizedProviderCode == "" {
		return SubscriptionState{}, false, ErrBillingSubscriptionStateProviderInvalid
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return SubscriptionState{}, false, ErrBillingSubscriptionStateUserEmailInvalid
	}

	stateRecord := billingSubscriptionStateRecord{}
	query := repository.database.WithContext(ctx).
		Where("provider_code = ? AND user_email = ?", normalizedProviderCode, normalizedUserEmail).
		Limit(1).
		Find(&stateRecord)
	if query.Error != nil {
		return SubscriptionState{}, false, query.Error
	}
	if query.RowsAffected == 0 {
		return SubscriptionState{}, false, nil
	}

	return mapSubscriptionStateRecord(stateRecord), true, nil
}

func (repository *subscriptionStateRepository) GetBySubscriptionID(
	ctx context.Context,
	providerCode string,
	subscriptionID string,
) (SubscriptionState, bool, error) {
	if repository == nil || repository.database == nil {
		return SubscriptionState{}, false, ErrBillingSubscriptionStateRepositoryUnavailable
	}
	normalizedProviderCode := strings.ToLower(strings.TrimSpace(providerCode))
	if normalizedProviderCode == "" {
		return SubscriptionState{}, false, ErrBillingSubscriptionStateProviderInvalid
	}
	normalizedSubscriptionID := strings.TrimSpace(subscriptionID)
	if normalizedSubscriptionID == "" {
		return SubscriptionState{}, false, ErrBillingSubscriptionStateSubscriptionIDInvalid
	}

	stateRecord := billingSubscriptionStateRecord{}
	query := repository.database.WithContext(ctx).
		Where("provider_code = ? AND subscription_id = ?", normalizedProviderCode, normalizedSubscriptionID).
		Order("updated_at DESC").
		Limit(1).
		Find(&stateRecord)
	if query.Error != nil {
		return SubscriptionState{}, false, query.Error
	}
	if query.RowsAffected == 0 {
		return SubscriptionState{}, false, nil
	}
	return mapSubscriptionStateRecord(stateRecord), true, nil
}

func mapSubscriptionStateRecord(stateRecord billingSubscriptionStateRecord) SubscriptionState {
	nextBillingAt := time.Time{}
	if stateRecord.NextBillingAt != nil {
		nextBillingAt = stateRecord.NextBillingAt.UTC()
	}
	lastEventOccurredAt := time.Time{}
	if stateRecord.LastEventOccurredAt != nil {
		lastEventOccurredAt = stateRecord.LastEventOccurredAt.UTC()
	}
	return SubscriptionState{
		ProviderCode:        stateRecord.ProviderCode,
		UserEmail:           stateRecord.UserEmail,
		Status:              stateRecord.Status,
		ProviderStatus:      stateRecord.ProviderStatus,
		ActivePlan:          stateRecord.ActivePlan,
		SubscriptionID:      stateRecord.SubscriptionID,
		NextBillingAt:       nextBillingAt,
		LastEventID:         stateRecord.LastEventID,
		LastEventType:       stateRecord.LastEventType,
		LastEventOccurredAt: lastEventOccurredAt,
		LastTransactionID:   stateRecord.LastTransactionID,
		UpdatedAt:           stateRecord.UpdatedAt,
	}
}

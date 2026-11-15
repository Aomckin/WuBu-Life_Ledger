package store

import (
	"context"

	"stockroom/internal/ledger"
)

// ItemsRepository is the independent Items write surface for the SQLite repository.
type ItemsRepository struct{ repository }

func (s *Store) Items() ItemsRepository { return s.ItemsRepository }

func (i ItemsRepository) RecordAcquisition(ctx context.Context, key string, input ledger.AcquisitionInput) (ledger.Purchase, bool, error) {
	return i.recordPurchase(ctx, key, "items:acquisition", input.PurchaseInput())
}

func (i ItemsRepository) GetAcquisition(ctx context.Context, id string) (ledger.Purchase, error) {
	return getAcquisition(ctx, i.db, id)
}

func (i ItemsRepository) AmendAcquisition(ctx context.Context, key, id string, p ledger.AcquisitionAmendment) (ledger.Purchase, bool, error) {
	return i.amendPurchase(ctx, key, id, ledger.PurchaseAmendment{PurchaseInput: p.PurchaseInput(), ExpectedUpdatedAt: p.ExpectedUpdatedAt})
}

func (i ItemsRepository) DeleteAcquisition(ctx context.Context, key, id string, p ledger.PurchaseDeletion) (ledger.PurchaseDeletionResult, bool, error) {
	return i.deletePurchase(ctx, key, id, p)
}

func (i ItemsRepository) Sell(ctx context.Context, key, id string, p ledger.SaleRecord) (ledger.DurableDetails, bool, error) {
	return i.durableAction(ctx, key, "items:durable", id, "sell", ledger.DurableActionInput{OccurredAt: p.OccurredAt, Source: p.Source, Note: p.Note})
}

func (i ItemsRepository) Return(ctx context.Context, key, id string, p ledger.InventoryInput) (ledger.InventoryResult, bool, error) {
	return i.InventoryAction(ctx, key, id, "RETURN", p)
}

// The reference scopes the receipt for callers coordinating multiple steps.
func (i ItemsRepository) RecordLinkedAcquisition(ctx context.Context, key, reference string, input ledger.AcquisitionInput) (ledger.Purchase, bool, error) {
	return i.recordPurchase(ctx, key, "items:linked-acquisition:"+reference, input.PurchaseInput())
}

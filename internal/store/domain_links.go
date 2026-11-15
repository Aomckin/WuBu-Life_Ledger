package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"stockroom/internal/ledger"
)

type LinksRepository struct{ repository }

func (s LinksRepository) CreateLink(ctx context.Context, key string, p ledger.LinkInput) (ledger.Link, bool, error) {
	var out ledger.Link
	replay, err := s.ledgerTx(ctx, key, "links:create", p, &out, func(tx *sql.Tx) error {
		var lookup string
		switch p.SourceType {
		case "ITEM_ACQUISITION":
			if p.Relation != "PURCHASE" && p.Relation != "REFUND" {
				return ledger.ErrInvalid
			}
			lookup = `SELECT EXISTS(SELECT 1 FROM purchase_events WHERE id=? AND deleted_at IS NULL)`
		case "MAINTENANCE":
			if p.Relation != "MAINTENANCE" && p.Relation != "REFUND" {
				return ledger.ErrInvalid
			}
			lookup = `SELECT EXISTS(SELECT 1 FROM maintenance_events WHERE id=?)`
		case "ITEM":
			if p.Relation != "SALE" && p.Relation != "RETURN" {
				return ledger.ErrInvalid
			}
			lookup = `SELECT EXISTS(SELECT 1 FROM items WHERE id=?)`
		default:
			return ledger.ErrInvalid
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, lookup, p.SourceID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM money_transactions WHERE id=? AND voided_at='')`, p.TransactionID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		out = ledger.Link{LinkInput: p}
		if err := insertLink(ctx, tx, p); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT id,created_at FROM domain_links WHERE source_type=? AND source_id=? AND transaction_id=? AND relation=?`, p.SourceType, p.SourceID, p.TransactionID, p.Relation).Scan(&out.ID, &out.CreatedAt)
	})
	return out, replay, err
}

func insertLink(ctx context.Context, tx *sql.Tx, p ledger.LinkInput) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO domain_links VALUES(?,?,?,?,?,?) ON CONFLICT(source_type,source_id,transaction_id,relation) DO NOTHING`, uuid.NewString(), p.SourceType, p.SourceID, p.TransactionID, p.Relation, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s LinksRepository) DeleteLink(ctx context.Context, key, id string) (map[string]bool, bool, error) {
	out := map[string]bool{"deleted": true}
	replay, err := s.ledgerTx(ctx, key, "links:delete:"+id, id, &out, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM domain_links WHERE id=?`, id)
		return err
	})
	return out, replay, err
}

func (s LinksRepository) DomainLinks(ctx context.Context, sourceType, sourceID, transactionID string, limit, offset int) ([]ledger.Link, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.id,l.source_type,l.source_id,l.transaction_id,l.relation,l.created_at,m.amount_cents
		FROM domain_links l LEFT JOIN money_transactions m ON m.id=l.transaction_id AND m.voided_at=''
		WHERE (?='' OR source_type=?) AND (?='' OR source_id=?) AND (?='' OR transaction_id=?) ORDER BY l.rowid DESC LIMIT ? OFFSET ?`, sourceType, sourceType, sourceID, sourceID, transactionID, transactionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ledger.Link{}
	for rows.Next() {
		var l ledger.Link
		if err = rows.Scan(&l.ID, &l.SourceType, &l.SourceID, &l.TransactionID, &l.Relation, &l.CreatedAt, &l.TransactionAmount); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

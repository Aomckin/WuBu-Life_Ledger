package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"stockroom/internal/ledger"
)

// ledgerTx obtains the SQLite writer lock before any reads. Idempotency and the
// event mutation therefore have a single serializable commit boundary.
func (s repository) ledgerTx(ctx context.Context, key, operation string, request any, result any, fn func(*sql.Tx) error) (bool, error) {
	if len(key) > 200 || strings.TrimSpace(key) != key {
		return false, fmt.Errorf("%w: invalid Idempotency-Key", ledger.ErrInvalid)
	}
	data, err := ledger.CanonicalRequest(request)
	if err != nil {
		return false, err
	}
	hash := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(hash[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE ledger_lock SET revision=revision+1 WHERE id=1`); err != nil {
		return false, err
	}
	if key != "" {
		var op, fp, response string
		err = tx.QueryRowContext(ctx, `SELECT operation,request_hash,response_json FROM idempotency_keys WHERE key=?`, key).Scan(&op, &fp, &response)
		if err == nil {
			if op != operation || fp != fingerprint {
				return false, ledger.ErrIdempotency
			}
			if err = json.Unmarshal([]byte(response), result); err != nil {
				return false, err
			}
			return true, tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	if err = fn(tx); err != nil {
		return false, err
	}
	if key != "" {
		b, e := json.Marshal(result)
		if e != nil {
			return false, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys VALUES(?,?,?,?,?)`, key, operation, fingerprint, string(b), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return false, err
		}
	}
	return false, tx.Commit()
}

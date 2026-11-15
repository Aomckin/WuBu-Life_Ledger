package store

import (
	"context"
	"os"
	"path/filepath"
	"stockroom/internal/ledger"
	"stockroom/internal/statement"
	"testing"
)

// The public native examples must reach the ledger adapter, not merely parse
// inside a bank package that the product never calls.
func TestNativeBankTemplatesPreviewCommitAndCurrencyBoundary(t *testing.T) {
	for _, template := range statement.Templates {
		if template.Group != "bank" {
			continue
		}
		t.Run(template.ID, func(t *testing.T) {
			totalNew := 0
			e := filepath.WalkDir(filepath.Join("..", "statement", "testdata", template.ID), func(path string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if d.IsDir() {
					return nil
				}
				s := ledgerDB(t)
				a := moneyAccount(t, s, "native-fixture", "BANK", 10000000)
				file, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				p := ledger.StatementInput{Filename: filepath.Base(path), File: file, AccountID: a.ID}
				out := commitNative(t, s, "fixture", ""+template.ID, p)
				if out.Batch.RowCount == 0 || out.Batch.RowCount != out.Batch.NewTransactions+out.Batch.MatchedExisting+out.Batch.Duplicates+out.Batch.Conflicts+out.Batch.Skipped {
					t.Fatal("invalid source accounting", out.Batch)
				}
				for _, r := range out.Rows {
					if r.Status == "CREATE_NEW" && r.Currency != "CNY" {
						t.Fatalf("foreign row posted as CNY: %+v", r)
					}
				}
				_, original, e := s.StatementFile(context.Background(), out.Batch.ID)
				if e != nil || string(original) != string(file) {
					t.Fatal("original changed", e)
				}
				totalNew += out.Batch.NewTransactions
				reconcileAll(t, s)
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			if template.ID != "bmo" && template.ID != "td" && totalNew == 0 {
				t.Fatal("no CNY bank sample reaches money facts")
			}
			t.Logf("%s: created %d CNY facts; original files retained", template.ID, totalNew)
		})
	}
}

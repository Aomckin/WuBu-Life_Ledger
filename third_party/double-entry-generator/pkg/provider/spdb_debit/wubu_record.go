// Copyright double-entry-generator contributors. Licensed under Apache-2.0.
// Wubu modification, 2026-11-03: expose existing single-record parsing so one
// malformed XLS row cannot discard other valid records from the same statement.
package spdb_debit

import "github.com/deb-sig/double-entry-generator/v2/pkg/ir"

func (sd *SpdbDebit) TranslateRecord(row []string) (*ir.IR, error) {
	if err := sd.translateToOrders(row); err != nil {
		return nil, err
	}
	return sd.convertToIR()
}

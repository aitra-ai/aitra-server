// Package metering is the real TokenSource: it derives a deployment's output
// (completion) tokens for a window from the EXISTING account_metering ledger — no
// new tables or columns. The ledger's Value column holds total tokens; the
// output-token count lives in the Extra JSON (completion_token_num), so this
// package sums that field, keyed by customer_id (= deployment SvcName). Reads are
// out-of-band; the inference request path is untouched.
package metering

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/types"
	"github.com/aitra-ai/aitra-server/energy/aggregator"
)

// Source implements aggregator.TokenSource over account_metering.
var _ aggregator.TokenSource = (*Source)(nil)

// tokenRow is the minimal projection of an account_metering row needed here.
type tokenRow struct {
	Extra string
}

// rowLister loads token metering rows for a deployment (customer_id) in a window.
// Splitting it out keeps the JSON parsing/summing unit-testable without a DB.
type rowLister interface {
	ListTokenRows(ctx context.Context, customerID string, start, end time.Time) ([]tokenRow, error)
}

// Source sums output tokens from the metering ledger.
type Source struct {
	rows rowLister
}

// NewSourceWithDB builds a TokenSource backed by the account_metering table.
func NewSourceWithDB(db *database.DB) *Source {
	return &Source{rows: &dbRowLister{db: db}}
}

// Name identifies the provenance recorded on every measurement record.
func (s *Source) Name() string { return "account_metering" }

// WindowTokens sums output (completion) tokens for meteringKey over [start, end).
// A malformed Extra on one row is skipped rather than failing the whole window.
func (s *Source) WindowTokens(ctx context.Context, meteringKey string, start, end time.Time) (uint64, error) {
	if meteringKey == "" {
		return 0, nil
	}
	rows, err := s.rows.ListTokenRows(ctx, meteringKey, start, end)
	if err != nil {
		return 0, err
	}
	var total uint64
	for _, r := range rows {
		out, err := outputTokens(r.Extra)
		if err != nil {
			continue
		}
		total += out
	}
	return total, nil
}

// outputTokens extracts completion_token_num (output tokens) from the metering
// Extra JSON. The aigateway records prompt/completion counts there while the
// ledger Value carries the prompt+completion total — J/token needs output only.
func outputTokens(extra string) (uint64, error) {
	if extra == "" {
		return 0, nil
	}
	var e struct {
		CompletionTokenNum string `json:"completion_token_num"`
	}
	if err := json.Unmarshal([]byte(extra), &e); err != nil {
		return 0, fmt.Errorf("parse metering extra: %w", err)
	}
	if e.CompletionTokenNum == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(e.CompletionTokenNum, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse completion_token_num %q: %w", e.CompletionTokenNum, err)
	}
	return n, nil
}

// dbRowLister reads from the account_metering table, reusing the existing schema.
type dbRowLister struct {
	db *database.DB
}

func (l *dbRowLister) ListTokenRows(ctx context.Context, customerID string, start, end time.Time) ([]tokenRow, error) {
	var ams []database.AccountMetering
	err := l.db.Operator.Core.NewSelect().
		Model(&ams).
		Column("extra").
		Where("customer_id = ?", customerID).
		Where("value_type = ?", types.TokenNumberType).
		Where("recorded_at >= ? and recorded_at < ?", start, end).
		Scan(ctx, &ams)
	if err != nil {
		return nil, fmt.Errorf("list token metering rows: %w", err)
	}
	rows := make([]tokenRow, len(ams))
	for i := range ams {
		rows[i] = tokenRow{Extra: ams[i].Extra}
	}
	return rows, nil
}

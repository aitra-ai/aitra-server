// Package sqlite is the default StorageBackend for aitra-meter: a pure-Go,
// CGO-free SQLite store (modernc.org/sqlite) for MeasurementRecords and the
// namespace chargeback rollup. It is self-contained so the OSS distribution can
// persist and bill without a database server; the platform can later register a
// Postgres-backed StorageBackend behind the same interface.
//
// The energy aggregator is a single low-rate writer (one batch per measurement
// window), so the connection pool is pinned to one connection: writes serialize,
// SQLite's single-writer limit is never contended, and an in-memory test DSN
// behaves as one database rather than a per-connection scratch space.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" sql driver (no CGO)

	"opencsg.com/csghub-server/energy"
)

// Backend is a SQLite-backed StorageBackend.
type Backend struct {
	db *sql.DB
}

// compile-time check that Backend satisfies the contract.
var _ energy.StorageBackend = (*Backend)(nil)

func init() {
	energy.RegisterStorage("sqlite", func(config map[string]string) (energy.StorageBackend, error) {
		return New(config["path"])
	})
}

// New opens (creating if absent) a SQLite database at path and ensures the
// schema exists. An empty path or ":memory:" yields a shared in-memory database,
// used by tests and the no-hardware smoke path.
func New(path string) (*Backend, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", path, err)
	}
	// Single writer: serialize access and keep the in-memory DSN to one DB.
	db.SetMaxOpenConns(1)

	b := &Backend{db: db}
	if err := b.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return b, nil
}

// dsn turns a filesystem path into a modernc.org/sqlite DSN. File-backed stores
// get WAL plus a busy timeout so a stalled read never wedges the writer; an empty
// or ":memory:" path becomes a shared-cache in-memory database.
func dsn(path string) string {
	if path == "" || path == ":memory:" {
		return "file::memory:?cache=shared"
	}
	return "file:" + path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
}

// schema is the MeasurementRecord table plus the index that makes the
// time-ranged chargeback rollup a bounded scan even at tens of thousands of rows
// (AC-11: 30-day query under 10s).
const schema = `
CREATE TABLE IF NOT EXISTS measurement_records (
	timestamp_unix_ms  INTEGER NOT NULL,
	cluster            TEXT    NOT NULL DEFAULT '',
	node               TEXT    NOT NULL DEFAULT '',
	namespace          TEXT    NOT NULL DEFAULT '',
	workload           TEXT    NOT NULL DEFAULT '',
	model              TEXT    NOT NULL DEFAULT '',
	hardware           TEXT    NOT NULL DEFAULT '',
	precision          TEXT    NOT NULL DEFAULT '',
	team               TEXT    NOT NULL DEFAULT '',
	cost_centre        TEXT    NOT NULL DEFAULT '',
	energy_joules      REAL    NOT NULL DEFAULT 0,
	output_tokens      INTEGER NOT NULL DEFAULT 0,
	j_per_token        REAL    NOT NULL DEFAULT 0,
	calibration_tier   TEXT    NOT NULL DEFAULT '',
	attribution_method TEXT    NOT NULL DEFAULT '',
	cv                 REAL    NOT NULL DEFAULT 0,
	stable             INTEGER NOT NULL DEFAULT 0,
	energy_provider    TEXT    NOT NULL DEFAULT '',
	inference_provider TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_mr_ns_ts ON measurement_records(namespace, timestamp_unix_ms);
CREATE INDEX IF NOT EXISTS idx_mr_ts    ON measurement_records(timestamp_unix_ms);
`

func (b *Backend) ensureSchema(ctx context.Context) error {
	if _, err := b.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("sqlite: ensure schema: %w", err)
	}
	return nil
}

const insertColumns = `(timestamp_unix_ms, cluster, node, namespace, workload, model, hardware, precision,
	team, cost_centre, energy_joules, output_tokens, j_per_token, calibration_tier,
	attribution_method, cv, stable, energy_provider, inference_provider)`

const valuePlaceholders = "(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)"

// columnsPerRow is the number of bound parameters per inserted row. maxBatchRows
// keeps a single INSERT well under SQLite's bound-variable limit
// (SQLITE_MAX_VARIABLE_NUMBER) so large cycles are chunked, not rejected.
const (
	columnsPerRow = 19
	maxBatchRows  = 500
)

// recordArgs flattens a record into positional args matching insertColumns.
func recordArgs(r energy.MeasurementRecord) []any {
	stable := 0
	if r.Stable {
		stable = 1
	}
	return []any{
		r.TimestampUnixMs, r.Cluster, r.Node, r.Namespace, r.Workload, r.Model, r.Hardware, r.Precision,
		r.Team, r.CostCentre, r.EnergyJoules, int64(r.OutputTokens), r.JPerToken, string(r.CalibrationTier),
		string(r.AttributionMethod), r.CV, stable, r.EnergyProvider, r.InferenceProvider,
	}
}

// Write inserts a single record.
func (b *Backend) Write(ctx context.Context, record energy.MeasurementRecord) error {
	_, err := b.db.ExecContext(ctx,
		"INSERT INTO measurement_records "+insertColumns+" VALUES "+valuePlaceholders,
		recordArgs(record)...)
	if err != nil {
		return fmt.Errorf("sqlite: write record: %w", err)
	}
	return nil
}

// WriteBatch inserts a batch of records in one transaction, so a cycle's records
// land atomically. An empty batch is a no-op.
func (b *Backend) WriteBatch(ctx context.Context, records []energy.MeasurementRecord) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin batch: %w", err)
	}
	// One transaction (atomic batch), chunked into multi-row INSERTs so each
	// statement stays under SQLite's bound-variable limit.
	for start := 0; start < len(records); start += maxBatchRows {
		end := start + maxBatchRows
		if end > len(records) {
			end = len(records)
		}
		if err := insertChunk(ctx, tx, records[start:end]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: write batch: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit batch: %w", err)
	}
	return nil
}

// insertChunk writes one multi-row INSERT for up to maxBatchRows records.
func insertChunk(ctx context.Context, tx *sql.Tx, records []energy.MeasurementRecord) error {
	var sb strings.Builder
	sb.WriteString("INSERT INTO measurement_records " + insertColumns + " VALUES ")
	args := make([]any, 0, len(records)*columnsPerRow)
	for i, r := range records {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(valuePlaceholders)
		args = append(args, recordArgs(r)...)
	}
	_, err := tx.ExecContext(ctx, sb.String(), args...)
	return err
}

// QueryChargeback aggregates records in [Start, End) by namespace. Energy and
// tokens are summed; a namespace is reported proportional if any of its windows
// used proportional attribution, so proportional attribution is never hidden in
// the rollup. PUE (defaulting to 1.0) is applied to derive kWh.
func (b *Backend) QueryChargeback(ctx context.Context, q energy.ChargebackQuery) ([]energy.NamespaceCharge, error) {
	pue := q.PUE
	if pue <= 0 {
		pue = 1.0
	}

	rows, err := b.db.QueryContext(ctx, `
		SELECT namespace,
		       SUM(energy_joules)  AS energy,
		       SUM(output_tokens)  AS tokens,
		       MAX(CASE WHEN attribution_method = ? THEN 1 ELSE 0 END) AS any_proportional
		FROM measurement_records
		WHERE timestamp_unix_ms >= ? AND timestamp_unix_ms < ?
		GROUP BY namespace
		ORDER BY namespace`,
		string(energy.AttributionProportional), q.Start.UnixMilli(), q.End.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("sqlite: query chargeback: %w", err)
	}
	defer rows.Close()

	var out []energy.NamespaceCharge
	for rows.Next() {
		var (
			ns      string
			energyJ float64
			tokens  int64
			anyProp int
		)
		if err := rows.Scan(&ns, &energyJ, &tokens, &anyProp); err != nil {
			return nil, fmt.Errorf("sqlite: scan chargeback row: %w", err)
		}
		method := energy.AttributionDirect
		if anyProp == 1 {
			method = energy.AttributionProportional
		}
		out = append(out, energy.NamespaceCharge{
			Namespace:         ns,
			EnergyJoules:      energyJ,
			EnergyKWhWithPUE:  energy.JoulesToKWh(energyJ) * pue,
			OutputTokens:      uint64(tokens),
			AttributionMethod: method,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate chargeback rows: %w", err)
	}
	return out, nil
}

// RetentionPurge deletes records older than before and returns the number
// removed. It bounds on-disk growth; the service calls it periodically. It is not
// part of the StorageBackend interface because retention is a store-local policy.
func (b *Backend) RetentionPurge(ctx context.Context, before time.Time) (int64, error) {
	res, err := b.db.ExecContext(ctx,
		"DELETE FROM measurement_records WHERE timestamp_unix_ms < ?", before.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("sqlite: retention purge: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Close releases the database handle.
func (b *Backend) Close() error {
	return b.db.Close()
}

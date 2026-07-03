// Package memory is an in-memory StorageBackend used for tests and the
// docker-compose no-hardware smoke path. It is not for production use.
package memory

import (
	"context"
	"sync"

	"opencsg.com/csghub-server/energy"
)

// Backend keeps records in a slice guarded by a mutex.
type Backend struct {
	mu      sync.Mutex
	records []energy.MeasurementRecord
}

// compile-time check that Backend satisfies the contract.
var _ energy.StorageBackend = (*Backend)(nil)

// New returns an empty in-memory backend.
func New() *Backend { return &Backend{} }

func init() {
	energy.RegisterStorage("memory", func(map[string]string) (energy.StorageBackend, error) {
		return New(), nil
	})
}

// Write appends a single record.
func (b *Backend) Write(_ context.Context, record energy.MeasurementRecord) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = append(b.records, record)
	return nil
}

// WriteBatch appends a batch of records.
func (b *Backend) WriteBatch(_ context.Context, records []energy.MeasurementRecord) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = append(b.records, records...)
	return nil
}

// QueryChargeback aggregates records in [Start, End) by namespace. A namespace
// is reported as proportional if any of its windows used proportional
// attribution, so proportional is never hidden in the rollup.
func (b *Backend) QueryChargeback(_ context.Context, q energy.ChargebackQuery) ([]energy.NamespaceCharge, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	startMs := q.Start.UnixMilli()
	endMs := q.End.UnixMilli()
	pue := q.PUE
	if pue <= 0 {
		pue = 1.0
	}

	byNS := map[string]*energy.NamespaceCharge{}
	for _, r := range b.records {
		if r.TimestampUnixMs < startMs || r.TimestampUnixMs >= endMs {
			continue
		}
		c, ok := byNS[r.Namespace]
		if !ok {
			c = &energy.NamespaceCharge{
				Namespace:         r.Namespace,
				AttributionMethod: energy.AttributionDirect,
			}
			byNS[r.Namespace] = c
		}
		c.EnergyJoules += r.EnergyJoules
		c.OutputTokens += r.OutputTokens
		if r.AttributionMethod == energy.AttributionProportional {
			c.AttributionMethod = energy.AttributionProportional
		}
	}

	out := make([]energy.NamespaceCharge, 0, len(byNS))
	for _, c := range byNS {
		c.EnergyKWhWithPUE = energy.JoulesToKWh(c.EnergyJoules) * pue
		out = append(out, *c)
	}
	return out, nil
}

// Close is a no-op for the in-memory backend.
func (b *Backend) Close() error { return nil }

// Records returns a copy of stored records, for test assertions.
func (b *Backend) Records() []energy.MeasurementRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]energy.MeasurementRecord, len(b.records))
	copy(out, b.records)
	return out
}

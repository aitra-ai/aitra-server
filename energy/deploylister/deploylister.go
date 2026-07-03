// Package deploylister is the real DeploymentLister: it lists running inference
// deployments from the EXISTING deploy table and projects each into the minimal
// view the aggregator needs. No new tables or columns. Read-only and out-of-band.
//
// The running-status and inference-type codes are injected by the caller rather
// than imported, so this package avoids the heavy root "common" dependency.
package deploylister

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/energy/aggregator"
)

// Lister implements aggregator.DeploymentLister.
var _ aggregator.DeploymentLister = (*Lister)(nil)

// deployReader loads running inference deployments. Split out so the field
// mapping is unit-testable without a database.
type deployReader interface {
	ListRunningInference(ctx context.Context) ([]database.Deploy, error)
}

// Lister maps deploy records into aggregator.Deployment values.
type Lister struct {
	reader deployReader
}

// NewListerWithDB builds a Lister backed by the deploy table. runningStatus and
// inferenceTypes are injected (common.Running, types.InferenceType /
// types.ServerlessType) to keep this package out of the root "common" import.
func NewListerWithDB(db *database.DB, runningStatus int, inferenceTypes []int64) *Lister {
	return &Lister{reader: &dbReader{db: db, runningStatus: runningStatus, inferenceTypes: inferenceTypes}}
}

// ListRunning returns the running inference/serverless deployments to measure.
func (l *Lister) ListRunning(ctx context.Context) ([]aggregator.Deployment, error) {
	deploys, err := l.reader.ListRunningInference(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]aggregator.Deployment, 0, len(deploys))
	for _, d := range deploys {
		out = append(out, toDeployment(d))
	}
	return out, nil
}

// toDeployment projects a deploy record into the aggregator's view.
//
// Energy attribution uses a pod-name regex derived from SvcName (Knative pods are
// named "<svc>-<rev>-deployment-<hash>"), NOT the node — a deployment may have
// replicas on several nodes and filtering by one node would undercount. The node
// is still recorded for the CV/idle series labels.
func toDeployment(d database.Deploy) aggregator.Deployment {
	return aggregator.Deployment{
		ID:          d.ID,
		Cluster:     d.ClusterID,
		Namespace:   d.OwnerNamespace, // chargeback identity (repo owner)
		Node:        d.ClusterNode,
		Model:       d.GitPath, // model identity; TODO: prefer a clean model name
		Hardware:    d.SKU,     // best-effort tier label; TODO: derive h100/910b
		MeteringKey: d.SvcName, // account_metering customer_id
		Scope: aggregator.Scope{
			PodSelector: podSelector(d.SvcName),
		},
	}
}

// podSelector builds an anchored prefix regex for a deployment's Knative pods.
func podSelector(svcName string) string {
	if svcName == "" {
		return ""
	}
	return "^" + svcName + "-"
}

// dbReader reads running inference deployments from the deploy table, mirroring
// the store's own running-inference predicate (minus the per-user visibility filter).
type dbReader struct {
	db             *database.DB
	runningStatus  int
	inferenceTypes []int64
}

func (r *dbReader) ListRunningInference(ctx context.Context) ([]database.Deploy, error) {
	var deploys []database.Deploy
	err := r.db.Operator.Core.NewSelect().
		Model(&deploys).
		Where("status = ? and type in (?)", r.runningStatus, bun.In(r.inferenceTypes)).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return []database.Deploy{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list running inference deployments: %w", err)
	}
	return deploys, nil
}

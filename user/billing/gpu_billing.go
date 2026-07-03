package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
)

// StartGPUBillingJob runs a background goroutine that bills running deployments every 5 minutes.
// It deducts credits from user_credits proportional to running time.
func StartGPUBillingJob(cfg *config.Config) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		// Run once immediately on startup
		runBillingCycle(cfg)
		for range ticker.C {
			runBillingCycle(cfg)
		}
	}()
}

func runBillingCycle(cfg *config.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	deployStore := database.NewDeploymentBillingStore()
	creditStore := database.NewUserCreditStore()

	running, err := deployStore.ListRunning(ctx)
	if err != nil {
		slog.Error("gpu billing: failed to list running deployments", "error", err)
		return
	}

	now := time.Now()
	for _, d := range running {
		// Calculate billable duration since last billing
		elapsed := now.Sub(d.LastBilledAt)
		if elapsed < 1*time.Minute {
			continue // skip if less than 1 minute since last billing
		}

		amount := d.PricePerHour * elapsed.Hours()
		if amount <= 0 {
			continue
		}

		// Check user balance
		balance, err := creditStore.Balance(ctx, d.UserID)
		if err != nil {
			slog.Warn("gpu billing: failed to check balance", "user_id", d.UserID, "error", err)
			continue
		}

		if balance <= 0 {
			// Auto-stop deployment when balance is exhausted
			slog.Info("gpu billing: auto-stopping deployment due to zero balance",
				"deploy_id", d.ID, "username", d.Username)
			if err := deployStore.Stop(ctx, d.ID); err != nil {
				slog.Error("gpu billing: failed to auto-stop deployment", "error", err)
			}
			continue
		}

		// Deduct from credits
		err = creditStore.Create(ctx, &database.UserCredit{
			UserID:    d.UserID,
			Username:  d.Username,
			AmountUSD: -amount,
			Note:      fmt.Sprintf("GPU deployment: %s (%s) — %.4f hrs", d.DeployName, d.SkuName, elapsed.Hours()),
			GrantedBy: "system",
			CreatedAt: now,
		})
		if err != nil {
			slog.Error("gpu billing: failed to deduct credit", "deploy_id", d.ID, "error", err)
			continue
		}

		// Update billing record
		if err := deployStore.UpdateBilling(ctx, d.ID, amount, now); err != nil {
			slog.Error("gpu billing: failed to update billing record", "deploy_id", d.ID, "error", err)
		}

		slog.Info("gpu billing: charged",
			"username", d.Username,
			"deploy", d.DeployName,
			"sku", d.SkuName,
			"amount_usd", fmt.Sprintf("$%.6f", amount),
			"elapsed_hrs", fmt.Sprintf("%.4f", elapsed.Hours()))
	}
}

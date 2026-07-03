package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/webhook"
	"opencsg.com/csghub-server/api/httpbase"
	"opencsg.com/csghub-server/builder/store/database"
	"opencsg.com/csghub-server/common/config"
)

type StripeHandler struct {
	webhookSecret string
	successURL    string
	cancelURL     string
	userStore     database.UserStore
}

func NewStripeHandler(cfg *config.Config) (*StripeHandler, error) {
	secretKey := cfg.Payment.StripeSecretKey
	if secretKey == "" {
		return nil, fmt.Errorf("STRIPE_SECRET_KEY not configured")
	}
	stripe.Key = secretKey

	// Default URLs — can be overridden via env
	successURL := "http://localhost:5173/app/billing?recharge=success"
	cancelURL := "http://localhost:5173/app/billing?recharge=cancelled"

	return &StripeHandler{
		webhookSecret: cfg.Payment.StripeWebhookSecret,
		successURL:    successURL,
		cancelURL:     cancelURL,
		userStore:     database.NewUserStore(),
	}, nil
}

// CreateCheckoutSession creates a Stripe Checkout session for the user to pay.
// POST /api/v1/user/recharge/checkout
// Body: { "amount_usd": 10.00 }
func (h *StripeHandler) CreateCheckoutSession(c *gin.Context) {
	username := httpbase.GetCurrentUser(c)
	if username == "" {
		httpbase.UnauthorizedError(c, fmt.Errorf("login required"))
		return
	}

	var body struct {
		AmountUSD float64 `json:"amount_usd"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		httpbase.BadRequest(c, "invalid request body")
		return
	}

	// Validate amount range: $1 - $500
	if body.AmountUSD < 1 || body.AmountUSD > 500 {
		httpbase.BadRequest(c, "amount must be between $1 and $500")
		return
	}

	// Convert to cents
	amountCents := int64(body.AmountUSD * 100)

	params := &stripe.CheckoutSessionParams{
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		Mode:               stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:         stripe.String(h.successURL),
		CancelURL:          stripe.String(h.cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String("usd"),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name:        stripe.String("aitra Platform Credit Top-Up"),
						Description: stripe.String(fmt.Sprintf("$%.2f credit for %s", body.AmountUSD, username)),
					},
					UnitAmount: stripe.Int64(amountCents),
				},
				Quantity: stripe.Int64(1),
			},
		},
		ClientReferenceID: stripe.String(username),
	}
	params.AddMetadata("username", username)
	params.AddMetadata("amount_usd", fmt.Sprintf("%.2f", body.AmountUSD))

	s, err := session.New(params)
	if err != nil {
		slog.Error("failed to create Stripe checkout session", "error", err, "username", username)
		httpbase.ServerError(c, fmt.Errorf("failed to create checkout session: %w", err))
		return
	}

	slog.Info("Stripe checkout session created", "session_id", s.ID, "username", username, "amount_usd", body.AmountUSD)
	httpbase.OK(c, gin.H{
		"checkout_url": s.URL,
		"session_id":   s.ID,
	})
}

// HandleWebhook handles Stripe webhook events.
// POST /api/v1/webhook/stripe
// No auth required — verified by Stripe signature.
func (h *StripeHandler) HandleWebhook(c *gin.Context) {
	const maxBodySize = 65536
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBodySize))
	if err != nil {
		slog.Error("failed to read webhook body", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Verify signature
	sigHeader := c.GetHeader("Stripe-Signature")
	event, err := webhook.ConstructEvent(body, sigHeader, h.webhookSecret)
	if err != nil {
		slog.Warn("Stripe webhook signature verification failed", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid signature"})
		return
	}

	switch event.Type {
	case "checkout.session.completed":
		h.handleCheckoutCompleted(c, event)
	default:
		slog.Debug("unhandled Stripe event type", "type", event.Type)
		c.JSON(http.StatusOK, gin.H{"received": true})
	}
}

func (h *StripeHandler) handleCheckoutCompleted(c *gin.Context, event stripe.Event) {
	var cs stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &cs); err != nil {
		slog.Error("failed to unmarshal checkout session", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event data"})
		return
	}

	username := cs.ClientReferenceID
	if username == "" {
		// Fallback to metadata
		if cs.Metadata != nil {
			username = cs.Metadata["username"]
		}
	}
	if username == "" {
		slog.Error("checkout session has no username", "session_id", cs.ID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing username"})
		return
	}

	// Get amount in USD (Stripe amounts are in cents)
	amountUSD := float64(cs.AmountTotal) / 100.0
	if amountUSD <= 0 {
		slog.Error("checkout session has zero amount", "session_id", cs.ID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "zero amount"})
		return
	}

	// Find user
	ctx := c.Request.Context()
	u, err := h.userStore.FindByUsername(ctx, username)
	if err != nil {
		slog.Error("user not found for Stripe webhook", "username", username, "error", err)
		c.JSON(http.StatusOK, gin.H{"received": true, "warning": "user not found"})
		return
	}

	// Deduplicate by Stripe session ID — check if we already granted this
	creditStore := database.NewUserCreditStore()
	existingGrants, _ := creditStore.ListByUser(ctx, u.ID)
	for _, g := range existingGrants {
		if g.Note == fmt.Sprintf("Stripe payment: %s", cs.ID) {
			slog.Info("duplicate Stripe webhook, already granted", "session_id", cs.ID, "username", username)
			c.JSON(http.StatusOK, gin.H{"received": true, "deduplicated": true})
			return
		}
	}

	// Grant credit
	err = creditStore.Create(ctx, &database.UserCredit{
		UserID:    u.ID,
		Username:  username,
		AmountUSD: amountUSD,
		Note:      fmt.Sprintf("Stripe payment: %s", cs.ID),
		GrantedBy: "stripe",
		CreatedAt: time.Now(),
	})
	if err != nil {
		slog.Error("failed to grant credit from Stripe", "error", err, "username", username, "amount", amountUSD)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to grant credit"})
		return
	}

	slog.Info("Stripe payment credited", "username", username, "amount_usd", amountUSD, "session_id", cs.ID)
	c.JSON(http.StatusOK, gin.H{"received": true, "credited": true})
}

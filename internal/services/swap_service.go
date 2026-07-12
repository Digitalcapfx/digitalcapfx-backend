package services

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/payments"
)

// SwapService gives DigitalFX users seamless access to the Rach unified swap
// engine (same-chain DEX + cross-chain bridge, routing chosen internally).
//
// DigitalFX is a Rach merchant: it authenticates the end user (JWT) and
// forwards the swap to the payments engine using the user's ID as the WaaS
// customer ID, so every swap executes from that user's own custody wallet.
// All pricing, routing, symbol/decimal resolution, slippage protection and
// settlement live in payments — this service is a thin, authenticated bridge.
type SwapService struct {
	payments *payments.Client
	logger   *zap.Logger
}

// NewSwapService constructs the swap bridge.
func NewSwapService(paymentsClient *payments.Client, logger *zap.Logger) *SwapService {
	return &SwapService{payments: paymentsClient, logger: logger}
}

// Tokens lists the symbols the user can pass instead of raw contract addresses.
// chain may be empty to return every supported chain.
func (s *SwapService) Tokens(ctx context.Context, chain string) (*payments.SupportedTokensResponse, error) {
	return s.payments.GetSupportedTokens(ctx, chain)
}

// SwapQuoteInput is a merchant-friendly quote request.
type SwapQuoteInput struct {
	FromChain string
	ToChain   string
	FromToken string
	ToToken   string
	Amount    string // human units (preferred), e.g. "5" or "1.5"
	AmountIn  string // base units (fallback)
}

// Quote returns a price quote for any supported pair.
func (s *SwapService) Quote(ctx context.Context, in SwapQuoteInput) (*payments.SwapQuoteResponse, error) {
	return s.payments.GetSwapQuote(ctx, payments.GetSwapQuoteParams{
		FromChain: in.FromChain,
		ToChain:   in.ToChain,
		FromToken: in.FromToken,
		ToToken:   in.ToToken,
		Amount:    in.Amount,
		AmountIn:  in.AmountIn,
	})
}

// SwapExecuteInput is a merchant-friendly execute request.
type SwapExecuteInput struct {
	FromChain    string
	ToChain      string
	FromToken    string
	ToToken      string
	Amount       string
	AmountIn     string
	AmountOutMin string
}

// Execute runs a swap from the authenticated user's WaaS wallet.
func (s *SwapService) Execute(ctx context.Context, userID uuid.UUID, in SwapExecuteInput) (*payments.ExecuteSwapResponse, error) {
	return s.payments.ExecuteSwap(ctx, userID.String(), payments.ExecuteSwapRequest{
		FromChain:    in.FromChain,
		ToChain:      in.ToChain,
		FromToken:    in.FromToken,
		ToToken:      in.ToToken,
		Amount:       in.Amount,
		AmountIn:     in.AmountIn,
		AmountOutMin: in.AmountOutMin,
	})
}

// History returns the authenticated user's paginated swap history.
func (s *SwapService) History(ctx context.Context, userID uuid.UUID, page, limit int) (*payments.GetSwapHistoryResponse, error) {
	return s.payments.GetSwapHistory(ctx, userID.String(), payments.GetSwapHistoryParams{
		Page:  page,
		Limit: limit,
	})
}

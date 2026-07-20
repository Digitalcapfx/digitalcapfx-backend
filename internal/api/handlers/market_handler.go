package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow any origin for now (or configure properly based on env)
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type MarketHandler struct {
	svc    *services.Services
	logger *zap.Logger
}

func NewMarketHandler(svc *services.Services, logger *zap.Logger) *MarketHandler {
	return &MarketHandler{
		svc:    svc,
		logger: logger,
	}
}

// GetPrices godoc
//	@Summary		Get market prices
//	@Description	Returns current cached prices for all tracked cryptocurrencies.
//	@Tags			Market
//	@Produce		json
//	@Success		200	{array}		services.CoinMarket
//	@Router			/market/prices [get]
func (h *MarketHandler) GetPrices(w http.ResponseWriter, r *http.Request) {
	prices := h.svc.Market.GetPrices()
	response.OK(w, prices)
}

// GetPrice godoc
//	@Summary		Get price by symbol
//	@Description	Returns current cached price for a specific cryptocurrency symbol.
//	@Tags			Market
//	@Produce		json
//	@Param			symbol	path		string	true	"Coin symbol (e.g. btc, eth)"
//	@Success		200		{object}	services.CoinMarket
//	@Failure		404		{object}	ErrorResponse
//	@Router			/market/prices/{symbol} [get]
func (h *MarketHandler) GetPrice(w http.ResponseWriter, r *http.Request) {
	symbol := chi.URLParam(r, "symbol")
	if symbol == "" {
		response.BadRequest(w, "MISSING_SYMBOL", "symbol is required")
		return
	}

	price, ok := h.svc.Market.GetPrice(symbol)
	if !ok {
		response.NotFound(w, "price not found for symbol")
		return
	}

	response.OK(w, price)
}

// WSHandler godoc
//	@Summary		Connect to market data websocket
//	@Description	Streams real-time market prices. Clients must connect via ws:// or wss://.
//	@Tags			Market
//	@Router			/market/ws [get]
func (h *MarketHandler) WSHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Push current prices immediately upon connection
	initial := h.svc.Market.GetPrices()
	if err := conn.WriteJSON(initial); err != nil {
		h.logger.Error("failed to write initial market data", zap.Error(err))
		return
	}

	// We could stream updates directly here by hooking into an event bus,
	// but for simplicity, we can poll the cache every few seconds and send updates.
	// In a real high-frequency trading app, we'd use channels to push exactly when data changes.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Setup a simple read loop to handle ping/pong and detect client disconnects
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			prices := h.svc.Market.GetPrices()
			if err := conn.WriteJSON(prices); err != nil {
				// Client disconnected or network error
				return
			}
		}
	}
}

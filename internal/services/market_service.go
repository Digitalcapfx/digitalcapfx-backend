package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/config"
)

// CoinMarket represents market data for one coin, matching the Rach Market Data
// CoinMarket schema (GET /v1/market/coins).
type CoinMarket struct {
	ID                           string  `json:"id" example:"bitcoin"`
	Symbol                       string  `json:"symbol" example:"btc"`
	Name                         string  `json:"name" example:"Bitcoin"`
	Image                        string  `json:"image" example:"https://cdn.rach.finance/coins/bitcoin.png"`
	CurrentPrice                 float64 `json:"current_price" example:"62409.00"`
	MarketCap                    float64 `json:"market_cap"`
	MarketCapRank                int     `json:"market_cap_rank"`
	TotalVolume                  float64 `json:"total_volume"`
	High24h                      float64 `json:"high_24h"`
	Low24h                       float64 `json:"low_24h"`
	PriceChange24h               float64 `json:"price_change_24h"`
	PriceChangePercentage24h     float64 `json:"price_change_percentage_24h" example:"2.15"`
	CirculatingSupply            float64 `json:"circulating_supply"`
	TotalSupply                  float64 `json:"total_supply"`
	MaxSupply                    float64 `json:"max_supply"`
	ATH                          float64 `json:"ath"`
	ATHChangePercentage          float64 `json:"ath_change_percentage"`
	LastUpdated                  string  `json:"last_updated"`
	PriceChangePercentage1hInCur float64 `json:"price_change_percentage_1h_in_currency" example:"0.34"`
	PriceChangePercentage7dInCur float64 `json:"price_change_percentage_7d_in_currency"`
}

type MarketService struct {
	cfg          *config.Config
	logger       *zap.Logger
	notification *NotificationService

	pricesMu sync.RWMutex
	prices   map[string]CoinMarket
}

func NewMarketService(cfg *config.Config, logger *zap.Logger, notification *NotificationService) *MarketService {
	return &MarketService{
		cfg:          cfg,
		logger:       logger,
		notification: notification,
		prices:       make(map[string]CoinMarket),
	}
}

// Run starts the market data background worker. It first fetches prices via REST to
// warm the cache, then opens a websocket to receive real-time updates.
func (s *MarketService) Run(ctx context.Context) {
	if s.cfg.PaymentsAPI.MarketDataURL == "" {
		s.logger.Warn("market service disabled — missing PaymentsAPI.MarketDataURL")
		return
	}

	s.logger.Info("starting market service worker")

	// Pre-warm cache
	if err := s.fetchRESTPrices(ctx); err != nil {
		s.logger.Warn("failed to pre-warm market prices via REST", zap.Error(err))
	} else {
		s.logger.Info("market prices pre-warmed")
	}

	// Reconnect loop for websocket
	for {
		select {
		case <-ctx.Done():
			return
		default:
			err := s.connectAndListenWS(ctx)
			if err != nil {
				s.logger.Error("market ws disconnected", zap.Error(err))
			}
			// Backoff before reconnecting
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}
}

// fetchRESTPrices warms the cache from GET /v1/market/coins. Auth is the
// merchant API key in the X-API-Key header (Rach Market Data ApiKeyAuth). The
// response is a paginated envelope: { as_of, total, page, limit, coins: [...] }.
func (s *MarketService) fetchRESTPrices(ctx context.Context) error {
	restURL := fmt.Sprintf("%s/v1/market/coins?limit=250", s.cfg.PaymentsAPI.MarketDataURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, restURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", s.cfg.PaymentsAPI.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}

	var out struct {
		AsOf  int64        `json:"as_of"`
		Coins []CoinMarket `json:"coins"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}

	s.pricesMu.Lock()
	for _, c := range out.Coins {
		s.prices[c.Symbol] = c
	}
	s.pricesMu.Unlock()

	return nil
}

// connectAndListenWS connects to the Rach Finance market data websocket.
func (s *MarketService) connectAndListenWS(ctx context.Context) error {
	// convert http/https to ws/wss
	wsBase := s.cfg.PaymentsAPI.MarketDataURL
	if len(wsBase) > 4 && wsBase[:4] == "http" {
		wsBase = "ws" + wsBase[4:]
	}
	wsURL := fmt.Sprintf("%s/v1/market/ws?key=%s", wsBase, s.cfg.PaymentsAPI.APIKey)
	
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	
	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		if resp != nil {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("dial failed (status %d): %s: %w", resp.StatusCode, string(b), err)
		}
		return fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()

	s.logger.Info("connected to market data websocket")

	// Rach requires an explicit subscribe before any data is streamed.
	// "*" subscribes to every tracked coin.
	if err := conn.WriteJSON(map[string]any{"op": "subscribe", "symbols": []string{"*"}}); err != nil {
		return fmt.Errorf("market ws subscribe: %w", err)
	}

	// Ping ticker to keep connection alive if needed
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second))
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				return fmt.Errorf("read message: %w", err)
			}

			// Server → client messages are enveloped: a "snapshot" carries a full
			// coins array; "tick" updates carry changed coins. Accept either a
			// coins array or a single coin.
			var msg struct {
				Op    string       `json:"op"`
				Coins []CoinMarket `json:"coins"`
				Coin  *CoinMarket  `json:"coin"`
			}
			if err := json.Unmarshal(message, &msg); err != nil {
				s.logger.Warn("failed to parse market ws message", zap.Error(err), zap.String("payload", string(message)))
				continue
			}

			updates := msg.Coins
			if msg.Coin != nil {
				updates = append(updates, *msg.Coin)
			}

			s.pricesMu.Lock()
			for _, c := range updates {
				old, ok := s.prices[c.Symbol]
				s.prices[c.Symbol] = c

				// Market spike detection: notify on a large sudden move.
				if ok {
					s.checkSpike(old, c)
				}
			}
			s.pricesMu.Unlock()
		}
	}
}

// checkSpike checks if a coin had a major sudden movement and sends a push if so.
func (s *MarketService) checkSpike(old, new CoinMarket) {
	if old.CurrentPrice == 0 {
		return
	}

	changePercent := ((new.CurrentPrice - old.CurrentPrice) / old.CurrentPrice) * 100

	// If sudden jump > 10%
	if changePercent >= 10.0 {
		s.logger.Info("market spike detected!", zap.String("coin", new.Symbol), zap.Float64("change", changePercent))
		if s.notification != nil {
			title := fmt.Sprintf("🚀 %s is taking off!", new.Name)
			body := fmt.Sprintf("%s just jumped %.2f%%! Current price: $%.2f", new.Name, changePercent, new.CurrentPrice)
			s.notification.SendGlobalPush(context.Background(), title, body, map[string]string{
				"type": "market_alert",
				"symbol": new.Symbol,
			})
		}
	} else if changePercent <= -10.0 {
		s.logger.Info("market dip detected!", zap.String("coin", new.Symbol), zap.Float64("change", changePercent))
		if s.notification != nil {
			title := fmt.Sprintf("📉 %s is dipping!", new.Name)
			body := fmt.Sprintf("%s dropped %.2f%%! Current price: $%.2f. Buy the dip?", new.Name, -changePercent, new.CurrentPrice)
			s.notification.SendGlobalPush(context.Background(), title, body, map[string]string{
				"type": "market_alert",
				"symbol": new.Symbol,
			})
		}
	}
}

// GetPrices returns all currently cached prices.
func (s *MarketService) GetPrices() []CoinMarket {
	s.pricesMu.RLock()
	defer s.pricesMu.RUnlock()

	var list []CoinMarket
	for _, c := range s.prices {
		list = append(list, c)
	}
	return list
}

// GetPrice returns the cached price for a single symbol.
func (s *MarketService) GetPrice(symbol string) (CoinMarket, bool) {
	s.pricesMu.RLock()
	defer s.pricesMu.RUnlock()

	c, ok := s.prices[symbol]
	return c, ok
}

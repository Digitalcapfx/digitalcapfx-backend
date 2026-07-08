package handlers

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
)

type MarketHandler struct {
	proxy  *httputil.ReverseProxy
	logger *zap.Logger
}

func NewMarketHandler(cfg *config.Config, logger *zap.Logger) *MarketHandler {
	target, err := url.Parse(cfg.PaymentsAPI.MarketDataURL)
	if err != nil {
		logger.Fatal("failed to parse payments market data url", zap.Error(err))
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director

	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Translate paths: change "/api/v1/market" to "/v1/market"
		req.URL.Path = strings.Replace(req.URL.Path, "/api/v1/market", "/v1/market", 1)

		// Authorize connection to internal microservice
		req.Header.Set("X-API-Key", cfg.PaymentsAPI.APIKey)

		// Map public/merchant WebSocket key query param to internal Payments API key
		q := req.URL.Query()
		if q.Get("key") != "" {
			q.Set("key", cfg.PaymentsAPI.APIKey)
			req.URL.RawQuery = q.Encode()
		}

		req.Host = target.Host
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("market proxy error", zap.Error(err), zap.String("url", r.URL.String()))
		response.ServiceUnavailable(w, "market data service currently unavailable")
	}

	return &MarketHandler{
		proxy:  proxy,
		logger: logger,
	}
}

// ProxyREST handles GET requests for market data
func (h *MarketHandler) ProxyREST(w http.ResponseWriter, r *http.Request) {
	h.proxy.ServeHTTP(w, r)
}

// ProxyWS handles WebSocket connections for market data streaming
func (h *MarketHandler) ProxyWS(w http.ResponseWriter, r *http.Request) {
	h.proxy.ServeHTTP(w, r)
}

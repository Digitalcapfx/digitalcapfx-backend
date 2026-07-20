package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type RatesHandler struct {
	svc *services.Services
}

func NewRatesHandler(svc *services.Services) *RatesHandler {
	return &RatesHandler{svc: svc}
}

// SetCurrencyRateRequest is the body for setting a currency's rates.
type SetCurrencyRateRequest struct {
	StandardRate float64 `json:"standard_rate" example:"609"`
	BuyRate      float64 `json:"buy_rate" example:"615"`
	SellRate     float64 `json:"sell_rate" example:"603"`
}

// ListRates godoc
//
//	@Summary      List currency rates
//	@Description  Returns the buy, sell and standard rate for every supported currency (EUR, GBP, USD, XAF, XOF, NGN). Rates are units of that currency per 1 USD. Use `standard_rate` to display a customer's balance in another currency; use `buy_rate` / `sell_rate` when the customer sells to / buys from the platform.
//	@Tags         rates
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {array}   db.CurrencyRate
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /rates [get]
func (h *RatesHandler) ListRates(w http.ResponseWriter, r *http.Request) {
	rates, err := h.svc.Rates.List(r.Context())
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, rates)
}

// GetRate godoc
//
//	@Summary      Get a currency's rates
//	@Description  Returns the buy, sell and standard rate for a single currency.
//	@Tags         rates
//	@Produce      json
//	@Security     BearerAuth
//	@Param        currency  path      string  true  "Currency code (USD, EUR, GBP, XAF, XOF, NGN)"
//	@Success      200       {object}  db.CurrencyRate
//	@Failure      401       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Router       /rates/{currency} [get]
func (h *RatesHandler) GetRate(w http.ResponseWriter, r *http.Request) {
	currency := chi.URLParam(r, "currency")
	if currency == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "currency is required")
		return
	}
	rate, err := h.svc.Rates.Get(r.Context(), currency)
	if err != nil {
		response.NotFound(w, "rate not found for currency")
		return
	}
	response.OK(w, rate)
}

// AdminListRates godoc
//
//	@Summary      List currency rates (admin)
//	@Description  Admin view of all currency buy/sell/standard rates. Requires the rates:view permission.
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {array}   db.CurrencyRate
//	@Failure      401  {object}  ErrorResponse
//	@Failure      403  {object}  ErrorResponse
//	@Router       /admin/currency-rates [get]
func (h *RatesHandler) AdminListRates(w http.ResponseWriter, r *http.Request) {
	rates, err := h.svc.Rates.List(r.Context())
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, rates)
}

// AdminSetRate godoc
//
//	@Summary      Set a currency's rates (admin)
//	@Description  Creates or updates the standard, buy and sell rate for a currency. Buy low / sell high for margin; standard is the display rate. Requires the rates:manage permission.
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        currency  path      string                  true  "Currency code (USD, EUR, GBP, XAF, XOF, NGN)"
//	@Param        body      body      SetCurrencyRateRequest  true  "Standard / buy / sell rates (per 1 USD)"
//	@Success      200       {object}  db.CurrencyRate
//	@Failure      400       {object}  ErrorResponse
//	@Failure      401       {object}  ErrorResponse
//	@Failure      403       {object}  ErrorResponse
//	@Router       /admin/currency-rates/{currency} [put]
func (h *RatesHandler) AdminSetRate(w http.ResponseWriter, r *http.Request) {
	adminID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	currency := chi.URLParam(r, "currency")
	if currency == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "currency is required")
		return
	}
	var body SetCurrencyRateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}

	rate, err := h.svc.Rates.Set(r.Context(), services.SetCurrencyRateInput{
		Currency:     currency,
		StandardRate: body.StandardRate,
		BuyRate:      body.BuyRate,
		SellRate:     body.SellRate,
		AdminID:      adminID,
	})
	if err != nil {
		response.BadRequest(w, "INVALID_RATE", err.Error())
		return
	}
	response.OK(w, rate)
}

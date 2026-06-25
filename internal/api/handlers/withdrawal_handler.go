package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type WithdrawalHandler struct {
	svc *services.Services
}

func NewWithdrawalHandler(svc *services.Services) *WithdrawalHandler {
	return &WithdrawalHandler{svc: svc}
}

// ─── Quote ────────────────────────────────────────────────────────────────────

// Quote godoc
//
//	@Summary      Preview fiat withdrawal
//	@Description  Returns the exact fee and destination amount for a withdrawal
//	              before the user submits. No funds are moved.
//	@Tags         withdrawals
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      WithdrawalQuoteRequestBody  true  "Quote request"
//	@Success      200   {object}  WithdrawalQuoteData
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /withdrawals/quote [post]
func (h *WithdrawalHandler) Quote(w http.ResponseWriter, r *http.Request) {
	var body WithdrawalQuoteRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.SourceCurrency == "" || body.SourceAmount <= 0 || body.DestinationCurrency == "" || body.DestinationType == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "source_currency, source_amount, destination_type and destination_currency are required")
		return
	}

	quote, err := h.svc.Withdrawal.Quote(r.Context(), services.WithdrawalQuoteRequest{
		SourceCurrency:      body.SourceCurrency,
		SourceAmount:        body.SourceAmount,
		DestinationType:     body.DestinationType,
		DestinationCurrency: body.DestinationCurrency,
	})
	if err != nil {
		if err == services.ErrRateNotConfigured {
			response.BadRequest(w, "RATE_NOT_CONFIGURED", "no withdrawal rate configured for this currency pair")
			return
		}
		response.BadRequest(w, "QUOTE_ERROR", err.Error())
		return
	}

	response.OK(w, quote)
}

// ─── Initiate ─────────────────────────────────────────────────────────────────

// Initiate godoc
//
//	@Summary      Initiate fiat withdrawal
//	@Description  Initiates a withdrawal from the user's Nilos-backed fiat account
//	              to a mobile money number (XAF/XOF via HUB2) or external bank
//	              (SEPA/SWIFT/FPS via Nilos). The business FX rate is applied for
//	              cross-currency withdrawals. Status starts as "processing".
//	@Tags         withdrawals
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      InitiateWithdrawalRequestBody  true  "Withdrawal request"
//	@Success      201   {object}  FiatWithdrawalResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      422   {object}  ErrorResponse  "Insufficient balance"
//	@Router       /withdrawals [post]
func (h *WithdrawalHandler) Initiate(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body InitiateWithdrawalRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if err := validateWithdrawalRequest(body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", err.Error())
		return
	}

	req := services.InitiateWithdrawalRequest{
		SourceCurrency:      body.SourceCurrency,
		SourceAmount:        body.SourceAmount,
		DestinationType:     body.DestinationType,
		DestinationCurrency: body.DestinationCurrency,
		DestinationCountry:  body.DestinationCountry,
		RecipientName:       body.RecipientName,
		PhoneNumber:         body.PhoneNumber,
		Operator:            body.Operator,
		BankName:            body.BankName,
		AccountNumber:       body.AccountNumber,
		IBAN:                body.IBAN,
		SwiftCode:           body.SwiftCode,
		SortCode:            body.SortCode,
		RoutingNumber:       body.RoutingNumber,
		BeneficiaryID:       body.BeneficiaryID,
	}

	withdrawal, err := h.svc.Withdrawal.Initiate(r.Context(), userID, req)
	if err != nil {
		switch err {
		case services.ErrInsufficientFiatFunds:
			response.JSON(w, http.StatusUnprocessableEntity, map[string]string{
				"code":    "INSUFFICIENT_BALANCE",
				"message": "insufficient available balance for this withdrawal",
			})
		case services.ErrRateNotConfigured:
			response.BadRequest(w, "RATE_NOT_CONFIGURED", "no withdrawal rate configured for this currency pair")
		case services.ErrAccountNotFound:
			response.BadRequest(w, "ACCOUNT_NOT_FOUND", "no fiat account found for the source currency")
		default:
			response.InternalError(w)
		}
		return
	}

	response.Created(w, withdrawal)
}

// ─── Get / List ───────────────────────────────────────────────────────────────

// GetWithdrawal godoc
//
//	@Summary      Get a fiat withdrawal by ID
//	@Tags         withdrawals
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "Withdrawal UUID"
//	@Success      200 {object}  FiatWithdrawalResponse
//	@Failure      404 {object}  ErrorResponse
//	@Router       /withdrawals/{id} [get]
func (h *WithdrawalHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "invalid withdrawal ID")
		return
	}

	withdrawal, err := h.svc.Withdrawal.Get(r.Context(), id, userID)
	if err != nil {
		response.NotFound(w, "withdrawal not found")
		return
	}
	response.OK(w, withdrawal)
}

// ListWithdrawals godoc
//
//	@Summary      List fiat withdrawals
//	@Description  Returns a paginated list of the caller's fiat withdrawal requests.
//	@Tags         withdrawals
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page      query  int  false  "Page number (default 1)"
//	@Param        per_page  query  int  false  "Results per page, max 50 (default 20)"
//	@Success      200  {object}  WithdrawalListResponse
//	@Router       /withdrawals [get]
func (h *WithdrawalHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))

	result, err := h.svc.Withdrawal.List(r.Context(), userID, int32(page), int32(perPage))
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, result)
}

// ─── Beneficiaries ────────────────────────────────────────────────────────────

// ListBeneficiaries godoc
//
//	@Summary      List saved beneficiaries
//	@Description  Returns all saved withdrawal destinations (banks and mobile money
//	              numbers) for the authenticated user.
//	@Tags         withdrawals
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {array}  BeneficiaryResponse
//	@Router       /withdrawals/beneficiaries [get]
func (h *WithdrawalHandler) ListBeneficiaries(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	bens, err := h.svc.Withdrawal.ListBeneficiaries(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, bens)
}

// SaveBeneficiary godoc
//
//	@Summary      Save a new beneficiary
//	@Description  Saves a bank account or mobile money number for quick repeat
//	              withdrawals. The Nilos recipient ID is lazily cached on first use.
//	@Tags         withdrawals
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      SaveBeneficiaryRequestBody  true  "Beneficiary"
//	@Success      201   {object}  BeneficiaryResponse
//	@Failure      400   {object}  ErrorResponse
//	@Router       /withdrawals/beneficiaries [post]
func (h *WithdrawalHandler) SaveBeneficiary(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body SaveBeneficiaryRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.Label == "" || body.Type == "" || body.DestinationCurrency == "" || body.Country == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "label, type, destination_currency and country are required")
		return
	}

	ben, err := h.svc.Withdrawal.SaveBeneficiary(r.Context(), userID, services.SaveBeneficiaryRequest{
		Label:               body.Label,
		Type:                body.Type,
		DestinationCurrency: body.DestinationCurrency,
		Country:             body.Country,
		PhoneNumber:         body.PhoneNumber,
		Operator:            body.Operator,
		BankName:            body.BankName,
		AccountNumber:       body.AccountNumber,
		IBAN:                body.IBAN,
		SwiftCode:           body.SwiftCode,
		SortCode:            body.SortCode,
		RoutingNumber:       body.RoutingNumber,
	})
	if err != nil {
		response.InternalError(w)
		return
	}
	response.Created(w, ben)
}

// DeleteBeneficiary godoc
//
//	@Summary      Delete a saved beneficiary
//	@Tags         withdrawals
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "Beneficiary UUID"
//	@Success      200 {object}  MessageResponse
//	@Failure      404 {object}  ErrorResponse
//	@Router       /withdrawals/beneficiaries/{id} [delete]
func (h *WithdrawalHandler) DeleteBeneficiary(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "invalid beneficiary ID")
		return
	}
	if err := h.svc.Withdrawal.DeleteBeneficiary(r.Context(), id, userID); err != nil {
		response.NotFound(w, "beneficiary not found")
		return
	}
	response.OK(w, map[string]string{"message": "beneficiary deleted"})
}

// ─── DTO types ────────────────────────────────────────────────────────────────

type WithdrawalQuoteRequestBody struct {
	SourceCurrency      string  `json:"source_currency"`
	SourceAmount        float64 `json:"source_amount"`
	DestinationType     string  `json:"destination_type"`
	DestinationCurrency string  `json:"destination_currency"`
}

type InitiateWithdrawalRequestBody struct {
	SourceCurrency      string     `json:"source_currency"`
	SourceAmount        float64    `json:"source_amount"`
	DestinationType     string     `json:"destination_type"`
	DestinationCurrency string     `json:"destination_currency"`
	DestinationCountry  string     `json:"destination_country"`
	RecipientName       string     `json:"recipient_name"`
	// Mobile money
	PhoneNumber string `json:"phone_number"`
	Operator    string `json:"operator"`
	// Bank
	BankName      string `json:"bank_name"`
	AccountNumber string `json:"account_number"`
	IBAN          string `json:"iban"`
	SwiftCode     string `json:"swift_code"`
	SortCode      string `json:"sort_code"`
	RoutingNumber string `json:"routing_number"`
	// Saved beneficiary (pre-fills destination fields)
	BeneficiaryID *uuid.UUID `json:"beneficiary_id"`
}

type SaveBeneficiaryRequestBody struct {
	Label               string `json:"label"`
	Type                string `json:"type"`
	DestinationCurrency string `json:"destination_currency"`
	Country             string `json:"country"`
	PhoneNumber         string `json:"phone_number"`
	Operator            string `json:"operator"`
	BankName            string `json:"bank_name"`
	AccountNumber       string `json:"account_number"`
	IBAN                string `json:"iban"`
	SwiftCode           string `json:"swift_code"`
	SortCode            string `json:"sort_code"`
	RoutingNumber       string `json:"routing_number"`
}

// Response shapes for Swagger docs.
type FiatWithdrawalResponse struct{}
type WithdrawalListResponse struct{}
type BeneficiaryResponse struct{}
type WithdrawalQuoteData struct{}

// validateWithdrawalRequest checks that required fields are present based on
// the destination type.
func validateWithdrawalRequest(body InitiateWithdrawalRequestBody) error {
	if body.SourceCurrency == "" {
		return errMsg("source_currency is required")
	}
	if body.SourceAmount <= 0 {
		return errMsg("source_amount must be greater than 0")
	}
	if body.DestinationType == "" {
		return errMsg("destination_type is required")
	}
	if body.DestinationCurrency == "" {
		return errMsg("destination_currency is required")
	}
	if body.DestinationCountry == "" {
		return errMsg("destination_country is required")
	}
	if body.RecipientName == "" {
		return errMsg("recipient_name is required")
	}
	switch body.DestinationType {
	case services.DestMobileMoney:
		if body.PhoneNumber == "" || body.Operator == "" {
			return errMsg("phone_number and operator are required for mobile_money withdrawals")
		}
	case services.DestBankSEPA, services.DestBankCEMAC:
		if body.IBAN == "" {
			return errMsg("iban is required for SEPA/CEMAC bank withdrawals")
		}
	case services.DestBankSWIFT:
		if body.SwiftCode == "" || body.AccountNumber == "" {
			return errMsg("swift_code and account_number are required for SWIFT withdrawals")
		}
	case services.DestBankFPS:
		if body.SortCode == "" || body.AccountNumber == "" {
			return errMsg("sort_code and account_number are required for FPS withdrawals")
		}
	case services.DestBankUEMOA:
		if body.AccountNumber == "" {
			return errMsg("account_number is required for UEMOA bank withdrawals")
		}
	}
	return nil
}

type validationErr struct{ msg string }

func (e validationErr) Error() string { return e.msg }
func errMsg(s string) error           { return validationErr{s} }

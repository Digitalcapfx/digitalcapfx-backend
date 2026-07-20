package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type VTUHandler struct {
	svc *services.VTUService
}

func NewVTUHandler(svc *services.VTUService) *VTUHandler {
	return &VTUHandler{svc: svc}
}

// PurchaseAirtime purchases airtime.
// @Summary Purchase Airtime
// @Tags VTU
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param req body PurchaseAirtimeRequest true "Airtime details"
// @Success 200 {object} VTUTransactionResponse
// @Failure 400 {object} ErrorResponse
// @Router /vtu/airtime [post]
func (h *VTUHandler) PurchaseAirtime(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "Unauthorized")
		return
	}

	var req PurchaseAirtimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "BAD_REQUEST", "invalid request body")
		return
	}

	if req.Amount <= 0 || req.Currency == "" || req.Phone == "" {
		response.BadRequest(w, "BAD_REQUEST", "missing required fields (amount, currency, phone)")
		return
	}

	tx, err := h.svc.PurchaseAirtime(r.Context(), userID, req.Amount, req.Currency, req.Phone, req.Operator)
	if err != nil {
		response.BadRequest(w, "BAD_REQUEST", err.Error())
		return
	}

	response.OK(w, VTUTransactionData{
		ID:          tx.ID.String(),
		Amount:      tx.Amount,
		Currency:    tx.Currency,
		ServiceType: tx.ServiceType,
		Provider:    tx.Provider,
		TargetPhone: derefStr(tx.TargetPhone, ""),
		Reference:   derefStr(tx.Reference, ""),
		Status:      tx.Status,
		CreatedAt:   tx.CreatedAt,
	})
}

// PurchaseData purchases a data bundle.
// @Summary Purchase Data Bundle
// @Tags VTU
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param req body PurchaseDataBundleRequest true "Data details"
// @Success 200 {object} VTUTransactionResponse
// @Router /vtu/data [post]
func (h *VTUHandler) PurchaseData(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "Unauthorized")
		return
	}

	var req PurchaseDataBundleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "BAD_REQUEST", "invalid request body")
		return
	}

	tx, err := h.svc.PurchaseData(r.Context(), userID, req.Amount, req.Currency, req.BundleID, req.Phone, req.Operator)
	if err != nil {
		response.BadRequest(w, "BAD_REQUEST", err.Error())
		return
	}

	response.OK(w, VTUTransactionData{
		ID:          tx.ID.String(),
		Amount:      tx.Amount,
		Currency:    tx.Currency,
		ServiceType: tx.ServiceType,
		Provider:    tx.Provider,
		TargetPhone: derefStr(tx.TargetPhone, ""),
		Reference:   derefStr(tx.Reference, ""),
		Status:      tx.Status,
		CreatedAt:   tx.CreatedAt,
	})
}

// PayBill pays a utility bill.
// @Summary Pay Utility Bill
// @Tags VTU
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param req body PayBillRequest true "Bill details"
// @Success 200 {object} VTUTransactionResponse
// @Router /vtu/bills/pay [post]
func (h *VTUHandler) PayBill(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "Unauthorized")
		return
	}

	var req PayBillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "BAD_REQUEST", "invalid request body")
		return
	}

	tx, err := h.svc.PayBill(r.Context(), userID, req.Amount, req.Currency, req.BillerID, req.AccountNumber)
	if err != nil {
		response.BadRequest(w, "BAD_REQUEST", err.Error())
		return
	}

	response.OK(w, VTUTransactionData{
		ID:            tx.ID.String(),
		Amount:        tx.Amount,
		Currency:      tx.Currency,
		ServiceType:   tx.ServiceType,
		Provider:      tx.Provider,
		TargetAccount: derefStr(tx.TargetAccount, ""),
		Reference:     derefStr(tx.Reference, ""),
		Status:        tx.Status,
		CreatedAt:     tx.CreatedAt,
	})
}

// ListVTUTransactions lists the user's VTU transactions.
// @Summary List VTU transactions
// @Tags VTU
// @Produce json
// @Security BearerAuth
// @Success 200 {object} VTUTransactionListResponse
// @Router /vtu/transactions [get]
func (h *VTUHandler) ListVTUTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "Unauthorized")
		return
	}

	txs, err := h.svc.ListTransactions(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	data := make([]VTUTransactionData, 0, len(txs))
	for _, tx := range txs {
		data = append(data, VTUTransactionData{
			ID:            tx.ID.String(),
			Amount:        tx.Amount,
			Currency:      tx.Currency,
			ServiceType:   tx.ServiceType,
			Provider:      tx.Provider,
			TargetPhone:   derefStr(tx.TargetPhone, ""),
			TargetAccount: derefStr(tx.TargetAccount, ""),
			Reference:     derefStr(tx.Reference, ""),
			Status:        tx.Status,
			CreatedAt:     tx.CreatedAt,
		})
	}
	response.OK(w, data)
}

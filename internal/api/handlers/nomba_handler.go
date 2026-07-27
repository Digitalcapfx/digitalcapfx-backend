package handlers

import (
	"net/http"
	"strings"

	"github.com/rachfinance/digitalfx/internal/clients/nomba"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// NombaHandler exposes the Nigerian NGN helper endpoints backed by Nomba: the
// bank list (for a bank picker) and account name-enquiry (to verify a recipient
// before an NGN transfer).
type NombaHandler struct {
	svc *services.Services
}

func NewNombaHandler(svc *services.Services) *NombaHandler {
	return &NombaHandler{svc: svc}
}

// NombaBanksResponse is the GET /nomba/banks payload.
type NombaBanksResponse struct {
	Banks []nomba.Bank `json:"banks"`
}

// NombaResolveAccountResponse is the GET /nomba/resolve-account payload.
type NombaResolveAccountResponse struct {
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
}

// ListBanks godoc
//
//	@Summary      List Nigerian banks (Nomba)
//	@Description  Returns the supported Nigerian bank codes and names. Use the `code` as `bank_code` when resolving an account or initiating an NGN bank transfer/withdrawal. Cache client-side — the list rarely changes.
//	@Tags         Fiat - NGN (Nomba)
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  NombaBanksResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      503  {object}  ErrorResponse
//	@Router       /nomba/banks [get]
func (h *NombaHandler) ListBanks(w http.ResponseWriter, r *http.Request) {
	banks, err := h.svc.Nomba.ListBanks(r.Context())
	if err != nil {
		response.ServiceUnavailable(w, err.Error())
		return
	}
	response.OK(w, NombaBanksResponse{Banks: banks})
}

// ResolveAccount godoc
//
//	@Summary      Resolve (check) a Nigerian bank account
//	@Description  Name-enquiry against a Nigerian bank account — returns the account holder name so the sender can confirm the recipient BEFORE transferring. This is the account check used during an NGN transfer. Provide `account_number` (10 digits) and `bank_code` (from GET /nomba/banks).
//	@Tags         Fiat - NGN (Nomba)
//	@Produce      json
//	@Security     BearerAuth
//	@Param        account_number  query     string  true  "10-digit account number"
//	@Param        bank_code       query     string  true  "Bank code from GET /nomba/banks"
//	@Success      200             {object}  NombaResolveAccountResponse
//	@Failure      400             {object}  ErrorResponse
//	@Failure      401             {object}  ErrorResponse
//	@Failure      503             {object}  ErrorResponse
//	@Router       /nomba/resolve-account [get]
func (h *NombaHandler) ResolveAccount(w http.ResponseWriter, r *http.Request) {
	accountNumber := strings.TrimSpace(r.URL.Query().Get("account_number"))
	bankCode := strings.TrimSpace(r.URL.Query().Get("bank_code"))
	if accountNumber == "" || bankCode == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "account_number and bank_code are required")
		return
	}

	res, err := h.svc.Nomba.LookupAccount(r.Context(), accountNumber, bankCode)
	if err != nil {
		response.ServiceUnavailable(w, err.Error())
		return
	}
	response.OK(w, NombaResolveAccountResponse{
		AccountNumber: res.AccountNumber,
		AccountName:   res.AccountName,
	})
}

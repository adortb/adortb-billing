package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/adortb/adortb-billing/internal/advertiser_billing"
	"github.com/adortb/adortb-billing/internal/platform"
	"github.com/adortb/adortb-billing/internal/publisher_billing"
	"github.com/adortb/adortb-billing/internal/repo"
)

// Handler HTTP handler 聚合
type Handler struct {
	advSvc  *advertiser_billing.Service
	pubSvc  *publisher_billing.Service
	platSvc *platform.Service
}

func NewHandler(
	advSvc *advertiser_billing.Service,
	pubSvc *publisher_billing.Service,
	platSvc *platform.Service,
) *Handler {
	return &Handler{advSvc: advSvc, pubSvc: pubSvc, platSvc: platSvc}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/advertiser/", h.routeAdvertiser)
	mux.HandleFunc("/v1/publisher/", h.routePublisher)
	mux.HandleFunc("/v1/admin/withdraw/", h.routeAdminWithdraw)
	mux.HandleFunc("/v1/platform/daily", h.getPlatformDaily)
	mux.HandleFunc("/health", h.health)
}

// ── Advertiser ────────────────────────────────────────────────────

func (h *Handler) routeAdvertiser(w http.ResponseWriter, r *http.Request) {
	// /v1/advertiser/{id}/{action}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/advertiser/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing advertiser id")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid advertiser id")
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case r.Method == http.MethodPost && action == "recharge":
		h.recharge(w, r, id)
	case r.Method == http.MethodGet && action == "account":
		h.getAdvertiserAccount(w, r, id)
	case r.Method == http.MethodGet && action == "transactions":
		h.listTransactions(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) recharge(w http.ResponseWriter, r *http.Request, id int64) {
	var req struct {
		Amount float64 `json:"amount"`
		Method string  `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be positive")
		return
	}
	tx, err := h.advSvc.Recharge(r.Context(), id, req.Amount)
	if err != nil {
		slog.Error("recharge", "err", err)
		writeError(w, http.StatusInternalServerError, "recharge failed")
		return
	}
	writeJSON(w, http.StatusOK, tx)
}

func (h *Handler) getAdvertiserAccount(w http.ResponseWriter, r *http.Request, id int64) {
	acc, err := h.advSvc.GetAccount(r.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrAccountNotFound) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, acc)
}

func (h *Handler) listTransactions(w http.ResponseWriter, r *http.Request, id int64) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	txs, err := h.advSvc.ListTransactions(r.Context(), id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, txs)
}

// ── Publisher ─────────────────────────────────────────────────────

func (h *Handler) routePublisher(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/publisher/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing publisher id")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid publisher id")
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case r.Method == http.MethodGet && action == "account":
		h.getPublisherAccount(w, r, id)
	case r.Method == http.MethodGet && action == "settlements":
		h.listSettlements(w, r, id)
	case r.Method == http.MethodPost && action == "withdraw":
		h.createWithdraw(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) getPublisherAccount(w http.ResponseWriter, r *http.Request, id int64) {
	acc, err := h.pubSvc.GetAccount(r.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrAccountNotFound) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, acc)
}

func (h *Handler) listSettlements(w http.ResponseWriter, r *http.Request, id int64) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return
	}
	settlements, err := h.pubSvc.ListSettlements(r.Context(), id, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, settlements)
}

func (h *Handler) createWithdraw(w http.ResponseWriter, r *http.Request, id int64) {
	var req struct {
		Amount   float64         `json:"amount"`
		BankInfo json.RawMessage `json:"bank_info"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be positive")
		return
	}
	wr, err := h.pubSvc.CreateWithdraw(r.Context(), id, req.Amount, req.BankInfo)
	if err != nil {
		if errors.Is(err, repo.ErrInsufficientBalance) {
			writeError(w, http.StatusBadRequest, "insufficient settled balance")
			return
		}
		slog.Error("create withdraw", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, wr)
}

// ── Admin ─────────────────────────────────────────────────────────

func (h *Handler) routeAdminWithdraw(w http.ResponseWriter, r *http.Request) {
	// /v1/admin/withdraw/{id}/approve
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/admin/withdraw/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing withdraw id")
		return
	}
	wid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid withdraw id")
		return
	}
	if parts[1] == "approve" && r.Method == http.MethodPost {
		h.approveWithdraw(w, r, wid)
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (h *Handler) approveWithdraw(w http.ResponseWriter, r *http.Request, wid int64) {
	var req struct {
		ReviewerID int64 `json:"reviewer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.pubSvc.ApproveWithdraw(r.Context(), wid, req.ReviewerID); err != nil {
		slog.Error("approve withdraw", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

// ── Platform ──────────────────────────────────────────────────────

func (h *Handler) getPlatformDaily(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		writeError(w, http.StatusBadRequest, "date is required")
		return
	}
	summary, err := h.platSvc.GetDailySummary(r.Context(), date)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── helpers ───────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

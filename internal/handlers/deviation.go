package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/models"
	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/repository"
	"github.com/go-chi/chi/v5"
)

type DeviationHandler struct {
	repo *repository.DeviationRepo
}

func NewDeviationHandler(r *repository.DeviationRepo) *DeviationHandler {
	return &DeviationHandler{repo: r}
}

func (h *DeviationHandler) Routes(r chi.Router) {
	r.Route("/deviations", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Get("/stats", h.Stats)
		r.Route("/{id}", func(r chi.Router) {
			r.Patch("/resolve", h.Resolve)
		})
	})
}

func (h *DeviationHandler) List(w http.ResponseWriter, r *http.Request) {
	companyID := r.URL.Query().Get("companyId")
	if !IsValidUUID(companyID) { respondError(w, http.StatusBadRequest, "invalid company_id"); return }
	if !RequireCompanyAccess(w, r, companyID) { return }
	results, err := h.repo.List(r.Context(), companyID)
	if err != nil { respondError(w, http.StatusInternalServerError, "list failed"); return }
	respondJSON(w, http.StatusOK, map[string]any{"deviations": results, "total": len(results)})
}

func (h *DeviationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var d models.Deviation
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil { respondError(w, http.StatusBadRequest, "invalid body"); return }
	if d.CompanyID == "" { respondError(w, http.StatusBadRequest, "company_id required"); return }
	if !RequireCompanyAccess(w, r, d.CompanyID) { return }
	if err := h.repo.Create(r.Context(), &d); err != nil { respondError(w, http.StatusInternalServerError, "create failed"); return }
	respondJSON(w, http.StatusCreated, d)
}

func (h *DeviationHandler) Stats(w http.ResponseWriter, r *http.Request) {
	companyID := r.URL.Query().Get("companyId")
	if !IsValidUUID(companyID) { respondError(w, http.StatusBadRequest, "invalid company_id"); return }
	if !RequireCompanyAccess(w, r, companyID) { return }
	stats, err := h.repo.Stats(r.Context(), companyID)
	if err != nil { respondError(w, http.StatusInternalServerError, "stats failed"); return }
	respondJSON(w, http.StatusOK, stats)
}

func (h *DeviationHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct { Resolution string `json:"resolution"`; ResolvedBy string `json:"resolvedBy"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil { respondError(w, http.StatusBadRequest, "invalid body"); return }
	// Scope check: resolve the deviation's owning company before mutating it.
	d, err := h.repo.GetByID(r.Context(), id)
	if err != nil { respondError(w, http.StatusInternalServerError, "resolve failed"); return }
	if d == nil { respondError(w, http.StatusNotFound, "deviation not found"); return }
	if !RequireCompanyAccess(w, r, d.CompanyID) { return }
	if err := h.repo.Update(r.Context(), id, map[string]any{"status": "resolved", "resolution": body.Resolution, "resolvedBy": body.ResolvedBy}); err != nil {
		respondError(w, http.StatusInternalServerError, "resolve failed"); return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
}

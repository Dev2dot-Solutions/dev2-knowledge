package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/ingestion"
	"github.com/go-chi/chi/v5"
)

type IngestionHandler struct {
	pipeline *ingestion.Pipeline
}

func NewIngestionHandler(p *ingestion.Pipeline) *IngestionHandler {
	return &IngestionHandler{pipeline: p}
}

func (h *IngestionHandler) Routes(r chi.Router) {
	r.Route("/ingest", func(r chi.Router) {
		r.Post("/start", h.Start)
		r.Get("/status", h.Status)
	})
}

func (h *IngestionHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CompanyID string `json:"company_id"`
		RepoName  string `json:"repo_name"`
		RepoURL   string `json:"repo_url"`
		LocalPath string `json:"local_path"`
		Language  string `json:"language"`
		Framework string `json:"framework"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.CompanyID == "" || req.RepoName == "" || req.LocalPath == "" {
		respondError(w, http.StatusBadRequest, "company_id, repo_name, and local_path required")
		return
	}
	result, err := h.pipeline.IngestRepository(r.Context(), req.CompanyID, req.RepoName, req.RepoURL, req.LocalPath, req.Language, req.Framework)
	if err != nil {
		log.Printf("[ingestion] Error: %v", err)
		respondError(w, http.StatusInternalServerError, "ingestion failed")
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *IngestionHandler) Status(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "idle"})
}

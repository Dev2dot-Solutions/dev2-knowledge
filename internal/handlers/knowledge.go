package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/models"
	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/repository"
	"github.com/go-chi/chi/v5"
)

type KnowledgeHandler struct {
	entityRepo *repository.EntityRepo
}

func NewKnowledgeHandler(er *repository.EntityRepo) *KnowledgeHandler {
	return &KnowledgeHandler{entityRepo: er}
}

func (h *KnowledgeHandler) Routes(r chi.Router) {
	r.Route("/knowledge", func(r chi.Router) {
		r.Get("/search", h.Search)
		r.Post("/fuzzy", h.FuzzySearch)
		r.Get("/entity/{type}/{id}", h.GetEntity)
		r.Get("/trace/{type}/{id}", h.TraceEntity)
	})
}

func (h *KnowledgeHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	companyID := r.URL.Query().Get("company_id")
	if query == "" || !IsValidUUID(companyID) {
		respondError(w, http.StatusBadRequest, "query and company_id are required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 { limit = 5 }

	typesParam := r.URL.Query().Get("types")
	var searchTypes []string
	if typesParam != "" {
		searchTypes = strings.Split(typesParam, ",")
	} else {
		for _, t := range models.Tier1Types {
			searchTypes = append(searchTypes, string(t))
		}
	}

	result, err := h.entityRepo.SearchCrossEntity(r.Context(), query, companyID, searchTypes, limit)
	if err != nil {
		log.Printf("[knowledge] Search error: %v", err)
		respondError(w, http.StatusInternalServerError, "search failed")
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *KnowledgeHandler) FuzzySearch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query     string   `json:"query"`
		CompanyID string   `json:"company_id"`
		Types     []string `json:"types,omitempty"`
		Limit     int      `json:"limit,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Query == "" || !IsValidUUID(body.CompanyID) {
		respondError(w, http.StatusBadRequest, "query and company_id required")
		return
	}
	if len(body.Types) == 0 {
		for _, t := range models.Tier1Types {
			body.Types = append(body.Types, string(t))
		}
	}
	if body.Limit <= 0 { body.Limit = 5 }
	result, err := h.entityRepo.SearchCrossEntity(r.Context(), body.Query, body.CompanyID, body.Types, body.Limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "search failed")
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *KnowledgeHandler) GetEntity(w http.ResponseWriter, r *http.Request) {
	entityType := chi.URLParam(r, "type")
	id := chi.URLParam(r, "id")
	if entityType == "" || !IsValidUUID(id) {
		respondError(w, http.StatusBadRequest, "invalid type or id")
		return
	}
	result, err := h.entityRepo.GetByID(r.Context(), entityType, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "not found")
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *KnowledgeHandler) TraceEntity(w http.ResponseWriter, r *http.Request) {
	entityType := chi.URLParam(r, "type")
	id := chi.URLParam(r, "id")
	if entityType == "" || !IsValidUUID(id) {
		respondError(w, http.StatusBadRequest, "invalid type or id")
		return
	}
	result, err := h.entityRepo.GetByID(r.Context(), entityType, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "trace failed")
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "not found")
		return
	}
	// Find related entities via entity_relationships
	respondJSON(w, http.StatusOK, result)
}

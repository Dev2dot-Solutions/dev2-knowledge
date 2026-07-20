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
	"go.mongodb.org/mongo-driver/bson"
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
		r.Get("/entities/{type}", h.ListEntities)
		r.Get("/entity/{type}/{id}", h.GetEntity)
		r.Get("/trace/{type}/{id}", h.TraceEntity)
	})
}

func (h *KnowledgeHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	companyID := r.URL.Query().Get("companyId")
	if query == "" || !IsValidUUID(companyID) {
		respondError(w, http.StatusBadRequest, "query and companyId are required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 { limit = 5 }

	typesParam := r.URL.Query().Get("types")
	var searchTypes []string
	if typesParam != "" {
		searchTypes = strings.Split(typesParam, ",")
	} else {
		for _, t := range models.DefaultSearchTypes {
			searchTypes = append(searchTypes, string(t))
		}
	}

	entityFilters := buildEntityFilters(r)
	result, err := h.entityRepo.SearchCrossEntity(r.Context(), query, companyID, searchTypes, limit, entityFilters)
	if err != nil {
		log.Printf("[knowledge] Search error: %v", err)
		respondError(w, http.StatusInternalServerError, "search failed")
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *KnowledgeHandler) FuzzySearch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query      string            `json:"query"`
		CompanyID  string            `json:"companyId"`
		Types      []string          `json:"types,omitempty"`
		Limit      int               `json:"limit,omitempty"`
		Filters    map[string]string `json:"filters,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Query == "" || !IsValidUUID(body.CompanyID) {
		respondError(w, http.StatusBadRequest, "query and companyId required")
		return
	}
	if len(body.Types) == 0 {
		for _, t := range models.DefaultSearchTypes {
			body.Types = append(body.Types, string(t))
		}
	}
	if body.Limit <= 0 { body.Limit = 5 }

	entityFilters := make(map[string]bson.M)
	if body.Filters != nil {
		for et, rawFilter := range body.Filters {
			for _, pair := range strings.Split(rawFilter, ",") {
				parts := strings.SplitN(pair, "=", 2)
				if len(parts) == 2 {
					if entityFilters[et] == nil {
						entityFilters[et] = bson.M{}
					}
					entityFilters[et][parts[0]] = parts[1]
				}
			}
		}
	}

	result, err := h.entityRepo.SearchCrossEntity(r.Context(), body.Query, body.CompanyID, body.Types, body.Limit, entityFilters)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "search failed")
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *KnowledgeHandler) ListEntities(w http.ResponseWriter, r *http.Request) {
	entityType := chi.URLParam(r, "type")
	companyID := r.URL.Query().Get("companyId")
	if !isListableEntityType(entityType) || !IsValidUUID(companyID) {
		respondError(w, http.StatusBadRequest, "valid type and companyId are required")
		return
	}

	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	results, err := h.entityRepo.List(
		r.Context(),
		entityType,
		companyID,
		r.URL.Query().Get("search"),
		r.URL.Query().Get("scope"),
		r.URL.Query().Get("tag"),
		limit,
	)
	if err != nil {
		log.Printf("[knowledge] ListEntities error: %v", err)
		respondError(w, http.StatusInternalServerError, "list failed")
		return
	}

	for _, result := range results {
		result["id"] = result["_id"]
		delete(result, "_id")
		if _, ok := result["name"]; !ok {
			for _, field := range []string{"rule", "topic", "term", "title"} {
				if value, exists := result[field]; exists {
					result["name"] = value
					break
				}
			}
		}
	}
	respondJSON(w, http.StatusOK, results)
}

func (h *KnowledgeHandler) GetEntity(w http.ResponseWriter, r *http.Request) {
	entityType := chi.URLParam(r, "type")
	id := chi.URLParam(r, "id")
	companyID := r.URL.Query().Get("companyId")
	if !isKnownEntityType(entityType) || !IsValidUUID(id) || !validCompanyScope(entityType, companyID) {
		respondError(w, http.StatusBadRequest, "valid type, id and companyId are required")
		return
	}
	result, err := h.entityRepo.GetByIDForCompany(r.Context(), entityType, id, companyID)
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
	companyID := r.URL.Query().Get("companyId")
	if !isKnownEntityType(entityType) || !IsValidUUID(id) || !validCompanyScope(entityType, companyID) {
		respondError(w, http.StatusBadRequest, "valid type, id and companyId are required")
		return
	}
	result, err := h.entityRepo.GetByIDForCompany(r.Context(), entityType, id, companyID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "trace failed")
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "not found")
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func isKnownEntityType(entityType string) bool {
	for _, known := range models.AllEntityTypes {
		if entityType == string(known) {
			return true
		}
	}
	return false
}

func isListableEntityType(entityType string) bool {
	if entityType == string(models.ETExternalDocs) {
		return true
	}
	for _, known := range models.Tier1Types {
		if entityType == string(known) {
			return true
		}
	}
	return false
}

func validCompanyScope(entityType, companyID string) bool {
	return !models.TenantScopedTypes[models.EntityType(entityType)] || IsValidUUID(companyID)
}

func buildEntityFilters(r *http.Request) map[string]bson.M {
	sourceType := r.URL.Query().Get("sourceType")
	urlDomain := r.URL.Query().Get("urlDomain")
	if sourceType == "" && urlDomain == "" {
		return nil
	}
	filter := bson.M{}
	if sourceType != "" {
		filter["sourceType"] = sourceType
	}
	if urlDomain != "" {
		filter["url"] = bson.M{"$regex": urlDomain, "$options": "i"}
	}
	return map[string]bson.M{"external_docs": filter}
}

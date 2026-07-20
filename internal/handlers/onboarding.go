package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ── Handler ─────────────────────────────────────────────────────────────────

type OnboardingHandler struct {
	db *mongo.Database
}

func NewOnboardingHandler(db *mongo.Database) *OnboardingHandler {
	return &OnboardingHandler{db: db}
}

func (h *OnboardingHandler) Routes(r chi.Router) {
	r.Route("/onboarding", func(r chi.Router) {
		r.Post("/start", h.Start)
		r.Get("/discover/{companyId}", h.RunDiscovery)
		r.Post("/discover/{companyId}/accept", h.AcceptDiscovery)
		r.Get("/questions", h.GetQuestions)
		r.Get("/session/{sessionId}", h.GetSession)
		r.Post("/session/{sessionId}/answers", h.SubmitAnswer)
		r.Post("/session/{sessionId}/ingest-docs", h.TriggerIngestion)
		r.Get("/validate/{companyId}", h.ListFindings)
		r.Post("/validate/resolve", h.ResolveFinding)
		r.Patch("/session/{sessionId}/advance", h.AdvanceStage)
		r.Post("/session/{sessionId}/pause", h.PauseSession)
		r.Post("/session/{sessionId}/resume", h.ResumeSession)
	})
}

// ── Session helpers ─────────────────────────────────────────────────────────

func sessionToResponse(s *models.OnboardingSession) models.StartSessionResponse {
	return models.StartSessionResponse{
		SessionID:       s.ID,
		CompanyID:       s.CompanyID,
		Stage:           models.StageNumber(s.CurrentStage),
		CompletedStages: s.CompletedStages,
		Status:          s.Status,
		CreatedAt:       s.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       s.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *OnboardingHandler) getSessionByID(ctx context.Context, sessionID string) (*models.OnboardingSession, error) {
	var session models.OnboardingSession
	err := h.db.Collection("onboarding_sessions").FindOne(ctx, bson.M{"_id": sessionID}).Decode(&session)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return &session, err
}

// ── 1. POST /onboarding/start ───────────────────────────────────────────────

func (h *OnboardingHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req models.StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.CompanyID == "" {
		respondError(w, http.StatusBadRequest, "company_id is required")
		return
	}

	// Check for existing in-progress session
	var existing models.OnboardingSession
	err := h.db.Collection("onboarding_sessions").FindOne(
		r.Context(),
		bson.M{"companyId": req.CompanyID, "status": "in_progress"},
	).Decode(&existing)
	if err == nil {
		respondJSON(w, http.StatusOK, sessionToResponse(&existing))
		return
	}

	now := time.Now().UTC()
	session := models.OnboardingSession{
		ID:              uuid.New().String(),
		CompanyID:       req.CompanyID,
		CurrentStage:    "connect",
		CompletedStages: []string{},
		Answers:         map[string]interface{}{},
		AutoDetected:    []interface{}{},
		ValidationResults: []interface{}{},
		Status:          "in_progress",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	_, err = h.db.Collection("onboarding_sessions").InsertOne(r.Context(), session)
	if err != nil {
		log.Printf("[onboarding] Start error: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	respondJSON(w, http.StatusCreated, sessionToResponse(&session))
}

// ── 1b. GET /onboarding/session/{sessionId} ─────────────────────────────────

func (h *OnboardingHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if !IsValidUUID(sessionID) {
		respondError(w, http.StatusBadRequest, "invalid session_id")
		return
	}

	session, err := h.getSessionByID(r.Context(), sessionID)
	if err != nil {
		log.Printf("[onboarding] GetSession error: %v", err)
		respondError(w, http.StatusInternalServerError, "get session failed")
		return
	}
	if session == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	respondJSON(w, http.StatusOK, sessionToResponse(session))
}

// ── 2. GET /onboarding/discover/{companyId} ─────────────────────────────────

func (h *OnboardingHandler) RunDiscovery(w http.ResponseWriter, r *http.Request) {
	companyID := chi.URLParam(r, "companyId")
	if !IsValidUUID(companyID) {
		respondError(w, http.StatusBadRequest, "invalid company_id")
		return
	}

	results := h.runAutoDiscovery(r.Context(), companyID)

	// Map to frontend Discovery shape: { id, type, name, description, accepted }
	type discoveryItem struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Accepted    bool   `json:"accepted"`
	}

	items := make([]discoveryItem, 0, len(results))
	for i, r := range results {
		name := r.ProposedConvention.Name
		if name == "" {
			name = r.Type + ": " + r.DetectedPattern
		}
		desc := r.ProposedConvention.Description
		if desc == "" {
			desc = r.DetectedPattern + " (confidence: " + fmt.Sprintf("%.0f", r.Confidence*100) + "%)"
		}
		items = append(items, discoveryItem{
			ID:          fmt.Sprintf("discovery-%d", i),
			Type:        r.Type,
			Name:        name,
			Description: desc,
			Accepted:    false,
		})
	}

	respondJSON(w, http.StatusOK, items)
}

// ── 3. POST /onboarding/discover/{companyId}/accept ─────────────────────────

func (h *OnboardingHandler) AcceptDiscovery(w http.ResponseWriter, r *http.Request) {
	companyID := chi.URLParam(r, "companyId")
	if !IsValidUUID(companyID) {
		respondError(w, http.StatusBadRequest, "invalid company_id")
		return
	}

	var req models.AcceptDiscoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}

	if req.SessionID == "" {
		respondError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	if len(req.Accepted) == 0 {
		respondError(w, http.StatusBadRequest, "accepted must be a non-empty array")
		return
	}

	// Fetch session to get auto-detected results
	session, err := h.getSessionByID(r.Context(), req.SessionID)
	if err != nil {
		log.Printf("[onboarding] AcceptDiscovery getSession error: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to get session")
		return
	}
	if session == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	// Decode auto_detected from session
	var autoDetectedResults []map[string]interface{}
	for _, ad := range session.AutoDetected {
		if m, ok := ad.(map[string]interface{}); ok {
			autoDetectedResults = append(autoDetectedResults, m)
		}
	}

	// Create Convention entities for each accepted discovery
	var created []map[string]string
	for _, item := range req.Accepted {
		var sourceResult map[string]interface{}
		for _, r := range autoDetectedResults {
			if r["type"] == item.Type {
				sourceResult = r
				break
			}
		}
		if sourceResult == nil {
			continue
		}

		propConvention, _ := sourceResult["proposedConvention"].(map[string]interface{})

		modified := item.Modified
		if modified == nil {
			modified = map[string]interface{}{}
		}

		convID := uuid.New().String()
		name := ""
		if n, ok := modified["name"].(string); ok {
			name = n
		} else if n, ok := propConvention["name"].(string); ok {
			name = n
		} else {
			name = fmt.Sprintf("Auto-detected: %s", item.Type)
		}

		description := ""
		if d, ok := modified["description"].(string); ok {
			description = d
		} else if d, ok := propConvention["description"].(string); ok {
			description = d
		}

		scope := item.Type
		if s, ok := modified["scope"].(string); ok {
			scope = s
		} else if s, ok := propConvention["scope"].(string); ok {
			scope = s
		}

		tags := []string{}
		if t, ok := modified["tags"].([]interface{}); ok {
			for _, tg := range t {
				if s, ok := tg.(string); ok {
					tags = append(tags, s)
				}
			}
		} else if t, ok := propConvention["tags"].([]interface{}); ok {
			for _, tg := range t {
				if s, ok := tg.(string); ok {
					tags = append(tags, s)
				}
			}
		}

		now := time.Now().UTC()

		// Enrich description with tags/priority for record-keeping
		enrichedDesc := description
		if len(tags) > 0 {
			enrichedDesc += fmt.Sprintf("\nTags: %s", strings.Join(tags, ", "))
		}

		convention := models.Convention{
			ID:          convID,
			CompanyID:   companyID,
			Name:        name,
			Description: enrichedDesc,
			Scope:       scope,
			Body:        "",
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		_, err = h.db.Collection("conventions").InsertOne(r.Context(), convention)
		if err != nil {
			log.Printf("[onboarding] AcceptDiscovery insert convention error: %v", err)
			continue
		}

		created = append(created, map[string]string{
			"type": item.Type,
			"conventionId": convID,
		})
	}

	// Update session auto_detected with accepted flags
	var updatedAutoDetected []interface{}
	for _, ad := range session.AutoDetected {
		m, ok := ad.(map[string]interface{})
		if !ok {
			updatedAutoDetected = append(updatedAutoDetected, ad)
			continue
		}
		for _, item := range req.Accepted {
			if m["type"] == item.Type {
				m["accepted"] = true
				if item.Modified != nil {
					m["modified"] = item.Modified
				}
				break
			}
		}
		updatedAutoDetected = append(updatedAutoDetected, m)
	}

	h.db.Collection("onboarding_sessions").UpdateOne(
		r.Context(),
		bson.M{"_id": req.SessionID},
		bson.M{"$set": bson.M{
			"autoDetected": updatedAutoDetected,
			"updatedAt":    time.Now().UTC(),
		}},
	)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"companyId":        companyID,
		"sessionId":        req.SessionID,
		"accepted":          len(created),
		"conventions":         created,
	})
}

// ── 4. GET /onboarding/questions ────────────────────────────────────────────

func (h *OnboardingHandler) GetQuestions(w http.ResponseWriter, r *http.Request) {
	questions := h.questionBank()
	respondJSON(w, http.StatusOK, questions)
}

// ── 5. POST /onboarding/session/{sessionId}/answers ─────────────────────────

func (h *OnboardingHandler) SubmitAnswer(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if !IsValidUUID(sessionID) {
		respondError(w, http.StatusBadRequest, "invalid session_id")
		return
	}

	var req models.AnswerSubmission
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}

	// Fetch session
	session, err := h.getSessionByID(r.Context(), sessionID)
	if err != nil {
		log.Printf("[onboarding] SubmitAnswer getSession error: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to get session")
		return
	}
	if session == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	// Build question lookup
	qBank := h.questionBank()
	qMap := make(map[string]models.Question)
	for _, q := range qBank {
		qMap[q.ID] = q
	}

	// Build answer mapping lookup
	qDefs := h.questionDefs()
	qDefMap := make(map[string]models.QuestionDef)
	for _, qd := range qDefs {
		qDefMap[qd.ID] = qd
	}

	var results []models.AnswerResult
	for _, answer := range req.Answers {
		if answer.Answer == "" || strings.TrimSpace(answer.Answer) == "" {
			// Skip empty answers
			continue
		}

		q, ok := qMap[answer.QuestionID]
		if !ok {
			log.Printf("[onboarding] SubmitAnswer unknown question: %s", answer.QuestionID)
			continue
		}

		// Get the answer mapping function for this question
		qd, hasDef := qDefMap[answer.QuestionID]
		var entityFields map[string]interface{}
		if hasDef && qd.AnswerMapping != nil {
			entityFields = qd.AnswerMapping(answer.Answer)
		} else {
			// Default mapping - create entity with answer text
			entityFields = map[string]interface{}{
				"name":        fmt.Sprintf("Answer: %s", q.Question),
				"description": answer.Answer,
			}
		}

		// Create entity in appropriate collection
		entityID, err := h.createEntityFromAnswer(r.Context(), session.CompanyID, q.EntityType, entityFields)
		if err != nil {
			log.Printf("[onboarding] SubmitAnswer create entity error: %v", err)
			continue
		}

		results = append(results, models.AnswerResult{
			QuestionID: answer.QuestionID,
			EntityType: q.EntityType,
			EntityID:   entityID,
		})
	}

	// Store answers in session
	if session.Answers == nil {
		session.Answers = map[string]interface{}{}
	}
	for _, r := range results {
		session.Answers[r.QuestionID] = map[string]interface{}{
			"entityType": r.EntityType,
			"entityId":   r.EntityID,
		}
	}

	_, err = h.db.Collection("onboarding_sessions").UpdateOne(
		r.Context(),
		bson.M{"_id": sessionID},
		bson.M{
			"$set": bson.M{
				"answers":    session.Answers,
				"updatedAt": time.Now().UTC(),
			},
		},
	)
	if err != nil {
		log.Printf("[onboarding] SubmitAnswer store error: %v", err)
		respondError(w, http.StatusInternalServerError, "store failed")
		return
	}

	if len(results) == 0 {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"results": []models.AnswerResult{},
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
	})
}

// createEntityFromAnswer creates a knowledge entity based on the entity type
func (h *OnboardingHandler) createEntityFromAnswer(ctx context.Context, companyID, entityType string, fields map[string]interface{}) (string, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	switch entityType {
	case "convention":
		doc := models.Convention{
			ID:          id,
			CompanyID:   companyID,
			Name:        getStringField(fields, "name", "Unnamed convention"),
			Description: getStringField(fields, "description", ""),
			Scope:       getStringField(fields, "scope", ""),
			Body:        "",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		_, err := h.db.Collection("conventions").InsertOne(ctx, doc)
		if err != nil {
			return "", fmt.Errorf("insert convention: %w", err)
		}
		return id, nil

	case "business_rule":
		doc := models.BusinessRule{
			ID:          id,
			CompanyID:   companyID,
			Rule:        getStringField(fields, "rule", ""),
			Description: getStringField(fields, "description", ""),
			Category:    getStringField(fields, "domain", ""),
			Source:      getStringField(fields, "source", "onboarding-interview"),
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		_, err := h.db.Collection("business_rules").InsertOne(ctx, doc)
		if err != nil {
			return "", fmt.Errorf("insert business_rule: %w", err)
		}
		return id, nil

	case "architecture_decision":
		alternatives := []string{}
		if alts, ok := fields["alternatives"].([]string); ok {
			alternatives = alts
		}
		doc := models.ArchitectureDecision{
			ID:           id,
			CompanyID:    companyID,
			Topic:        getStringField(fields, "topic", "Architecture decision"),
			Decision:     getStringField(fields, "decision", ""),
			Rationale:    getStringField(fields, "rationale", ""),
			Alternatives: alternatives,
			Date:         now.Format("2006-01-02"),
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		_, err := h.db.Collection("architecture_decisions").InsertOne(ctx, doc)
		if err != nil {
			return "", fmt.Errorf("insert architecture_decision: %w", err)
		}
		return id, nil

	case "domain_term":
		doc := models.DomainTerm{
			ID:          id,
			CompanyID:   companyID,
			Term:        getStringField(fields, "term", "Term"),
			Definition:  getStringField(fields, "definition", ""),
			Abbreviation: getStringField(fields, "abbreviation", ""),
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		_, err := h.db.Collection("domain_terms").InsertOne(ctx, doc)
		if err != nil {
			return "", fmt.Errorf("insert domain_term: %w", err)
		}
		return id, nil

	case "process":
		steps := []string{}
		if s, ok := fields["steps"].([]string); ok {
			steps = s
		}
		doc := models.Process{
			ID:          id,
			CompanyID:   companyID,
			Name:        getStringField(fields, "name", "Process"),
			Description: getStringField(fields, "description", ""),
			Steps:       steps,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		_, err := h.db.Collection("processes").InsertOne(ctx, doc)
		if err != nil {
			return "", fmt.Errorf("insert process: %w", err)
		}
		return id, nil

	default:
		return "", fmt.Errorf("unknown entity type: %s", entityType)
	}
}

func getStringField(m map[string]interface{}, key, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}

// ── 6. POST /onboarding/session/{sessionId}/ingest-docs ─────────────────────

func (h *OnboardingHandler) TriggerIngestion(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if !IsValidUUID(sessionID) {
		respondError(w, http.StatusBadRequest, "invalid session_id")
		return
	}

	session, err := h.getSessionByID(r.Context(), sessionID)
	if err != nil {
		log.Printf("[onboarding] TriggerIngestion getSession error: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to get session")
		return
	}
	if session == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	result := h.ingestDocuments(r.Context(), session.CompanyID)

	respondJSON(w, http.StatusOK, result)
}

// ── 7. GET /onboarding/validate/{companyId} ─────────────────────────────────

func (h *OnboardingHandler) ListFindings(w http.ResponseWriter, r *http.Request) {
	companyID := chi.URLParam(r, "companyId")
	if !IsValidUUID(companyID) {
		respondError(w, http.StatusBadRequest, "invalid company_id")
		return
	}

	cur, err := h.db.Collection("onboarding_findings").Find(r.Context(), bson.M{"companyId": companyID})
	if err != nil {
		log.Printf("[onboarding] ListFindings error: %v", err)
		respondError(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer cur.Close(r.Context())

	var findings []models.ValidationFinding
	if err := cur.All(r.Context(), &findings); err != nil {
		log.Printf("[onboarding] ListFindings decode error: %v", err)
		respondError(w, http.StatusInternalServerError, "list failed")
		return
	}
	if findings == nil {
		findings = []models.ValidationFinding{}
	}

	respondJSON(w, http.StatusOK, findings)
}

// ── 8. POST /onboarding/validate/resolve ────────────────────────────────────

func (h *OnboardingHandler) ResolveFinding(w http.ResponseWriter, r *http.Request) {
	var req models.ResolveFindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.FindingID == "" {
		respondError(w, http.StatusBadRequest, "findingId required")
		return
	}

	_, err := h.db.Collection("onboarding_findings").UpdateOne(
		r.Context(),
		bson.M{"_id": req.FindingID},
		bson.M{"$set": bson.M{"resolved": true}},
	)
	if err != nil {
		log.Printf("[onboarding] ResolveFinding error: %v", err)
		respondError(w, http.StatusInternalServerError, "resolve failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── 9. PATCH /onboarding/session/{sessionId}/advance ────────────────────────

func (h *OnboardingHandler) AdvanceStage(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if !IsValidUUID(sessionID) {
		respondError(w, http.StatusBadRequest, "invalid session_id")
		return
	}

	var req models.AdvanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Stage < 1 || req.Stage > len(models.StageOrder) {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("stage must be between 1 and %d", len(models.StageOrder)))
		return
	}

	// Fetch current session
	session, err := h.getSessionByID(r.Context(), sessionID)
	if err != nil {
		log.Printf("[onboarding] AdvanceStage getSession error: %v", err)
		respondError(w, http.StatusInternalServerError, "advance failed")
		return
	}
	if session == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	if session.Status == "completed" {
		respondError(w, http.StatusBadRequest, "cannot advance a completed session")
		return
	}
	if session.Status == "paused" {
		respondError(w, http.StatusBadRequest, "session is paused. Resume before advancing.")
		return
	}

	targetStageName := models.StageName(req.Stage)

	// Build completed stages list (append current stage if not already there)
	completed := session.CompletedStages
	currentCompleted := session.CurrentStage
	if currentCompleted != "complete" {
		found := false
		for _, s := range completed {
			if s == currentCompleted {
				found = true
				break
			}
		}
		if !found {
			completed = append(completed, currentCompleted)
		}
	}

	// Check if we're transitioning to complete
	newStatus := session.Status
	if targetStageName == "complete" {
		newStatus = "completed"
	}

	_, err = h.db.Collection("onboarding_sessions").UpdateOne(
		r.Context(),
		bson.M{"_id": sessionID},
		bson.M{"$set": bson.M{
			"currentStage":    targetStageName,
			"completedStages": completed,
			"status":           newStatus,
			"updatedAt":       time.Now().UTC(),
		}},
	)
	if err != nil {
		log.Printf("[onboarding] AdvanceStage error: %v", err)
		respondError(w, http.StatusInternalServerError, "advance failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── 10. POST /onboarding/session/{sessionId}/pause ─────────────────────────

func (h *OnboardingHandler) PauseSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if !IsValidUUID(sessionID) {
		respondError(w, http.StatusBadRequest, "invalid session_id")
		return
	}

	session, err := h.getSessionByID(r.Context(), sessionID)
	if err != nil {
		log.Printf("[onboarding] PauseSession getSession error: %v", err)
		respondError(w, http.StatusInternalServerError, "pause failed")
		return
	}
	if session == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	if session.Status != "in_progress" {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("cannot pause a session with status: %s", session.Status))
		return
	}

	_, err = h.db.Collection("onboarding_sessions").UpdateOne(
		r.Context(),
		bson.M{"_id": sessionID},
		bson.M{"$set": bson.M{"status": "paused", "updatedAt": time.Now().UTC()}},
	)
	if err != nil {
		log.Printf("[onboarding] PauseSession error: %v", err)
		respondError(w, http.StatusInternalServerError, "pause failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── 11. POST /onboarding/session/{sessionId}/resume ────────────────────────

func (h *OnboardingHandler) ResumeSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if !IsValidUUID(sessionID) {
		respondError(w, http.StatusBadRequest, "invalid session_id")
		return
	}

	session, err := h.getSessionByID(r.Context(), sessionID)
	if err != nil {
		log.Printf("[onboarding] ResumeSession getSession error: %v", err)
		respondError(w, http.StatusInternalServerError, "resume failed")
		return
	}
	if session == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	if session.Status != "paused" {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("cannot resume a session with status: %s", session.Status))
		return
	}

	_, err = h.db.Collection("onboarding_sessions").UpdateOne(
		r.Context(),
		bson.M{"_id": sessionID},
		bson.M{"$set": bson.M{"status": "in_progress", "updatedAt": time.Now().UTC()}},
	)
	if err != nil {
		log.Printf("[onboarding] ResumeSession error: %v", err)
		respondError(w, http.StatusInternalServerError, "resume failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ══════════════════════════════════════════════════════════════════════════
// AUTO-DISCOVERY ENGINE
// ══════════════════════════════════════════════════════════════════════════

// ── Naming pattern regexes ───────────────────────────────────────────────

var namingPatterns = []struct {
	name  string
	regex *regexp.Regexp
}{
	{name: "kebab-case", regex: regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)},
	{name: "camelCase", regex: regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)},
	{name: "PascalCase", regex: regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)},
	{name: "snake_case", regex: regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)},
}

var featureDirs = map[string]bool{
	"auth": true, "users": true, "payments": true, "billing": true,
	"orders": true, "products": true, "checkout": true, "admin": true,
	"api": true, "dashboard": true, "profile": true, "settings": true,
	"notifications": true, "search": true, "reports": true, "analytics": true,
	"comments": true, "pages": true, "posts": true, "threads": true,
	"messages": true, "chat": true, "inventory": true, "shipping": true,
	"cart": true, "reviews": true, "ratings": true, "categories": true,
	"tags": true, "account": true, "session": true, "oauth": true,
	"webhooks": true, "email": true, "templates": true,
}

var layerDirs = map[string]bool{
	"controllers": true, "services": true, "repositories": true,
	"models": true, "views": true, "routes": true, "middleware": true,
	"handlers": true, "resolvers": true, "schemas": true, "validators": true,
	"interfaces": true, "adapters": true, "ports": true, "domain": true,
	"infrastructure": true, "application": true, "presentation": true,
	"data": true, "config": true, "utils": true, "helpers": true,
	"lib": true, "core": true, "shared": true, "components": true,
	"layouts": true, "pages": true, "composables": true, "stores": true,
	"types": true, "constants": true, "enums": true, "decorators": true,
	"filters": true, "guards": true, "interceptors": true, "pipes": true,
	"providers": true, "modules": true, "entities": true, "dtos": true,
	"mappers": true, "factories": true, "strategies": true,
}

func (h *OnboardingHandler) runAutoDiscovery(ctx context.Context, companyID string) []models.AutoDetectResult {
	// Run all detectors in parallel
	type detectorResult struct {
		result models.AutoDetectResult
		err    error
	}

	detectors := []func(context.Context, string) models.AutoDetectResult{
		h.detectFileNaming,
		h.detectModuleStructure,
		h.detectErrorHandling,
		h.detectTestingPatterns,
		h.detectAPIPatterns,
		h.detectImportStyle,
	}

	results := make([]models.AutoDetectResult, len(detectors))
	var wg sync.WaitGroup

	for i, det := range detectors {
		wg.Add(1)
		go func(idx int, detector func(context.Context, string) models.AutoDetectResult) {
			defer wg.Done()
			results[idx] = detector(ctx, companyID)
		}(i, det)
	}

	wg.Wait()
	return results
}

// ── 1. File naming detection ──────────────────────────────────────────────

func (h *OnboardingHandler) detectFileNaming(ctx context.Context, companyID string) models.AutoDetectResult {
	filePaths, err := h.getFilePaths(ctx, companyID, 500)
	if err != nil || len(filePaths) == 0 {
		return degradedResult("file_naming", "No files found to analyze")
	}

	counts := map[string]int{"kebab-case": 0, "camelCase": 0, "PascalCase": 0, "snake_case": 0, "other": 0}
	evidenceByPattern := map[string][]string{
		"kebab-case": {}, "camelCase": {}, "PascalCase": {}, "snake_case": {}, "other": {},
	}

	for _, fp := range filePaths {
		filename := path.Base(fp)
		ext := path.Ext(filename)
		nameWithoutExt := filename
		if len(ext) > 0 && len(ext) < len(filename) {
			nameWithoutExt = filename[:len(filename)-len(ext)]
		}

		matched := false
		for _, np := range namingPatterns {
			if np.regex.MatchString(nameWithoutExt) {
				counts[np.name]++
				ev := evidenceByPattern[np.name]
				if len(ev) < 5 {
					evidenceByPattern[np.name] = append(ev, fp)
				}
				matched = true
				break
			}
		}
		if !matched {
			counts["other"]++
			ev := evidenceByPattern["other"]
			if len(ev) < 5 {
				evidenceByPattern["other"] = append(ev, fp)
			}
		}
	}

	total := len(filePaths)
	var dominantPattern string
	var maxCount int
	for _, np := range namingPatterns {
		if counts[np.name] > maxCount {
			maxCount = counts[np.name]
			dominantPattern = np.name
		}
	}
	if maxCount == 0 {
		dominantPattern = "mixed / undetermined"
	}

	confidence := float64(maxCount) / float64(total)
	evidence := evidenceByPattern[dominantPattern]
	if evidence == nil {
		evidence = []string{}
	}
	if len(evidence) > 10 {
		evidence = evidence[:10]
	}

	displayPattern := dominantPattern
	if dominantPattern == "" || maxCount == 0 {
		displayPattern = "mixed / undetermined"
	}

	desc := fmt.Sprintf("%s is the dominant file naming convention (%d%% of files).",
		displayPattern, int(confidence*100))

	return models.AutoDetectResult{
		Type:            "file_naming",
		DetectedPattern: displayPattern,
		Confidence:      float64(int(confidence*100)) / 100,
		Evidence:        evidence,
		ProposedConvention: models.ProposedConvention{
			Name:        fmt.Sprintf("File naming: %s", displayPattern),
			Description: desc,
			Scope:       "file_naming",
			Tags:        []string{"naming", "style"},
			Priority:    3,
		},
	}
}

// ── 2. Module structure detection ─────────────────────────────────────────

func (h *OnboardingHandler) detectModuleStructure(ctx context.Context, companyID string) models.AutoDetectResult {
	filePaths, err := h.getFilePaths(ctx, companyID, 500)
	if err != nil || len(filePaths) == 0 {
		return degradedResult("module_structure", "No files found to analyze")
	}

	topDirs := map[string]int{}
	for _, fp := range filePaths {
		parts := strings.Split(fp, "/")
		if len(parts) < 2 {
			continue
		}
		topDirs[parts[0]]++
	}

	featureCount := 0
	layerCount := 0
	otherCount := 0
	var evidence []string

	for dir := range topDirs {
		if featureDirs[dir] {
			featureCount++
			if len(evidence) < 5 {
				evidence = append(evidence, dir+"/")
			}
		} else if layerDirs[dir] {
			layerCount++
			if len(evidence) < 5 {
				evidence = append(evidence, dir+"/")
			}
		} else {
			otherCount++
		}
	}

	totalDirs := len(topDirs)
	if totalDirs == 0 {
		return degradedResult("module_structure", "No top-level directories found")
	}

	featureRatio := float64(featureCount) / float64(totalDirs)
	layerRatio := float64(layerCount) / float64(totalDirs)

	var detectedPattern, description string
	var confidence float64

	if featureRatio > 0.2 && layerRatio > 0.2 {
		detectedPattern = "hybrid"
		description = fmt.Sprintf("Mixed structure: ~%d%% feature-like directories and ~%d%% layer-like directories.",
			int(featureRatio*100), int(layerRatio*100))
		confidence = featureRatio
		if layerRatio > confidence {
			confidence = layerRatio
		}
	} else if featureRatio > layerRatio {
		detectedPattern = "feature-based"
		description = fmt.Sprintf("Primarily feature-based structure (~%d%% of directories).", int(featureRatio*100))
		confidence = featureRatio
	} else {
		detectedPattern = "layer-based"
		description = fmt.Sprintf("Primarily layer-based structure (~%d%% of directories).", int(layerRatio*100))
		confidence = layerRatio
	}

	if evidence == nil {
		evidence = []string{}
	}
	if len(evidence) > 10 {
		evidence = evidence[:10]
	}

	return models.AutoDetectResult{
		Type:            "module_structure",
		DetectedPattern: detectedPattern,
		Confidence:      float64(int(confidence*100)) / 100,
		Evidence:        evidence,
		ProposedConvention: models.ProposedConvention{
			Name:        fmt.Sprintf("Module structure: %s", detectedPattern),
			Description: description,
			Scope:       "module_structure",
			Tags:        []string{"architecture", "structure"},
			Priority:    4,
		},
	}
}

// ── 3. Error handling detection ───────────────────────────────────────────

func (h *OnboardingHandler) detectErrorHandling(ctx context.Context, companyID string) models.AutoDetectResult {
	funcNames := h.getFunctionNames(ctx, companyID, 500)
	if len(funcNames) == 0 {
		return degradedResult("error_handling", "No functions found to analyze")
	}

	patternCounts := map[string]int{
		"try-catch": 0, "result-type": 0, "custom-exception": 0,
		"null-check": 0, "undetermined": 0,
	}
	var evidence []string

	for _, name := range funcNames {
		lower := strings.ToLower(name)
		switch {
		case strings.HasPrefix(lower, "try") || strings.HasPrefix(lower, "attempt"):
			patternCounts["try-catch"]++
			if len(evidence) < 5 {
				evidence = append(evidence, name)
			}
		case strings.Contains(lower, "result") || strings.Contains(lower, "option") || strings.Contains(lower, "either"):
			patternCounts["result-type"]++
			if len(evidence) < 5 {
				evidence = append(evidence, name)
			}
		case strings.HasSuffix(lower, "error") || strings.HasSuffix(lower, "exception") || strings.Contains(lower, "validate"):
			patternCounts["custom-exception"]++
			if len(evidence) < 5 {
				evidence = append(evidence, name)
			}
		case strings.Contains(lower, "null") || strings.Contains(lower, "undefined") || strings.Contains(lower, "empty"):
			patternCounts["null-check"]++
			if len(evidence) < 5 {
				evidence = append(evidence, name)
			}
		default:
			patternCounts["undetermined"]++
		}
	}

	total := 0
	for _, c := range patternCounts {
		total += c
	}
	if total == 0 {
		return degradedResult("error_handling", "Could not determine error handling patterns")
	}

	dominantPattern := "undetermined"
	maxCount := 0
	for pattern, count := range patternCounts {
		if count > maxCount && pattern != "undetermined" {
			maxCount = count
			dominantPattern = pattern
		}
	}
	if maxCount == 0 {
		return degradedResult("error_handling", "No clear error handling pattern detected")
	}

	confidence := float64(maxCount) / float64(total)
	labelMap := map[string]string{
		"try-catch":       "Try/Catch exception handling",
		"result-type":     "Result/Option/Either types",
		"custom-exception": "Custom exceptions / validation",
		"null-check":      "Null/undefined checks",
	}

	label := labelMap[dominantPattern]
	if label == "" {
		label = dominantPattern
	}

	if evidence == nil {
		evidence = []string{}
	}
	if len(evidence) > 10 {
		evidence = evidence[:10]
	}

	return models.AutoDetectResult{
		Type:            "error_handling",
		DetectedPattern: label,
		Confidence:      float64(int(confidence*100)) / 100,
		Evidence:        evidence,
		ProposedConvention: models.ProposedConvention{
			Name:        fmt.Sprintf("Error handling: %s", label),
			Description: fmt.Sprintf("Dominant error handling pattern is \"%s\" (%d%% of analyzed functions).", label, int(confidence*100)),
			Scope:       "error_handling",
			Tags:        []string{"error-handling", "pattern"},
			Priority:    4,
		},
	}
}

// ── 4. Testing pattern detection ──────────────────────────────────────────

func (h *OnboardingHandler) detectTestingPatterns(ctx context.Context, companyID string) models.AutoDetectResult {
	filePaths, err := h.getFilePaths(ctx, companyID, 1000)
	if err != nil || len(filePaths) == 0 {
		return degradedResult("testing", "No files found to analyze")
	}

	testPatterns := []struct {
		name  string
		regex *regexp.Regexp
	}{
		{name: "*.test.*", regex: regexp.MustCompile(`\.test\.(ts|tsx|js|jsx|kt|go)$`)},
		{name: "*.spec.*", regex: regexp.MustCompile(`\.spec\.(ts|tsx|js|jsx|kt|go)$`)},
		{name: "*_test.*", regex: regexp.MustCompile(`_test\.(go|py|rs)$`)},
		{name: "__tests__/", regex: regexp.MustCompile(`__tests__/`)},
		{name: "test_*", regex: regexp.MustCompile(`test_`)},
	}

	var testFiles []struct{ path, pattern string }
	coLocatedCount := 0
	separateDirCount := 0

	for _, fp := range filePaths {
		for _, tp := range testPatterns {
			if tp.regex.MatchString(fp) {
				testFiles = append(testFiles, struct{ path, pattern string }{fp, tp.name})
				if strings.Contains(fp, "__tests__") || strings.Contains(fp, "/test/") || strings.Contains(fp, "/tests/") {
					separateDirCount++
				} else {
					coLocatedCount++
				}
				break
			}
		}
	}

	if len(testFiles) == 0 {
		return degradedResult("testing", "No test files found with common naming patterns")
	}

	dominantLocation := "co-located"
	if separateDirCount > coLocatedCount {
		dominantLocation = "separate test directory"
	}

	hasSpec := false
	hasTest := false
	for _, tf := range testFiles {
		if strings.Contains(tf.pattern, "spec") {
			hasSpec = true
		}
		if strings.Contains(tf.pattern, "test") {
			hasTest = true
		}
	}

	framework := "unknown"
	if hasSpec && hasTest {
		framework = "Jest/Vitest-style (spec and test patterns)"
	} else if hasSpec {
		framework = "RSpec/Jasmine/Vitest-style (spec patterns)"
	} else {
		for _, tf := range testFiles {
			if tf.pattern == "*_test.*" {
				framework = "Go/Python/Rust standard (_test patterns)"
				break
			}
		}
	}

	confidence := float64(len(testFiles)) / float64(len(filePaths))
	if confidence > 1.0 {
		confidence = 1.0
	}

	evidence := make([]string, 0, 10)
	for _, tf := range testFiles {
		if len(evidence) >= 10 {
			break
		}
		evidence = append(evidence, tf.path)
	}

	return models.AutoDetectResult{
		Type:            "testing",
		DetectedPattern: fmt.Sprintf("%s / %s", dominantLocation, framework),
		Confidence:      float64(int(confidence*100)) / 100,
		Evidence:        evidence,
		ProposedConvention: models.ProposedConvention{
			Name:        "Testing convention",
			Description: fmt.Sprintf("%d test files found. Tests are %s using %s patterns.", len(testFiles), dominantLocation, framework),
			Scope:       "testing",
			Tags:        []string{"testing", "quality"},
			Priority:    3,
		},
	}
}

// ── 5. API pattern detection ──────────────────────────────────────────────

func (h *OnboardingHandler) detectAPIPatterns(ctx context.Context, companyID string) models.AutoDetectResult {
	filePaths, err := h.getFilePaths(ctx, companyID, 500)
	if err != nil || len(filePaths) == 0 {
		return degradedResult("api_design", "No files found to analyze")
	}

	var apiFiles []string
	patterns := map[string]int{"rest": 0, "graphql": 0, "trpc": 0, "unknown": 0}

	for _, fp := range filePaths {
		lower := strings.ToLower(fp)
		if strings.Contains(lower, "/api/") || strings.Contains(lower, "/route") ||
			strings.Contains(lower, "controller") || strings.Contains(lower, "resolver") ||
			strings.Contains(lower, "/graphql") || strings.Contains(lower, "trpc") {
			apiFiles = append(apiFiles, fp)

			if strings.Contains(lower, "graphql") || strings.Contains(lower, "resolver") {
				patterns["graphql"]++
			} else if strings.Contains(lower, "trpc") {
				patterns["trpc"]++
			} else {
				patterns["rest"]++
			}
		}
	}

	if len(apiFiles) == 0 {
		return degradedResult("api_design", "No API-related files found")
	}

	totalAPI := len(apiFiles)
	dominantStyle := "REST"
	maxCount := patterns["rest"]
	if patterns["graphql"] > maxCount {
		dominantStyle = "GraphQL"
		maxCount = patterns["graphql"]
	}
	if patterns["trpc"] > maxCount {
		dominantStyle = "tRPC"
		maxCount = patterns["trpc"]
	}

	confidence := float64(maxCount) / float64(totalAPI)

	evidence := apiFiles
	if len(evidence) > 10 {
		evidence = evidence[:10]
	}

	return models.AutoDetectResult{
		Type:            "api_design",
		DetectedPattern: fmt.Sprintf("%s (%d%% of API files)", dominantStyle, int(confidence*100)),
		Confidence:      float64(int(confidence*100)) / 100,
		Evidence:        evidence,
		ProposedConvention: models.ProposedConvention{
			Name:        fmt.Sprintf("API pattern: %s", dominantStyle),
			Description: fmt.Sprintf("The codebase predominantly uses %s API conventions with %d API-related files detected.", dominantStyle, totalAPI),
			Scope:       "api_design",
			Tags:        []string{"api", "architecture"},
			Priority:    3,
		},
	}
}

// ── 6. Import style detection ─────────────────────────────────────────────

func (h *OnboardingHandler) detectImportStyle(ctx context.Context, companyID string) models.AutoDetectResult {
	importRows, err := h.getImportTargets(ctx, companyID, 500)
	if err != nil || len(importRows) == 0 {
		return degradedResult("import_style", "No import statements found to analyze")
	}

	defaultCount := 0
	namedCount := 0
	namespaceCount := 0
	var evidence []string

	for _, source := range importRows {
		switch {
		case strings.Contains(source, "*"):
			namespaceCount++
			if len(evidence) < 5 {
				evidence = append(evidence, fmt.Sprintf("* as %s", source))
			}
		case strings.Contains(source, "{") || strings.Contains(source, ","):
			namedCount++
			if len(evidence) < 5 {
				evidence = append(evidence, fmt.Sprintf("{ %s }", source))
			}
		default:
			defaultCount++
			if len(evidence) < 5 {
				evidence = append(evidence, fmt.Sprintf("default %s", source))
			}
		}
	}

	total := defaultCount + namedCount + namespaceCount
	if total == 0 {
		return degradedResult("import_style", "Could not classify import styles")
	}

	dominantStyle := "default imports"
	maxCount := defaultCount
	if namedCount > maxCount {
		dominantStyle = "named imports"
		maxCount = namedCount
	}
	if namespaceCount > maxCount {
		dominantStyle = "namespace imports"
		maxCount = namespaceCount
	}

	confidence := float64(maxCount) / float64(total)

	return models.AutoDetectResult{
		Type:            "import_style",
		DetectedPattern: fmt.Sprintf("%s (%d%% of %d imports)", dominantStyle, int(confidence*100), total),
		Confidence:      float64(int(confidence*100)) / 100,
		Evidence:        evidence,
		ProposedConvention: models.ProposedConvention{
			Name:        fmt.Sprintf("Import style: %s", dominantStyle),
			Description: fmt.Sprintf("The codebase predominantly uses %s. Default imports: %d, Named: %d, Namespace: %d.", dominantStyle, defaultCount, namedCount, namespaceCount),
			Scope:       "import_style",
			Tags:        []string{"style", "imports"},
			Priority:    2,
		},
	}
}

// ── Discovery helpers ─────────────────────────────────────────────────────

func degradedResult(detectorType, reason string) models.AutoDetectResult {
	return models.AutoDetectResult{
		Type:            detectorType,
		DetectedPattern: "insufficient data",
		Confidence:      0,
		Evidence:        []string{},
		ProposedConvention: models.ProposedConvention{
			Name:        fmt.Sprintf("%s: insufficient data", detectorType),
			Description: fmt.Sprintf("Auto-detection skipped. Reason: %s", reason),
			Scope:       detectorType,
			Tags:        []string{},
			Priority:    0,
		},
	}
}

func (h *OnboardingHandler) getFilePaths(ctx context.Context, companyID string, limit int) ([]string, error) {
	// Get repos for company
	repoCursor, err := h.db.Collection("repos").Find(ctx, bson.M{"companyId": companyID}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer repoCursor.Close(ctx)

	var repoIDs []string
	for repoCursor.Next(ctx) {
		var r struct {
			ID string `bson:"_id"`
		}
		if err := repoCursor.Decode(&r); err == nil {
			repoIDs = append(repoIDs, r.ID)
		}
	}

	if len(repoIDs) == 0 {
		return nil, nil
	}

	fileCursor, err := h.db.Collection("files").Find(ctx,
		bson.M{"repoId": bson.M{"$in": repoIDs}},
		options.Find().SetProjection(bson.M{"path": 1}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	defer fileCursor.Close(ctx)

	var paths []string
	for fileCursor.Next(ctx) {
		var f struct {
			Path string `bson:"path"`
		}
		if err := fileCursor.Decode(&f); err == nil {
			paths = append(paths, f.Path)
		}
	}

	return paths, nil
}

func (h *OnboardingHandler) getFunctionNames(ctx context.Context, companyID string, limit int) []string {
	repoCursor, err := h.db.Collection("repos").Find(ctx, bson.M{"companyId": companyID}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil
	}
	defer repoCursor.Close(ctx)

	var repoIDs []string
	for repoCursor.Next(ctx) {
		var r struct{ ID string `bson:"_id"` }
		if err := repoCursor.Decode(&r); err == nil {
			repoIDs = append(repoIDs, r.ID)
		}
	}
	if len(repoIDs) == 0 {
		return nil
	}

	fileIDs := h.getFileIDsByRepoIDs(ctx, repoIDs, limit*2)
	if len(fileIDs) == 0 {
		return nil
	}

	fnCursor, err := h.db.Collection("functions").Find(ctx,
		bson.M{"fileId": bson.M{"$in": fileIDs}},
		options.Find().SetProjection(bson.M{"name": 1}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil
	}
	defer fnCursor.Close(ctx)

	var names []string
	for fnCursor.Next(ctx) {
		var fn struct{ Name string `bson:"name"` }
		if err := fnCursor.Decode(&fn); err == nil {
			names = append(names, fn.Name)
		}
	}
	return names
}

func (h *OnboardingHandler) getFileIDsByRepoIDs(ctx context.Context, repoIDs []string, limit int) []string {
	cursor, err := h.db.Collection("files").Find(ctx,
		bson.M{"repoId": bson.M{"$in": repoIDs}},
		options.Find().SetProjection(bson.M{"_id": 1}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil
	}
	defer cursor.Close(ctx)

	var ids []string
	for cursor.Next(ctx) {
		var f struct{ ID string `bson:"_id"` }
		if err := cursor.Decode(&f); err == nil {
			ids = append(ids, f.ID)
		}
	}
	return ids
}

func (h *OnboardingHandler) getImportTargets(ctx context.Context, companyID string, limit int) ([]string, error) {
	repoCursor, err := h.db.Collection("repos").Find(ctx, bson.M{"companyId": companyID}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer repoCursor.Close(ctx)

	var repoIDs []string
	for repoCursor.Next(ctx) {
		var r struct{ ID string `bson:"_id"` }
		if err := repoCursor.Decode(&r); err == nil {
			repoIDs = append(repoIDs, r.ID)
		}
	}
	if len(repoIDs) == 0 {
		return nil, nil
	}

	fileIDs := h.getFileIDsByRepoIDs(ctx, repoIDs, limit*2)
	if len(fileIDs) == 0 {
		return nil, nil
	}

	importCursor, err := h.db.Collection("imports").Find(ctx,
		bson.M{"fileId": bson.M{"$in": fileIDs}},
		options.Find().SetProjection(bson.M{"sourceEntity": 1}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	defer importCursor.Close(ctx)

	var targets []string
	for importCursor.Next(ctx) {
		var imp struct{ SourceEntity string `bson:"sourceEntity"` }
		if err := importCursor.Decode(&imp); err == nil {
			targets = append(targets, imp.SourceEntity)
		}
	}
	return targets, nil
}

// ══════════════════════════════════════════════════════════════════════════
// DOCUMENT INGESTION
// ══════════════════════════════════════════════════════════════════════════

func (h *OnboardingHandler) ingestDocuments(ctx context.Context, companyID string) models.DocIngestionResult {
	result := models.DocIngestionResult{
		RepoPath: "",
		Errors:   []string{},
	}

	// Get repos for this company
	repoCursor, err := h.db.Collection("repos").Find(ctx, bson.M{"companyId": companyID}, options.Find().SetProjection(bson.M{"_id": 1, "name": 1, "url": 1}))
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to query repos: %v", err))
		return result
	}
	defer repoCursor.Close(ctx)

	type repoInfo struct {
		ID   string `bson:"_id"`
		Name string `bson:"name"`
		URL  string `bson:"url"`
		// LocalPath is not stored — using name as placeholder
	}
	var repos []repoInfo
	if err := repoCursor.All(ctx, &repos); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to decode repos: %v", err))
		return result
	}

	if len(repos) == 0 {
		result.Errors = append(result.Errors, "no repos found for this company")
		return result
	}

	// For each repo, look for file paths that indicate documentation
	for _, repo := range repos {
		docFiles, err := h.findDocFiles(ctx, repo.ID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("repo %s: %v", repo.Name, err))
			continue
		}

		if len(docFiles) == 0 {
			continue
		}

		result.RepoPath = repo.Name

		// Process doc files and create entities
		for _, docPath := range docFiles {
			h.processDocFile(ctx, companyID, repo.ID, docPath, &result)
		}
	}

	return result
}

func (h *OnboardingHandler) findDocFiles(ctx context.Context, repoID string) ([]string, error) {
	docPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)readme\.md$`),
		regexp.MustCompile(`(?i)contributing\.(md|adoc)$`),
		regexp.MustCompile(`(?i)architecture\.md$`),
		regexp.MustCompile(`(?i)adr.*\.md$`),
		regexp.MustCompile(`(?i)changelog\.md$`),
		regexp.MustCompile(`(?i)license(\.md|\.txt)?$`),
		regexp.MustCompile(`(?i)code_of_conduct\.md$`),
		regexp.MustCompile(`(?i)security\.md$`),
		regexp.MustCompile(`(?i)docs/.*\.md$`),
		regexp.MustCompile(`(?i)documentation/.*\.md$`),
	}

	cursor, err := h.db.Collection("files").Find(ctx,
		bson.M{"repoId": repoID},
		options.Find().SetProjection(bson.M{"path": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docFiles []string
	for cursor.Next(ctx) {
		var f struct{ Path string `bson:"path"` }
		if err := cursor.Decode(&f); err != nil {
			continue
		}
		for _, pat := range docPatterns {
			if pat.MatchString(f.Path) {
				docFiles = append(docFiles, f.Path)
				break
			}
		}
	}

	return docFiles, nil
}

func (h *OnboardingHandler) processDocFile(ctx context.Context, companyID, repoID, docPath string, result *models.DocIngestionResult) {
	// Determine entity type from the doc file path
	lower := strings.ToLower(docPath)

	now := time.Now().UTC()

	switch {
	case strings.Contains(lower, "architecture") || strings.Contains(lower, "adr"):
		// Create architecture decision
		doc := models.ArchitectureDecision{
			ID:        uuid.New().String(),
			CompanyID: companyID,
			Topic:     fmt.Sprintf("Architecture: %s", path.Base(docPath)),
			Decision:  fmt.Sprintf("Extracted from documentation file: %s", docPath),
			Rationale: "Auto-extracted during onboarding document ingestion",
			Date:      now.Format("2006-01-02"),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := h.db.Collection("architecture_decisions").InsertOne(ctx, doc); err == nil {
			result.ArchitectureDecisionsFound++
		}

	case strings.Contains(lower, "contributing") || strings.Contains(lower, "convention") || strings.Contains(lower, "standards") || strings.Contains(lower, "code_of_conduct") || strings.Contains(lower, "security"):
		// Create convention
		doc := models.Convention{
			ID:          uuid.New().String(),
			CompanyID:   companyID,
			Name:        fmt.Sprintf("Documented: %s", path.Base(docPath)),
			Description: fmt.Sprintf("Extracted from %s during onboarding.", docPath),
			Scope:       "documented",
			Body:        "",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if _, err := h.db.Collection("conventions").InsertOne(ctx, doc); err == nil {
			result.ConventionsFound++
		}

	case strings.Contains(lower, "readme") || strings.Contains(lower, "getting-started") || strings.Contains(lower, "workflow") || strings.Contains(lower, "process"):
		// Create process
		doc := models.Process{
			ID:          uuid.New().String(),
			CompanyID:   companyID,
			Name:        fmt.Sprintf("Process: %s", path.Base(docPath)),
			Description: fmt.Sprintf("Extracted from %s during onboarding.", docPath),
			Steps:       []string{},
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if _, err := h.db.Collection("processes").InsertOne(ctx, doc); err == nil {
			result.ProcessesFound++
		}

	case strings.Contains(lower, "license"):
		// Create business rule
		doc := models.BusinessRule{
			ID:          uuid.New().String(),
			CompanyID:   companyID,
			Rule:        fmt.Sprintf("License file: %s", path.Base(docPath)),
			Description: fmt.Sprintf("Extracted from %s during onboarding.", docPath),
			Category:    "licensing",
			Source:      "onboarding-ingestion",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if _, err := h.db.Collection("business_rules").InsertOne(ctx, doc); err == nil {
			result.ConventionsFound++
		}

	default:
		// Generic documentation — create domain term or convention
		doc := models.DomainTerm{
			ID:          uuid.New().String(),
			CompanyID:   companyID,
			Term:        fmt.Sprintf("Doc reference: %s", path.Base(docPath)),
			Definition:  fmt.Sprintf("Extracted from %s during onboarding.", docPath),
			Abbreviation: "",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if _, err := h.db.Collection("domain_terms").InsertOne(ctx, doc); err == nil {
			result.DomainTermsFound++
		}
	}
}

// ══════════════════════════════════════════════════════════════════════════
// QUESTION BANK
// ══════════════════════════════════════════════════════════════════════════

func (h *OnboardingHandler) questionBank() []models.Question {
	return []models.Question{
		// ── Section A: Tech Stack & Architecture ──
		{
			ID: "q1", Section: "A", Type: "text",
			Question: "What framework(s) are you using and why?",
			EntityType: "architecture_decision",
			Prompt: "e.g., Nuxt 4 for SSR with Vue, Spring Boot for microservices, etc.",
			SkipAllowed: true,
		},
		{
			ID: "q2", Section: "A", Type: "text",
			Question: "What is your architecture pattern?",
			EntityType: "convention",
			Prompt: "e.g., Layered architecture, Hexagonal / Ports & Adapters, Clean Architecture, Feature-based",
			SkipAllowed: true,
		},
		{
			ID: "q3", Section: "A", Type: "text",
			Question: "How do you structure your modules or packages?",
			EntityType: "convention",
			Prompt: "e.g., by feature (payments/, auth/), by layer (controllers/, services/), or hybrid",
			SkipAllowed: true,
		},

		// ── Section B: Conventions & Patterns ──
		{
			ID: "q4", Section: "B", Type: "text",
			Question: "How do you handle errors in your code?",
			EntityType: "convention",
			Prompt: "e.g., try/catch with custom exceptions, Result/Option types, sentinel values, panic recovery",
			SkipAllowed: true,
		},
		{
			ID: "q5", Section: "B", Type: "text",
			Question: "What is your testing strategy?",
			EntityType: "convention",
			Prompt: "e.g., unit + integration + e2e, TDD, test coverage targets, framework choices",
			SkipAllowed: true,
		},
		{
			ID: "q6", Section: "B", Type: "text",
			Question: "How do you handle configuration and environment variables?",
			EntityType: "convention",
			Prompt: "e.g., .env files, environment-specific configs, config service, feature flags",
			SkipAllowed: true,
		},
		{
			ID: "q7", Section: "B", Type: "text",
			Question: "What logging approach do you use?",
			EntityType: "convention",
			Prompt: "e.g., structured logging (JSON), log levels, centralised log aggregation",
			SkipAllowed: true,
		},
		{
			ID: "q8", Section: "B", Type: "text",
			Question: "What code review standards do you enforce?",
			EntityType: "process",
			Prompt: "e.g., required reviewers, linter rules, PR templates, approval count",
			SkipAllowed: true,
		},

		// ── Section C: Business Rules ──
		{
			ID: "q9", Section: "C", Type: "text",
			Question: "What business rules are critical to get right?",
			EntityType: "business_rule",
			Prompt: "e.g., pricing calculations, access control rules, data retention policies",
			SkipAllowed: true,
		},
		{
			ID: "q10", Section: "C", Type: "text",
			Question: "Are there any regulatory or compliance requirements you must follow?",
			EntityType: "business_rule",
			Prompt: "e.g., GDPR, SOC2, HIPAA, PCI-DSS, data localisation laws",
			SkipAllowed: true,
		},
		{
			ID: "q11", Section: "C", Type: "text",
			Question: "What data validation rules must always be enforced?",
			EntityType: "business_rule",
			Prompt: "e.g., email format, age restrictions, field length limits, uniqueness constraints",
			SkipAllowed: true,
		},

		// ── Section D: Architecture Decisions ──
		{
			ID: "q12", Section: "D", Type: "text",
			Question: "What major architecture decisions have you made and why?",
			EntityType: "architecture_decision",
			Prompt: "e.g., chose Postgres over Mongo for relational integrity, adopted event-driven communication",
			SkipAllowed: true,
		},
		{
			ID: "q13", Section: "D", Type: "text",
			Question: "Are there any decisions you would reverse if you could?",
			EntityType: "architecture_decision",
			Prompt: "e.g., a library choice that proved problematic, a monolith that should have been split earlier",
			SkipAllowed: true,
		},
		{
			ID: "q14", Section: "D", Type: "text",
			Question: "What technical debt are you currently aware of?",
			EntityType: "architecture_decision",
			Prompt: "e.g., outdated dependencies, missing tests, workarounds that need refactoring",
			SkipAllowed: true,
		},

		// ── Section E: Domain Language ──
		{
			ID: "q15", Section: "E", Type: "text",
			Question: "What terms have specific meanings in your company or domain?",
			EntityType: "domain_term",
			Prompt: "e.g., \"lead\", \"opportunity\", \"deal\" in a CRM; \"epic\", \"story\" in agile",
			SkipAllowed: true,
		},
		{
			ID: "q16", Section: "E", Type: "text",
			Question: "What abbreviations or acronyms do you commonly use?",
			EntityType: "domain_term",
			Prompt: "e.g., ARR (Annual Recurring Revenue), NPS (Net Promoter Score), SLA (Service Level Agreement)",
			SkipAllowed: true,
		},

		// ── Section F: Process ──
		{
			ID: "q17", Section: "F", Type: "text",
			Question: "Walk me through your development workflow from ticket to deployment.",
			EntityType: "process",
			Prompt: "e.g., ticket creation → branch from main → PR → review → merge → CI/CD → staging → production",
			SkipAllowed: true,
		},
		{
			ID: "q18", Section: "F", Type: "text",
			Question: "How do you handle hotfixes?",
			EntityType: "process",
			Prompt: "e.g., hotfix branch from main, expedited review, direct deployment",
			SkipAllowed: true,
		},
		{
			ID: "q19", Section: "F", Type: "text",
			Question: "What is your release process?",
			EntityType: "process",
			Prompt: "e.g., release branches, semantic versioning, changelog generation, release candidates",
			SkipAllowed: true,
		},
		{
			ID: "q20", Section: "F", Type: "text",
			Question: "How do you handle incidents?",
			EntityType: "process",
			Prompt: "e.g., alerting → on-call rotation → incident channel → post-mortem",
			SkipAllowed: true,
		},
	}
}

// ── Question definitions with answer mappings ─────────────────────────────

func (h *OnboardingHandler) questionDefs() []models.QuestionDef {
	return []models.QuestionDef{
		{
			Question: models.Question{
				ID: "q1", Section: "A", Type: "text",
				Question: "What framework(s) are you using and why?",
				EntityType: "architecture_decision",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"topic":       "Framework selection",
					"decision":    answer,
					"rationale":   "Captured during onboarding knowledge seeding",
					"alternatives": []string{},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q2", Section: "A", Type: "text",
				Question: "What is your architecture pattern?",
				EntityType: "convention",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Architecture pattern",
					"description": answer,
					"scope":       "architecture",
					"tags":        []string{"architecture", "pattern"},
					"priority":    5,
				}
			},
		},
		{
			Question: models.Question{
				ID: "q3", Section: "A", Type: "text",
				Question: "How do you structure your modules or packages?",
				EntityType: "convention",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Module structure",
					"description": answer,
					"scope":       "module_structure",
					"tags":        []string{"architecture", "structure"},
					"priority":    4,
				}
			},
		},
		{
			Question: models.Question{
				ID: "q4", Section: "B", Type: "text",
				Question: "How do you handle errors in your code?",
				EntityType: "convention",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Error handling pattern",
					"description": answer,
					"scope":       "error_handling",
					"tags":        []string{"error-handling", "pattern"},
					"priority":    4,
				}
			},
		},
		{
			Question: models.Question{
				ID: "q5", Section: "B", Type: "text",
				Question: "What is your testing strategy?",
				EntityType: "convention",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Testing strategy",
					"description": answer,
					"scope":       "testing",
					"tags":        []string{"testing", "quality"},
					"priority":    4,
				}
			},
		},
		{
			Question: models.Question{
				ID: "q6", Section: "B", Type: "text",
				Question: "How do you handle configuration and environment variables?",
				EntityType: "convention",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Configuration management",
					"description": answer,
					"scope":       "configuration",
					"tags":        []string{"configuration", "devops"},
					"priority":    3,
				}
			},
		},
		{
			Question: models.Question{
				ID: "q7", Section: "B", Type: "text",
				Question: "What logging approach do you use?",
				EntityType: "convention",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Logging approach",
					"description": answer,
					"scope":       "observability",
					"tags":        []string{"logging", "observability"},
					"priority":    3,
				}
			},
		},
		{
			Question: models.Question{
				ID: "q8", Section: "B", Type: "text",
				Question: "What code review standards do you enforce?",
				EntityType: "process",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Code review process",
					"description": answer,
					"steps":       []string{},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q9", Section: "C", Type: "text",
				Question: "What business rules are critical to get right?",
				EntityType: "business_rule",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"domain": "Critical business rules",
					"rule":   answer,
					"source": "onboarding-interview",
					"tags":   []string{"critical"},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q10", Section: "C", Type: "text",
				Question: "Are there any regulatory or compliance requirements you must follow?",
				EntityType: "business_rule",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"domain": "Regulatory compliance",
					"rule":   answer,
					"source": "onboarding-interview",
					"tags":   []string{"compliance", "regulatory"},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q11", Section: "C", Type: "text",
				Question: "What data validation rules must always be enforced?",
				EntityType: "business_rule",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"domain": "Data validation",
					"rule":   answer,
					"source": "onboarding-interview",
					"tags":   []string{"validation"},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q12", Section: "D", Type: "text",
				Question: "What major architecture decisions have you made and why?",
				EntityType: "architecture_decision",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"topic":       "Major architecture decisions",
					"decision":    answer,
					"rationale":   "Captured during onboarding knowledge seeding",
					"alternatives": []string{},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q13", Section: "D", Type: "text",
				Question: "Are there any decisions you would reverse if you could?",
				EntityType: "architecture_decision",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"topic":       "Decisions to reconsider",
					"decision":    answer,
					"rationale":   "Identified during onboarding as potential technical debt",
					"alternatives": []string{},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q14", Section: "D", Type: "text",
				Question: "What technical debt are you currently aware of?",
				EntityType: "architecture_decision",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"topic":       "Known technical debt",
					"decision":    answer,
					"rationale":   "Documented during onboarding for future planning",
					"alternatives": []string{},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q15", Section: "E", Type: "text",
				Question: "What terms have specific meanings in your company or domain?",
				EntityType: "domain_term",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"term":       "Company-specific terminology",
					"definition": answer,
					"context":    "General domain language",
				}
			},
		},
		{
			Question: models.Question{
				ID: "q16", Section: "E", Type: "text",
				Question: "What abbreviations or acronyms do you commonly use?",
				EntityType: "domain_term",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"term":       "Common abbreviations and acronyms",
					"definition": answer,
					"context":    "Company glossary",
				}
			},
		},
		{
			Question: models.Question{
				ID: "q17", Section: "F", Type: "text",
				Question: "Walk me through your development workflow from ticket to deployment.",
				EntityType: "process",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Development workflow",
					"description": answer,
					"steps":       []string{},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q18", Section: "F", Type: "text",
				Question: "How do you handle hotfixes?",
				EntityType: "process",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Hotfix process",
					"description": answer,
					"steps":       []string{},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q19", Section: "F", Type: "text",
				Question: "What is your release process?",
				EntityType: "process",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Release process",
					"description": answer,
					"steps":       []string{},
				}
			},
		},
		{
			Question: models.Question{
				ID: "q20", Section: "F", Type: "text",
				Question: "How do you handle incidents?",
				EntityType: "process",
			},
			SkipAllowed: true,
			AnswerMapping: func(answer string) map[string]interface{} {
				return map[string]interface{}{
					"name":        "Incident response process",
					"description": answer,
					"steps":       []string{},
				}
			},
		},
	}
}

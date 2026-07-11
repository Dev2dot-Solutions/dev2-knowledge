package nats

import (
	"context"
	"encoding/json"
	"log"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/ingestion"
	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/models"
	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/repository"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

const (
	SubjectKnowledgeSearch    = "knowledge.search"
	SubjectKnowledgeEntityGet = "knowledge.entity.get"
	SubjectKnowledgeEntityResolve = "knowledge.entity.resolve"
	SubjectKnowledgeIngest    = "knowledge.ingest"
	SubjectKnowledgeLink      = "knowledge.link"
	SubjectKnowledgeIngested  = "knowledge.ingested"
	SubjectKnowledgeUpdated   = "knowledge.entity.updated"
)

type Handler struct {
	nc         *nats.Conn
	enc        *nats.EncodedConn
	entityRepo *repository.EntityRepo
	pipeline   *ingestion.Pipeline
}

func NewHandler(nc *nats.Conn, er *repository.EntityRepo, pipe *ingestion.Pipeline) (*Handler, error) {
	if nc == nil { return &Handler{}, nil }
	enc, err := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
	if err != nil { return nil, err }
	h := &Handler{nc: nc, enc: enc, entityRepo: er, pipeline: pipe}
	nc.Subscribe(SubjectKnowledgeSearch, h.handleSearch)
	nc.Subscribe(SubjectKnowledgeEntityGet, h.handleEntityGet)
	nc.Subscribe(SubjectKnowledgeEntityResolve, h.handleEntityResolve)
	nc.Subscribe(SubjectKnowledgeIngest, h.handleIngest)
	nc.Subscribe(SubjectKnowledgeLink, h.handleLink)
	log.Printf("[nats] Subscribed to knowledge.* subjects")
	return h, nil
}

func (h *Handler) handleSearch(msg *nats.Msg) {
	var req models.KnowledgeSearchRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		h.respondError(msg, "invalid request: "+err.Error()); return
	}
	types := req.Types
	if len(types) == 0 {
		for _, t := range models.Tier1Types { types = append(types, string(t)) }
	}
	limit := req.Limit
	if limit <= 0 { limit = 5 }
	result, err := h.entityRepo.SearchCrossEntity(context.Background(), req.Query, req.CompanyID, types, limit)
	if err != nil { h.respondError(msg, "search failed: "+err.Error()); return }
	data, _ := json.Marshal(result)
	msg.Respond(data)
}

func (h *Handler) handleEntityGet(msg *nats.Msg) {
	var req models.KnowledgeEntityGetRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil { h.respondError(msg, "invalid request"); return }
	result, err := h.entityRepo.GetByID(context.Background(), req.Type, req.ID)
	if err != nil { h.respondError(msg, "lookup failed: "+err.Error()); return }
	if result == nil { h.respondError(msg, "entity not found"); return }
	data, _ := json.Marshal(result)
	msg.Respond(data)
}

func (h *Handler) handleEntityResolve(msg *nats.Msg) {
	var req models.KnowledgeEntityResolveRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil { h.respondError(msg, "invalid request"); return }
	result, err := h.entityRepo.ResolveEntityIdentity(context.Background(), req.Type, req.ID)
	if err != nil { h.respondError(msg, "resolve failed: "+err.Error()); return }
	if result == nil { h.respondError(msg, "entity not found"); return }
	data, _ := json.Marshal(result)
	msg.Respond(data)
}

func (h *Handler) handleIngest(msg *nats.Msg) {
	var req models.KnowledgeIngestRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil { h.respondError(msg, "invalid request"); return }
	result, err := h.pipeline.IngestRepository(context.Background(),
		req.CompanyID, req.RepoName, req.RepoURL, req.LocalPath, req.Language, req.Framework)
	if err != nil { h.respondError(msg, "ingestion failed: "+err.Error()); return }
	data, _ := json.Marshal(result)
	msg.Respond(data)
	h.enc.Publish(SubjectKnowledgeIngested, map[string]any{
		"repo_id": result.RepoID, "company_id": req.CompanyID, "repo_name": req.RepoName,
		"files_processed": result.FilesProcessed, "duration_ms": result.DurationMs, "status": "success",
	})
}

func (h *Handler) handleLink(msg *nats.Msg) {
	var req models.KnowledgeLinkRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil { log.Printf("[nats] Invalid link: %v", err); return }
	go h.linkTicketToKnowledge(req)
}

func (h *Handler) linkTicketToKnowledge(req models.KnowledgeLinkRequest) {
	queryText := req.Title
	if req.Description != "" { queryText += " " + req.Description }
	types := []string{"conventions", "business_rules", "domain_terms", "architecture_decisions", "processes"}
	result, err := h.entityRepo.SearchCrossEntity(context.Background(), queryText, req.CompanyID, types, 5)
	if err != nil { log.Printf("[nats] Link search failed: %v", err); return }
	for entityType, hits := range result.Results {
		for _, hit := range hits {
			h.entityRepo.Create(context.Background(), "entity_relationships", map[string]any{
				"_id": uuid.New().String(), "source_entity_type": "tickets",
				"source_entity_id": req.TicketID, "relationship_type": "references",
				"target_entity_type": entityType, "target_entity_id": hit.ID,
			})
		}
	}
	log.Printf("[nats] Linked ticket %s to %d entities", req.TicketID, result.TotalMatches)
}

func (h *Handler) respondError(msg *nats.Msg, errMsg string) {
	resp := map[string]string{"error": errMsg}
	data, _ := json.Marshal(resp)
	msg.Respond(data)
}

func (h *Handler) Close() {
	if h.nc != nil { h.nc.Drain() }
}

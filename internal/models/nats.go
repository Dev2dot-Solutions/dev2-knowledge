package models

// KnowledgeSearchRequest is received via NATS for knowledge.search.
type KnowledgeSearchRequest struct {
	Query     string   `json:"query"`
	CompanyID string   `json:"company_id"`
	Types     []string `json:"types,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

// KnowledgeEntityGetRequest is received via NATS for knowledge.entity.get.
type KnowledgeEntityGetRequest struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CompanyID string `json:"company_id,omitempty"`
}

// KnowledgeEntityResolveRequest is received via NATS for knowledge.entity.resolve.
type KnowledgeEntityResolveRequest struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// KnowledgeEntityResolveResponse is the response for entity.resolve.
type KnowledgeEntityResolveResponse struct {
	Type  string  `json:"type"`
	ID    string  `json:"id"`
	Name  *string `json:"name,omitempty"`
	Label *string `json:"label,omitempty"`
}

// KnowledgeLinkRequest is received via NATS for knowledge.link (from dev2-tickets).
type KnowledgeLinkRequest struct {
	TicketID    string `json:"ticket_id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	CompanyID   string `json:"company_id"`
}

// KnowledgeIngestRequest is received via NATS for knowledge.ingest.
type KnowledgeIngestRequest struct {
	CompanyID string `json:"company_id"`
	RepoName  string `json:"repo_name"`
	RepoURL   string `json:"repo_url,omitempty"`
	LocalPath string `json:"local_path"`
	Language  string `json:"language,omitempty"`
	Framework string `json:"framework,omitempty"`
}

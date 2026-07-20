package models

// KnowledgeSearchRequest is received via NATS for knowledge.search.
type KnowledgeSearchRequest struct {
	Query     string   `json:"query"`
	CompanyID string   `json:"companyId"`
	Types     []string `json:"types,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

// KnowledgeEntityGetRequest is received via NATS for knowledge.entity.get.
type KnowledgeEntityGetRequest struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CompanyID string `json:"companyId,omitempty"`
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
	TicketID    string `json:"ticketId"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	CompanyID   string `json:"companyId"`
}

// KnowledgeIngestRequest is received via NATS for knowledge.ingest.
type KnowledgeIngestRequest struct {
	CompanyID string `json:"companyId"`
	RepoName  string `json:"repoName"`
	RepoURL   string `json:"repoUrl,omitempty"`
	LocalPath string `json:"localPath"`
	Language  string `json:"language,omitempty"`
	Framework string `json:"framework,omitempty"`
}

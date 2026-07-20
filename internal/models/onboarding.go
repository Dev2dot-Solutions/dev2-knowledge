package models

import "time"

// ── Stage constants ─────────────────────────────────────────────────────────

var StageOrder = []string{"connect", "discover", "interview", "ingest_docs", "validate", "complete"}

var StageLabels = []string{"Connect", "Discover", "Interview", "Ingest Docs", "Validate", "Go Live"}

func StageName(num int) string {
	if num >= 1 && num <= len(StageOrder) {
		return StageOrder[num-1]
	}
	return StageOrder[0]
}

func StageNumber(name string) int {
	for i, s := range StageOrder {
		if s == name {
			return i + 1
		}
	}
	return 1
}

// ── Onboarding Entities ─────────────────────────────────────────────────────

type OnboardingSession struct {
	ID                 string                 `json:"id" bson:"_id"`
	CompanyID          string                 `json:"companyId" bson:"companyId"`
	CurrentStage       string                 `json:"currentStage" bson:"currentStage"`
	CompletedStages    []string               `json:"completedStages" bson:"completedStages"`
	Answers            map[string]interface{} `json:"answers" bson:"answers"`
	AutoDetected       []interface{}          `json:"autoDetected" bson:"autoDetected"`
	ValidationResults  []interface{}          `json:"validationResults" bson:"validationResults"`
	Status             string                 `json:"status" bson:"status"`
	CreatedAt          time.Time              `json:"createdAt" bson:"createdAt"`
	UpdatedAt          time.Time              `json:"updatedAt" bson:"updatedAt"`
}

type Discovery struct {
	ID          string    `json:"id" bson:"_id"`
	CompanyID   string    `json:"companyId" bson:"companyId"`
	Type        string    `json:"type" bson:"type"`
	Name        string    `json:"name" bson:"name"`
	Description string    `json:"description,omitempty" bson:"description,omitempty"`
	Accepted    bool      `json:"accepted" bson:"accepted"`
	CreatedAt   time.Time `json:"createdAt" bson:"createdAt"`
}

type Question struct {
	ID         string   `json:"id"`
	Section    string   `json:"section"`
	Question   string   `json:"question"`
	Type       string   `json:"type"`
	Options    []string `json:"options,omitempty"`
	EntityType string   `json:"entityType"`
	Prompt     string   `json:"prompt,omitempty"`
	SkipAllowed bool    `json:"skipAllowed"`
}

type Answer struct {
	QuestionID string `json:"questionId"`
	Answer     string `json:"answer"`
}

type AnswerSubmission struct {
	Answers []Answer `json:"answers"`
}

type AnswerResult struct {
	QuestionID string `json:"questionId"`
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
}

type ValidationFinding struct {
	ID        string    `json:"id" bson:"_id"`
	CompanyID string    `json:"companyId" bson:"companyId"`
	Type      string    `json:"type" bson:"type"`
	Message   string    `json:"message" bson:"message"`
	Severity  string    `json:"severity" bson:"severity"`
	Resolved  bool      `json:"resolved" bson:"resolved"`
	CreatedAt time.Time `json:"createdAt" bson:"createdAt"`
}

// ── Discovery types ─────────────────────────────────────────────────────────

type AutoDetectResult struct {
	Type              string             `json:"type"`
	DetectedPattern   string             `json:"detectedPattern"`
	Confidence        float64            `json:"confidence"`
	Evidence          []string           `json:"evidence"`
	ProposedConvention ProposedConvention `json:"proposedConvention"`
}

type ProposedConvention struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Scope       string   `json:"scope"`
	Tags        []string `json:"tags"`
	Priority    int      `json:"priority"`
}

type DiscoverResponse struct {
	CompanyID       string            `json:"companyId"`
	Results         []AutoDetectResult `json:"results"`
	TotalDetections int               `json:"totalDetections"`
}

// ── Request / Response types ────────────────────────────────────────────────

type StartRequest struct {
	CompanyID string `json:"companyId"`
	UserID    string `json:"userId"`
}

type StartSessionResponse struct {
	SessionID      string   `json:"sessionId"`
	CompanyID      string   `json:"companyId"`
	Stage          int      `json:"stage"`
	CompletedStages []string `json:"completedStages"`
	Status         string   `json:"status"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
}

type AdvanceRequest struct {
	Stage int `json:"stage"`
}

type AcceptDiscoveryRequest struct {
	SessionID  string                `json:"sessionId"`
	Accepted   []AcceptedDiscoveryItem `json:"accepted"`
}

type AcceptedDiscoveryItem struct {
	Type     string                 `json:"type"`
	Modified map[string]interface{} `json:"modified,omitempty"`
}

type ResolveFindingRequest struct {
	FindingID string `json:"findingId"`
}

// ── Ingestion types ─────────────────────────────────────────────────────────

type DocIngestionResult struct {
	RepoPath                  string   `json:"repoPath"`
	ConventionsFound          int      `json:"conventionsFound"`
	ArchitectureDecisionsFound int     `json:"architectureDecisionsFound"`
	DomainTermsFound          int      `json:"domainTermsFound"`
	ProcessesFound            int      `json:"processesFound"`
	Errors                    []string `json:"errors"`
}

// ── Question answer mapping ─────────────────────────────────────────────────

type QuestionAnswerMapping func(answer string) map[string]interface{}

type QuestionDef struct {
	Question
	Prompt     string
	EntityType string
	SkipAllowed bool
	AnswerMapping QuestionAnswerMapping
}

// ── IngestionResult (shared with pipeline) ──────────────────────────────────

type IngestionResult struct {
	RepoID             string   `json:"repoId"`
	FilesProcessed     int      `json:"filesProcessed"`
	FilesFailed        int      `json:"filesFailed"`
	FunctionsFound     int      `json:"functionsFound"`
	ClassesFound       int      `json:"classesFound"`
	ImportsFound       int      `json:"importsFound"`
	RelationshipsBuilt int      `json:"relationshipsBuilt"`
	DocsFound          int      `json:"docsFound"`
	DurationMs         int64    `json:"durationMs"`
	Errors             []string `json:"errors,omitempty"`
}

// ── Knowledge search types (shared with repository) ─────────────────────────

type KnowledgeSearchResponse struct {
	Query        string                 `json:"query"`
	Results      map[string][]SearchHit `json:"results"`
	TotalMatches int                    `json:"totalMatches"`
}

type SearchHit struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score,omitempty"`
	URL        string  `json:"url,omitempty"`
	SourceType string  `json:"sourceType,omitempty"`
}

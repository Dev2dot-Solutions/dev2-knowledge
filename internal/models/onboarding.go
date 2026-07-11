package models

import "time"

type OnboardingStatus string

const (
	OBStatusNotStarted         OnboardingStatus = "not_started"
	OBStatusInProgress         OnboardingStatus = "in_progress"
	OBStatusQuestionsComplete  OnboardingStatus = "questions_complete"
	OBStatusDiscoveryComplete  OnboardingStatus = "discovery_complete"
	OBStatusValidationPending  OnboardingStatus = "validation_pending"
	OBStatusComplete           OnboardingStatus = "complete"
	OBStatusPaused             OnboardingStatus = "paused"
)

type OnboardingQuestion struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Category string `json:"category,omitempty"`
	Type     string `json:"type,omitempty"` // text, select, multi_select
	Options  []string `json:"options,omitempty"`
	Required bool   `json:"required"`
	Order    int    `json:"order"`
}

type OnboardingAnswer struct {
	QuestionID string `bson:"question_id" json:"question_id"`
	Answer     any    `bson:"answer" json:"answer"`
	AnsweredAt time.Time `bson:"answered_at" json:"answered_at"`
}

type OnboardingSession struct {
	ID            string             `bson:"_id" json:"id"`
	CompanyID     string             `bson:"company_id" json:"company_id"`
	CreatedBy     string             `bson:"created_by" json:"created_by"`
	Status        OnboardingStatus   `bson:"status" json:"status"`
	CurrentStep   int                `bson:"current_step" json:"current_step"`
	Answers       []OnboardingAnswer `bson:"answers,omitempty" json:"answers,omitempty"`
	DiscoveryData map[string]any     `bson:"discovery_data,omitempty" json:"discovery_data,omitempty"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
}

// IngestionResult reports the outcome of a code ingestion run.
type IngestionResult struct {
	RepoID             string   `json:"repo_id"`
	FilesProcessed     int      `json:"files_processed"`
	FilesFailed        int      `json:"files_failed"`
	FunctionsFound     int      `json:"functions_found"`
	ClassesFound       int      `json:"classes_found"`
	ImportsFound       int      `json:"imports_found"`
	RelationshipsBuilt int      `json:"relationships_built"`
	DurationMs         int64    `json:"duration_ms"`
	Errors             []string `json:"errors,omitempty"`
}

// KnowledgeSearchResponse is the unified search response.
type KnowledgeSearchResponse struct {
	Query        string                     `json:"query"`
	Results      map[string][]SearchHit     `json:"results"`
	TotalMatches int                        `json:"total_matches"`
}

type SearchHit struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score,omitempty"`
}

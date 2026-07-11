package models

import "time"

type Deviation struct {
	ID             string    `bson:"_id" json:"id"`
	CompanyID      string    `bson:"company_id" json:"company_id"`
	ConventionID   string    `bson:"convention_id" json:"convention_id"`
	ConventionName string    `bson:"convention_name" json:"convention_name"`
	EntityType     string    `bson:"entity_type" json:"entity_type"`
	CodeSnippet    string    `bson:"code_snippet,omitempty" json:"code_snippet,omitempty"`
	ExpectedPattern string   `bson:"expected_pattern,omitempty" json:"expected_pattern,omitempty"`
	ActualPattern  string    `bson:"actual_pattern,omitempty" json:"actual_pattern,omitempty"`
	Severity       string    `bson:"severity" json:"severity"`
	Confidence     string    `bson:"confidence" json:"confidence"`
	Status         string    `bson:"status" json:"status"` // pending_review, accepted, rejected
	Source         string    `bson:"source,omitempty" json:"source,omitempty"`
	Resolution     string    `bson:"resolution,omitempty" json:"resolution,omitempty"`
	ResolvedBy     string    `bson:"resolved_by,omitempty" json:"resolved_by,omitempty"`
	ResolvedAt     *time.Time `bson:"resolved_at,omitempty" json:"resolved_at,omitempty"`
	CreatedAt      time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time `bson:"updated_at" json:"updated_at"`
}

type DeviationStats struct {
	Total    int              `json:"total"`
	ByStatus map[string]int64 `json:"by_status"`
	BySeverity map[string]int64 `json:"by_severity"`
	Open     int64            `json:"open"`
	Resolved int64            `json:"resolved"`
}

package models

import "time"

type Deviation struct {
	ID             string    `bson:"_id" json:"id"`
	CompanyID      string    `bson:"companyId" json:"companyId"`
	ConventionID   string    `bson:"conventionId" json:"conventionId"`
	ConventionName string    `bson:"conventionName" json:"conventionName"`
	EntityType     string    `bson:"entityType" json:"entityType"`
	CodeSnippet    string    `bson:"codeSnippet,omitempty" json:"codeSnippet,omitempty"`
	ExpectedPattern string   `bson:"expectedPattern,omitempty" json:"expectedPattern,omitempty"`
	ActualPattern  string    `bson:"actualPattern,omitempty" json:"actualPattern,omitempty"`
	Severity       string    `bson:"severity" json:"severity"`
	Confidence     string    `bson:"confidence" json:"confidence"`
	Status         string    `bson:"status" json:"status"` // pending_review, accepted, rejected
	Source         string    `bson:"source,omitempty" json:"source,omitempty"`
	Resolution     string    `bson:"resolution,omitempty" json:"resolution,omitempty"`
	ResolvedBy     string    `bson:"resolvedBy,omitempty" json:"resolvedBy,omitempty"`
	ResolvedAt     *time.Time `bson:"resolvedAt,omitempty" json:"resolvedAt,omitempty"`
	CreatedAt      time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt      time.Time `bson:"updatedAt" json:"updatedAt"`
}

type DeviationStats struct {
	Total    int              `json:"total"`
	ByStatus map[string]int64 `json:"byStatus"`
	BySeverity map[string]int64 `json:"bySeverity"`
	Open     int64            `json:"open"`
	Resolved int64            `json:"resolved"`
}

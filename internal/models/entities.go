package models

import "time"

// ── Entity Type constants ───────────────────────────────────────────────────

type EntityType string

const (
	ETConventions          EntityType = "conventions"
	ETBusinessRules        EntityType = "business_rules"
	ETArchitectureDecisions EntityType = "architecture_decisions"
	ETDomainTerms          EntityType = "domain_terms"
	ETProcesses            EntityType = "processes"
	ETRepos                EntityType = "repos"
	ETFiles                EntityType = "files"
	ETFunctions            EntityType = "functions"
	ETClasses              EntityType = "classes"
	ETImports              EntityType = "imports"
	ETFunctionCalls        EntityType = "function_calls"
	ETFileContains         EntityType = "file_contains"
	ETCompanies            EntityType = "companies"
	ETUsers                EntityType = "users"
	ETTickets              EntityType = "tickets"
	ETTicketConversations  EntityType = "ticket_conversations"
	ETDeviations           EntityType = "deviations"
	ETEntityRelationships  EntityType = "entity_relationships"
	ETExternalDocs         EntityType = "external_docs"
)

var Tier1Types = []EntityType{
	ETConventions, ETBusinessRules, ETArchitectureDecisions, ETDomainTerms, ETProcesses,
}

var DefaultSearchTypes = []EntityType{
	ETConventions, ETBusinessRules, ETArchitectureDecisions, ETDomainTerms, ETProcesses,
	ETExternalDocs,
}

var AllEntityTypes = []EntityType{
	ETConventions, ETBusinessRules, ETArchitectureDecisions, ETDomainTerms, ETProcesses,
	ETRepos, ETFiles, ETFunctions, ETClasses, ETImports, ETFunctionCalls, ETFileContains,
	ETCompanies, ETUsers, ETTickets, ETTicketConversations, ETDeviations, ETEntityRelationships,
	ETExternalDocs,
}

var TenantScopedTypes = map[EntityType]bool{
	ETConventions: true, ETBusinessRules: true, ETArchitectureDecisions: true,
	ETDomainTerms: true, ETProcesses: true, ETRepos: true, ETUsers: true, ETTickets: true,
	ETExternalDocs: true,
}

// ── Core Knowledge Entities ─────────────────────────────────────────────────

type Convention struct {
	ID          string    `bson:"_id" json:"id"`
	CompanyID   string    `bson:"companyId" json:"companyId"`
	Name        string    `bson:"name" json:"name"`
	Description string    `bson:"description,omitempty" json:"description,omitempty"`
	Scope       string    `bson:"scope,omitempty" json:"scope,omitempty"`
	Body        string    `bson:"body,omitempty" json:"body,omitempty"`
	CreatedAt   time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt" json:"updatedAt"`
}

type BusinessRule struct {
	ID          string    `bson:"_id" json:"id"`
	CompanyID   string    `bson:"companyId" json:"companyId"`
	Rule        string    `bson:"rule" json:"rule"`
	Description string    `bson:"description,omitempty" json:"description,omitempty"`
	Category    string    `bson:"category,omitempty" json:"category,omitempty"`
	Source      string    `bson:"source,omitempty" json:"source,omitempty"`
	CreatedAt   time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt" json:"updatedAt"`
}

type ArchitectureDecision struct {
	ID           string    `bson:"_id" json:"id"`
	CompanyID    string    `bson:"companyId" json:"companyId"`
	Topic        string    `bson:"topic" json:"topic"`
	Decision     string    `bson:"decision" json:"decision"`
	Rationale    string    `bson:"rationale,omitempty" json:"rationale,omitempty"`
	Alternatives []string  `bson:"alternatives,omitempty" json:"alternatives,omitempty"`
	Date         string    `bson:"date,omitempty" json:"date,omitempty"`
	CreatedAt    time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt    time.Time `bson:"updatedAt" json:"updatedAt"`
}

type DomainTerm struct {
	ID          string    `bson:"_id" json:"id"`
	CompanyID   string    `bson:"companyId" json:"companyId"`
	Term        string    `bson:"term" json:"term"`
	Definition  string    `bson:"definition,omitempty" json:"definition,omitempty"`
	Abbreviation string   `bson:"abbreviation,omitempty" json:"abbreviation,omitempty"`
	CreatedAt   time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt" json:"updatedAt"`
}

type Process struct {
	ID          string    `bson:"_id" json:"id"`
	CompanyID   string    `bson:"companyId" json:"companyId"`
	Name        string    `bson:"name" json:"name"`
	Description string    `bson:"description,omitempty" json:"description,omitempty"`
	Steps       []string  `bson:"steps,omitempty" json:"steps,omitempty"`
	CreatedAt   time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt" json:"updatedAt"`
}

// ── External Documentation Entities ─────────────────────────────────────────

type ExternalDoc struct {
	ID               string    `bson:"_id" json:"id"`
	CompanyID        string    `bson:"companyId" json:"companyId"`
	URL              string    `bson:"url" json:"url"`
	Title            string    `bson:"title,omitempty" json:"title,omitempty"`
	SourceType       string    `bson:"sourceType" json:"sourceType"`
	Body             string    `bson:"body,omitempty" json:"body,omitempty"`
	SectionHierarchy []string  `bson:"sectionHierarchy,omitempty" json:"sectionHierarchy,omitempty"`
	Metadata         map[string]any `bson:"metadata,omitempty" json:"metadata,omitempty"`
	LastFetchedAt    time.Time `bson:"lastFetchedAt" json:"lastFetchedAt"`
	CreatedAt        time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time `bson:"updatedAt" json:"updatedAt"`
}

// ── Code Entities (from ingestion) ──────────────────────────────────────────

type Repo struct {
	ID        string    `bson:"_id" json:"id"`
	CompanyID string    `bson:"companyId" json:"companyId"`
	Name      string    `bson:"name" json:"name"`
	URL       string    `bson:"url,omitempty" json:"url,omitempty"`
	Language  string    `bson:"language,omitempty" json:"language,omitempty"`
	Framework string    `bson:"framework,omitempty" json:"framework,omitempty"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
}

type File struct {
	ID             string    `bson:"_id" json:"id"`
	RepoID         string    `bson:"repoId" json:"repoId"`
	Path           string    `bson:"path" json:"path"`
	Language       string    `bson:"language,omitempty" json:"language,omitempty"`
	LastModifiedAt time.Time `bson:"lastModifiedAt,omitempty" json:"lastModifiedAt,omitempty"`
	CreatedAt      time.Time `bson:"createdAt" json:"createdAt"`
}

type Function struct {
	ID         string  `bson:"_id" json:"id"`
	FileID     string  `bson:"fileId" json:"fileId"`
	Name       string  `bson:"name" json:"name"`
	Signature  string  `bson:"signature,omitempty" json:"signature,omitempty"`
	LineStart  int     `bson:"lineStart,omitempty" json:"lineStart,omitempty"`
	LineEnd    int     `bson:"lineEnd,omitempty" json:"lineEnd,omitempty"`
	DocComment string  `bson:"docComment,omitempty" json:"docComment,omitempty"`
}

type Class struct {
	ID          string   `bson:"_id" json:"id"`
	FileID      string   `bson:"fileId" json:"fileId"`
	Name        string   `bson:"name" json:"name"`
	ParentClass string   `bson:"parentClass,omitempty" json:"parentClass,omitempty"`
	Interfaces  []string `bson:"interfaces,omitempty" json:"interfaces,omitempty"`
}

type Import struct {
	ID           string `bson:"_id" json:"id"`
	FileID       string `bson:"fileId" json:"fileId"`
	SourceEntity string `bson:"sourceEntity" json:"sourceEntity"`
	TargetEntity string `bson:"targetEntity" json:"targetEntity"`
}

type FunctionCall struct {
	ID               string `bson:"_id" json:"id"`
	CallerFunctionID string `bson:"callerFunctionId" json:"callerFunctionId"`
	CalleeFunctionID string `bson:"calleeFunctionId" json:"calleeFunctionId"`
}

type FileContains struct {
	ID                  string `bson:"_id" json:"id"`
	FileID              string `bson:"fileId" json:"fileId"`
	ContainedEntityType string `bson:"containedEntityType" json:"containedEntityType"`
	ContainedEntityID   string `bson:"containedEntityId" json:"containedEntityId"`
}

type EntityRelationship struct {
	ID               string `bson:"_id" json:"id"`
	SourceEntityType string `bson:"sourceEntityType" json:"sourceEntityType"`
	SourceEntityID   string `bson:"sourceEntityId" json:"sourceEntityId"`
	TargetEntityType string `bson:"targetEntityType" json:"targetEntityType"`
	TargetEntityID   string `bson:"targetEntityId" json:"targetEntityId"`
	RelationshipType string `bson:"relationshipType" json:"relationshipType"`
}

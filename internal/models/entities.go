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
)

var Tier1Types = []EntityType{
	ETConventions, ETBusinessRules, ETArchitectureDecisions, ETDomainTerms, ETProcesses,
}

var AllEntityTypes = []EntityType{
	ETConventions, ETBusinessRules, ETArchitectureDecisions, ETDomainTerms, ETProcesses,
	ETRepos, ETFiles, ETFunctions, ETClasses, ETImports, ETFunctionCalls, ETFileContains,
	ETCompanies, ETUsers, ETTickets, ETTicketConversations, ETDeviations, ETEntityRelationships,
}

var TenantScopedTypes = map[EntityType]bool{
	ETConventions: true, ETBusinessRules: true, ETArchitectureDecisions: true,
	ETDomainTerms: true, ETProcesses: true, ETRepos: true, ETUsers: true, ETTickets: true,
}

// ── Core Knowledge Entities ─────────────────────────────────────────────────

type Convention struct {
	ID          string    `bson:"_id" json:"id"`
	CompanyID   string    `bson:"company_id" json:"company_id"`
	Name        string    `bson:"name" json:"name"`
	Description string    `bson:"description,omitempty" json:"description,omitempty"`
	Scope       string    `bson:"scope,omitempty" json:"scope,omitempty"`
	Body        string    `bson:"body,omitempty" json:"body,omitempty"`
	CreatedAt   time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at" json:"updated_at"`
}

type BusinessRule struct {
	ID          string    `bson:"_id" json:"id"`
	CompanyID   string    `bson:"company_id" json:"company_id"`
	Rule        string    `bson:"rule" json:"rule"`
	Description string    `bson:"description,omitempty" json:"description,omitempty"`
	Category    string    `bson:"category,omitempty" json:"category,omitempty"`
	Source      string    `bson:"source,omitempty" json:"source,omitempty"`
	CreatedAt   time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at" json:"updated_at"`
}

type ArchitectureDecision struct {
	ID           string    `bson:"_id" json:"id"`
	CompanyID    string    `bson:"company_id" json:"company_id"`
	Topic        string    `bson:"topic" json:"topic"`
	Decision     string    `bson:"decision" json:"decision"`
	Rationale    string    `bson:"rationale,omitempty" json:"rationale,omitempty"`
	Alternatives []string  `bson:"alternatives,omitempty" json:"alternatives,omitempty"`
	Date         string    `bson:"date,omitempty" json:"date,omitempty"`
	CreatedAt    time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at" json:"updated_at"`
}

type DomainTerm struct {
	ID          string    `bson:"_id" json:"id"`
	CompanyID   string    `bson:"company_id" json:"company_id"`
	Term        string    `bson:"term" json:"term"`
	Definition  string    `bson:"definition,omitempty" json:"definition,omitempty"`
	Abbreviation string   `bson:"abbreviation,omitempty" json:"abbreviation,omitempty"`
	CreatedAt   time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at" json:"updated_at"`
}

type Process struct {
	ID          string    `bson:"_id" json:"id"`
	CompanyID   string    `bson:"company_id" json:"company_id"`
	Name        string    `bson:"name" json:"name"`
	Description string    `bson:"description,omitempty" json:"description,omitempty"`
	Steps       []string  `bson:"steps,omitempty" json:"steps,omitempty"`
	CreatedAt   time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at" json:"updated_at"`
}

// ── Code Entities (from ingestion) ──────────────────────────────────────────

type Repo struct {
	ID        string    `bson:"_id" json:"id"`
	CompanyID string    `bson:"company_id" json:"company_id"`
	Name      string    `bson:"name" json:"name"`
	URL       string    `bson:"url,omitempty" json:"url,omitempty"`
	Language  string    `bson:"language,omitempty" json:"language,omitempty"`
	Framework string    `bson:"framework,omitempty" json:"framework,omitempty"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

type File struct {
	ID             string    `bson:"_id" json:"id"`
	RepoID         string    `bson:"repo_id" json:"repo_id"`
	Path           string    `bson:"path" json:"path"`
	Language       string    `bson:"language,omitempty" json:"language,omitempty"`
	LastModifiedAt time.Time `bson:"last_modified_at,omitempty" json:"last_modified_at,omitempty"`
	CreatedAt      time.Time `bson:"created_at" json:"created_at"`
}

type Function struct {
	ID         string  `bson:"_id" json:"id"`
	FileID     string  `bson:"file_id" json:"file_id"`
	Name       string  `bson:"name" json:"name"`
	Signature  string  `bson:"signature,omitempty" json:"signature,omitempty"`
	LineStart  int     `bson:"line_start,omitempty" json:"line_start,omitempty"`
	LineEnd    int     `bson:"line_end,omitempty" json:"line_end,omitempty"`
	DocComment string  `bson:"doc_comment,omitempty" json:"doc_comment,omitempty"`
}

type Class struct {
	ID          string   `bson:"_id" json:"id"`
	FileID      string   `bson:"file_id" json:"file_id"`
	Name        string   `bson:"name" json:"name"`
	ParentClass string   `bson:"parent_class,omitempty" json:"parent_class,omitempty"`
	Interfaces  []string `bson:"interfaces,omitempty" json:"interfaces,omitempty"`
}

type Import struct {
	ID           string `bson:"_id" json:"id"`
	FileID       string `bson:"file_id" json:"file_id"`
	SourceEntity string `bson:"source_entity" json:"source_entity"`
	TargetEntity string `bson:"target_entity" json:"target_entity"`
}

type FunctionCall struct {
	ID               string `bson:"_id" json:"id"`
	CallerFunctionID string `bson:"caller_function_id" json:"caller_function_id"`
	CalleeFunctionID string `bson:"callee_function_id" json:"callee_function_id"`
}

type FileContains struct {
	ID                  string `bson:"_id" json:"id"`
	FileID              string `bson:"file_id" json:"file_id"`
	ContainedEntityType string `bson:"contained_entity_type" json:"contained_entity_type"`
	ContainedEntityID   string `bson:"contained_entity_id" json:"contained_entity_id"`
}

type EntityRelationship struct {
	ID               string `bson:"_id" json:"id"`
	SourceEntityType string `bson:"source_entity_type" json:"source_entity_type"`
	SourceEntityID   string `bson:"source_entity_id" json:"source_entity_id"`
	TargetEntityType string `bson:"target_entity_type" json:"target_entity_type"`
	TargetEntityID   string `bson:"target_entity_id" json:"target_entity_id"`
	RelationshipType string `bson:"relationship_type" json:"relationship_type"`
}

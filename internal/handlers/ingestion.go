package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/ingestion"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/mongo"
)

type IngestionHandler struct {
	pipeline    *ingestion.Pipeline
	docPipeline *ingestion.DocPipeline
	mongoDB     *mongo.Database
}

func NewIngestionHandler(p *ingestion.Pipeline, dp *ingestion.DocPipeline, db *mongo.Database) *IngestionHandler {
	return &IngestionHandler{pipeline: p, docPipeline: dp, mongoDB: db}
}

func (h *IngestionHandler) Routes(r chi.Router) {
	r.Route("/ingest", func(r chi.Router) {
		r.Get("/github/repos", h.ListGitHubRepos)
		r.Post("/start", h.Start)
		r.Get("/status", h.Status)
		r.Post("/online-doc", h.IngestDoc)
	})
}

func (h *IngestionHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CompanyID string `json:"companyId"`
		RepoName  string `json:"repoName"`
		RepoURL   string `json:"repoUrl"`
		LocalPath string `json:"localPath"`
		Language  string `json:"language"`
		Framework string `json:"framework"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.CompanyID == "" || req.RepoName == "" {
		respondError(w, http.StatusBadRequest, "company_id and repo_name are required")
		return
	}
	if !RequireCompanyAccess(w, r, req.CompanyID) {
		return
	}
	// Check if repo already exists for this company
	var existingCount int64
	if h.mongoDB != nil {
		existingCount, _ = h.mongoDB.Collection("repos").CountDocuments(r.Context(), map[string]interface{}{
			"name":       req.RepoName,
			"companyId": req.CompanyID,
		})
	}
	if existingCount > 0 {
		// Check if force flag is set — if so, delete existing records
		if r.URL.Query().Get("force") == "true" {
			h.mongoDB.Collection("repos").DeleteMany(r.Context(), map[string]interface{}{
				"name": req.RepoName, "companyId": req.CompanyID,
			})
			for _, coll := range []string{"files", "functions", "classes", "imports", "function_calls", "file_contains", "external_docs"} {
				h.mongoDB.Collection(coll).DeleteMany(r.Context(), map[string]interface{}{
					"repoName": req.RepoName, "companyId": req.CompanyID,
				})
			}
			log.Printf("[ingestion] deleted existing records for %s/%s", req.CompanyID, req.RepoName)
		} else {
			respondJSON(w, http.StatusConflict, map[string]interface{}{
				"error":   "repo already ingested",
				"repo":    req.RepoName,
				"message": "This repo has already been ingested. Send ?force=true to re-ingest.",
			})
			return
		}
	}

	// Clone the repo if no local path was supplied (the usual case from the admin UI)
	localPath := req.LocalPath
	if localPath == "" {
		cloned, err := h.cloneRepo(r.Context(), req.CompanyID, req.RepoName, r.Header.Get("Authorization"))
		if err != nil {
			log.Printf("[ingestion] clone failed for %s/%s: %v", req.CompanyID, req.RepoName, err)
			respondErrorCode(w, http.StatusBadGateway, "clone_failed", "Could not clone the repository: "+err.Error())
			return
		}
		localPath = cloned
	}

	result, err := h.pipeline.IngestRepository(r.Context(), req.CompanyID, req.RepoName, req.RepoURL, localPath, req.Language, req.Framework)
	if err != nil {
		log.Printf("[ingestion] Error: %v", err)
		respondErrorCode(w, http.StatusInternalServerError, "pipeline_error", "Ingestion failed: "+err.Error())
		return
	}

	if result.FilesProcessed == 0 && result.FilesFailed == 0 && result.DocsFound == 0 {
		// Nothing was ingested — remove the stub repo record so the repo
		// shows as "not ingested" rather than a misleading "pending".
		if h.mongoDB != nil && result.RepoID != "" {
			if _, err := h.mongoDB.Collection("repos").DeleteOne(r.Context(), map[string]interface{}{
				"_id": result.RepoID,
			}); err != nil {
				log.Printf("[ingestion] failed to remove stub repo %s: %v", result.RepoID, err)
			}
		}
		respondErrorCode(w, http.StatusUnprocessableEntity, "no_files", "Clone succeeded but no parseable source files or markdown documents were found (code: .ts, .js, .kt, .go, .rs, .vue, .html, .css; docs: .md).")
		return
	}

	// Record stats so the status endpoint can report the repo as ingested
	if h.mongoDB != nil {
		if _, err := h.mongoDB.Collection("repos").UpdateOne(r.Context(),
			map[string]interface{}{"name": req.RepoName, "companyId": req.CompanyID},
			map[string]interface{}{"$set": map[string]interface{}{
				"stats": map[string]interface{}{
					"files":       result.FilesProcessed,
					"docs":        result.DocsFound,
					"functions":   result.FunctionsFound,
					"classes":     result.ClassesFound,
					"imports":     result.ImportsFound,
					"relations":   result.RelationshipsBuilt,
					"durationMs": result.DurationMs,
				},
				"updatedAt": time.Now().UTC().Format(time.RFC3339),
			}},
		); err != nil {
			log.Printf("[ingestion] failed to write repo stats for %s/%s: %v", req.CompanyID, req.RepoName, err)
		}
	}

	respondJSON(w, http.StatusOK, result)
}

func (h *IngestionHandler) cloneRepo(ctx context.Context, companyID, repoName, authHeader string) (string, error) {
	// Get GitHub credentials from company config
	type ghCfg struct {
		Org string `json:"org"`
		PAT string `json:"pat"`
	}
	type compCfg struct {
		GitHub ghCfg `json:"github"`
	}

	configSvcURL := "http://company-config:8083/companies/" + companyID
	client := &http.Client{Timeout: 5 * time.Second}
	// Service auth: API key when configured, else forward the caller's JWT.
	cfgReq, err := http.NewRequestWithContext(ctx, "GET", configSvcURL, nil)
	if err != nil {
		return "", fmt.Errorf("build config request: %w", err)
	}
	setServiceAuth(cfgReq, authHeader)
	resp, err := client.Do(cfgReq)
	if err != nil {
		return "", fmt.Errorf("config service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("config service returned status %d", resp.StatusCode)
	}

	var cfg compCfg
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return "", fmt.Errorf("parse config: %w", err)
	}

	if cfg.GitHub.Org == "" || cfg.GitHub.PAT == "" {
		return "", fmt.Errorf("GitHub not configured for this company")
	}

	// Build the clone URL
	cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", cfg.GitHub.PAT, cfg.GitHub.Org, repoName)
	workspaceDir := "/data/workspace"
	localPath := workspaceDir + "/" + companyID + "/" + repoName

	// Remove any previous clone so re-ingestion is idempotent
	if err := os.RemoveAll(localPath); err != nil {
		return "", fmt.Errorf("clear workspace dir: %w", err)
	}
	if err := os.MkdirAll(localPath, 0755); err != nil {
		return "", fmt.Errorf("create workspace dir: %w", err)
	}

	// Run git clone
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", cloneURL, localPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 200 {
			detail = detail[:200]
		}
		return "", fmt.Errorf("git clone failed: %v (%s)", err, detail)
	}

	log.Printf("[ingestion] cloned %s/%s to %s", cfg.GitHub.Org, repoName, localPath)
	return localPath, nil
}

func (h *IngestionHandler) Status(w http.ResponseWriter, r *http.Request) {
	companyID := r.URL.Query().Get("companyId")

	// companyId is optional for admins/API-key callers; a scoped JWT caller
	// without one falls back to their own company.
	if companyID != "" {
		if !RequireCompanyAccess(w, r, companyID) {
			return
		}
	} else if !GetIsAdmin(r) {
		companyID = GetCompanyID(r)
	}

	type repoStats struct {
		Name      string                 `json:"name"`
		Language  string                 `json:"language"`
		Stats     map[string]interface{} `json:"stats,omitempty"`
		UpdatedAt string                 `json:"updatedAt,omitempty"`
	}

	repos := make([]repoStats, 0)

	if h.mongoDB != nil && companyID != "" {
		cursor, err := h.mongoDB.Collection("repos").Find(r.Context(), map[string]interface{}{
			"companyId": companyID,
		})
		if err == nil {
			defer cursor.Close(r.Context())
			for cursor.Next(r.Context()) {
				var doc map[string]interface{}
				if err := cursor.Decode(&doc); err != nil {
					continue
				}
				name, _ := doc["name"].(string)
				lang, _ := doc["language"].(string)
				stats, _ := doc["stats"].(map[string]interface{})
				updated, _ := doc["updatedAt"].(string)
				repos = append(repos, repoStats{
					Name:      name,
					Language:  lang,
					Stats:     stats,
					UpdatedAt: updated,
				})
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "idle",
		"repos":  repos,
	})
}

// setServiceAuth authenticates an outbound request to a sibling service: the shared
// API key when configured (works for NATS/background triggers with no caller JWT),
// otherwise the caller's forwarded JWT.
func setServiceAuth(req *http.Request, fallbackAuthHeader string) {
	if k := os.Getenv("SERVICE_API_KEY"); k != "" {
		req.Header.Set("X-API-Key", k)
		return
	}
	if fallbackAuthHeader != "" {
		req.Header.Set("Authorization", fallbackAuthHeader)
	}
}

func (h *IngestionHandler) ListGitHubRepos(w http.ResponseWriter, r *http.Request) {
	companyID := r.URL.Query().Get("companyId")
	if companyID == "" {
		respondError(w, http.StatusBadRequest, "companyId is required")
		return
	}
	if !RequireCompanyAccess(w, r, companyID) {
		return
	}

	// Fetch company config from the config service (returns decrypted values)
	configSvcURL := "http://company-config:8083/companies/" + companyID
	client := &http.Client{Timeout: 5 * time.Second}
	// Service auth: API key when configured, else forward the caller's JWT.
	cfgReq, err := http.NewRequestWithContext(r.Context(), "GET", configSvcURL, nil)
	if err != nil {
		log.Printf("[ingestion] list repos: failed to build config request for company %s: %v", companyID, err)
		respondErrorCode(w, http.StatusInternalServerError, "config_error", "Could not build the company config request.")
		return
	}
	setServiceAuth(cfgReq, r.Header.Get("Authorization"))
	resp, err := client.Do(cfgReq)
	if err != nil {
		log.Printf("[ingestion] list repos: config service unreachable for company %s: %v", companyID, err)
		respondErrorCode(w, http.StatusBadGateway, "config_unreachable", "Could not reach the company config service — it may be down or still starting.")
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		log.Printf("[ingestion] list repos: company %s not found in config service", companyID)
		respondErrorCode(w, http.StatusBadGateway, "config_error", "The company config service has no record of this company.")
		return
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("[ingestion] list repos: config service returned %d for company %s: %s", resp.StatusCode, companyID, truncateForLog(body))
		respondErrorCode(w, http.StatusBadGateway, "config_error", fmt.Sprintf("The company config service returned an error (status %d). If credentials were saved previously, check that the encryption key has not changed.", resp.StatusCode))
		return
	}

	var cfg struct {
		GitHub struct {
			Org string `json:"org"`
			PAT string `json:"pat"`
		} `json:"github"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		log.Printf("[ingestion] list repos: failed to parse config for company %s: %v", companyID, err)
		respondErrorCode(w, http.StatusBadGateway, "config_error", "Could not parse the response from the company config service.")
		return
	}

	if cfg.GitHub.Org == "" || cfg.GitHub.PAT == "" {
		respondErrorCode(w, http.StatusUnprocessableEntity, "github_not_configured", "GitHub is not configured for this company — set the organisation and personal access token on the company settings page.")
		return
	}

	// Call GitHub API
	ghClient := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequestWithContext(r.Context(), "GET",
		"https://api.github.com/orgs/"+cfg.GitHub.Org+"/repos?per_page=100&sort=updated&type=all", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.GitHub.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	ghResp, err := ghClient.Do(req)
	if err != nil {
		log.Printf("[ingestion] list repos: GitHub API request failed for org %s: %v", cfg.GitHub.Org, err)
		respondErrorCode(w, http.StatusBadGateway, "github_unreachable", "Could not reach the GitHub API — check outbound network access from the knowledge service.")
		return
	}
	defer ghResp.Body.Close()

	ghBody, _ := io.ReadAll(ghResp.Body)
	if ghResp.StatusCode != http.StatusOK {
		upstreamMsg := githubUpstreamMessage(ghBody)
		log.Printf("[ingestion] list repos: GitHub API returned %d for org %s: %s", ghResp.StatusCode, cfg.GitHub.Org, upstreamMsg)
		respondErrorCode(w, http.StatusBadGateway, "github_error", githubErrorHint(ghResp.StatusCode, cfg.GitHub.Org, upstreamMsg))
		return
	}

	var ghRepos []map[string]interface{}
	json.Unmarshal(ghBody, &ghRepos)

	type repoItem struct {
		ID           interface{} `json:"id"`
		Name         string      `json:"name"`
		FullName     string      `json:"fullName"`
		Description  interface{} `json:"description"`
		Language     interface{} `json:"language"`
		DefaultBranch string     `json:"defaultBranch"`
		UpdatedAt    interface{} `json:"updatedAt"`
	}

	repos := make([]repoItem, 0, len(ghRepos))
	for _, r := range ghRepos {
		name, _ := r["name"].(string)
		fullName, _ := r["full_name"].(string)
		repos = append(repos, repoItem{
			ID:           r["id"],
			Name:         name,
			FullName:     fullName,
			Description:  r["description"],
			Language:     r["language"],
			DefaultBranch: func() string { if v, ok := r["default_branch"].(string); ok { return v }; return "" }(),
			UpdatedAt:    r["updated_at"],
		})
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"repos": repos})
}

// githubUpstreamMessage extracts the "message" field from a GitHub API error body.
func githubUpstreamMessage(body []byte) string {
	var parsed struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Message == "" {
		return strings.TrimSpace(string(body))
	}
	return parsed.Message
}

// githubErrorHint converts a GitHub API failure into an actionable, human-readable message.
func githubErrorHint(status int, org, upstreamMsg string) string {
	switch status {
	case http.StatusUnauthorized:
		return "GitHub rejected the access token (401). The token may be expired or revoked — generate a new personal access token and save it on the company settings page."
	case http.StatusForbidden:
		return fmt.Sprintf("GitHub denied access (403) for org '%s'. Check the token has the 'repo' scope, and if the organisation enforces SAML SSO, authorise the token for it.", org)
	case http.StatusNotFound:
		return fmt.Sprintf("GitHub found no organisation named '%s' (404). Check the organisation name — personal user accounts are not organisations.", org)
	default:
		if upstreamMsg == "" {
			return fmt.Sprintf("GitHub API returned status %d.", status)
		}
		return fmt.Sprintf("GitHub API returned status %d: %s", status, upstreamMsg)
	}
}

// truncateForLog caps a response body for safe logging.
func truncateForLog(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

func (h *IngestionHandler) IngestDoc(w http.ResponseWriter, r *http.Request) {
	var req ingestion.DocIngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.URL == "" || req.CompanyID == "" {
		respondError(w, http.StatusBadRequest, "url and company_id are required")
		return
	}
	if !RequireCompanyAccess(w, r, req.CompanyID) {
		return
	}
	result, err := h.docPipeline.IngestURL(r.Context(), req)
	if err != nil {
		log.Printf("[ingestion] Doc ingest error: %v", err)
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

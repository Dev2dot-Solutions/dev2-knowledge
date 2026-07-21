package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// WorkspaceHandler serves HTTP endpoints for browsing and editing files
// in the workspace filesystem (cloned repos per company/project).
type WorkspaceHandler struct {
	workspaceDir string
}

func NewWorkspaceHandler(workspaceDir string) *WorkspaceHandler {
	return &WorkspaceHandler{workspaceDir: workspaceDir}
}

func (h *WorkspaceHandler) Routes(r chi.Router) {
	r.Route("/workspace", func(r chi.Router) {
		r.Get("/tree", h.ListTree)    // GET /workspace/tree?companyId=&projectId=&path=&repos=
		r.Get("/file", h.ReadFile)    // GET /workspace/file?companyId=&projectId=&path=
		r.Put("/file", h.WriteFile)   // PUT /workspace/file
	})
}

// ---------------------------------------------------------------------------
// GET /workspace/tree — list directory contents
// ---------------------------------------------------------------------------

func (h *WorkspaceHandler) ListTree(w http.ResponseWriter, r *http.Request) {
	companyID := r.URL.Query().Get("companyId")
	projectID := r.URL.Query().Get("projectId")
	relPath := r.URL.Query().Get("path")
	reposFilter := r.URL.Query().Get("repos") // comma-separated repo names

	if relPath == "" {
		relPath = "."
	}

	if companyID == "" || projectID == "" {
		respondError(w, http.StatusBadRequest, "companyId and projectId are required")
		return
	}

	dirPath, err := h.resolvePath(companyID, projectID, relPath)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			respondError(w, http.StatusNotFound, "directory not found: "+relPath)
		} else {
			respondError(w, http.StatusInternalServerError, "read directory error: "+err.Error())
		}
		return
	}

	type TreeItem struct {
		Name string `json:"name"`
		Type string `json:"type"` // "file" | "directory"
		Size int64  `json:"size"`
	}

	items := make([]TreeItem, 0, len(entries))

	// Parse the repo filter list (for root-level filtering)
	var allowedRepos map[string]bool
	if relPath == "." && reposFilter != "" {
		allowedRepos = make(map[string]bool)
		for _, r := range strings.Split(reposFilter, ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				allowedRepos[r] = true
			}
		}
	}

	for _, e := range entries {
		// Skip .git directories at the top level
		if e.Name() == ".git" && relPath == "." {
			continue
		}
		// At root level, filter by project's repo list if provided
		if relPath == "." && allowedRepos != nil && !allowedRepos[e.Name()] {
			continue
		}
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		entryType := "file"
		if e.IsDir() {
			entryType = "directory"
		}
		items = append(items, TreeItem{Name: e.Name(), Type: entryType, Size: size})
	}

	// Sort: directories first, then alphabetically
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type != items[j].Type {
			return items[i].Type == "directory"
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"path":  relPath,
		"items": items,
	})
}

// ---------------------------------------------------------------------------
// GET /workspace/file — read file content
// ---------------------------------------------------------------------------

func (h *WorkspaceHandler) ReadFile(w http.ResponseWriter, r *http.Request) {
	companyID := r.URL.Query().Get("companyId")
	projectID := r.URL.Query().Get("projectId")
	relPath := r.URL.Query().Get("path")

	if companyID == "" || projectID == "" {
		respondError(w, http.StatusBadRequest, "companyId and projectId are required")
		return
	}
	if relPath == "" {
		respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	fullPath, err := h.resolvePath(companyID, projectID, relPath)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			respondError(w, http.StatusNotFound, "file not found: "+relPath)
		} else {
			respondError(w, http.StatusInternalServerError, "stat error: "+err.Error())
		}
		return
	}
	if info.IsDir() {
		respondError(w, http.StatusBadRequest, "path is a directory: "+relPath)
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "read error: "+err.Error())
		return
	}

	size := len(data)
	content := string(data)
	truncated := false

	// Truncate very large files (500KB)
	if size > 500*1024 {
		content = content[:500*1024]
		truncated = true
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"path":      relPath,
		"content":   content,
		"size":      size,
		"truncated": truncated,
	})
}

// ---------------------------------------------------------------------------
// PUT /workspace/file — save file content
// ---------------------------------------------------------------------------

func (h *WorkspaceHandler) WriteFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CompanyID string `json:"companyId"`
		ProjectID string `json:"projectId"`
		Path      string `json:"path"`
		Content   string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}

	if req.CompanyID == "" || req.ProjectID == "" {
		respondError(w, http.StatusBadRequest, "companyId and projectId are required")
		return
	}
	if req.Path == "" {
		respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	fullPath, err := h.resolvePath(req.CompanyID, req.ProjectID, req.Path)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		respondError(w, http.StatusInternalServerError, "create directory error: "+err.Error())
		return
	}

	if err := os.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
		respondError(w, http.StatusInternalServerError, "write error: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"path": req.Path,
		"size": len(req.Content),
	})
}

// ---------------------------------------------------------------------------
// Path resolution with traversal protection.
// Repos are stored under project-scoped directories:
//   {workspaceDir}/{companyId}/{projectId}/repos/{repoName} — it is used for filtering
// at the tree root level via the `repos` query parameter.
// ---------------------------------------------------------------------------

// resolvePath resolves a path against the workspace.
// Strategy: try the project-scoped path first
//   {workspaceDir}/{companyId}/{projectId}/repos/{relPath}
// If that directory doesn't exist, fall back to the flat company-level path
//   {workspaceDir}/{companyId}/{relPath}
// The fallback supports repos cloned before the project-scoped structure was
// introduced. The repos filter param at the tree root handles project binding.
func (h *WorkspaceHandler) resolvePath(companyID, projectID, relPath string) (string, error) {
	wsRoot := filepath.Join(h.workspaceDir, companyID, projectID)
	targetPath := filepath.Join(wsRoot, "repos", relPath)
	cleaned := filepath.Clean(targetPath)
	absRoot, _ := filepath.Abs(wsRoot)
	absTarget, _ := filepath.Abs(cleaned)
	if !strings.HasPrefix(absTarget, absRoot) {
		return "", os.ErrNotExist
	}

	// Fallback: if project-scoped directory doesn't exist, try flat company-level
	if _, err := os.Stat(cleaned); os.IsNotExist(err) {
		flatRoot := filepath.Join(h.workspaceDir, companyID)
		flatPath := filepath.Join(flatRoot, relPath)
		flatCleaned := filepath.Clean(flatPath)
		flatAbs, _ := filepath.Abs(flatRoot)
		flatTarget, _ := filepath.Abs(flatCleaned)
		if strings.HasPrefix(flatTarget, flatAbs) {
			if _, flatErr := os.Stat(flatCleaned); flatErr == nil {
				return flatCleaned, nil
			}
		}
	}

	return cleaned, nil
}

package ingestion

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/models"
	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/repository"
	"github.com/google/uuid"
)

// ParseResult mirrors the Rust sidecar output.
type ParseResult struct {
	Path      string            `json:"path"`
	Functions []ParseFunction   `json:"functions"`
	Classes   []ParseClass      `json:"classes"`
	Imports   []ParseImport     `json:"imports"`
	Calls     []ParseCall       `json:"calls"`
	Error     *string           `json:"error"`
}

type ParseFunction struct {
	Name       string  `json:"name"`
	Signature  string  `json:"signature"`
	LineStart  int     `json:"lineStart"`
	LineEnd    int     `json:"lineEnd"`
	DocComment *string `json:"docComment"`
}

type ParseClass struct {
	Name        string   `json:"name"`
	ParentClass *string  `json:"parentClass"`
	Interfaces  []string `json:"interfaces"`
}

type ParseImport struct {
	SourceEntity string `json:"sourceEntity"`
	TargetEntity string `json:"targetEntity"`
}

type ParseCall struct {
	CallerName string `json:"callerName"`
	CalleeName string `json:"calleeName"`
}

type Pipeline struct {
	entityRepo      *repository.EntityRepo
	ingestParserPath string
	workspaceDir    string
}

func NewPipeline(er *repository.EntityRepo, parserPath, wsDir string) *Pipeline {
	return &Pipeline{entityRepo: er, ingestParserPath: parserPath, workspaceDir: wsDir}
}

// IngestRepository runs the full ingestion pipeline.
func (p *Pipeline) IngestRepository(ctx context.Context, companyID, repoName, repoURL, localPath, language, framework string) (*models.IngestionResult, error) {
	start := time.Now()
	result := &models.IngestionResult{}

	// 1. Create or find repo record
	repoID, err := p.ensureRepo(ctx, companyID, repoName, repoURL, language, framework)
	if err != nil {
		return nil, fmt.Errorf("ensure repo: %w", err)
	}
	result.RepoID = repoID

	// 2. Walk directory and find parseable files
	parseableExts := map[string]string{
		".ts": "typescript", ".tsx": "tsx", ".js": "javascript", ".jsx": "javascript",
		".mjs": "javascript", ".cjs": "javascript", ".mts": "typescript", ".cts": "typescript",
		".kt": "kotlin", ".kts": "kotlin", ".go": "go", ".vue": "vue", ".rs": "rust",
		".html": "html", ".htm": "html", ".css": "css", ".scss": "css",
	}

	var files []string
	filepath.Walk(localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() { return nil }
		ext := filepath.Ext(path)
		if _, ok := parseableExts[ext]; ok {
			files = append(files, path)
		}
		return nil
	})

	// 2b. Ingest markdown documentation (READMEs, docs-only repos)
	docsCount, err := p.ingestMarkdown(ctx, companyID, repoName, localPath)
	if err != nil {
		log.Printf("[ingestion] markdown ingestion error: %v", err)
	}
	result.DocsFound = docsCount

	if len(files) == 0 {
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// 3. Call Rust sidecar
	log.Printf("[ingestion] Parsing %d files via sidecar...", len(files))
	parseResults, err := p.runSidecar(files)
	if err != nil {
		return nil, fmt.Errorf("sidecar: %w", err)
	}

	// 4. Store results
	fnMap := make(map[string]string) // key: fileID:fnName → function entity ID
	var relationships []any

	for _, pr := range parseResults {
		fileID := uuid.New().String()

		// Create file record
		if err := p.entityRepo.Create(ctx, "files", map[string]any{
			"_id": fileID, "repoId": repoID, "path": pr.Path, "language": filepath.Ext(pr.Path),
			"companyId": companyID, "repoName": repoName,
		}); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", pr.Path, err))
			result.FilesFailed++
			continue
		}
		result.FilesProcessed++

		// Store functions
		for _, fn := range pr.Functions {
			fnID := uuid.New().String()
			if err := p.entityRepo.Create(ctx, "functions", map[string]any{
				"_id": fnID, "fileId": fileID, "name": fn.Name, "signature": fn.Signature,
				"lineStart": fn.LineStart, "lineEnd": fn.LineEnd, "docComment": fn.DocComment,
				"companyId": companyID, "repoName": repoName,
			}); err != nil {
				continue
			}
			fnMap[fileID+":"+fn.Name] = fnID
			result.FunctionsFound++

			// FileContains relationship
			relationships = append(relationships, map[string]any{
				"_id": uuid.New().String(), "fileId": fileID,
				"containedEntityType": "function", "containedEntityId": fnID,
				"companyId": companyID, "repoName": repoName,
			})
		}

		// Store classes
		for _, cls := range pr.Classes {
			clsID := uuid.New().String()
			if err := p.entityRepo.Create(ctx, "classes", map[string]any{
				"_id": clsID, "fileId": fileID, "name": cls.Name,
				"parentClass": cls.ParentClass, "interfaces": cls.Interfaces,
				"companyId": companyID, "repoName": repoName,
			}); err != nil {
				continue
			}
			result.ClassesFound++
			relationships = append(relationships, map[string]any{
				"_id": uuid.New().String(), "fileId": fileID,
				"containedEntityType": "class", "containedEntityId": clsID,
				"companyId": companyID, "repoName": repoName,
			})
		}

		// Store imports
		for _, imp := range pr.Imports {
			if imp.SourceEntity == "" && imp.TargetEntity == "" { continue }
			p.entityRepo.Create(ctx, "imports", map[string]any{
				"_id": uuid.New().String(), "fileId": fileID,
				"sourceEntity": imp.SourceEntity, "targetEntity": imp.TargetEntity,
				"companyId": companyID, "repoName": repoName,
			})
			result.ImportsFound++
		}
	}

	// 5. Build function call relationships
	for _, pr := range parseResults {
		for _, call := range pr.Calls {
			callerID, ok := fnMap[fmt.Sprintf("%s:%s", getFileBaseName(pr.Path), call.CallerName)]
			if !ok { continue }
			var calleeID string
			for k, v := range fnMap {
				if len(k) > len(call.CalleeName) && k[len(k)-len(call.CalleeName):] == ":"+call.CalleeName {
					calleeID = v; break
				}
			}
			if calleeID == "" || callerID == calleeID { continue }
			if err := p.entityRepo.Create(ctx, "function_calls", map[string]any{
				"_id": uuid.New().String(), "callerFunctionId": callerID, "calleeFunctionId": calleeID,
				"companyId": companyID, "repoName": repoName,
			}); err != nil {
				continue
			}
			result.RelationshipsBuilt++
		}
	}

	// 6. Batch insert file_contains relationships
	if len(relationships) > 0 {
		anyRel := make([]any, len(relationships))
		for i, r := range relationships { anyRel[i] = r }
		p.entityRepo.InsertMany(ctx, "file_contains", anyRel)
	}

	result.DurationMs = time.Since(start).Milliseconds()
	log.Printf("[ingestion] Done: %d files, %d fns, %d cls, %d imps, %d rels in %dms",
		result.FilesProcessed, result.FunctionsFound, result.ClassesFound,
		result.ImportsFound, result.RelationshipsBuilt, result.DurationMs)
	return result, nil
}

// ingestMarkdown walks the repo for Markdown files and stores each as an
// ExternalDoc (sourceType=markdown) so READMEs and docs-only repos become
// searchable knowledge. Returns the number of documents stored.
func (p *Pipeline) ingestMarkdown(ctx context.Context, companyID, repoName, localPath string) (int, error) {
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		"dist": true, "target": true, ".nuxt": true, ".next": true,
	}
	var mdFiles []string
	filepath.Walk(localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".md" || ext == ".markdown" {
			mdFiles = append(mdFiles, path)
		}
		return nil
	})

	count := 0
	for _, path := range mdFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("[ingestion] markdown read failed %s: %v", path, err)
			continue
		}
		body := string(data)
		rel, err := filepath.Rel(localPath, path)
		if err != nil {
			rel = path
		}

		title := ""
		var hierarchy []string
		scanner := bufio.NewScanner(strings.NewReader(body))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "#") {
				heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
				if heading == "" {
					continue
				}
				if title == "" {
					title = heading
				}
				hierarchy = append(hierarchy, heading)
				if len(hierarchy) >= 20 {
					break
				}
			}
		}
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}

		now := time.Now().UTC()
		doc := map[string]any{
			"_id":        uuid.New().String(),
			"companyId": companyID,
			"repoName":  repoName,
			"url":       "repo://" + repoName + "/" + filepath.ToSlash(rel),
			"title":     title,
			"sourceType": "markdown",
			"body":       body,
			"sectionHierarchy": hierarchy,
			"lastFetchedAt":    now,
			"createdAt":        now,
			"updatedAt":        now,
		}
		if err := p.entityRepo.Create(ctx, "external_docs", doc); err != nil {
			log.Printf("[ingestion] markdown store failed %s: %v", rel, err)
			continue
		}
		count++
	}
	if count > 0 {
		log.Printf("[ingestion] stored %d markdown docs for %s/%s", count, companyID, repoName)
	}
	return count, nil
}

func (p *Pipeline) ensureRepo(ctx context.Context, companyID, name, url, lang, framework string) (string, error) {
	// Simple check: create every time (idempotent enough for now)
	id := uuid.New().String()
	err := p.entityRepo.Create(ctx, "repos", map[string]any{
		"_id": id, "companyId": companyID, "name": name, "url": url,
		"language": lang, "framework": framework,
	})
	if err != nil {
		// May already exist — try lookup by name + company
		return "", fmt.Errorf("create repo: %w", err)
	}
	return id, nil
}

func (p *Pipeline) runSidecar(files []string) ([]ParseResult, error) {
	cmd := exec.Command(p.ingestParserPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start sidecar: %w", err)
	}

	writer := bufio.NewWriter(stdin)
	for _, f := range files {
		writer.WriteString(f + "\n")
	}
	writer.Flush()
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return nil, fmt.Errorf("sidecar wait: %w: %s", err, detail)
	}

	var results []ParseResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("parse sidecar output: %w", err)
	}
	return results, nil
}

func getFileBaseName(path string) string {
	return filepath.Base(path)
}

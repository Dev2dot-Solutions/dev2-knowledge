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
	LineStart  int     `json:"line_start"`
	LineEnd    int     `json:"line_end"`
	DocComment *string `json:"doc_comment"`
}

type ParseClass struct {
	Name        string   `json:"name"`
	ParentClass *string  `json:"parent_class"`
	Interfaces  []string `json:"interfaces"`
}

type ParseImport struct {
	SourceEntity string `json:"source_entity"`
	TargetEntity string `json:"target_entity"`
}

type ParseCall struct {
	CallerName string `json:"caller_name"`
	CalleeName string `json:"callee_name"`
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
		".kt": "kotlin", ".kts": "kotlin", ".go": "go",
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
			"_id": fileID, "repo_id": repoID, "path": pr.Path, "language": filepath.Ext(pr.Path),
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
				"_id": fnID, "file_id": fileID, "name": fn.Name, "signature": fn.Signature,
				"line_start": fn.LineStart, "line_end": fn.LineEnd, "doc_comment": fn.DocComment,
			}); err != nil {
				continue
			}
			fnMap[fileID+":"+fn.Name] = fnID
			result.FunctionsFound++

			// FileContains relationship
			relationships = append(relationships, map[string]any{
				"_id": uuid.New().String(), "file_id": fileID,
				"contained_entity_type": "function", "contained_entity_id": fnID,
			})
		}

		// Store classes
		for _, cls := range pr.Classes {
			clsID := uuid.New().String()
			if err := p.entityRepo.Create(ctx, "classes", map[string]any{
				"_id": clsID, "file_id": fileID, "name": cls.Name,
				"parent_class": cls.ParentClass, "interfaces": cls.Interfaces,
			}); err != nil {
				continue
			}
			result.ClassesFound++
			relationships = append(relationships, map[string]any{
				"_id": uuid.New().String(), "file_id": fileID,
				"contained_entity_type": "class", "contained_entity_id": clsID,
			})
		}

		// Store imports
		for _, imp := range pr.Imports {
			if imp.SourceEntity == "" && imp.TargetEntity == "" { continue }
			p.entityRepo.Create(ctx, "imports", map[string]any{
				"_id": uuid.New().String(), "file_id": fileID,
				"source_entity": imp.SourceEntity, "target_entity": imp.TargetEntity,
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
				"_id": uuid.New().String(), "caller_function_id": callerID, "callee_function_id": calleeID,
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

func (p *Pipeline) ensureRepo(ctx context.Context, companyID, name, url, lang, framework string) (string, error) {
	// Simple check: create every time (idempotent enough for now)
	id := uuid.New().String()
	err := p.entityRepo.Create(ctx, "repos", map[string]any{
		"_id": id, "company_id": companyID, "name": name, "url": url,
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

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

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
		return nil, fmt.Errorf("sidecar wait: %w", err)
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

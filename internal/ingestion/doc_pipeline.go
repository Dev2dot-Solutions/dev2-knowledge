package ingestion

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/repository"
	"github.com/google/uuid"
)

const defaultMaxBodySize = 5 * 1024 * 1024 // 5MB

type DocPipeline struct {
	entityRepo  *repository.EntityRepo
	client      *http.Client
	maxBodySize int64
}

type DocIngestRequest struct {
	URL        string `json:"url"`
	CompanyID  string `json:"companyId"`
	SourceType string `json:"sourceType,omitempty"`
}

type DocIngestResult struct {
	EntityID   string `json:"entityId"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	BodyLength int    `json:"bodyLength"`
	DurationMs int64  `json:"durationMs"`
}

func NewDocPipeline(entityRepo *repository.EntityRepo) *DocPipeline {
	return &DocPipeline{
		entityRepo: entityRepo,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		maxBodySize: defaultMaxBodySize,
	}
}

func (p *DocPipeline) IngestURL(ctx context.Context, req DocIngestRequest) (*DocIngestResult, error) {
	start := time.Now()

	parsedURL, err := url.Parse(req.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return nil, fmt.Errorf("invalid URL: must be http or https")
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", req.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("User-Agent", "Dev2Knowledge/1.0")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch returned HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, p.maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	sourceType := req.SourceType
	if sourceType == "" {
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "text/html") {
			sourceType = "html"
		} else if strings.Contains(ct, "text/markdown") {
			sourceType = "markdown"
		} else if strings.Contains(ct, "text/plain") {
			sourceType = "markdown"
		} else if strings.Contains(ct, "application/json") || strings.Contains(ct, "application/x-yaml") {
			sourceType = "openapi"
		} else {
			sourceType = "html"
		}
	}

	var (
		title            string
		markdownBody     string
		sectionHierarchy []string
	)

	switch sourceType {
	case "html":
		title, markdownBody, sectionHierarchy = htmlToMarkdown(string(bodyBytes))
	case "markdown":
		markdownBody = string(bodyBytes)
		if lines := strings.SplitN(markdownBody, "\n", 2); len(lines) > 0 {
			title = strings.TrimPrefix(lines[0], "# ")
		}
	default:
		return nil, fmt.Errorf("unsupported source_type: %s", sourceType)
	}

	now := time.Now().UTC()
	entityID := uuid.New().String()
	doc := map[string]any{
		"_id":               entityID,
		"companyId":        req.CompanyID,
		"url":               req.URL,
		"title":             title,
		"sourceType":       sourceType,
		"body":              markdownBody,
		"sectionHierarchy": sectionHierarchy,
		"lastFetchedAt":   now,
		"createdAt":        now,
		"updatedAt":        now,
	}

	if err := p.entityRepo.Create(ctx, "external_docs", doc); err != nil {
		return nil, fmt.Errorf("store external_doc: %w", err)
	}

	return &DocIngestResult{
		EntityID:   entityID,
		URL:        req.URL,
		Title:      title,
		BodyLength: len(markdownBody),
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

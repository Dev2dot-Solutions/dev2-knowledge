# dev2-knowledge

Knowledge graph, code ingestion, online documentation ingestion, deviation tracking, and onboarding service.

A Go microservice that owns all knowledge graph entities (conventions, business
rules, domain terms, architecture decisions, processes, repos, files, functions,
classes, imports, calls, relationships), the code ingestion pipeline (using the
Rust `ingest-parser` sidecar for tree-sitter parsing), deviation tracking, and
the onboarding flow.

## Architecture

```
                    ┌──────────────┐
                    │  ingest-     │  stdin/stdout
                    │  parser      │◀──── Rust sidecar
                    │  (Rust)      │
                    └──────┬───────┘
                           │ JSON
┌──────────────┐   NATS    ▼ ┌──────────────┐
│  dev2-chat   │◀──────────▶│ dev2-         │
│              │ knowledge.*│ knowledge     │
└──────────────┘            │ (Go :8082)    │
                            │               │
┌──────────────┐   NATS     │  MongoDB      │
│  dev2-tickets│───────────▶│  (dev2-       │
│              │knowledge   │  knowledge)   │
└──────────────┘ .link      └──────────────┘
```

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | /health | Health check |
| GET | /knowledge/search | Full-text search across knowledge graph |
| POST | /knowledge/fuzzy | Cross-entity fuzzy search |
| GET | /knowledge/entity/{type}/{id} | Get single entity |
| GET | /knowledge/trace/{type}/{id} | Trace entity relationships |
| GET | /deviations | List deviations |
| POST | /deviations | Record deviation |
| GET | /deviations/stats | Deviation stats |
| PATCH | /deviations/{id}/resolve | Resolve deviation |
| POST | /ingest/start | Trigger code ingestion |
| GET | /ingest/status | Ingestion status |
| POST | /ingest/online-doc | Ingest documentation from a URL (stores as ExternalDoc) |

## NATS Subjects

| Subject | Type | Handler |
|---------|------|---------|
| knowledge.search | Request-reply | Cross-entity text search |
| knowledge.entity.get | Request-reply | Get entity by type + ID |
| knowledge.entity.resolve | Request-reply | Resolve entity label |
| knowledge.ingest | Request-reply | Trigger repo ingestion |
| knowledge.link | Fire-and-forget | Link ticket to knowledge graph |
| knowledge.ingested | Event | Ingestion complete notification |
| knowledge.doc.ingest | Request-reply | Trigger online doc ingestion from URL |
| knowledge.doc.ingested | Event | Doc ingestion complete notification |

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| PORT | 8080 | HTTP port |
| MONGO_URI | mongodb://root:dev2@mongodb:27017/dev2knowledge | MongoDB |
| NATS_URL | nats://nats:4223 | NATS server |
| INGEST_PARSER_PATH | ingest-parser | Path to Rust sidecar binary |
| WORKSPACE_DIR | /data/workspace | Workspace for cloned repos |

## Dependencies

- MongoDB (all knowledge data)
- NATS (optional — for service-to-service calls)
- ingest-parser (optional — Rust binary for tree-sitter parsing)

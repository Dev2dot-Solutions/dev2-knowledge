package repository

import (
	"context"
	"fmt"
	"log"
	"regexp"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type EntityRepo struct {
	db *mongo.Database
}

func NewEntityRepo(db *mongo.Database) *EntityRepo {
	return &EntityRepo{db: db}
}

// EnsureIndexes creates the text indexes that $text search depends on for every
// searchable entity collection. Idempotent — mongo allows only one text index per
// collection, so pre-existing (legacy) indexes are left in place and simply logged.
func (r *EntityRepo) EnsureIndexes(ctx context.Context) {
	for _, et := range models.DefaultSearchTypes {
		coll := r.collectionFor(string(et))
		model := mongo.IndexModel{
			Keys:    bson.M{"$**": "text"},
			Options: options.Index().SetName("knowledge_text"),
		}
		if _, err := coll.Indexes().CreateOne(ctx, model); err != nil {
			log.Printf("[repository] text index on %s not created: %v (an existing index may already cover search)", et, err)
		}
	}
}

func (r *EntityRepo) collectionFor(entityType string) *mongo.Collection {
	return r.db.Collection(entityType)
}

func (r *EntityRepo) GetByID(ctx context.Context, entityType, id string) (map[string]any, error) {
	return r.GetByIDForCompany(ctx, entityType, id, "")
}

func (r *EntityRepo) GetByIDForCompany(ctx context.Context, entityType, id, companyID string) (map[string]any, error) {
	filter := bson.M{"_id": id}
	if companyID != "" && models.TenantScopedTypes[models.EntityType(entityType)] {
		filter["companyId"] = companyID
	}
	var result map[string]any
	err := r.collectionFor(entityType).FindOne(ctx, filter).Decode(&result)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get %s/%s: %w", entityType, id, err)
	}
	return result, nil
}

func (r *EntityRepo) List(ctx context.Context, entityType, companyID, search, scope, tag string, limit int64) ([]map[string]any, error) {
	filter := bson.M{}
	if models.TenantScopedTypes[models.EntityType(entityType)] {
		filter["companyId"] = companyID
	}
	if scope != "" && scope != "__all__" {
		filter["scope"] = scope
	}
	if tag != "" {
		filter["tags"] = tag
	}
	if search != "" {
		pattern := regexp.QuoteMeta(search)
		filter["$or"] = bson.A{
			bson.M{"name": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"description": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"rule": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"topic": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"term": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"definition": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"decision": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"rationale": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"body": bson.M{"$regex": pattern, "$options": "i"}},
		}
	}

	cur, err := r.collectionFor(entityType).Find(
		ctx,
		filter,
		options.Find().SetSort(bson.D{{Key: "updatedAt", Value: -1}, {Key: "createdAt", Value: -1}}).SetLimit(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", entityType, err)
	}
	defer cur.Close(ctx)

	results := make([]map[string]any, 0)
	if err := cur.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("decode %s list: %w", entityType, err)
	}
	return results, nil
}

func (r *EntityRepo) SearchText(ctx context.Context, entityType, query, companyID string, limit int, extraFilter ...bson.M) ([]map[string]any, error) {
	coll := r.collectionFor(entityType)
	filter := bson.M{"$text": bson.M{"$search": query}}
	if companyID != "" && models.TenantScopedTypes[models.EntityType(entityType)] {
		filter["companyId"] = companyID
	}
	for _, ef := range extraFilter {
		for k, v := range ef {
			filter[k] = v
		}
	}
	cur, err := coll.Find(ctx, filter, options.Find().SetSort(bson.M{"score": bson.M{"$meta": "textScore"}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("search %s: %w", entityType, err)
	}
	defer cur.Close(ctx)
	var results []map[string]any
	if err := cur.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("decode %s: %w", entityType, err)
	}
	return results, nil
}

func (r *EntityRepo) Create(ctx context.Context, entityType string, doc any) error {
	_, err := r.collectionFor(entityType).InsertOne(ctx, doc)
	return err
}

func (r *EntityRepo) InsertMany(ctx context.Context, entityType string, docs []any) error {
	if len(docs) == 0 {
		return nil
	}
	const chunkSize = 100
	for i := 0; i < len(docs); i += chunkSize {
		end := i + chunkSize
		if end > len(docs) {
			end = len(docs)
		}
		if _, err := r.collectionFor(entityType).InsertMany(ctx, docs[i:end]); err != nil {
			return fmt.Errorf("insert %s: %w", entityType, err)
		}
	}
	return nil
}

func (r *EntityRepo) Delete(ctx context.Context, entityType, id string) error {
	_, err := r.collectionFor(entityType).DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *EntityRepo) ResolveEntityIdentity(ctx context.Context, entityType, id string) (*models.KnowledgeEntityResolveResponse, error) {
	doc, err := r.GetByID(ctx, entityType, id)
	if err != nil || doc == nil {
		return nil, err
	}
	resp := &models.KnowledgeEntityResolveResponse{Type: entityType, ID: id}
	if name, ok := doc["name"].(string); ok {
		resp.Name = &name
	} else if rule, ok := doc["rule"].(string); ok {
		s := rule
		if len(s) > 120 { s = s[:120] }
		resp.Name = &s
	} else if term, ok := doc["term"].(string); ok {
		resp.Name = &term
	} else if topic, ok := doc["topic"].(string); ok {
		resp.Name = &topic
	} else if title, ok := doc["title"].(string); ok {
		resp.Name = &title
	}
	return resp, nil
}

func (r *EntityRepo) Count(ctx context.Context, entityType string) (int64, error) {
	return r.collectionFor(entityType).CountDocuments(ctx, bson.M{})
}

func (r *EntityRepo) SearchCrossEntity(ctx context.Context, query, companyID string, types []string, limit int, entityFilters map[string]bson.M) (*models.KnowledgeSearchResponse, error) {
	resp := &models.KnowledgeSearchResponse{Query: query, Results: make(map[string][]models.SearchHit)}
	if limit <= 0 { limit = 5 }
	for _, et := range types {
		var extra []bson.M
		if entityFilters != nil {
			if ef, ok := entityFilters[et]; ok {
				extra = append(extra, ef)
			}
		}
		docs, err := r.SearchText(ctx, et, query, companyID, limit, extra...)
		if err != nil {
			log.Printf("[repository] search on %s failed: %v", et, err)
			continue
		}
		for _, doc := range docs {
			hit := docToSearchHit(et, doc)
			resp.Results[et] = append(resp.Results[et], hit)
			resp.TotalMatches++
		}
	}
	return resp, nil
}

func docToSearchHit(entityType string, doc map[string]any) models.SearchHit {
	hit := models.SearchHit{ID: fmt.Sprintf("%v", doc["_id"])}
	if name, ok := doc["name"].(string); ok { hit.Name = name
	} else if rule, ok := doc["rule"].(string); ok { hit.Name = truncate(rule, 120)
	} else if term, ok := doc["term"].(string); ok { hit.Name = term
	} else if topic, ok := doc["topic"].(string); ok { hit.Name = topic
	} else if title, ok := doc["title"].(string); ok { hit.Name = title }
	if desc, ok := doc["description"].(string); ok { hit.Snippet = truncate(desc, 200)
	} else if body, ok := doc["body"].(string); ok { hit.Snippet = truncate(body, 200)
	} else if rule, ok := doc["rule"].(string); ok { hit.Snippet = truncate(rule, 200)
	} else if definition, ok := doc["definition"].(string); ok { hit.Snippet = truncate(definition, 200)
	} else if decision, ok := doc["decision"].(string); ok { hit.Snippet = truncate(decision, 200)
	} else if rationale, ok := doc["rationale"].(string); ok { hit.Snippet = truncate(rationale, 200) }
	if score, ok := doc["score"].(float64); ok { hit.Score = score }
	if url, ok := doc["url"].(string); ok { hit.URL = url }
	if st, ok := doc["sourceType"].(string); ok { hit.SourceType = st }
	return hit
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max { return s }
	return string(runes[:max])
}

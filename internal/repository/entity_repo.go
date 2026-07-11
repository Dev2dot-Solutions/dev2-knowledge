package repository

import (
	"context"
	"fmt"

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

func (r *EntityRepo) collectionFor(entityType string) *mongo.Collection {
	return r.db.Collection(entityType)
}

func (r *EntityRepo) GetByID(ctx context.Context, entityType, id string) (map[string]any, error) {
	var result map[string]any
	err := r.collectionFor(entityType).FindOne(ctx, bson.M{"_id": id}).Decode(&result)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get %s/%s: %w", entityType, id, err)
	}
	return result, nil
}

func (r *EntityRepo) SearchText(ctx context.Context, entityType, query, companyID string, limit int) ([]map[string]any, error) {
	coll := r.collectionFor(entityType)
	filter := bson.M{"$text": bson.M{"$search": query}}
	if companyID != "" && models.TenantScopedTypes[models.EntityType(entityType)] {
		filter["company_id"] = companyID
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
	}
	return resp, nil
}

func (r *EntityRepo) Count(ctx context.Context, entityType string) (int64, error) {
	return r.collectionFor(entityType).CountDocuments(ctx, bson.M{})
}

func (r *EntityRepo) SearchCrossEntity(ctx context.Context, query, companyID string, types []string, limit int) (*models.KnowledgeSearchResponse, error) {
	resp := &models.KnowledgeSearchResponse{Query: query, Results: make(map[string][]models.SearchHit)}
	if limit <= 0 { limit = 5 }
	for _, et := range types {
		docs, err := r.SearchText(ctx, et, query, companyID, limit)
		if err != nil { continue }
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
	} else if topic, ok := doc["topic"].(string); ok { hit.Name = topic }
	if desc, ok := doc["description"].(string); ok { hit.Snippet = truncate(desc, 200)
	} else if body, ok := doc["body"].(string); ok { hit.Snippet = truncate(body, 200)
	} else if rule, ok := doc["rule"].(string); ok { hit.Snippet = truncate(rule, 200) }
	if score, ok := doc["score"].(float64); ok { hit.Score = score }
	return hit
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max { return s }
	return string(runes[:max])
}

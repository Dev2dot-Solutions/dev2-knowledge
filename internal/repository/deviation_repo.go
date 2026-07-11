package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/models"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type DeviationRepo struct {
	coll *mongo.Collection
}

func NewDeviationRepo(db *mongo.Database) *DeviationRepo {
	return &DeviationRepo{coll: db.Collection("deviations")}
}

func (r *DeviationRepo) Create(ctx context.Context, d *models.Deviation) error {
	if d.ID == "" { d.ID = uuid.New().String() }
	now := time.Now().UTC()
	d.CreatedAt = now; d.UpdatedAt = now
	_, err := r.coll.InsertOne(ctx, d)
	return err
}

func (r *DeviationRepo) GetByID(ctx context.Context, id string) (*models.Deviation, error) {
	var d models.Deviation
	err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&d)
	if err == mongo.ErrNoDocuments { return nil, nil }
	if err != nil { return nil, fmt.Errorf("get deviation: %w", err) }
	return &d, nil
}

func (r *DeviationRepo) List(ctx context.Context, companyID string) ([]models.Deviation, error) {
	cur, err := r.coll.Find(ctx, bson.M{"company_id": companyID})
	if err != nil { return nil, err }
	defer cur.Close(ctx)
	var results []models.Deviation
	if err := cur.All(ctx, &results); err != nil { return nil, err }
	if results == nil { results = []models.Deviation{} }
	return results, nil
}

func (r *DeviationRepo) Update(ctx context.Context, id string, updates map[string]any) error {
	updates["updated_at"] = time.Now().UTC()
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": updates})
	return err
}

func (r *DeviationRepo) Stats(ctx context.Context, companyID string) (*models.DeviationStats, error) {
	filter := bson.M{"company_id": companyID}
	total, _ := r.coll.CountDocuments(ctx, filter)
	open, _ := r.coll.CountDocuments(ctx, bson.M{"company_id": companyID, "status": "pending_review"})
	resolved, _ := r.coll.CountDocuments(ctx, bson.M{"company_id": companyID, "status": "resolved"})

	// By status
	statusCur, _ := r.coll.Aggregate(ctx, bson.A{
		bson.M{"$match": filter},
		bson.M{"$group": bson.M{"_id": "$status", "count": bson.M{"$sum": 1}}},
	})
	byStatus := make(map[string]int64)
	if statusCur != nil {
		defer statusCur.Close(ctx)
		for statusCur.Next(ctx) {
			var s struct { ID string `bson:"_id"`; Count int64 `bson:"count"` }
			statusCur.Decode(&s)
			byStatus[s.ID] = s.Count
		}
	}

	return &models.DeviationStats{Total: int(total), Open: open, Resolved: resolved, ByStatus: byStatus}, nil
}

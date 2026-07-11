package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/nats-io/nats.go"

	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/config"
	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/database"
	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/handlers"
	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/ingestion"
	kh "github.com/Dev2dot-Solutions/dev2-knowledge/internal/nats"
	"github.com/Dev2dot-Solutions/dev2-knowledge/internal/repository"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// MongoDB
	mongoClient, err := database.NewMongoClient(ctx, cfg.MongoURI)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer mongoClient.Disconnect(ctx)
	mongoDB := mongoClient.Database(cfg.MongoDatabase)
	log.Printf("Connected to MongoDB: %s/%s", cfg.MongoURI, cfg.MongoDatabase)

	// Repositories
	entityRepo := repository.NewEntityRepo(mongoDB)
	deviationRepo := repository.NewDeviationRepo(mongoDB)

	// Ingestion pipeline
	pipeline := ingestion.NewPipeline(entityRepo, cfg.IngestParserPath, cfg.WorkspaceDir)

	// NATS (optional)
	var nc *nats.Conn
	var natsHandler *kh.Handler
	if ncConn, err := nats.Connect(cfg.NATSURL); err != nil {
		log.Printf("NATS not available — continuing without: %v", cfg.NATSURL, err)
		natsHandler, _ = kh.NewHandler(nil, entityRepo, pipeline)
	} else {
		nc = ncConn
		natsHandler, err = kh.NewHandler(nc, entityRepo, pipeline)
		if err != nil {
			log.Fatalf("Failed to create NATS handler: %v", err)
		}
		log.Printf("Connected to NATS: %s", cfg.NATSURL)
	}
	if nc != nil {
		defer natsHandler.Close()
	}

	// Router
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"dev2-knowledge"}`))
	})

	// Register handlers
	handlers.NewKnowledgeHandler(entityRepo).Routes(r)
	handlers.NewDeviationHandler(deviationRepo).Routes(r)
	handlers.NewIngestionHandler(pipeline).Routes(r)

	// Server
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 300 * time.Second, // Long timeout for ingestion
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
		cancel()
	}()

	log.Printf("dev2-knowledge starting on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}

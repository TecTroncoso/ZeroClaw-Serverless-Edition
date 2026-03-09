// Package memory provides memory storage implementations for ZeroClaw.
// This package includes a Supabase/pgvector implementation for serverless deployments.
package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zeroclaw/zeroclaw-go/internal/core"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// SupabaseMemory implements the Memory interface using Supabase with pgvector.
// This replaces the SQLite-based memory from the original ZeroClaw Rust implementation.
type SupabaseMemory struct {
	// db is the PostgreSQL connection pool.
	db *sql.DB
	// embeddingService generates embeddings for semantic search.
	embeddingService core.EmbeddingService
	// defaultDimension is the embedding dimension (1536 for OpenAI ada-002).
	defaultDimension int
}

// SupabaseConfig holds the configuration for Supabase connection.
type SupabaseConfig struct {
	// ConnectionString is the PostgreSQL connection URL.
	// Format: postgresql://user:password@host:port/database?sslmode=require
	ConnectionString string
	// EmbeddingService generates embeddings (optional, can be set later).
	EmbeddingService core.EmbeddingService
	// EmbeddingDimension is the dimension of embedding vectors.
	EmbeddingDimension int
}

// NewSupabaseMemory creates a new Supabase memory backend.
func NewSupabaseMemory(cfg *SupabaseConfig) (*SupabaseMemory, error) {
	if cfg.ConnectionString == "" {
		return nil, fmt.Errorf("connection string is required")
	}

	// Open database connection
	db, err := sql.Open("postgres", cfg.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for serverless
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	dimension := cfg.EmbeddingDimension
	if dimension == 0 {
		dimension = 1536 // Default for OpenAI embeddings
	}

	return &SupabaseMemory{
		db:                db,
		embeddingService:  cfg.EmbeddingService,
		defaultDimension:  dimension,
	}, nil
}

// Name returns the backend name.
func (m *SupabaseMemory) Name() string {
	return "supabase"
}

// Store saves a memory entry without computing an embedding.
func (m *SupabaseMemory) Store(ctx context.Context, key, content string, category core.MemoryCategory, sessionID *string) error {
	// If we have an embedding service, compute the embedding
	if m.embeddingService != nil {
		embedding, err := m.embeddingService.GenerateEmbedding(ctx, content)
		if err != nil {
			// Log warning but continue without embedding
			fmt.Printf("Warning: failed to generate embedding: %v\n", err)
		} else {
			return m.StoreWithEmbedding(ctx, key, content, category, sessionID, embedding)
		}
	}

	// Store without embedding
	return m.storeWithoutEmbedding(ctx, key, content, category, sessionID)
}

// storeWithoutEmbedding inserts a memory entry without an embedding.
func (m *SupabaseMemory) storeWithoutEmbedding(ctx context.Context, key, content string, category core.MemoryCategory, sessionID *string) error {
	query := `
		INSERT INTO memory_entries (key, content, category, session_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (key) 
		DO UPDATE SET 
			content = EXCLUDED.content,
			category = EXCLUDED.category,
			session_id = EXCLUDED.session_id,
			updated_at = NOW()
	`

	var sessionIDVal interface{}
	if sessionID != nil {
		sessionIDVal = *sessionID
	}

	_, err := m.db.ExecContext(ctx, query, key, content, string(category), sessionIDVal)
	if err != nil {
		return fmt.Errorf("failed to store memory: %w", err)
	}

	return nil
}

// StoreWithEmbedding saves a memory entry with a pre-computed embedding.
func (m *SupabaseMemory) StoreWithEmbedding(ctx context.Context, key, content string, category core.MemoryCategory, sessionID *string, embedding []float32) error {
	query := `
		INSERT INTO memory_entries (key, content, category, session_id, embedding)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (key) 
		DO UPDATE SET 
			content = EXCLUDED.content,
			category = EXCLUDED.category,
			session_id = EXCLUDED.session_id,
			embedding = EXCLUDED.embedding,
			updated_at = NOW()
	`

	var sessionIDVal interface{}
	if sessionID != nil {
		sessionIDVal = *sessionID
	}

	// Convert embedding to PostgreSQL array format
	embeddingStr := m.embeddingToPostgresArray(embedding)

	_, err := m.db.ExecContext(ctx, query, key, content, string(category), sessionIDVal, embeddingStr)
	if err != nil {
		return fmt.Errorf("failed to store memory with embedding: %w", err)
	}

	return nil
}

// Recall retrieves memories matching a query using semantic similarity.
func (m *SupabaseMemory) Recall(ctx context.Context, query string, limit int, sessionID *string) ([]core.MemoryEntry, error) {
	// Generate embedding for the query
	if m.embeddingService == nil {
		return nil, fmt.Errorf("embedding service is required for semantic search")
	}

	embedding, err := m.embeddingService.GenerateEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	return m.RecallWithEmbedding(ctx, embedding, limit, sessionID)
}

// RecallWithEmbedding retrieves memories using a pre-computed embedding vector.
func (m *SupabaseMemory) RecallWithEmbedding(ctx context.Context, embedding []float32, limit int, sessionID *string) ([]core.MemoryEntry, error) {
	query := `
		SELECT id, key, content, category, timestamp, session_id, score, metadata
		FROM search_memories($1, 0.5, $2, NULL, $3)
	`

	embeddingStr := m.embeddingToPostgresArray(embedding)
	var sessionIDVal interface{}
	if sessionID != nil {
		sessionIDVal = *sessionID
	}

	rows, err := m.db.QueryContext(ctx, query, embeddingStr, limit, sessionIDVal)
	if err != nil {
		return nil, fmt.Errorf("failed to search memories: %w", err)
	}
	defer rows.Close()

	var entries []core.MemoryEntry
	for rows.Next() {
		entry, err := m.scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating memory entries: %w", err)
	}

	return entries, nil
}

// Get retrieves a specific memory by key.
func (m *SupabaseMemory) Get(ctx context.Context, key string) (*core.MemoryEntry, error) {
	query := `
		SELECT id, key, content, category, timestamp, session_id, score, metadata
		FROM memory_entries
		WHERE key = $1
	`

	row := m.db.QueryRowContext(ctx, query, key)
	entry, err := m.scanMemoryEntry(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get memory: %w", err)
	}

	return entry, nil
}

// List returns all memory entries, optionally filtered by category and session.
func (m *SupabaseMemory) List(ctx context.Context, category *core.MemoryCategory, sessionID *string) ([]core.MemoryEntry, error) {
	query := `
		SELECT id, key, content, category, timestamp, session_id, score, metadata
		FROM memory_entries
		WHERE ($1::text IS NULL OR category = $1)
		  AND ($2::text IS NULL OR session_id = $2)
		ORDER BY timestamp DESC
	`

	var categoryVal interface{}
	if category != nil {
		categoryVal = string(*category)
	}
	var sessionIDVal interface{}
	if sessionID != nil {
		sessionIDVal = *sessionID
	}

	rows, err := m.db.QueryContext(ctx, query, categoryVal, sessionIDVal)
	if err != nil {
		return nil, fmt.Errorf("failed to list memories: %w", err)
	}
	defer rows.Close()

	var entries []core.MemoryEntry
	for rows.Next() {
		entry, err := m.scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating memory entries: %w", err)
	}

	return entries, nil
}

// Forget removes a memory by key.
func (m *SupabaseMemory) Forget(ctx context.Context, key string) (bool, error) {
	query := `DELETE FROM memory_entries WHERE key = $1`

	result, err := m.db.ExecContext(ctx, query, key)
	if err != nil {
		return false, fmt.Errorf("failed to delete memory: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected > 0, nil
}

// Count returns the total number of memories.
func (m *SupabaseMemory) Count(ctx context.Context) (int, error) {
	query := `SELECT count_memories(NULL, NULL)`

	var count int
	err := m.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count memories: %w", err)
	}

	return count, nil
}

// HealthCheck verifies the backend is healthy.
func (m *SupabaseMemory) HealthCheck(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := m.db.PingContext(ctx)
	return err == nil
}

// Close closes the database connection.
func (m *SupabaseMemory) Close() error {
	return m.db.Close()
}

// SetEmbeddingService sets the embedding service for semantic search.
func (m *SupabaseMemory) SetEmbeddingService(service core.EmbeddingService) {
	m.embeddingService = service
}

// scanMemoryEntry scans a row into a MemoryEntry.
func (m *SupabaseMemory) scanMemoryEntry(scanner interface {
	Scan(dest ...interface{}) error
}) (*core.MemoryEntry, error) {
	var entry core.MemoryEntry
	var categoryStr string
	var timestamp time.Time
	var sessionID sql.NullString
	var score sql.NullFloat64
	var metadataBytes []byte

	err := scanner.Scan(
		&entry.ID,
		&entry.Key,
		&entry.Content,
		&categoryStr,
		&timestamp,
		&sessionID,
		&score,
		&metadataBytes,
	)
	if err != nil {
		return nil, err
	}

	entry.Category = core.MemoryCategory(categoryStr)
	entry.Timestamp = timestamp

	if sessionID.Valid {
		entry.SessionID = &sessionID.String
	}
	if score.Valid {
		entry.Score = &score.Float64
	}
	if len(metadataBytes) > 0 {
		var metadata map[string]interface{}
		if err := json.Unmarshal(metadataBytes, &metadata); err == nil {
			entry.Metadata = metadata
		}
	}

	return &entry, nil
}

// embeddingToPostgresArray converts a float32 slice to PostgreSQL array format.
func (m *SupabaseMemory) embeddingToPostgresArray(embedding []float32) string {
	if len(embedding) == 0 {
		return "[]"
	}

	result := "["
	for i, v := range embedding {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("%f", v)
	}
	result += "]"
	return result
}

// ============================================================================
// HYBRID SEARCH (Vector + Full-Text)
// ============================================================================

// HybridConfig holds configuration for hybrid search.
type HybridConfig struct {
	// SemanticWeight weight for vector similarity (0-1)
	SemanticWeight float64
	// FTSWeight weight for full-text search (0-1)
	FTSWeight float64
	// RRFk parameter for Reciprocal Rank Fusion
	RRFk int
}

// DefaultHybridConfig returns sensible defaults for hybrid search.
func DefaultHybridConfig() *HybridConfig {
	return &HybridConfig{
		SemanticWeight: 0.5,
		FTSWeight:      0.3,
		RRFk:           60,
	}
}

// RecallHybrid performs hybrid search combining vector similarity with full-text search.
// This is the recommended search method as it combines the best of both approaches:
// - Vector search: semantic similarity, handles synonyms, paraphrases
// - FTS: exact keyword matching, handles acronyms, specific terms
//
// The function uses RRF (Reciprocal Rank Fusion) to combine rankings from both methods.
func (m *SupabaseMemory) RecallHybrid(ctx context.Context, queryText string, queryEmbedding []float32, limit int, sessionID *string) ([]core.MemoryEntry, error) {
	// Validate inputs
	if queryEmbedding == nil || len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("query embedding is required for hybrid search")
	}
	if queryText == "" {
		return nil, fmt.Errorf("query text is required for hybrid search")
	}

	// Use default config
	cfg := DefaultHybridConfig()

	// Prepare query
	query := `
		SELECT id, key, content, category, timestamp, session_id, score, metadata,
		       semantic_score, fts_score, rrf_score
		FROM hybrid_search_memories($1, $2, $3, $4, $5, $6, $7)
	`

	embeddingStr := m.embeddingToPostgresArray(queryEmbedding)
	var sessionIDVal interface{}
	if sessionID != nil {
		sessionIDVal = *sessionID
	}

	// Execute query
	rows, err := m.db.QueryContext(ctx, query,
		embeddingStr,          // $1: query_embedding
		queryText,             // $2: query_text
		limit,                 // $3: match_count
		sessionIDVal,          // $4: session_id
		cfg.SemanticWeight,    // $5: semantic_weight
		cfg.FTSWeight,         // $6: fts_weight
		cfg.RRFk,              // $7: rrf_k
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute hybrid search: %w", err)
	}
	defer rows.Close()

	// Scan results
	var entries []core.MemoryEntry
	for rows.Next() {
		entry, err := m.scanHybridEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hybrid search results: %w", err)
	}

	return entries, nil
}

// RecallHybridWithConfig performs hybrid search with custom configuration.
func (m *SupabaseMemory) RecallHybridWithConfig(ctx context.Context, queryText string, queryEmbedding []float32, limit int, sessionID *string, cfg *HybridConfig) ([]core.MemoryEntry, error) {
	// Validate inputs
	if queryEmbedding == nil || len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("query embedding is required for hybrid search")
	}
	if queryText == "" {
		return nil, fmt.Errorf("query text is required for hybrid search")
	}

	if cfg == nil {
		cfg = DefaultHybridConfig()
	}

	// Prepare query
	query := `
		SELECT id, key, content, category, timestamp, session_id, score, metadata,
		       semantic_score, fts_score, rrf_score
		FROM hybrid_search_memories($1, $2, $3, $4, $5, $6, $7)
	`

	embeddingStr := m.embeddingToPostgresArray(queryEmbedding)
	var sessionIDVal interface{}
	if sessionID != nil {
		sessionIDVal = *sessionID
	}

	// Execute query
	rows, err := m.db.QueryContext(ctx, query,
		embeddingStr,
		queryText,
		limit,
		sessionIDVal,
		cfg.SemanticWeight,
		cfg.FTSWeight,
		cfg.RRFk,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute hybrid search: %w", err)
	}
	defer rows.Close()

	var entries []core.MemoryEntry
	for rows.Next() {
		entry, err := m.scanHybridEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hybrid search results: %w", err)
	}

	return entries, nil
}

// HybridEntry represents a memory entry with additional hybrid search scores.
type HybridEntry struct {
	core.MemoryEntry
	SemanticScore float64 `json:"semantic_score"`
	FTSScore     float64 `json:"fts_score"`
	RRFScore     float64 `json:"rrf_score"`
}

// scanHybridEntry scans a row from hybrid search into a HybridEntry.
func (m *SupabaseMemory) scanHybridEntry(scanner interface{ Scan(dest ...interface{}) error }) (*core.MemoryEntry, error) {
	var entry core.MemoryEntry
	var categoryStr string
	var timestamp time.Time
	var sessionID sql.NullString
	var score sql.NullFloat64
	var metadataBytes []byte
	var semanticScore, ftsScore, rrfScore float64

	err := scanner.Scan(
		&entry.ID,
		&entry.Key,
		&entry.Content,
		&categoryStr,
		&timestamp,
		&sessionID,
		&score,
		&metadataBytes,
		&semanticScore,
		&ftsScore,
		&rrfScore,
	)
	if err != nil {
		return nil, err
	}

	entry.Category = core.MemoryCategory(categoryStr)
	entry.Timestamp = timestamp

	if sessionID.Valid {
		entry.SessionID = &sessionID.String
	}
	if score.Valid {
		entry.Score = &score.Float64
	}
	if len(metadataBytes) > 0 {
		var metadata map[string]interface{}
		if err := json.Unmarshal(metadataBytes, &metadata); err == nil {
			entry.Metadata = metadata
		}
	}

	return &entry, nil
}

// SearchFTS performs pure full-text search without vector similarity.
// Useful when you only need keyword matching.
func (m *SupabaseMemory) SearchFTS(ctx context.Context, queryText string, limit int, sessionID *string, category *string) ([]core.MemoryEntry, error) {
	if queryText == "" {
		return nil, fmt.Errorf("query text is required for FTS search")
	}

	query := `
		SELECT id, key, content, category, timestamp, session_id, score, metadata
		FROM search_memories_fts($1, $2, $3, $4)
	`

	var sessionIDVal interface{}
	if sessionID != nil {
		sessionIDVal = *sessionID
	}

	var categoryVal interface{}
	if category != nil {
		categoryVal = *category
	}

	rows, err := m.db.QueryContext(ctx, query, queryText, limit, sessionIDVal, categoryVal)
	if err != nil {
		return nil, fmt.Errorf("failed to execute FTS search: %w", err)
	}
	defer rows.Close()

	var entries []core.MemoryEntry
	for rows.Next() {
		entry, err := m.scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating FTS search results: %w", err)
	}

	return entries, nil
}

// ============================================================================
// HYBRID SEARCH (Optional Advanced Feature)
// ============================================================================

// HybridSearch performs combined keyword and semantic search.
func (m *SupabaseMemory) HybridSearch(ctx context.Context, queryText string, embedding []float32, limit int, semanticWeight float64, sessionID *string) ([]core.MemoryEntry, error) {
	query := `
		SELECT id, key, content, category, timestamp, session_id, score, metadata
		FROM hybrid_search_memories($1, $2, $3, $4, NULL, $5)
	`

	embeddingStr := m.embeddingToPostgresArray(embedding)
	var sessionIDVal interface{}
	if sessionID != nil {
		sessionIDVal = *sessionID
	}

	rows, err := m.db.QueryContext(ctx, query, queryText, embeddingStr, limit, semanticWeight, sessionIDVal)
	if err != nil {
		return nil, fmt.Errorf("failed to hybrid search memories: %w", err)
	}
	defer rows.Close()

	var entries []core.MemoryEntry
	for rows.Next() {
		entry, err := m.scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating memory entries: %w", err)
	}

	return entries, nil
}

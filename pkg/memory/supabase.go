// Package memory provides memory storage implementations for ZeroClaw.
// This package includes a Supabase/pgvector implementation for serverless deployments.
package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/zeroclaw/zeroclaw-go/pkg/core"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// containsParam checks if a connection string already contains a specific parameter.
func containsParam(connStr, param string) bool {
	return strings.Contains(connStr, param+"=") || strings.Contains(connStr, "&"+param)
}

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

	// Ensure connection string has proper IPv4 fallback for Vercel serverless
	// Vercel has issues with IPv6 connections, so we force IPv4
	connStr := cfg.ConnectionString
	if !containsParam(connStr, "prefer_simple_protocol") {
		if strings.Contains(connStr, "?") {
			connStr += "&prefer_simple_protocol=on"
		} else {
			connStr += "?prefer_simple_protocol=on"
		}
	}

	// Open database connection
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for serverless
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify connection with retry logic for serverless cold starts
	var pingErr error
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pingErr = db.PingContext(ctx)
		cancel()
		if pingErr == nil {
			break
		}
		time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
	}
	if pingErr != nil {
		// Return nil memory backend instead of error - allows graceful degradation
		fmt.Printf("ZeroClaw: WARNING - Database connection failed, memory features disabled: %v\n", pingErr)
		db.Close() // Clean up the failed connection
		return nil, nil
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

// Smart Chunking constants for memory storage.
const (
	// ChunkMaxLen is the maximum character length per chunk for embedding.
	ChunkMaxLen = 500
	// ChunkOverlap is the number of overlapping characters between consecutive chunks.
	ChunkOverlap = 50
)

// Store saves a memory entry. If the content exceeds ChunkMaxLen, it is split
// into semantically-aware chunks and each chunk is stored as a separate entry
// with its own embedding, dramatically improving RAG recall precision.
func (m *SupabaseMemory) Store(ctx context.Context, key, content string, category core.MemoryCategory, sessionID *string) error {
	// Guard against nil receiver - critical for serverless environments
	if m == nil {
		return fmt.Errorf("memory backend not initialized")
	}

	// Guard against nil database connection
	if m.db == nil {
		return fmt.Errorf("database connection not available")
	}

	// Smart Chunking: split long content into smaller chunks for better semantic search
	chunks := ChunkText(content, ChunkMaxLen, ChunkOverlap)
	if len(chunks) == 0 {
		return nil // Nothing to store
	}

	// If only one chunk (short content), store normally with original key
	if len(chunks) == 1 {
		return m.storeSingleEntry(ctx, key, content, category, sessionID)
	}

	// Multiple chunks: store each with a suffixed key
	log.Printf("ZeroClaw: Smart Chunking split content into %d chunks (key=%s)", len(chunks), key)
	
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	
	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, cText string) {
			defer wg.Done()
			chunkKey := fmt.Sprintf("%s_chunk_%d", key, idx)
			if err := m.storeSingleEntry(ctx, chunkKey, cText, category, sessionID); err != nil {
				log.Printf("Warning: failed to store chunk %d/%d for key=%s: %v", idx+1, len(chunks), key, err)
				
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(i, chunk)
	}

	wg.Wait()
	return firstErr
}

// storeSingleEntry stores a single memory entry, computing an embedding if available.
func (m *SupabaseMemory) storeSingleEntry(ctx context.Context, key, content string, category core.MemoryCategory, sessionID *string) error {
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
	// Guard against nil receiver - critical for serverless environments
	if m == nil {
		return nil, fmt.Errorf("memory backend not initialized")
	}

	// Guard against nil database connection
	if m.db == nil {
		return nil, fmt.Errorf("database connection not available")
	}

	// If no embedding service, fall back to full-text search (graceful degradation)
	if m.embeddingService == nil {
		return m.SearchFTS(ctx, query, limit, sessionID, nil)
	}

	// Generate embedding for the query
	embedding, err := m.embeddingService.GenerateEmbedding(ctx, query)
	if err != nil {
		// If embedding generation fails, fall back to FTS
		fmt.Printf("ZeroClaw: WARNING - Embedding generation failed, falling back to FTS: %v\n", err)
		return m.SearchFTS(ctx, query, limit, sessionID, nil)
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

	log.Printf("DEBUG: RecallWithEmbedding session_id=%q, limit=%d, embedding_len=%d", func() string { if sessionID != nil { return *sessionID } else { return "<nil>" } }(), limit, len(embedding))
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

	log.Printf("DEBUG: RecallWithEmbedding found %d entries", len(entries))
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

// GetRecentHistory retrieves the most recent N conversation turns for a session.
func (m *SupabaseMemory) GetRecentHistory(ctx context.Context, sessionID *string, limit int) ([]core.MemoryEntry, error) {
	if sessionID == nil || *sessionID == "" {
		return nil, nil // Cannot fetch history without a session
	}

	query := `
		SELECT id, key, content, category, timestamp, session_id, NULL as score, metadata
		FROM memory_entries
		WHERE session_id = $1 AND category = 'conversation'
		ORDER BY timestamp DESC
		LIMIT $2
	`

	rows, err := m.db.QueryContext(ctx, query, *sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent history: %w", err)
	}
	defer rows.Close()

	var entries []core.MemoryEntry
	for rows.Next() {
		entry, err := m.scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		// Supabase uses 1536 by default in the struct but we are omitting embedding here anyway.
		entries = append(entries, *entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating history entries: %w", err)
	}

	// Reverse the entries so they are in chronological order (oldest first)
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	return entries, nil
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
// HYBRID SEARCH (Vector + Full-Text with RRF)
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
//
// SQL function signature (schema_definitive.sql):
//
//	hybrid_search_memories(
//		query_embedding vector(1536), -- $1
//		query_text TEXT, -- $2
//		match_count INT DEFAULT 10, -- $3
//		p_session_id TEXT DEFAULT NULL,-- $4
//		semantic_weight FLOAT DEFAULT 0.5, -- $5
//		fts_weight FLOAT DEFAULT 0.3, -- $6
//		rrf_k INT DEFAULT 60 -- $7
//	)
func (m *SupabaseMemory) RecallHybrid(ctx context.Context, queryText string, queryEmbedding []float32, limit int, sessionID *string) ([]core.MemoryEntry, error) {
	// Validate inputs
	if queryEmbedding == nil || len(queryEmbedding) == 0 {
		fmt.Printf("DEBUG: queryEmbedding is empty or nil in RecallHybrid. Length: %d\n", len(queryEmbedding))
		return nil, fmt.Errorf("query embedding is required for hybrid search")
	}
	if queryText == "" {
		return nil, fmt.Errorf("query text is required for hybrid search")
	}

	// Use default config
	cfg := DefaultHybridConfig()

	// Prepare query - matches hybrid_search_memories signature exactly
	query := `
		SELECT * FROM (
			SELECT id, key, content, category, timestamp, session_id, score, metadata,
				   semantic_score, fts_score, rrf_score
			FROM hybrid_search_memories($1, $2, $3, $4, $5, $6, $7)
		) sub
		WHERE semantic_score > 0.7 OR fts_score > 0.1
	`

	embeddingStr := m.embeddingToPostgresArray(queryEmbedding)
	var sessionIDVal interface{}
	if sessionID != nil {
		sessionIDVal = *sessionID
	}

	// Execute query with parameters in EXACT order matching SQL function
	rows, err := m.db.QueryContext(ctx, query,
		embeddingStr,         // $1: query_embedding
		queryText,            // $2: query_text
		limit,                // $3: match_count
		sessionIDVal,         // $4: p_session_id
		cfg.SemanticWeight,   // $5: semantic_weight
		cfg.FTSWeight,        // $6: fts_weight
		cfg.RRFk,             // $7: rrf_k
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

	// Debug log for hybrid search results
	log.Printf("DEBUG: RecallHybrid found %d memories for session %s", len(entries), func() string { if sessionID != nil { return *sessionID } else { return "nil" } }())

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

	// Prepare query - matches hybrid_search_memories signature exactly
	query := `
		SELECT * FROM (
			SELECT id, key, content, category, timestamp, session_id, score, metadata,
				   semantic_score, fts_score, rrf_score
			FROM hybrid_search_memories($1, $2, $3, $4, $5, $6, $7)
		) sub
		WHERE semantic_score > 0.7 OR fts_score > 0.1
	`

	embeddingStr := m.embeddingToPostgresArray(queryEmbedding)
	var sessionIDVal interface{}
	if sessionID != nil {
		sessionIDVal = *sessionID
	}

	// Execute query with parameters in EXACT order matching SQL function
	rows, err := m.db.QueryContext(ctx, query,
		embeddingStr,         // $1: query_embedding
		queryText,            // $2: query_text
		limit,                // $3: match_count
		sessionIDVal,         // $4: p_session_id
		cfg.SemanticWeight,   // $5: semantic_weight
		cfg.FTSWeight,        // $6: fts_weight
		cfg.RRFk,             // $7: rrf_k
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

// scanHybridEntry scans a row from hybrid search into a MemoryEntry.
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
//
// SQL function signature (schema_definitive.sql):
//
//	search_memories_fts(
//		query_text TEXT, -- $1
//		match_count INT DEFAULT 10, -- $2
//		p_session_id TEXT DEFAULT NULL,-- $3
//		p_category TEXT DEFAULT NULL -- $4
//	)
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

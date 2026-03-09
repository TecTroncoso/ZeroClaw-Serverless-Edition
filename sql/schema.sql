-- ============================================================================
-- ZeroClaw Go - Supabase/pgvector Schema
-- ============================================================================
-- Execute this script in the Supabase SQL Editor to set up the vector database
-- for semantic memory storage and retrieval.
-- ============================================================================

-- 1. Enable the pgvector extension (if not already enabled)
-- This is required for vector similarity search operations.
CREATE EXTENSION IF NOT EXISTS vector;

-- 2. Create the memory_entries table
-- This table stores all memory entries with their embeddings and metadata.
CREATE TABLE IF NOT EXISTS memory_entries (
    -- Unique identifier for each memory entry
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Key for lookup (e.g., "user_preference_theme")
    key TEXT NOT NULL,
    
    -- The actual content/text of the memory
    content TEXT NOT NULL,
    
    -- Category for organization: 'core', 'daily', 'conversation', or custom
    category TEXT NOT NULL DEFAULT 'conversation',
    
    -- ISO 8601 timestamp of when the memory was created
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Optional session ID for grouping related memories
    session_id TEXT,
    
    -- Embedding vector (1536 dimensions for OpenAI text-embedding-ada-002)
    -- Adjust dimension count based on your embedding model:
    -- - OpenAI ada-002: 1536
    -- - OpenAI text-embedding-3-small: 1536
    -- - OpenAI text-embedding-3-large: 3072
    -- - Cohere embed-multilingual: 1024
    embedding vector(1536),
    
    -- Similarity score (populated during search queries)
    score FLOAT,
    
    -- JSON metadata for flexible additional data
    metadata JSONB DEFAULT '{}'::jsonb,
    
    -- Created at timestamp for auditing
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Updated at timestamp for auditing
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. Create indexes for efficient querying

-- Index on key for fast lookups
CREATE INDEX IF NOT EXISTS idx_memory_entries_key ON memory_entries(key);

-- Index on category for filtering
CREATE INDEX IF NOT EXISTS idx_memory_entries_category ON memory_entries(category);

-- Index on session_id for session-scoped queries
CREATE INDEX IF NOT EXISTS idx_memory_entries_session_id ON memory_entries(session_id);

-- Index on timestamp for time-based queries
CREATE INDEX IF NOT EXISTS idx_memory_entries_timestamp ON memory_entries(timestamp DESC);

-- 4. Create HNSW index for vector similarity search
-- HNSW (Hierarchical Navigable Small World) is optimal for high-performance
-- approximate nearest neighbor search. It's faster than IVFFlat for most use cases.
-- 
-- Parameters:
-- - m: Number of bi-directional links created for each node (default: 16)
--   Higher values = better recall but more memory usage
-- - ef_construction: Size of dynamic candidate list during index construction (default: 64)
--   Higher values = better index quality but slower construction
CREATE INDEX IF NOT EXISTS idx_memory_entries_embedding_hnsw 
ON memory_entries 
USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);

-- Alternative: IVFFlat index (better for larger datasets > 1M rows)
-- Uncomment below if you prefer IVFFlat over HNSW
-- 
-- CREATE INDEX IF NOT EXISTS idx_memory_entries_embedding_ivfflat 
-- ON memory_entries 
-- USING ivfflat (embedding vector_cosine_ops)
-- WITH (lists = 100);

-- 5. Create a function to update the updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 6. Create trigger to auto-update updated_at
CREATE TRIGGER update_memory_entries_updated_at
    BEFORE UPDATE ON memory_entries
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- 7. Create a function for semantic similarity search
-- This function performs cosine similarity search on embeddings
CREATE OR REPLACE FUNCTION search_memories(
    query_embedding vector(1536),
    match_threshold FLOAT DEFAULT 0.7,
    match_count INT DEFAULT 10,
    filter_category TEXT DEFAULT NULL,
    filter_session_id TEXT DEFAULT NULL
)
RETURNS TABLE (
    id UUID,
    key TEXT,
    content TEXT,
    category TEXT,
    timestamp TIMESTAMPTZ,
    session_id TEXT,
    score FLOAT,
    metadata JSONB
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        me.id,
        me.key,
        me.content,
        me.category,
        me.timestamp,
        me.session_id,
        1 - (me.embedding <=> query_embedding) AS score,
        me.metadata
    FROM memory_entries me
    WHERE 
        (filter_category IS NULL OR me.category = filter_category)
        AND (filter_session_id IS NULL OR me.session_id = filter_session_id)
        AND me.embedding IS NOT NULL
    ORDER BY me.embedding <=> query_embedding
    LIMIT match_count;
END;
$$ LANGUAGE plpgsql;

-- 8. Create a function for hybrid search (keyword + semantic)
-- Combines full-text search with vector similarity
CREATE OR REPLACE FUNCTION hybrid_search_memories(
    query_text TEXT,
    query_embedding vector(1536),
    match_count INT DEFAULT 10,
    semantic_weight FLOAT DEFAULT 0.7,
    filter_category TEXT DEFAULT NULL,
    filter_session_id TEXT DEFAULT NULL
)
RETURNS TABLE (
    id UUID,
    key TEXT,
    content TEXT,
    category TEXT,
    timestamp TIMESTAMPTZ,
    session_id TEXT,
    score FLOAT,
    metadata JSONB
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        me.id,
        me.key,
        me.content,
        me.category,
        me.timestamp,
        me.session_id,
        -- Combine semantic and keyword scores
        (semantic_weight * (1 - (me.embedding <=> query_embedding))) +
        ((1 - semantic_weight) * 
         ts_rank_cd(
             to_tsvector('english', me.content),
             plainto_tsquery('english', query_text)
         )
        ) AS score,
        me.metadata
    FROM memory_entries me
    WHERE 
        (filter_category IS NULL OR me.category = filter_category)
        AND (filter_session_id IS NULL OR me.session_id = filter_session_id)
        AND me.embedding IS NOT NULL
    ORDER BY score DESC
    LIMIT match_count;
END;
$$ LANGUAGE plpgsql;

-- 9. Create a function to insert or update memory with embedding
CREATE OR REPLACE FUNCTION upsert_memory(
    p_key TEXT,
    p_content TEXT,
    p_category TEXT DEFAULT 'conversation',
    p_session_id TEXT DEFAULT NULL,
    p_embedding vector(1536) DEFAULT NULL,
    p_metadata JSONB DEFAULT '{}'::jsonb
)
RETURNS UUID AS $$
DECLARE
    v_id UUID;
BEGIN
    INSERT INTO memory_entries (key, content, category, session_id, embedding, metadata)
    VALUES (p_key, p_content, p_category, p_session_id, p_embedding, p_metadata)
    ON CONFLICT (key) 
    DO UPDATE SET 
        content = EXCLUDED.content,
        category = EXCLUDED.category,
        session_id = EXCLUDED.session_id,
        embedding = EXCLUDED.embedding,
        metadata = EXCLUDED.metadata,
        updated_at = NOW()
    RETURNING id INTO v_id;
    
    RETURN v_id;
END;
$$ LANGUAGE plpgsql;

-- 10. Create a function to delete memory by key
CREATE OR REPLACE FUNCTION delete_memory(p_key TEXT)
RETURNS BOOLEAN AS $$
DECLARE
    v_deleted INT;
BEGIN
    DELETE FROM memory_entries WHERE key = p_key;
    GET DIAGNOSTICS v_deleted = ROW_COUNT;
    RETURN v_deleted > 0;
END;
$$ LANGUAGE plpgsql;

-- 11. Create a function to count memories
CREATE OR REPLACE FUNCTION count_memories(
    filter_category TEXT DEFAULT NULL,
    filter_session_id TEXT DEFAULT NULL
)
RETURNS INT AS $$
DECLARE
    v_count INT;
BEGIN
    SELECT COUNT(*) INTO v_count
    FROM memory_entries
    WHERE 
        (filter_category IS NULL OR category = filter_category)
        AND (filter_session_id IS NULL OR session_id = filter_session_id);
    
    RETURN v_count;
END;
$$ LANGUAGE plpgsql;

-- 12. Grant necessary permissions (adjust 'authenticated' role as needed)
GRANT SELECT, INSERT, UPDATE, DELETE ON memory_entries TO authenticated;
GRANT EXECUTE ON FUNCTION search_memories TO authenticated;
GRANT EXECUTE ON FUNCTION hybrid_search_memories TO authenticated;
GRANT EXECUTE ON FUNCTION upsert_memory TO authenticated;
GRANT EXECUTE ON FUNCTION delete_memory TO authenticated;
GRANT EXECUTE ON FUNCTION count_memories TO authenticated;

-- ============================================================================
-- SAMPLE DATA FOR TESTING (Optional - uncomment to insert test data)
-- ============================================================================
-- INSERT INTO memory_entries (key, content, category, session_id) VALUES
-- ('user_name', 'The user prefers to be called Alex', 'core', NULL),
-- ('project_context', 'Working on a Go port of ZeroClaw for Vercel', 'daily', 'session-001'),
-- ('conversation_summary', 'Discussed architecture for serverless deployment', 'conversation', 'session-001');

-- ============================================================================
-- VERIFICATION QUERIES (Run these to verify setup)
-- ============================================================================
-- SELECT * FROM memory_entries LIMIT 5;
-- SELECT count_memories();
-- SELECT * FROM search_memories('[0.1, 0.2, ...]'::vector(1536), 0.5, 5);

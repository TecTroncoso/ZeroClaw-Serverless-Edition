-- ============================================================================
-- ZeroClaw Go - Hybrid Search SQL Functions (RRF + Enhanced)
-- ============================================================================
-- This script adds advanced hybrid search capabilities using Reciprocal Rank Fusion (RRF)
-- to combine vector similarity with full-text search (FTS) in PostgreSQL.
--
-- Execute this script in the Supabase SQL Editor to add the enhanced functions.
-- ============================================================================

-- ============================================================================
-- ENHANCED HYBRID SEARCH WITH RECIPROCAL RANK FUSION (RRF)
-- ============================================================================

-- Drop existing function if exists
DROP FUNCTION IF EXISTS hybrid_search_memories CASCADE;

-- Create enhanced hybrid search using RRF (Reciprocal Rank Fusion)
-- RRF is more robust than simple weighted sum as it doesn't require score normalization
CREATE OR REPLACE FUNCTION hybrid_search_memories(
    query_embedding vector(1536),
    query_text TEXT,
    match_count INT DEFAULT 10,
    session_id TEXT DEFAULT NULL,
    semantic_weight FLOAT DEFAULT 0.5,
    fts_weight FLOAT DEFAULT 0.3,
    rrf_k INT DEFAULT 60
)
RETURNS TABLE (
    id UUID,
    key TEXT,
    content TEXT,
    category TEXT,
    timestamp TIMESTAMPTZ,
    session_id TEXT,
    score FLOAT,
    metadata JSONB,
    semantic_score FLOAT,
    fts_score FLOAT,
    rrf_score FLOAT
) AS $$
BEGIN
    RETURN QUERY
    WITH 
    -- Semantic search results (vector similarity)
    semantic_results AS (
        SELECT 
            me.id,
            1 - (me.embedding <=> query_embedding) AS semantic_score,
            ROW_NUMBER() OVER (ORDER BY me.embedding <=> query_embedding) AS semantic_rank
        FROM memory_entries me
        WHERE me.embedding IS NOT NULL
          AND (session_id IS NULL OR me.session_id = session_id)
    ),
    -- Full-text search results (FTS with websearch_to_tsquery)
    fts_results AS (
        SELECT 
            me.id,
            ts_rank_cd(
                to_tsvector('english', me.content),
                websearch_to_tsquery('english', query_text)
            ) AS fts_score,
            ROW_NUMBER() OVER (ORDER BY 
                ts_rank_cd(
                    to_tsvector('english', me.content),
                    websearch_to_tsquery('english', query_text)
                ) DESC
            ) AS fts_rank
        FROM memory_entries me
        WHERE to_tsvector('english', me.content) @@ websearch_to_tsquery('english', query_text)
          AND (session_id IS NULL OR me.session_id = session_id)
    ),
    -- Combine results using RRF
    combined AS (
        SELECT 
            COALESCE(sr.id, fr.id) AS id,
            COALESCE(sr.semantic_score, 0) AS semantic_score,
            COALESCE(fr.fts_score, 0) AS fts_score,
            COALESCE(sr.semantic_rank, 1000) AS semantic_rank,
            COALESCE(fr.fts_rank, 1000) AS fts_rank
        FROM semantic_results sr
        FULL OUTER JOIN fts_results fr ON sr.id = fr.id
    ),
    -- Calculate RRF score
    rrf_calc AS (
        SELECT 
            c.id,
            c.semantic_score,
            c.fts_score,
            -- RRF formula: sum(1 / (k + rank)) for each ranking
            (
                COALESCE(semantic_weight / (rrf_k + c.semantic_rank), 0) +
                COALESCE(fts_weight / (rrf_k + c.fts_rank), 0) +
                COALESCE((1 - semantic_weight - fts_weight) / (rrf_k + COALESCE(c.semantic_rank, c.fts_rank)), 0)
            ) AS rrf_score
        FROM combined c
    )
    -- Final select with all scores
    SELECT 
        me.id,
        me.key,
        me.content,
        me.category,
        me.timestamp,
        me.session_id,
        rc.rrf_score AS score,
        me.metadata,
        rc.semantic_score,
        rc.fts_score,
        rc.rrf_score
    FROM rrf_calc rc
    JOIN memory_entries me ON rc.id = me.id
    ORDER BY rc.rrf_score DESC
    LIMIT match_count;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- SIMPLIFIED HYBRID SEARCH (Weighted Sum - Original Approach)
-- ============================================================================

-- Keep for backward compatibility
CREATE OR REPLACE FUNCTION hybrid_search_memories_weighted(
    query_embedding vector(1536),
    query_text TEXT,
    match_count INT DEFAULT 10,
    session_id TEXT DEFAULT NULL,
    semantic_weight FLOAT DEFAULT 0.7,
    text_weight FLOAT DEFAULT 0.3
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
DECLARE
    max_fts_score FLOAT;
BEGIN
    -- Get max FTS score for normalization
    SELECT COALESCE(MAX(ts_rank_cd(
        to_tsvector('english', me.content),
        websearch_to_tsquery('english', query_text)
    )), 1) INTO max_fts_score
    FROM memory_entries me
    WHERE to_tsvector('english', me.content) @@ websearch_to_tsquery('english', query_text);

    RETURN QUERY
    SELECT 
        me.id,
        me.key,
        me.content,
        me.category,
        me.timestamp,
        me.session_id,
        (
            -- Semantic component (0-1 based on cosine similarity)
            semantic_weight * (1 - (me.embedding <=> query_embedding)) +
            -- Text component (normalized 0-1)
            text_weight * COALESCE(
                ts_rank_cd(
                    to_tsvector('english', me.content),
                    websearch_to_tsquery('english', query_text)
                ) / NULLIF(max_fts_score, 0),
                0
            )
        ) AS score,
        me.metadata
    FROM memory_entries me
    WHERE 
        me.embedding IS NOT NULL
        AND (session_id IS NULL OR me.session_id = session_id)
        AND (
            me.embedding IS NOT NULL
            OR to_tsvector('english', me.content) @@ websearch_to_tsquery('english', query_text)
        )
    ORDER BY score DESC
    LIMIT match_count;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- KEYWORD-ONLY SEARCH (Pure FTS)
-- ============================================================================

CREATE OR REPLACE FUNCTION search_memories_fts(
    query_text TEXT,
    match_count INT DEFAULT 10,
    session_id TEXT DEFAULT NULL,
    category TEXT DEFAULT NULL
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
        ts_rank_cd(
            to_tsvector('english', me.content),
            websearch_to_tsquery('english', query_text)
        ) AS score,
        me.metadata
    FROM memory_entries me
    WHERE 
        to_tsvector('english', me.content) @@ websearch_to_tsquery('english', query_text)
        AND (session_id IS NULL OR me.session_id = session_id)
        AND (category IS NULL OR me.category = category)
    ORDER BY score DESC
    LIMIT match_count;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- ADD FTS INDEX FOR BETTER PERFORMANCE
-- ============================================================================

-- Create GIN index on content for full-text search (if not exists)
CREATE INDEX IF NOT EXISTS idx_memory_entries_content_fts 
ON memory_entries USING GIN (to_tsvector('english', content));

-- Create composite index for category + session filtering
CREATE INDEX IF NOT EXISTS idx_memory_entries_category_session 
ON memory_entries (category, session_id);

-- ============================================================================
-- VERIFICATION
-- ============================================================================

SELECT 'Hybrid search functions created successfully' AS status;

-- Test the function (will return empty if no memories exist)
SELECT * FROM hybrid_search_memories(
    '[0.1,0.2,0.3]'::vector(1536),  -- sample embedding
    'test query',
    10,
    NULL,
    0.5,  -- semantic weight
    0.3,  -- fts weight
    60    -- rrf k parameter
) LIMIT 5;

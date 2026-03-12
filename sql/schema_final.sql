-- ============================================================================
-- ZeroClaw Go - Supabase/pgvector Schema (DEFINITIVE VERSION)
-- ============================================================================
-- Este es el ÚNICO archivo SQL necesario para configurar la base de datos.
-- Ejecuta este script completo en el SQL Editor de Supabase.
-- ============================================================================

-- ============================================================================
-- PASO 1: LIMPIEZA DE FUNCIONES DUPLICADAS
-- ============================================================================
-- Eliminamos TODAS las versiones existentes para evitar errores de ambigüedad

DROP FUNCTION IF EXISTS hybrid_search_memories(vector(2048), text, int, text, float, float, int);
DROP FUNCTION IF EXISTS hybrid_search_memories(vector(2048), text, int, text, float, float);
DROP FUNCTION IF EXISTS hybrid_search_memories(vector(2048), text, int, text, float);
DROP FUNCTION IF EXISTS hybrid_search_memories(vector(2048), text, int, text);
DROP FUNCTION IF EXISTS hybrid_search_memories(text, vector(2048), int, float, text, text);
DROP FUNCTION IF EXISTS hybrid_search_memories(text, vector(2048), int, float, text);
DROP FUNCTION IF EXISTS hybrid_search_memories(vector, text, int, text, float, float, int);
DROP FUNCTION IF EXISTS hybrid_search_memories_weighted(vector(2048), text, int, text, float, float);
DROP FUNCTION IF EXISTS hybrid_search_memories_weighted(vector(2048), text, int, text, float);
DROP FUNCTION IF EXISTS search_memories(vector(2048), float, int, text, text);
DROP FUNCTION IF EXISTS search_memories_fts(text, int, text, text);
DROP FUNCTION IF EXISTS count_memories(text, text);
DROP FUNCTION IF EXISTS upsert_memory(text, text, text, text, vector(2048), jsonb);
DROP FUNCTION IF EXISTS delete_memory(text);

-- ============================================================================
-- PASO 2: EXTENSIÓN PGVECTOR
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS vector;

-- ============================================================================
-- PASO 3: TABLA memory_entries
-- ============================================================================

CREATE TABLE IF NOT EXISTS memory_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key TEXT NOT NULL UNIQUE,
    content TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'conversation',
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    session_id TEXT,
    embedding vector(2048),
    score FLOAT,
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================================
-- PASO 4: ÍNDICES
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_memory_entries_key ON memory_entries(key);
CREATE INDEX IF NOT EXISTS idx_memory_entries_category ON memory_entries(category);
CREATE INDEX IF NOT EXISTS idx_memory_entries_session_id ON memory_entries(session_id);
CREATE INDEX IF NOT EXISTS idx_memory_entries_timestamp ON memory_entries(timestamp DESC);

-- Índice HNSW para búsqueda vectorial (similitud semántica)
CREATE INDEX IF NOT EXISTS idx_memory_entries_embedding_hnsw
ON memory_entries
USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);

-- Índice GIN para Full-Text Search
CREATE INDEX IF NOT EXISTS idx_memory_entries_content_fts
ON memory_entries USING GIN (to_tsvector('english', content));

-- ============================================================================
-- PASO 5: TRIGGER PARA updated_at
-- ============================================================================

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS update_memory_entries_updated_at ON memory_entries;
CREATE TRIGGER update_memory_entries_updated_at
BEFORE UPDATE ON memory_entries
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

-- ============================================================================
-- PASO 6: FUNCIÓN search_memories (búsqueda semántica pura)
-- ============================================================================
-- Usada por: RecallWithEmbedding() en Go
-- Parámetros: query_embedding, match_threshold, match_count, filter_category, filter_session_id

CREATE OR REPLACE FUNCTION search_memories(
    query_embedding vector(2048),
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
    "timestamp" TIMESTAMPTZ,
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

-- ============================================================================
-- PASO 7: FUNCIÓN hybrid_search_memories (RRF)
-- ============================================================================
-- Usada por: RecallHybrid() y RecallHybridWithConfig() en Go
-- Firma exacta requerida por Go (7 parámetros):
--   $1: query_embedding vector(2048)
--   $2: query_text TEXT
--   $3: match_count INT
--   $4: p_session_id TEXT
--   $5: semantic_weight FLOAT
--   $6: fts_weight FLOAT
--   $7: rrf_k INT

CREATE OR REPLACE FUNCTION hybrid_search_memories(
    query_embedding vector(2048),
    query_text TEXT,
    match_count INT DEFAULT 10,
    p_session_id TEXT DEFAULT NULL,
    semantic_weight FLOAT DEFAULT 0.5,
    fts_weight FLOAT DEFAULT 0.3,
    rrf_k INT DEFAULT 60
)
RETURNS TABLE (
    id UUID,
    key TEXT,
    content TEXT,
    category TEXT,
    "timestamp" TIMESTAMPTZ,
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
    semantic_results AS (
        SELECT
            me.id,
            1 - (me.embedding <=> query_embedding) AS semantic_score,
            ROW_NUMBER() OVER (ORDER BY me.embedding <=> query_embedding) AS semantic_rank
        FROM memory_entries me
        WHERE me.embedding IS NOT NULL
        AND (p_session_id IS NULL OR me.session_id = p_session_id)
    ),
    fts_results AS (
        SELECT
            me.id,
            ts_rank_cd(
                to_tsvector('english', me.content),
                websearch_to_tsquery('english', query_text)
            ) AS fts_score,
            ROW_NUMBER() OVER (ORDER BY ts_rank_cd(
                to_tsvector('english', me.content),
                websearch_to_tsquery('english', query_text)
            ) DESC) AS fts_rank
        FROM memory_entries me
        WHERE to_tsvector('english', me.content) @@ websearch_to_tsquery('english', query_text)
        AND (p_session_id IS NULL OR me.session_id = p_session_id)
    ),
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
    rrf_calc AS (
        SELECT
            c.id,
            c.semantic_score,
            c.fts_score,
            (
                COALESCE(semantic_weight / (rrf_k + c.semantic_rank), 0) +
                COALESCE(fts_weight / (rrf_k + c.fts_rank), 0) +
                COALESCE((1 - semantic_weight - fts_weight) / (rrf_k + COALESCE(c.semantic_rank, c.fts_rank)), 0)
            ) AS rrf_score
        FROM combined c
    )
    SELECT
        me.id,
        me.key,
        me.content,
        me.category,
        me.timestamp,
        me.session_id,
        rc.rrf_score::FLOAT AS score,
        me.metadata,
        rc.semantic_score::FLOAT,
        rc.fts_score::FLOAT,
        rc.rrf_score::FLOAT
    FROM rrf_calc rc
    JOIN memory_entries me ON rc.id = me.id
    ORDER BY rc.rrf_score DESC
    LIMIT match_count;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- PASO 8: FUNCIÓN search_memories_fts (Full-Text Search pura)
-- ============================================================================
-- Usada por: SearchFTS() en Go
-- Parámetros: query_text, match_count, p_session_id, p_category

CREATE OR REPLACE FUNCTION search_memories_fts(
    query_text TEXT,
    match_count INT DEFAULT 10,
    p_session_id TEXT DEFAULT NULL,
    p_category TEXT DEFAULT NULL
)
RETURNS TABLE (
    id UUID,
    key TEXT,
    content TEXT,
    category TEXT,
    "timestamp" TIMESTAMPTZ,
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
        )::FLOAT AS score,
        me.metadata
    FROM memory_entries me
    WHERE to_tsvector('english', me.content) @@ websearch_to_tsquery('english', query_text)
    AND (p_session_id IS NULL OR me.session_id = p_session_id)
    AND (p_category IS NULL OR me.category = p_category)
    ORDER BY score DESC
    LIMIT match_count;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- PASO 9: FUNCIÓN count_memories
-- ============================================================================
-- Usada por: Count() en Go

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

-- ============================================================================
-- PASO 10: PERMISOS
-- ============================================================================

GRANT SELECT, INSERT, UPDATE, DELETE ON memory_entries TO authenticated;
GRANT EXECUTE ON FUNCTION search_memories TO authenticated;
GRANT EXECUTE ON FUNCTION hybrid_search_memories TO authenticated;
GRANT EXECUTE ON FUNCTION search_memories_fts TO authenticated;
GRANT EXECUTE ON FUNCTION count_memories TO authenticated;

-- ============================================================================
-- VERIFICACIÓN FINAL
-- ============================================================================

SELECT '✅ Schema ZeroClaw Go instalado correctamente' AS status;
SELECT 'Tabla memory_entries creada con ' || COUNT(*) || ' registros' AS info FROM memory_entries;

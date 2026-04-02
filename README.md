<p align="center">
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Vercel-Serverless-000000?style=for-the-badge&logo=vercel&logoColor=white" alt="Vercel">
  <img src="https://img.shields.io/badge/Supabase-pgvector-3FCF8E?style=for-the-badge&logo=supabase&logoColor=white" alt="Supabase">
  <img src="https://img.shields.io/badge/Architecture-Serverless--First-FF6F00?style=for-the-badge&logo=serverless&logoColor=white" alt="Serverless">
  <img src="https://img.shields.io/badge/License-MIT-blue?style=for-the-badge" alt="License">
</p>

<h1 align="center">⚡ ZeroClaw Go — Serverless Edition</h1>

<p align="center">
  <strong>Framework de Agentes IA Autónomos ultra-ligero, diseñado exclusivamente para Serverless.</strong><br>
  <sub>100% Go · Cero dependencias pesadas · Cero costos inactivo · Despliega en 5 minutos.</sub>
</p>

<p align="center">
  <code>v1.0.0 — Code Freeze</code>
</p>

---

## 🧠 La Arquitectura (Por qué es increíble)

ZeroClaw no es otro wrapper de la API de OpenAI. Es un **framework completo de agentes** que piensa, recuerda, busca en internet, envía emails, agenda citas — y lo hace todo dentro de **una sola función serverless de 60 segundos**.

### ☁️ Serverless-First

| Aspecto | Detalle |
|---|---|
| **Latencia inactivo** | **$0 / 0ms** — la función solo existe cuando un mensaje llega |
| **Cold start** | ~200ms gracias al binario compilado de Go (sin intérprete, sin VM) |
| **Max Duration** | 60s (Vercel Hobby tier Go) — suficiente para 5 iteraciones de tool-calling |
| **RAM** | 1024MB — protegido con `LimitReader` de 512KB en todas las lecturas HTTP |
| **Dependencias** | **1 sola** (`lib/pq` para PostgreSQL). Zero bloat |

### 🧬 Cerebro Dividido (Split Providers)

ZeroClaw separa el **razonamiento** de la **vectorización** en dos providers completamente independientes:

```
┌─────────────────────────────────────────────────┐
│                   CHAT PROVIDER                  │
│  Cerebras · Groq · OpenAI · xAI · OpenRouter    │
│  → Modelo rápido para razonar (ej. Qwen, Llama) │
│  → OPENAI_API_KEY + OPENAI_BASE_URL             │
├─────────────────────────────────────────────────┤
│               EMBEDDING PROVIDER                 │
│  OpenRouter · OpenAI · Fireworks · Together      │
│  → Modelo denso para vectorizar (2048 dims)      │
│  → EMBEDDING_API_KEY + EMBEDDING_BASE_URL        │
└─────────────────────────────────────────────────┘
```

**¿Por qué?** Porque el modelo más rápido del mundo para chatear (Cerebras @ 2100 tok/s) no soporta embeddings. Y el mejor modelo gratuito de embeddings (NVIDIA Nemotron 2048d vía OpenRouter) no genera texto. Separándolos, obtienes **velocidad máxima + embeddings de alta dimensionalidad** sin compromiso.

### 🔍 RAG Híbrido (pgvector + FTS con RRF)

La memoria no es un simple `SELECT * WHERE content LIKE '%query%'`. Es un sistema de recuperación de triple vía:

```
Query del usuario
       │
       ├──→  🧲 Vector Search (cosine similarity, 2048 dims)
       │         └─→ Captura sinónimos, paráfrasis, conceptos similares
       │
       ├──→  📝 Full-Text Search (tsvector + websearch_to_tsquery)
       │         └─→ Captura keywords exactos, acrónimos, nombres propios
       │
       └──→  🔀 Reciprocal Rank Fusion (RRF, k=60)
                 └─→ Combina ambos rankings en un score unificado
```

Los textos largos se fragmentan automáticamente con **Smart Chunking** (500 chars, 50 overlap) para maximizar la precisión del RAG.

### ⚡ Alto Rendimiento

- **Ejecución paralela de tools** — Si el LLM pide `web_search` + `core_memory_save` simultáneamente, se ejecutan en goroutines concurrentes con `sync.WaitGroup`. Tiempo total = `max(tool_a, tool_b)`, no `sum`.
- **Streaming con Anti-Flood** — Las respuestas se envían progresivamente editando el mensaje original en Telegram/Discord. El throttle adaptativo empieza en 1.5s y escala hasta 4s para no exceder los rate limits de las plataformas.
- **Stream Fallback** — Si la conexión SSE se rompe mid-stream (TCP RST, 429), el sistema cae automáticamente a una petición non-streaming con retry integrado (backoff lineal, 2 reintentos).
- **Embedding reuse** — El embedding generado para buscar en memoria se reutiliza para almacenar el mensaje del usuario, ahorrando 1 API call y ~200ms por request.

---

## 🛠️ Catálogo de Herramientas (Tools)

El agente invoca herramientas de forma autónoma durante su ciclo de razonamiento. Todas están blindadas con `context.WithTimeout` para entornos serverless.

| Herramienta | Descripción | Timeout |
|---|---|---|
| 🔎 `web_search` | Busca en la web vía DuckDuckGo (gratis), Tavily o Brave Search. Máx 2 resultados para optimizar tokens. | 5s |
| 🌐 `web_fetch` | Extrae texto limpio de URLs. Soporta **multi-URL fallback**: si la primera falla, prueba la siguiente. Sanitiza HTML eliminando scripts, nav, footer, ads. Capped a 512KB. | 5s |
| 📡 `http_request` | Peticiones GET/POST a cualquier API REST con headers customizables. Auto-detecta JSON. Respuesta capped a 512KB. | 8s |
| 📧 `send_email` | Envía emails vía SMTP nativo de Go (`net/smtp`). Sin SDKs. Soporta Gmail con App Passwords. | 10s |
| 📅 `schedule_calendar_event` | Agenda eventos en Google Calendar disparando un webhook de Make.com. Zero SDKs de Google Cloud. | 5s |
| 💾 `core_memory_save` | Guarda hechos importantes del usuario (nombre, preferencias, fechas) en memoria de largo plazo con embeddings vectoriales. | 10s |

> **Nota**: `core_memory_save` se inyecta dinámicamente solo cuando la conexión a Supabase está activa. Si la BD no está disponible, el agente funciona sin memoria (graceful degradation).

---

## ⚙️ Guía de Despliegue (Paso a Paso)

### Paso 1 — Configurar Supabase

1. Crea un nuevo proyecto en [supabase.com](https://supabase.com).
2. Abre el **SQL Editor** y ejecuta el contenido completo de [`sql/schema_final.sql`](sql/schema_final.sql):

<details>
<summary>📋 Click para ver el SQL completo (schema_final.sql)</summary>

```sql
-- Extensión pgvector
CREATE EXTENSION IF NOT EXISTS vector;

-- Tabla principal
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

-- Índices
CREATE INDEX IF NOT EXISTS idx_memory_entries_key ON memory_entries(key);
CREATE INDEX IF NOT EXISTS idx_memory_entries_category ON memory_entries(category);
CREATE INDEX IF NOT EXISTS idx_memory_entries_session_id ON memory_entries(session_id);
CREATE INDEX IF NOT EXISTS idx_memory_entries_timestamp ON memory_entries(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_memory_entries_content_fts
ON memory_entries USING GIN (to_tsvector('english', content));

-- Funciones: search_memories, hybrid_search_memories (RRF),
-- search_memories_fts, count_memories
-- (ver archivo completo para las definiciones)
```

</details>

3. Obtén tu **Connection String** en: `Project Settings > Database > Connection String > URI`
   - Usa **Transaction Mode** (puerto `6543`)
   - Formato: `postgresql://postgres.[ref]:[password]@aws-0-[region].pooler.supabase.com:6543/postgres`

### Paso 2 — Desplegar en Vercel

1. Haz Push de tu repositorio a GitHub.
2. Importa el proyecto en [vercel.com](https://vercel.com) → **New Project**.
3. **No requiere configuración de build** — Vercel detecta Go automáticamente.
4. Configura las [Variables de Entorno](#-variables-de-entorno) en `Project Settings > Environment Variables`.
5. Deploya. Tu endpoint será: `https://<TU-DOMINIO>.vercel.app/api/webhook`

> El archivo `vercel.json` ya configura `maxDuration: 60` para la función.

### Paso 3 — Configurar Webhooks de Canales

**La URL base es siempre:** `https://<TU-DOMINIO>.vercel.app/api/webhook`

#### 🤖 Telegram

Abre esta URL en tu navegador (una vez):

```
https://api.telegram.org/bot<TU_TOKEN>/setWebhook?url=https://<TU-DOMINIO>.vercel.app/api/webhook?channel=telegram
```

#### 🎮 Discord

1. En el [Discord Developer Portal](https://discord.com/developers), ve a tu App > **General Information** y copia el `APPLICATION ID` y `PUBLIC KEY`.
2. En **Installation** > configura la URL de Interactions Endpoint:
   ```
   https://<TU-DOMINIO>.vercel.app/api/webhook?channel=discord
   ```
3. **Registra el comando `/chat`** visitando esta URL en tu navegador (una sola vez):
   ```
   https://<TU-DOMINIO>.vercel.app/api/webhook?setup_discord=true
   ```
   Verás un mensaje `✅` confirmando el registro.

---

## 🔑 Variables de Entorno

### Proveedores de IA (Chat)

| Variable | Requerida | Ejemplo | Descripción |
|---|---|---|---|
| `OPENAI_API_KEY` | ✅ | `sk-...` | API Key del proveedor LLM para razonamiento |
| `OPENAI_MODEL` | ❌ | `gpt-4o-mini` | Modelo de chat (default: `gpt-4o-mini`) |
| `OPENAI_BASE_URL` | ❌ | `https://api.cerebras.ai/v1` | Base URL del provider (default: OpenAI) |

### Proveedores de IA (Embeddings)

| Variable | Requerida | Ejemplo | Descripción |
|---|---|---|---|
| `EMBEDDING_API_KEY` | ❌ | `sk-or-...` | API Key separada para embeddings. Si no se configura, usa el Chat Provider |
| `EMBEDDING_MODEL` | ❌ | `nvidia/llama-nemotron-embed-vl-1b-v2:free` | Modelo de embeddings (default: `text-embedding-3-small`) |
| `EMBEDDING_BASE_URL` | ❌ | `https://openrouter.ai/api/v1` | Base URL del provider de embeddings |

### Base de Datos

| Variable | Requerida | Ejemplo | Descripción |
|---|---|---|---|
| `SUPABASE_DB_URL` | ✅ | `postgresql://...:6543/postgres?sslmode=require` | Connection string de Supabase (Transaction Mode) |

### Canales de Comunicación

| Variable | Requerida | Ejemplo | Descripción |
|---|---|---|---|
| `TELEGRAM_BOT_TOKEN` | ❌ | `123456:ABC-DEF...` | Token del bot de Telegram (@BotFather) |
| `DISCORD_BOT_TOKEN` | ❌ | `MTIz...` | Token del bot de Discord |
| `DISCORD_APP_ID` | ❌ | `1122334455...` | Application ID de Discord |
| `DISCORD_PUBLIC_KEY` | ❌ | `abcdef123...` | Public Key para verificar firmas Ed25519 |

### Email (SMTP)

| Variable | Requerida | Ejemplo | Descripción |
|---|---|---|---|
| `SMTP_USER` | ❌ | `tu@gmail.com` | Correo emisor |
| `SMTP_PASSWORD` | ❌ | `abcdefghijklmnop` | App Password de Google (16 chars) |
| `SMTP_HOST` | ❌ | `smtp.gmail.com` | Host SMTP (default: `smtp.gmail.com`) |
| `SMTP_PORT` | ❌ | `587` | Puerto SMTP (default: `587`) |

### Integraciones

| Variable | Requerida | Ejemplo | Descripción |
|---|---|---|---|
| `MAKE_CALENDAR_WEBHOOK` | ❌ | `https://hook.us1.make.com/...` | Webhook de Make.com para Google Calendar |

### Avanzado

| Variable | Requerida | Ejemplo | Descripción |
|---|---|---|---|
| `SYSTEM_PROMPT` | ❌ | `Eres un asistente...` | Sobrescribe el system prompt completo del agente |
| `MASTER_SESSION_ID` | ❌ | `mi_bot_global` | Unifica la memoria entre todos los canales |
| `AIEOS_PROFILE` | ❌ | `{"identity":{...}}` | JSON personalizado de identidad AIEOS v1.1 |
| `SEARCH_API_KEY` | ❌ | `tvly-...` | API Key para Tavily o Brave Search |
| `SEARCH_PROVIDER` | ❌ | `duckduckgo` | Provider de búsqueda: `duckduckgo`, `tavily`, `brave` |

---

## 📁 Estructura del Proyecto

```
zeroclaw-go/
├── api/
│   └── webhook.go              # Entrypoint Serverless — Vercel Function
├── pkg/
│   ├── agent/
│   │   └── loop.go             # Bucle de razonamiento del agente (max 5 iteraciones)
│   ├── channels/
│   │   ├── common.go           # Tipos compartidos (IncomingMessage, Channel interface)
│   │   ├── telegram.go         # Adaptador Telegram (Streaming via EditMessage)
│   │   ├── discord.go          # Adaptador Discord (Ed25519 verify, Background Worker)
│   │   ├── slack.go            # Adaptador Slack (URL verification, Events API)
│   │   └── whatsapp.go         # Adaptador WhatsApp (Cloud API)
│   ├── core/
│   │   ├── interfaces.go       # Contratos: Memory, Provider, Tool, Channel
│   │   ├── identity.go         # System Prompt Builder con inyección temporal
│   │   └── aieos.go            # Motor de identidad AIEOS v1.1
│   ├── memory/
│   │   ├── supabase.go         # Backend RAG: pgvector + FTS + RRF híbrido
│   │   └── chunker.go          # Smart Chunking (500 chars, 50 overlap)
│   ├── providers/
│   │   └── openai.go           # Cliente universal OpenAI-compatible (Stream + Fallback)
│   └── tools/
│       ├── websearch.go        # Web Search (DDG/Tavily/Brave)
│       ├── webfetch.go         # Web Fetch (multi-URL fallback, HTML sanitizer)
│       ├── httprequest.go      # HTTP Request (GET/POST genérico)
│       ├── sendemail.go        # Email via net/smtp
│       ├── makecalendar.go     # Calendar via Make.com webhook
│       └── memorystore.go      # Core Memory Save (directed by AI)
├── sql/
│   └── schema_final.sql        # Schema PostgreSQL definitivo (2048 dims)
├── go.mod                      # 1 dependencia: lib/pq
└── vercel.json                 # maxDuration: 60
```

---

<p align="center">
  <sub>Built with ❤️ and Go for the Serverless Future</sub><br>
  <sub><strong>ZeroClaw Go — Serverless Edition v1.0.0</strong></sub>
</p>

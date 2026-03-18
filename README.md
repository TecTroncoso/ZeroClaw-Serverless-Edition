# 🚀 ZeroClaw Go (Serverless Edition)

<p align="center">
<img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go" alt="Go Version">
<img src="https://img.shields.io/badge/Platform-Vercel-000000?style=for-the-badge&logo=vercel" alt="Platform">
<img src="https://img.shields.io/badge/Database-Supabase-3FCF8E?style=for-the-badge&logo=supabase" alt="Database">
<img src="https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge" alt="License">
</p>

**ZeroClaw Go** es un framework de agentes de IA autónomos ultra-ligero, diseñado **exclusivamente** para entornos Serverless. Construido sobre cimientos sólidos de Go y desplegado en Vercel, ofrece una arquitectura moderna sin la complejidad de infraestructura tradicional.

## 🎯 Por qué ZeroClaw Go?

Los agentes de IA tradicionales requieren servidores permanentemente activos, lo que significa **cold-starts lentos** y **costes continuos** incluso cuando no hay actividad. ZeroClaw Go revoluciona este paradigma:

- ⚡ **Cold-Start Cero**: Vercel compiló tu código antes del primer request
- 💰 **Costo Cero Inactivo**: Solo paga cuando realmente se usa
- 🌐 **Escala Automática**: De 0 a millones de requests sin configuración
- 🔄 **Webhooks Nativos**: Cada plataforma entrega eventos directamente a tu función
- 🧠 **Memoria Persistente**: Supabase + pgvector para recall semántico

---

## 🆕 Novedades (Marzo 2026)

**🟢 Indicador "Escribiendo..." (Telegram)**:
- Llamada **sincrónica** a `sendChatAction = typing` antes de procesar memoria/LLM. Diseñado así porque Vercel Serverless puede congelar goroutines fire-and-forget. El usuario ve feedback inmediato.

**⚡ Ejecución Paralela de Herramientas**:
- `executeTools` refactorizado con `goroutines` + `sync.WaitGroup` + `sync.Mutex`. Si el LLM invoca 3 herramientas, se ejecutan en paralelo: O(N) → O(1). Métricas de latencia logueadas automáticamente.

**🧠 Smart Chunking para Memoria**:
- Textos largos (>500 chars) se dividen en chunks semánticamente coherentes antes de generar embeddings. Cada chunk se almacena como fila independiente con su propio vector, mejorando drásticamente la precisión del recall RAG.

**📝 Memoria Conversacional Mejorada**:
- Inyección de contexto conversacional reciente + memoria semántica. `MinMessageCharsForMemory = 1` para que mensajes cortos ("suma 3+3") también persistan.

---
	
	## ✨ Características Principales

### 📡 Arquitectura Multi-Canal vía Webhook

Conecta tu agente a múltiples plataformas de mensajería simultáneamente:

| Canal | Protocolo | Estado |
|-------|-----------|--------|
| Telegram | Bot API Webhook | ✅ Listo |
| Discord | Interactions Webhook | ✅ Listo |
| Slack | Events API Webhook | ✅ Listo |
| WhatsApp | Cloud API Webhook | ✅ Listo |

Cada canal es **independiente** y se configura vía tokens de entorno.

### 🧠 Memoria Híbrida Avanzada (RRF)

El sistema de memoria combina lo mejor de dos mundos usando **Reciprocal Rank Fusion (RRF)**:

- **Búsqueda Vectorial** (pgvector): Similitud semántica con embeddings
- **Búsqueda de Texto Completo** (FTS): Coincidencia exacta de palabras clave
- **Fusión RRF**: Ranking óptimo combinando ambos métodos

```sql
-- La función hybrid_search_memories combina ambos métodos
SELECT * FROM hybrid_search_memories(
    embedding,      -- $1: Vector de búsqueda (2048 dims)
    'query text',   -- $2: Texto para FTS
    10,             -- $3: Límite de resultados
    session_id,     -- $4: Filtrar por sesión
    0.5,            -- $5: Peso semántico
    0.3,            -- $6: Peso FTS
    60              -- $7: Parámetro RRF k
);
```

### 🔌 Provider Universal

Un solo provider, infinitas posibilidades. Compatible con **cualquier API OpenAI-compatible**:

| Proveedor | Base URL | Modelos Recomendados |
|-----------|----------|---------------------|
| OpenAI | `https://api.openai.com/v1` | gpt-4o-mini, gpt-4o |
| Groq | `https://api.groq.com/openai/v1` | llama-3.1-8b-instant |
| OpenRouter | `https://openrouter.ai/api/v1` | openai/gpt-4o-mini |
| xAI | `https://api.x.ai/v1` | grok-beta |
| Together AI | `https://api.together.xyz/v1` | meta-llama/Llama-3-8b |
| Fireworks AI | `https://api.fireworks.ai/inference/v1` | accounts/fireworks/models/llama-v3-8b-chat |
| Cerebras | `https://api.cerebras.ai/v1` | llama3.1-8b |

### 🛠️ Herramientas Serverless-Safe

Todas las herramientas están diseñadas con **timeouts estrictos** (< 10s) para funcionar en Vercel:

| Herramienta | Descripción | Timeout |
|-------------|-------------|---------|
| **WebSearch** | Búsqueda web (DuckDuckGo, Tavily, Brave) | 15s |
| **WebFetch** | Obtener y limpiar contenido de URLs | 8s |
| **HTTP Request** | Llamadas API GET/POST genéricas | 8s |

### 🎭 Sistema de Identidad AIEOS

Identidad estructurada basada en el estándar [AIEOS v1.1](https://aieos.org):

- **Identity**: Nombre, bio, origen, residencia
- **Psychology**: MBTI, traits OCEAN, brújula moral
- **Linguistics**: Estilo, formalidad, frases características
- **Motivations**: Impulso core, goals, fears
- **Capabilities**: Skills, tools, limitaciones
- **Directives**: Reglas de operación y seguridad

---

## 📂 Estructura del Proyecto

```
zeroclaw-go/
├── api/
│   └── webhook.go              # 📍 Entry point de Vercel Serverless
├── pkg/                         # 📦 Código fuente del framework
│   ├── agent/
│   │   └── loop.go             # 🤖 Core del agente (parallel tool-calling loop)
│   ├── channels/
│   │   ├── common.go           # 📇 Interfaz Channel (Name, Send, SendTyping)
│   │   ├── telegram.go         # 📱 Canal Telegram + SendTyping
│   │   ├── discord.go          # 💬 Canal Discord + SendTyping
│   │   ├── slack.go            # 💼 Canal Slack
│   │   └── whatsapp.go         # 💭 Canal WhatsApp
│   ├── core/
│   │   ├── interfaces.go       # 🔧 Interfaces (Channel, Memory, Provider)
│   │   ├── identity.go         # 🎭 Sistema de identidad + System Prompt
│   │   └── aieos.go            # 📋 Parser AIEOS
│   ├── memory/
│   │   ├── supabase.go         # 🗄️ Memoria con Supabase/pgvector + Smart Chunking
│   │   ├── chunker.go          # ✂️ Utilidad de Smart Chunking para RAG
│   │   └── chunker_test.go     # 🧪 Tests del chunker
│   ├── providers/
│   │   └── openai.go           # 🔌 Provider universal OpenAI-compatible
│   └── tools/
│       ├── websearch.go        # 🔍 Herramienta de búsqueda web
│       ├── webfetch.go         # 🌐 Herramienta de fetch de URLs
│       ├── httprequest.go      # 📡 Herramienta HTTP genérica
│       └── memorystore.go      # 💾 Herramienta de almacenamiento de memoria
├── sql/
│   └── schema_final.sql        # 🗃️ Único archivo SQL necesario
├── go.mod                      # 📦 Definición de módulo Go
└── README.md                   # 📖 Este archivo
```

---

## ☁️ Guía de Despliegue

### Paso 1: Preparar el Repositorio

```bash
# En GitHub (interfaz web o local)
# 1. Crear nuevo repositorio en GitHub
# 2. Subir todo el código de zeroclaw-go (excepto internal/ si existe)
# 3. El repositorio debe contener:
#    - api/webhook.go (entry point)
#    - pkg/ (código fuente)
#    - go.mod (Vercel detecta Go automáticamente)
#    - sql/schema_final.sql (base de datos)
```

### Paso 2: Configurar Supabase

1. **Crear proyecto**: Ir a [supabase.com](https://supabase.com) → New Project

2. **Configurar base de datos**:
   - Ir a **SQL Editor**
   - Copiar y ejecutar **completamente** el archivo [`sql/schema_final.sql`](sql/schema_final.sql)
   - ⚠️ **IMPORTANTE**: Ejecutar SOLO este archivo, no otros scripts SQL

3. **Obtener conexión**:
   - Settings → Database → Connection string
   - Seleccionar **Transaction** mode (puerto 6543)
   - Formato: `postgresql://postgres.[ref]:[password]@aws-0-[region].pooler.supabase.com:6543/postgres?sslmode=require`

### Paso 3: Desplegar en Vercel

1. **Importar repositorio**:
   - Ir a [vercel.com](https://vercel.com) → Add New → Project
   - Seleccionar el repositorio de GitHub

2. **Configuración automática**:
   - Vercel detectará automáticamente:
     - Framework: **Go** (por `go.mod`)
     - Build Command: `go build -o /tmp/main ./api`

3. **Variables de entorno** (ver sección siguiente)

4. **Deploy**: Click en **Deploy** 🎉

### Paso 4: Configurar Webhooks de Canales

Una vez desplegado, configura los webhooks de cada plataforma:

💡 **Nota sobre Auto-Detección:** ZeroClaw es capaz de auto-detectar de qué plataforma proviene el webhook. Si lo prefieres, puedes omitir el parámetro `?channel=...` en las siguientes URLs y configurar apuntando directamente al endpoint `/api/webhook`.

#### 📱 Telegram

```
URL del webhook:
https://api.telegram.org/bot<TOKEN>/setWebhook?url=https://<TU_DOMINIO>/api/webhook?channel=telegram
```

Reemplazar:
- `<TOKEN>` → Tu Bot Token (de @BotFather)
- `<TU_DOMINIO>` → Tu dominio de Vercel (ej: `mi-agente.vercel.app`)

#### 💬 Discord

1. Crear aplicación en [Discord Developer Portal](https://discord.com/developers/applications)
2. Ir a **Interactions Endpoint URL**:
```
https://<TU_DOMINIO>/api/webhook?channel=discord
```
3. Install → Generate OAuth URL → Authorize

#### 💼 Slack

1. Crear app en [Slack API](https://api.slack.com/apps)
2. Event Subscriptions → Enable → Request URL:
```
https://<TU_DOMINIO>/api/webhook?channel=slack
```
3. Subscribe to events: `message.channels`, `message.groups`
4. OAuth & Permissions → Install to Workspace

#### 💭 WhatsApp (Cloud API)

1. Ir a [Meta Developers](https://developers.facebook.com/)
2. WhatsApp → API Setup → Webhooks
3. Callback URL:
```
https://<TU_DOMINIO>/api/webhook?channel=whatsapp
```
4. Verify token: cualquier string que configures

---

## ⚙️ Variables de Entorno

### 🔧 Requeridas

| Variable | Descripción | Ejemplo |
|----------|-------------|---------|
| `SUPABASE_DB_URL` | URI de conexión PostgreSQL (Transaction mode) | `postgresql://postgres.xxx:pass@host:6543/postgres?sslmode=require` |
| `OPENAI_API_KEY` | Clave de API del provider de chat | `sk-proj-xxx` |

### 🎛️ Provider de Chat (Opcionales)

| Variable | Default | Descripción |
|----------|---------|-------------|
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | URL base del provider de chat |
| `OPENAI_MODEL` | `gpt-4o-mini` | Modelo a usar para respuestas |

### 🧠 Provider de Embeddings (Opcionales)

Permiten usar un provider separado para generar embeddings. Si no se configuran, se usa el provider de chat como fallback.

| Variable | Default | Descripción |
|----------|---------|-------------|
| `EMBEDDING_API_KEY` | (usa `OPENAI_API_KEY`) | API key del provider de embeddings |
| `EMBEDDING_BASE_URL` | `https://api.openai.com/v1` | URL base del provider de embeddings |
| `EMBEDDING_MODEL` | `text-embedding-3-small` | Modelo de embeddings |

> ⚠️ **NOTA**: El modelo de embeddings debe producir ≤2048 dimensiones (límite de la columna `vector(2048)` en Supabase). pgvector no soporta índices HNSW/IVFFlat para >2000 dimensiones, por lo que se usa búsqueda secuencial.

**Ejemplos de configuración de providers:**

```bash
# Cerebras (ultra-rápido) + OpenRouter (embeddings gratuitos)
OPENAI_API_KEY=csk-xxx
OPENAI_BASE_URL=https://api.cerebras.ai/v1
OPENAI_MODEL=qwen-3-235b-a22b-instruct-2507
EMBEDDING_API_KEY=sk-or-xxx
EMBEDDING_BASE_URL=https://openrouter.ai/api/v1
EMBEDDING_MODEL=nvidia/llama-nemotron-embed-vl-1b-v2:free

# Groq (rápido y barato)
OPENAI_BASE_URL=https://api.groq.com/openai/v1
OPENAI_MODEL=llama-3.1-8b-instant

# OpenRouter (muchos modelos)
OPENAI_BASE_URL=https://openrouter.ai/api/v1
OPENAI_MODEL=openai/gpt-4o-mini

# xAI (Grok)
OPENAI_BASE_URL=https://api.x.ai/v1
OPENAI_MODEL=grok-beta
```

### 📡 Canales (Opcionales)

| Variable | Descripción |
|----------|-------------|
| `TELEGRAM_BOT_TOKEN` | Token de Bot de Telegram |
| `DISCORD_BOT_TOKEN` | Token de Bot de Discord |
| `SLACK_BOT_TOKEN` | Token de Bot de Slack (xoxb-...) |
| `WHATSAPP_TOKEN` | WhatsApp Cloud API Access Token |
| `WHATSAPP_PHONE_ID` | Phone Number ID de WhatsApp |

### 🧠 Memoria y Agent (Opcionales)

| Variable | Default | Descripción |
|----------|---------|-------------|
| `DEFAULT_SESSION_ID` | `default` | ID de sesión por defecto |
| `SYSTEM_PROMPT` | (AIEOS default) | Prompt de sistema custom |
| `AGENT_NAME` | `ZeroClaw` | Nombre del agente |
| `AGENT_ROLE` | `AI Assistant` | Rol del agente |

### 🔍 Herramientas (Opcionales)

| Variable | Default | Descripción |
|----------|---------|-------------|
| `SEARCH_API_KEY` | (vacío) | API key para Tavily/Brave |
| `SEARCH_PROVIDER` | `duckduckgo` | Provider: `duckduckgo`, `tavily`, o `brave` |
| `WEBFETCH_TIMEOUT` | `8` | Timeout en segundos para la herramienta WebFetch |
| `WEBFETCH_MAX_CHARS` | `4000` | Máximo de caracteres a extraer en WebFetch |
| `HTTPREQUEST_TIMEOUT` | `8` | Timeout en segundos para la herramienta HTTPRequest |

---

## 🗃️ Base de Datos

### Esquema Único

El proyecto utiliza **una sola tabla** en Supabase:

```sql
memory_entries (
    id UUID PRIMARY KEY,
    key TEXT NOT NULL UNIQUE,
    content TEXT NOT NULL,
    category TEXT NOT NULL,
    timestamp TIMESTAMPTZ,
    session_id TEXT,
    embedding vector(2048),
    score FLOAT,
    metadata JSONB,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
)
```

### Índices

| Índice | Tipo | Propósito |
|--------|------|-----------|
| `idx_memory_entries_key` | B-tree | Lookups por key |
| `idx_memory_entries_category` | B-tree | Filtrado por categoría |
| `idx_memory_entries_session_id` | B-tree | Filtrado por sesión |
| `idx_memory_entries_timestamp` | B-tree | Ordenamiento temporal |
| `idx_memory_entries_content_fts` | GIN | Full-text search |

> 💡 **Nota**: No se usa índice vectorial (HNSW/IVFFlat) porque pgvector en Supabase los limita a 2000 dimensiones. La búsqueda secuencial es eficiente para <100k filas.

### Funciones SQL

| Función | Propósito |
|---------|-----------|
| `search_memories()` | Búsqueda semántica pura |
| `hybrid_search_memories()` | Búsqueda híbrida RRF (7 parámetros) |
| `search_memories_fts()` | Full-text search pura |
| `count_memories()` | Contar memorias |

---

## 📄 Licencia

MIT License - see [LICENSE](LICENSE) for details.

---

<p align="center">
<sub>Built with ❤️ for the Serverless Future</sub>
</p>

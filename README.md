# 🚀 ZeroClaw Go (Serverless Edition)

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Platform-Vercel-000000?style=for-the-badge&logo=vercel" alt="Platform">
  <img src="https://img.shields.io/badge/Database-Supabase-3FCF8E?style=for-the-badge&logo=supabase" alt="Database">
  <img src="https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge" alt="License">
</p>

**ZeroClaw Go** es un framework de agentes de IA autónomos ultra-ligero, diseñado **exclusivamente** para entornos Serverless. Construido sobre cimientos sólida de Go y desplegado en Vercel, ofrece una arquitectura modernas sin la complejidad de infrastructura tradicional.

## 🎯 Por qué ZeroClaw Go?

Los agentes de IA tradicionales requieren servidores permanentemente activos, lo que significa **cold-starts lentos** y **costes continuos** incluso cuando no hay actividad. ZeroClaw Go revolutiona este paradigma:

- ⚡ **Cold-Start Cero**: Vercel compiló tu código antes del primer request
- 💰 **Costo Cero Inactivo**: Solo paga cuando realmente se usa
- 🌐 **Escala Automática**: De 0 a millones de requests sin configuración
- 🔄 **Webhooks Nativos**: Cada platform entrega eventos directamente a tu función
- 🧠 **Memoria Persistente**: Supabase + pgvector para recall semántico

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

Cada canal es **independiente** y se configura vía tokens de entorno. El agente routing determina automáticamente qué canal recibió el mensaje.

### 🧠 Memoria Híbrida Avanzada

El sistema de memoria combina lo mejor de dos mundos:

- **Búsqueda Vectorial** (pgvector): Similitud semántica con embeddings de OpenAI
- **Búsqueda de Texto Completo** (FTS): Coincidencia exacta de palabras clave
- **Fusión RRF**: [Reciprocal Rank Fusion](https://dl.acm.org/doi/10.1145/3404835.3462952) para ranking óptimo

```sql
-- La función hybrid_search_memories combina ambos métodos
SELECT * FROM hybrid_search_memories(
    embedding,      -- Vector de búsqueda
    'query text',  -- Texto para FTS
    10,            -- Límite de resultados
    session_id     -- Filtrar por sesión
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

Todo programático, sin archivos `.md` locales.

---

## 📂 Estructura del Proyecto

```
zeroclaw-go/
├── api/
│   └── webhook.go          # 📍 Entry point de Vercel Serverless
├── internal/
│   ├── agent/
│   │   └── loop.go        # 🤖 Core del agente (tool-calling loop)
│   ├── channels/
│   │   ├── telegram.go    # 📱 Canal Telegram
│   │   ├── discord.go     # 💬 Canal Discord
│   │   ├── slack.go       # 💼 Canal Slack
│   │   └── whatsapp.go    # 💭 Canal WhatsApp
│   ├── core/
│   │   ├── interfaces.go  # 🔧 Definiciones de interfaces
│   │   ├── identity.go   # 🎭 Sistema de identidad
│   │   └── aieos.go     # 📋 Parser AIEOS
│   ├── memory/
│   │   └── supabase.go   # 🗄️ Implementación de memoria
│   ├── providers/
│   │   └── openai.go     # 🔌 Provider universal
│   └── tools/
│       ├── websearch.go  # 🔍 Herramienta de búsqueda
│       ├── webfetch.go   # 🌐 Herramienta de fetch
│       └── httprequest.go # 📡 Herramienta HTTP
├── sql/
│   ├── schema.sql        # 🗃️ Schema base de Supabase
│   └── hybrid_search.sql # 🔬 Funciones de búsqueda híbrida
├── go.mod                # 📦 Definición de módulo Go
└── README.md             # 📖 Este archivo
```

---

## ☁️ Guía de Despliegue

### Paso 1: Preparar el Repositorio

```bash
# En GitHub (interfaz web o local)
# 1. Crear nuevo repositorio en GitHub
# 2. Subir todo el código de zeroclaw-go
# 3. El repositorio debe contener:
#    - api/webhook.go (entry point)
#    - go.mod (Vercel detecta Go automáticamente)
```

### Paso 2: Configurar Supabase

1. **Crear proyecto**: Ir a [supabase.com](https://supabase.com) → New Project
2. **Configurar base de datos**:
   - Ir a **SQL Editor**
   - Copiar y ejecutar [`sql/schema.sql`](sql/schema.sql)
   - Copiar y ejecutar [`sql/hybrid_search.sql`](sql/hybrid_search.sql)
3. **Obtener conexión**:
   - Settings → Database → Connection string
   - Formato: `postgresql://postgres.[ref]:[password]@aws-0-[region].pooler.supabase.com:6543/postgres?sslmode=require`

### Paso 3: Desplegar en Vercel

1. **Importar repositorio**:
   - Ir a [vercel.com](https://vercel.com) → Add New → Project
   - Seleccionar el repositorio de GitHub

2. **Configuración automática**:
   - Vercel detectará automáticamente:
     - Framework: **Go** (por `go.mod`)
     - Build Command: `go build -o /tmp/main ./api`
     - Output Directory: (no requerido)

3. **Variables de entorno** (ver sección siguiente):

| Variable | Valor |
|----------|-------|
| `SUPABASE_DB_URL` | `postgresql://...` (del paso 2) |
| `OPENAI_API_KEY` | `sk-...` |

4. **Deploy**: Click en **Deploy** 🎉

### Paso 4: Configurar Webhooks de Canales

Una vez desplegado, configura los webhooks de cada plataforma:

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
| `SUPABASE_DB_URL` | URI de conexión PostgreSQL | `postgresql://postgres.xxx:pass@host:6543/postgres?sslmode=require` |
| `OPENAI_API_KEY` | Clave de API del provider | `sk-proj-xxx` |

### 🎛️ Provider (Opcionales)

| Variable | Default | Descripción |
|----------|---------|-------------|
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | URL base del provider |
| `OPENAI_MODEL` | `gpt-4o-mini` | Modelo a usar |
| `EMBEDDING_MODEL` | `text-embedding-3-small` | Modelo de embeddings |

**Ejemplos de configuración de providers:**

```bash
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

| Variable | Descripción |
|----------|-------------|
| `SEARCH_API_KEY` | API key para Tavily/Brave (opcional para DuckDuckGo) |
| `SEARCH_PROVIDER` | `duckduckgo` (default), `tavily`, o `brave` |

---

## 📄 Licencia

MIT License - see [LICENSE](LICENSE) for details.

---

<p align="center">
  <sub>Built with ❤️ for the Serverless Future</sub>
</p>

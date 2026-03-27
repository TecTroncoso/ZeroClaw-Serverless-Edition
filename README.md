# 🚀 ZeroClaw Go (Serverless Edition)

<p align="center">
<img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go" alt="Go Version">
<img src="https://img.shields.io/badge/Platform-Vercel-000000?style=for-the-badge&logo=vercel" alt="Platform">
<img src="https://img.shields.io/badge/Database-Supabase-3FCF8E?style=for-the-badge&logo=supabase" alt="Database">
<img src="https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge" alt="License">
</p>

**ZeroClaw Go** es un framework de agentes de IA autónomos ultra-ligero, diseñado **exclusivamente** para entornos Serverless y Edge. Construido sobre Go y desplegado en Vercel, ofrece una arquitectura omni-canal moderna sin la complejidad de la infraestructura tradicional.

## 🎯 ¿Por qué ZeroClaw Go?

Los frameworks de IA tradicionales están pensados para servidores permanentemente activos. ZeroClaw revoluciona este enfoque:

- ⚡ **Latencia Ultra-Baja**: Consultas vectoriales concurrentes y expresiones regulares cacheadas globalmente.
- 🪙 **Costo Cero Inactivo**: Solo pagas cuando el agente procesa un mensaje bajo Vercel.
- 🔄 **Streams No-Bloqueantes**: El agente puede editar mensajes en Discord/Telegram en streams asíncronos sin frenar la lectura del LLM y manteniendo vivos los contenedores temporales gracias a `sync.WaitGroup`.
- 🧠 **Memoria Híbrida Inteligente**: Usa **Supabase (pgvector)** con *Reciprocal Rank Fusion* (RRF) y *Smart Chunking* para combinar lo mejor de la búsqueda semántica y FTS.

---

## ✨ Características Principales

### 📡 Omni-Canal Nativo
Un único cerebro para todas tus plataformas, alimentado mediante Webhooks. Cada plataforma es completamente independiente o pueden unificarse con `MASTER_SESSION_ID`.
* **Soportados:** Telegram, Discord, Slack, WhatsApp (Cloud API).

### 🧠 Memoria Híbrida & Smart Chunking
Divide textos largos semánticamente asegurando recuperar fragmentos exactos para el contexto RAG antes de inyectarlos al sistema:
- Búsqueda Vectorial (`pgvector`, 2048 dims)
- Búsqueda de Texto Completo (FTS)
- Fusionados mediante RRF para exactitud absoluta.

### 🔌 Provider Universal (OpenAI-Compatible)
Funciona inmediatamente con cualquier proveedor de API compatible con OpenAI:
- OpenAI (`gpt-4o-mini`, `gpt-4o`)
- Groq, Cerebras, xAI (Grok), Together AI, OpenRouter, Fireworks.

### 🛠️ Herramientas Serverless-Safe
Ejecución concurrente en tiempo `O(1)` de un ilimitado número de flujos con timeouts estrictos para sobrevivir a la ejecución de Vercel.
- **WebSearch** (DuckDuckGo, Tavily, Brave)
- **WebFetch** (Extracción veloz de URLs con limpieza automática de HTML)
- **HTTPRequest** (Peticiones REST genéricas)

---

## ☁️ Guía Rápida de Despliegue

### 1. Base de Datos (Supabase)
1. Crea un proyecto en [Supabase](https://supabase.com).
2. Ejecuta el archivo SQL unificado: [`sql/schema_final.sql`](sql/schema_final.sql) en el SQL Editor de tu db.
3. Copia tu Connection String en **modo Transacción** (puerto `6543`, `prefer_simple_protocol=on`).

### 2. Despliegue en Vercel
Conecta y despliega este repositorio en Vercel. Vercel detectará el compilador de Go automáticamente del `go.mod`. 
Configura las siguientes **Variables de Entorno**:

| Variable | Ejemplo / Default | Descripción |
|----------|---------|-------------|
| `SUPABASE_DB_URL` | `postgresql://...:6543/postgres?sslmode=require` | Conexión a la base de datos |
| `OPENAI_API_KEY` | `sk-proj...` | Tu API Key del proveedor (Chat) |
| `OPENAI_MODEL` | `gpt-4o-mini` | Modelo LLM default a resolver peticiones |
| `OPENAI_BASE_URL`| `https://api.openai.com/v1` | URL base del Universal Provider |
| `[CANAL]_TOKEN` | `xxxx` | Token del bot (ej. `TELEGRAM_BOT_TOKEN`, `DISCORD_BOT_TOKEN`) |

*(Consulta los comentarios técnicos en el código para variables avanzadas como `EMBEDDING_API_KEY`, Providers paralelos de Embeddings, o las credenciales Application ID y Public Key requeridas exclusivamente para Discord).*

### 3. Webhooks de Canales
Configura la URL de tu proyecto desplegado en los dashboards de desarrollador y plataformas respectivas. 

**Ejemplo Telegram**:
```text
https://api.telegram.org/bot<TOKEN>/setWebhook?url=https://<TU_DOMINIO_VERCEL>/api/webhook?channel=telegram
```
*(Nota: ZeroClaw Auto-Detecta el canal revisando el body de la petición, el query parameter `?channel=` es un standard opcional).*

---

<p align="center">
<sub>Built with ❤️ for the Serverless Future</sub>
</p>

# 🚀 ZeroClaw Go (Serverless Edition)

<p align="center">
<img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go" alt="Go Version">
<img src="https://img.shields.io/badge/Platform-Vercel-000000?style=for-the-badge&logo=vercel" alt="Platform">
<img src="https://img.shields.io/badge/Database-Supabase-3FCF8E?style=for-the-badge&logo=supabase" alt="Database">
<img src="https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge" alt="License">
</p>

**ZeroClaw Go** es un framework de agentes de IA autónomos ultra-ligero, diseñado **exclusivamente** para entornos Serverless y Edge. Construido sobre Go y optimizado para ser desplegado en Vercel, ofrece una arquitectura omni-canal moderna (Telegram, Discord, Slack, WhatsApp) sin la complejidad, latencia ni costos de la infraestructura tradicional.

---

## 🎯 ¿Por qué ZeroClaw Go?

A diferencia de los frameworks de agentes que asumen servidores en ejecución continua (long-polling), ZeroClaw revoluciona el enfoque para el paradigma Serverless:

- ⚡ **Latencia Ultra-Baja**: Arquitectura eficiente y *cold starts* mínimos usando el compilador de Go en Vercel.
- 🪙 **Costo Cero Inactivo**: La concurrencia asíncrona te permite pagar exclusivamente cuando el agente recibe o procesa un mensaje.
- 🔄 **Workarounds Innovadores en Vercel**: Vercel no permite peticiones superiores a 10s en planes gratuitos, ni streams interactivos asíncronos en Discord. ZeroClaw interviene con patrones *Background Worker* y delegación inteligente de carga.
- 🧠 **Memoria Híbrida Inteligente**: Integración nativa con **Supabase (pgvector)** implementando *Reciprocal Rank Fusion* (RRF) y *Smart Chunking* para aunar búsqueda semántica y de texto completo.

---

## 📁 Estructura del Proyecto

```text
C:.
|   go.mod                  # Dependencias de Go (ultra-ligeras)
|   README.md               # Este archivo de documentación
|   vercel.json             # Configuración de despliegue para Vercel
|
+---api
|       webhook.go          # ENTRYPOINT PRINCIPAL para Vercel Serverless Function
|
+---cmd
|   \---register_discord
|           main.go         # Script de utilidad para registrar Slash Commands en Discord
|
+---pkg
|   +---agent
|   |       loop.go         # Bucle principal de pensamiento del Agente LLM
|   |
|   +---channels
|   |       common.go       # Estructuras comunes de mensajes (IncomingMessage)
|   |       discord.go      # Adaptador nativo de Discord (Signature verify, Patch)
|   |       slack.go        # Adaptador nativo de Slack
|   |       telegram.go     # Adaptador nativo de Telegram
|   |       whatsapp.go     # Adaptador nativo de WhatsApp
|   |
|   +---core
|   |       aieos.go        # Carga de perfiles de identidad abstractos
|   |       identity.go     # Acondicionador del System Prompt del AI (con inyección RTC)
|   |       interfaces.go   # Tipos fundamentales del Framework
|   |
|   +---memory
|   |       chunker.go      # Fragmentación inteligente de texto (Smart Chunking)
|   |       supabase.go     # Adaptador completo para Supabase (pgvector)
|   |
|   +---providers
|   |       openai.go       # Adaptador Universal LLM (OpenAI, Groq, OpenRouter, etc)
|   |
|   \---tools
|           httprequest.go  # Herramienta Serverless: Peticiones HTTP genéricas
|           makecalendar.go # Herramienta Serverless: Scheduler vía Make.com Webhooks
|           memorystore.go  # Herramienta Serverless: Almacenamiento Core dirigido por AI
|           sendemail.go    # Herramienta Serverless: Envío SMTP nativo
|           webfetch.go     # Herramienta Serverless: Scraping ligero y sanitización HTML
|           websearch.go    # Herramienta Serverless: Búsqueda Web (DuckDuckGo, Brave...)
|
\---sql
        schema_final.sql    # Setup relacional y de vectores listo para Supabase SQL Editor
```

---

## ✨ Características y Herramientas (Serverless-Safe)

ZeroClaw incorpora herramientas eficientes (*Tools/Functions*) blindadas con strict timeouts (`context.WithTimeout`) adaptadas a Vercel.

1. **Memoria Core (`core_memory_save`)**: El agente decide cuándo debe recordar al usuario (nombre, preferencias, etc.) y lo guarda en base de datos.
2. **WebSearch & Fetch (`websearch`, `webfetch`)**: Capacidades de rastreo e indexación de páginas y búsquedas en tiempo real.
3. **Peticiones HTTP (`http_request`)**: GET, POST a cualquier API mediante JSONs dinámicos.
4. **Email & Agenda (`send_email`, `schedule_calendar_event`)**:
   - El agente envía correos usando estándares de Go `net/smtp` vía credenciales estables, con cero dependencias ajenas.
   - Crea eventos de calendario disparando **Make.com Webhooks**, salvaguardando al proyecto de pesar cientos de megas (evitando SDKs masivos de la nube).

*Todo gestionado por el adaptador `api/webhook.go` y orquestado en `pkg/agent/loop.go`.*

---

## 📅 Configuración de Asistente Personal (Email & Calendario)

ZeroClaw puede interactuar con el mundo real agendando citas en tu calendario y enviando correos electrónicos. Sigue esta sencilla guía paso a paso para activarlo:

### 1. Configuración de Email (SMTP Nativo)

El bot utiliza el protocolo puro y ultra-ligero `net/smtp` de Go para evitar depender de SDKs voluminosos.

**Instrucciones para enviar desde un correo Gmail:**

1. Ve a los ajustes de tu cuenta de Google.
2. Navega a **Seguridad** -> **Verificación en 2 pasos**.
3. Busca la opción inferior **"Contraseñas de aplicaciones"** (*App Passwords*).
4. Genera una nueva contraseña (te dará una clave de 16 caracteres, ej: `abcd efgh ijkl mnop`).
5. Copia esa clave; esta será tu `SMTP_PASSWORD`.

Agrega las siguientes variables a tu entorno (Vercel):

- `SMTP_USER`: Tu correo de Gmail completo (ej: `tu.correo@gmail.com`).
- `SMTP_PASSWORD`: La clave de 16 letras obtenida (sin espacios).
- `SMTP_HOST`: URL del servidor (por defecto, puedes colocar `smtp.gmail.com`).
- `SMTP_PORT`: Puerto saliente (por defecto `587`).

### 2. Configuración de Google Calendar (Vía Make.com)

En lugar de forzar al bot a cargar cientos de dependencias de la consola de Google Cloud, usamos **Make.com** como un puente Serverless 100% gratuito.

**Pasos en Make.com:**

1. Crea una cuenta gratuita en [Make.com](https://www.make.com).
2. Crea un **nuevo escenario** (*Create a new scenario*) y añade el módulo "**Webhooks -> Custom webhook**". Ponle un nombre y copia la URL que te genera.
3. Haz clic en añadir otro módulo, busca e inserta "**Google Calendar -> Create an Event**" y conecta/autoriza tu cuenta principal de Google.
4. Mapea los datos dinámicos recibidos por el webhook hacia el Evento de Google:
   - `Event Name` -> Selecciona la variable `title`.
   - `Start Date` -> Selecciona la variable `start_date`.
   - `End Date` -> Selecciona la variable `end_date`.
   - `Description` -> Selecciona la variable `description`.
5. Pulsa el botón inferior izquierdo para **Activar el escenario** (*Scheduling: ON*).

Agrega la siguiente variable a tu entorno (Vercel):

- `MAKE_CALENDAR_WEBHOOK`: Pega aquí la URL exacta que copiaste en el paso 2 de Make.com.

---

## 🚀 Guía Rápida de Instalación (Despliegue)

### 1. Preparar la Base de Datos (Supabase)

La memoria híbrida requiere una base robusta:

1. Crea un nuevo proyecto en [Supabase](https://supabase.com).
2. Abre la consola / SQL Editor e inserta y ejecuta todo el bloque de [`sql/schema_final.sql`](sql/schema_final.sql).
3. Obtén tu cadena de conexión URI en la configuración (Database -> Connection String -> URI), usando **Modo Transacción** (puerto `6543`, `prefer_simple_protocol=on`).

### 2. Desplegar a Vercel

Realiza un Push al repositorio en Github/Gitlab y vincúlalo para crear un Proyecto en [Vercel](https://vercel.com).
Añade las siguientes **Variables de Entorno** en la pestaña de configuración del proyecto (`Project Settings` > `Environment Variables`):

| Variable | Ejemplo / Por Defecto | Propósito |
|----------|----------------------|-----------|
| `SUPABASE_DB_URL` | `postgresql://...:6543/postgres?sslmode=require` | Conexión vital a la memoria RAG del agente. |
| `OPENAI_API_KEY` | `sk-proj...` | Clave API del proveedor LLM a elegir. |
| `OPENAI_MODEL` | `gpt-4o-mini` | Nombre del modelo que procesará el razonamiento. |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | Sobrescribe para usar Groq/OpenRouter. |
| `EMBEDDING_API_KEY` | `sk-proj...` (opcional) | API Key independiente para generar Embeddings (si usas distinto proveedor). |
| `EMBEDDING_MODEL` | `text-embedding-3-small` | Modelo usado para vectorizar los chunks en Supabase. |
| `EMBEDDING_BASE_URL` | `https://api.openai.com/v1` | URL base independiente para el endpoint de Embeddings. |
| `TELEGRAM_BOT_TOKEN` | `123456:ABC-DEF...` | Token del bot de Telegram. |
| `DISCORD_BOT_TOKEN` | `MTIz...` | Token del bot de Discord. |
| `DISCORD_APP_ID` | `1122334455...` | Requerido por Discord para gestionar Interacciones y Webhooks. |
| `DISCORD_PUBLIC_KEY` | `abcdef123...` | Requerido por Discord para verificar las firmas de los Webhooks. |
| `SYSTEM_PROMPT` | `Eres ZeroClaw...` (opcional) | Sobrescribe el profile de instrucciones base del agente. |
| `MAKE_CALENDAR_WEBHOOK` | `https://hook.us1.make.com/...` | Webhook de Make para agendar eventos de calendario. |
| `SMTP_USER` | `tuemail@gmail.com` | Correo emisor para el agente. |
| `SMTP_PASSWORD` | Clave de 16 letras | Autenticación SMTP (App Password de Google). |
| `SMTP_HOST` | `smtp.gmail.com` | Host principal de conexión de correo (default). |
| `SMTP_PORT` | `587` | Puerto de salida seguro del servidor de correos (default). |
| `MASTER_SESSION_ID` | `mi_bot_global` (opcional) | Unifica la memoria del agente entre canales. Si se configura, el bot compartirá el mismo historial y recuerdos sin importar si el usuario le habla por Telegram o Discord. |
| `AIEOS_PROFILE` | `{"identity":{...}}` (opcional) | Permite inyectar un JSON personalizado para cambiar la identidad y psicología del bot (nombre, rol, personalidad). |

> ⚠️ **Nota sobre Tiempos de Ejecución en Vercel**
>
> El archivo `vercel.json` configura `maxDuration: 60`, lo que aprovecha el tiempo máximo de ejecución permitido por el plan gratuito (Hobby) de Vercel. Esto es **vital** para darle tiempo suficiente al LLM de razonar, ejecutar herramientas encadenadas y devolver una respuesta completa sin que Vercel corte la conexión prematuramente.

### 3. Configurar Webhooks de Canales (Uso)

Para que las plataformas se comuniquen con Vercel, debes registrar la URL del servidor usando los apartados de desarrolladores de cada plataforma:

- **La URL base será siempre:** `https://<TU_DOMINIO_VERCEL>.vercel.app/api/webhook`

- **En Telegram**: Accede a este link desde tu navegador para fijar tu Webhook:
  `https://api.telegram.org/bot<TOKEN>/setWebhook?url=https://<DOMAIN>/api/webhook?channel=telegram`

- **En Discord**:
  1. Registra en el *Discord Developer Portal* > tu App > **Interactivity & Eventing** la URL de Endpoint de Interacciones: `https://<DOMAIN>/api/webhook?channel=discord`.
  2. **¡Paso clave!** Para registrar globalmente tu comando `/chat` en los servidores donde el bot sea invitado, simplemente visita esta URL temporal en tu navegador **una sola vez**:
     `https://<DOMAIN>/api/webhook?setup_discord=true`
     *(ZeroClaw registrará automáticamente la Interfaz del Slash Command usando tu TOKEN publicando éxito "✅").*

*(Nota general: ZeroClaw inspecciona y deduce dinámicamente el Payload del Webhook si en algún caso omites el query params `?channel=`, ajustando automáticamente).*

---

<p align="center">
<sub>Built with ❤️ for the Serverless Future</sub>
</p>

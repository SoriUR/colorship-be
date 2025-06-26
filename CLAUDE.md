# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go backend service for "RedFlagged" - a psychology-focused chat application that provides relationship advice and emotional support. The service integrates with OpenAI's API to provide AI-powered conversations through a professional psychologist persona.

## Development Commands

### Running the application
```bash
go run .
```

### Building the application
```bash
go build -o redflagged .
```

### Installing dependencies
```bash
go mod tidy
```

### Testing (if tests exist)
```bash
go test ./...
```

## Architecture

### Core Components

**HTTP Server Setup (`main.go:15-51`)**
- Single-file HTTP server using `net/http`
- PostgreSQL database connection via Supabase
- Environment-based configuration
- Routes defined with `http.HandleFunc`

**API Endpoints**
- `POST /api/sign_up` - User registration with UUID-based tokens
- `GET /api/launch` - User authentication and credit info
- `POST /api/chat` - Main chat functionality with OpenAI integration
- `GET /api/chat` - Retrieve chat history
- `GET /api/chats` - List user's chats
- `POST /api/webhook/revenuecat` - Handle RevenueCat purchase webhooks
- `GET /api/confirmation` - Confirm transaction processing

**Authentication System (`getUserIDFromRequest.go`)**
- Bearer token authentication
- Tokens stored in `users.access_token` database field
- User lookup by token for each authenticated request

**Chat System (`chat.go`, `domain.go`)**
- Multi-modal chat supporting text, images, and voice messages
- System prompt loaded from `.prompt` file
- Message history stored in PostgreSQL with timestamps
- OpenAI API integration with GPT-4o (paid) and GPT-3.5-turbo (free)
- Image and voice file handling via Supabase storage with signed URLs
- Voice transcription caching to avoid repeated API calls

**Credit System**
- Free message limits and paid message credits
- Automatic credit deduction per message
- RevenueCat integration for in-app purchases
- Two product tiers: 10 messages and 100 messages

**Database Schema**
- `users` - User authentication and basic info
- `user_credits` - Message limits and payment status
- `chats` - Chat sessions per user
- `messages` - Individual messages with support for image/voice arrays and cached transcriptions
- `processed_transactions` - RevenueCat transaction tracking

### External Integrations

**OpenAI API**
- Vision API for image processing
- Whisper API for voice transcription
- Model selection based on user's paid status
- Custom system prompt for psychology/relationship advice

**Supabase**
- PostgreSQL database hosting
- Storage bucket for images with signed URL generation
- Separate `redflagged-voices` bucket for voice files
- Database connection via `SUPABASE_DB_URL`

**RevenueCat**
- Webhook processing for purchase events
- Product ID mapping to message credits
- Transaction deduplication

## Environment Variables

Required environment variables:
- `SUPABASE_DB_URL` - PostgreSQL connection string
- `OPENAI_API_KEY` - OpenAI API key
- `SUPABASE_URL` - Supabase project URL
- `SUPABASE_SERVICE_ROLE` - Service role key for storage access
- `SUPABASE_BUCKET_NAME` - Storage bucket name
- `REVENUE_CAT_API_KEY` - RevenueCat API key
- `REVENUE_CAT_WEBHOOK_TOKEN` - RevenueCat webhook authentication
- `PORT` - Server port (defaults to 8080)

## Key Files

- `.prompt` - System prompt for AI conversations (psychology/relationship advice)
- `domain.go` - All struct definitions for requests/responses
- `errors.go` - Centralized error handling with structured responses
- `chat.go` - Core chat functionality and OpenAI integration
- `webhook.go` - RevenueCat webhook processing and credit management

## API Structure

### Chat Request Format
```json
{
  "chat_id": "optional-existing-chat-id",
  "prompt": "User message text",
  "image_paths": ["path/to/image1.jpg", "path/to/image2.png"],
  "voice_paths": ["path/to/voice1.m4a", "path/to/voice2.wav"]
}
```

### Message Response Format
```json
{
  "role": "user|assistant|system",
  "content": "Message text content",
  "image_paths": ["path/to/image.jpg"],
  "voice_paths": ["path/to/voice.m4a"],
  "voice_transcription": "[Голосовое сообщение 1]: Transcribed text",
  "timestamp": "2024-01-01T12:00:00Z"
}
```

### Required Database Schema Updates
```sql
ALTER TABLE messages ADD COLUMN voice_paths TEXT[];
ALTER TABLE messages ADD COLUMN voice_transcription TEXT;
```

## Development Notes

- All database operations use raw SQL queries
- Error responses follow a consistent JSON structure
- Image and voice processing require paid message credits
- Voice transcriptions are cached in database to avoid repeated API calls
- Images use `SUPABASE_BUCKET_NAME` bucket, voices use hardcoded `redflagged-voices` bucket
- System messages are excluded from user-facing chat history
- Russian language used in error messages and logging
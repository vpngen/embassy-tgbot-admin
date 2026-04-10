# Embassy Telegram Bot Admin Panel

Admin panel for managing conversation flows, decision templates, and ministry messages used by the Embassy Telegram bot.

## Requirements

- Go 1.25+

## Quick Start

```bash
# Install dependencies
go mod download

# Create an admin user
go run . --add-user admin:testpass #example

# Start the server
go run .
```

The server listens on `:8080` by default.

## Configuration

Flags and their corresponding environment variables:

| Flag | Env Variable | Default | Description |
|------|-------------|---------|-------------|
| `--addr` | `ADMIN_ADDR` | `:8080` | HTTP listen address |
| `--flows-dir` | `ADMIN_FLOWS_DIR` | `flows` | Directory for flow JSON files |
| `--users-file` | `ADMIN_USERS_FILE` | `users.json` | Path to users JSON file |
| `--bot-webhook` | `ADMIN_BOT_WEBHOOK` | _(empty)_ | URL to POST when flows are published |
| `--add-user` | — | — | Add/update user (`username:password`), then exit |

## API Endpoints

### Authentication

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/login` | Basic Auth | Returns a JWT token (valid 24h) |

### Flows

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/flows` | Bearer | List all flows |
| `GET` | `/api/flows/{id}` | Bearer | Get a flow by ID |
| `PUT` | `/api/flows/{id}` | Bearer | Update a flow |
| `DELETE` | `/api/flows/{id}` | Bearer | Delete a flow |
| `POST` | `/api/flows/publish` | Bearer | Publish flows to the bot |

### Decisions

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/decisions` | Bearer | Get decision templates |
| `PUT` | `/api/decisions` | Bearer | Update decision templates |

### Ministry Messages

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/ministry` | Bearer | Get ministry messages |
| `PUT` | `/api/ministry` | Bearer | Update ministry messages |

All protected endpoints accept an optional `?lang=en` query parameter for English translations (default is Russian).

## Authentication Flow

1. `POST /api/login` with HTTP Basic Auth credentials
2. Receive a JWT token in the response body
3. Send the token as `Authorization: Bearer <token>` on subsequent requests

Tokens expire after 24 hours. The signing key is generated in memory on startup, so restarting the server invalidates all active tokens.

## Project Structure

```
├── main.go              # Entrypoint, CLI flags, server setup
├── users.json           # Admin credentials (bcrypt hashes)
├── flows/               # Flow and template JSON files
│   ├── main.json
│   ├── main_en.json
│   ├── decisions.json
│   ├── decisions_en.json
│   ├── ministry.json
│   └── ministry_en.json
└── internal/
    ├── auth/            # JWT auth, bcrypt password hashing
    ├── handler/         # HTTP handlers and routing
    ├── model/           # Data types (Flow, Stage, Button, etc.)
    └── storage/         # File-based JSON storage
```

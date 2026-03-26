# url-shortener

Gin-based Go API scaffold with PostgreSQL and Redis connectivity.

## Prerequisites

- Go (project uses Go toolchain 1.25)
- Docker + Docker Compose

## Run stack in Docker Compose

```bash
docker compose up -d --build
```

This starts:
- Gin API on `http://localhost:8081`
- PostgreSQL on `localhost:5432`
- Redis on `localhost:6379`

## Endpoints

- `GET /health` - process health
- `GET /ping` - ping/pong endpoint
- `GET /ready` - PostgreSQL + Redis readiness

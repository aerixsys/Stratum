# VPS Deployment (Compose Production Path)

This release targets self-hosted VPS deployment with Docker Compose for production.
For local development with a direct binary run, see the quick start in `README.md`.

## 1) Prerequisites

- Linux VPS with Docker Engine + Docker Compose plugin.
- Bedrock access configured for your AWS credentials.
- Firewall only exposing your reverse proxy (not raw container port).

## 2) Prepare runtime config

Use only existing Stratum config keys (no additional runtime env variables).

```bash
cp .env.example .env
# edit .env with production values
chmod 600 .env
```

Model policy is required and tracked in git:

- `config/model-policy.yaml`
- Edit this file to update blocked model patterns
- Restart Stratum after changes (`docker compose restart`)
- Verify with `GET /v1/models`

Required at minimum:

- `API_KEY`
- `AWS_REGION`
- AWS credentials (or instance role/attached credentials chain)

## 3) Build and start

```bash
docker compose build --pull
docker compose up -d
```

Verify health:

```bash
curl -sS http://127.0.0.1:8000/ready
```

## 4) Operate

Logs:

```bash
docker compose logs -f --tail=200
```

Restart:

```bash
docker compose restart
```

Stop:

```bash
docker compose down
```

## 5) Upgrade and rollback

Upgrade:

```bash
git pull
docker compose build --pull
docker compose up -d
```

Rollback (to previous git tag/commit):

```bash
git checkout <previous-tag-or-commit>
docker compose build --pull
docker compose up -d
```

## 6) Reverse proxy guidance

Bind container port to loopback only (`127.0.0.1:8000:8000`) and front it with Nginx/Caddy.

Example Nginx snippet:

```nginx
server {
  listen 443 ssl;
  server_name your-domain.example;

  location / {
    proxy_pass http://127.0.0.1:8000;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Request-ID $request_id;
    proxy_buffering off;
  }
}
```

## 7) Security baseline

- Keep `.env` out of git.
- Restrict `.env` permissions to owner only.
- Rotate `API_KEY` and AWS credentials regularly (see `docs/secret-rotation.md`).

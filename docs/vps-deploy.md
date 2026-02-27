# VPS Deployment Runbook

Production deployment of Stratum on a self-hosted VPS using Docker Compose.

## Prerequisites

| Requirement | Details |
| --- | --- |
| Linux VPS | Docker Engine and Docker Compose plugin installed |
| AWS | Bedrock access enabled on your account |
| Reverse proxy | Nginx, Caddy, or Traefik in front of Stratum |
| Firewall | Only proxy ports exposed publicly |

## 1) Configure

```bash
cp .env.example .env
chmod 600 .env
```

Set at minimum:
- `API_KEY`
- `AWS_REGION`
- AWS credentials (or use an IAM role)

The repository includes a curated default `config/model-policy.yaml`. Adjust it if
you want to expose a broader or narrower model set.

## 2) Build and Start

```bash
docker compose build --pull
docker compose up -d
```

## 3) Verify

```bash
curl -sS http://127.0.0.1:8000/ready
curl -sS http://127.0.0.1:8000/v1/models \
  -H "Authorization: Bearer <API_KEY>"
```

## 4) Day-2 Operations

| Task | Command |
| --- | --- |
| View logs | `docker compose logs -f --tail=200` |
| Restart | `docker compose restart` |
| Stop | `docker compose down` |
| Upgrade | `git pull && docker compose build --pull && docker compose up -d` |
| Rollback | `git checkout <tag> && docker compose build --pull && docker compose up -d` |

## 5) Nginx Reverse Proxy

```nginx
server {
  listen 443 ssl;
  server_name your-domain.example;

  location / {
    proxy_pass http://127.0.0.1:8000;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_buffering off;
  }
}
```

## Security Notes

- Keep `.env` out of git and at `0600` permissions
- Rotate `API_KEY` and AWS credentials regularly
- Stratum does not apply local rate limiting; enforce throttling at the edge
- See [secret-rotation.md](secret-rotation.md)

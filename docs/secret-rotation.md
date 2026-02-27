# Secret Rotation Runbook

Covers rotation of the gateway `API_KEY` and AWS credentials.

## Baseline

| Item | Requirement |
| --- | --- |
| Storage | Keep secrets in `.env` (or inject via environment) |
| Permissions | `chmod 600 .env` |
| Git | Never commit secrets; `.env` is gitignored |

## Standard Rotation

1. Generate new secret values.
2. Update `.env`:
   - `API_KEY`
   - `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` (if using static credentials)
3. Recreate containers:

```bash
docker compose up -d --force-recreate
```

4. Validate health:

```bash
curl -sS http://127.0.0.1:8000/ready
```

5. Validate auth with the new key:

```bash
curl -sS http://127.0.0.1:8000/v1/models \
  -H "Authorization: Bearer <NEW_API_KEY>"
```

## Emergency Checklist (Suspected Leakage)

- Rotate keys immediately
- Invalidate old keys in all clients
- Recreate containers: `docker compose up -d --force-recreate`
- Check logs for suspicious activity
- Tighten network exposure and access controls

## Rollback

1. Restore the previous known-good `.env` backup.
2. Recreate containers:

```bash
docker compose up -d --force-recreate
```

3. Re-run health and auth checks.

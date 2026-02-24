# Secret Rotation Runbook (VPS Local Env Model)

This runbook covers rotation for:

- Gateway `API_KEY`
- AWS credentials used by Stratum

No AWS-managed secret deployment stack is required for this release.

## Storage and permissions

- Keep runtime env file outside public/shared paths.
- Recommended file mode: `0600`.
- Recommended owner: service operator account.

Example:

```bash
chmod 600 .env
```

## Rotation procedure

1. Generate replacement secret(s).
2. Update `.env` values:
- `API_KEY`
- `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` (if using static credentials)
3. Restart service:

```bash
docker compose up -d --force-recreate
```

4. Verify health:

```bash
curl -sS http://127.0.0.1:8000/ready
```

5. Verify authenticated request path with new key:

```bash
curl -sS http://127.0.0.1:8000/v1/models \
  -H "Authorization: Bearer <NEW_API_KEY>"
```

## Rollback

If validation fails:

1. Restore previous `.env` backup.
2. Recreate containers:

```bash
docker compose up -d --force-recreate
```

3. Re-run health and auth checks.

## Incident response note

If key leakage is suspected:

1. Rotate immediately.
2. Invalidate old key in all clients.
3. Review logs for suspicious request patterns.
4. Tighten network exposure and operator access on VPS.

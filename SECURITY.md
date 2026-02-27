# Security Policy

## Reporting a Vulnerability

Do not open a public GitHub issue for security vulnerabilities.

Use GitHub private vulnerability reporting for this repository:
- Security
- Advisories
- Report a vulnerability

Include:
- affected endpoint, package, or config area
- reproduction steps
- impact
- suggested fix (optional)

## Response Timeline

- Acknowledgement: within 3 business days
- Triage and severity: best effort
- Fix and release: coordinated with reporter
- Public disclosure: after remediation ships

## Operator Security Baseline

- Keep `.env` out of git and at `0600`
- Rotate `API_KEY` and AWS credentials regularly
- Put Stratum behind a reverse proxy
- Enforce throttling at the edge

See [docs/secret-rotation.md](docs/secret-rotation.md) for the rotation runbook.

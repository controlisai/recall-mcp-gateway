# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Recall MCP Gateway, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

### How to Report

1. **GitHub Security Advisories (preferred):** Go to the [Security Advisories](https://github.com/controlisai/recall-mcp-gateway/security/advisories) page and click "Report a vulnerability."

2. **Email:** Send details to **security@controlis.ai**.

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

| Action | Timeline |
|--------|----------|
| Acknowledge receipt | Within 24 hours |
| Initial assessment | Within 48 hours |
| Fix for critical/high severity | Within 72 hours |
| Fix for medium/low severity | Next scheduled release |
| Public disclosure | After fix is released |

### Severity Assessment

We use CVSS v3.1 to assess severity:

- **Critical (9.0-10.0):** Remote code execution, auth bypass, data exfiltration
- **High (7.0-8.9):** Privilege escalation, significant data exposure
- **Medium (4.0-6.9):** Denial of service, limited data exposure
- **Low (0.1-3.9):** Information disclosure with minimal impact

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Previous minor | Best effort |
| Older versions | No |

## Security Hardening

For production deployment security guidance, see:
- [Security Hardening Guide](docs/client/security-hardening-guide.md)
- [Deployment Guide](docs/client/deployment-guide.md)

## Recognition

We appreciate responsible disclosure. Contributors who report valid security issues will be acknowledged in the release notes (unless they prefer to remain anonymous).

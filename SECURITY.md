# Security Policy

## Supported version

Security fixes are developed against the current default branch and the latest published add-on image.

## Reporting a vulnerability

Do not report security vulnerabilities in public GitHub Issues.

Use GitHub's Report a vulnerability private reporting feature if it is enabled for this repository. Include:

- A clear description and impact.
- Reproduction steps using a local test environment.
- Affected version or commit.
- Suggested mitigation, if known.

Do not include real Google cookies, API keys, Admin tokens, database files, or other credentials in a report.

## Operational guidance

- Keep the Admin token and API key unique and private.
- Do not expose port 8083 to the public internet.
- Restrict LAN access to trusted clients.
- Keep URL fetching and media generation disabled unless required.
- Back up Home Assistant before testing an upgrade.

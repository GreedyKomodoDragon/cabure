# Security Policy

## Reporting a Vulnerability

Do not open a public issue for suspected security vulnerabilities.

Report vulnerabilities through the repository hosting platform's private security reporting mechanism if it is enabled. If private reporting is not available, contact the maintainer through the contact address published on the repository profile and include:

- A description of the issue
- Affected versions or commit range
- Reproduction steps or a proof of concept
- Any suggested remediation

Please allow time for investigation before public disclosure.

## Scope

Security reports are especially relevant for:

- Credential handling
- Repository checkout and path validation
- Namespace isolation
- Cluster-scoped resource controls
- Apply and prune behavior

## Supported Versions

Until Cabure has formal stable releases, security fixes are expected to land on the latest maintained branch or default branch.

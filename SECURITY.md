# Security policy

## Supported version

The current `main` branch and latest Docker Hub image receive fixes.

## Deployment boundary

Stockroom has no built-in authentication. The supplied Compose file binds it to
`127.0.0.1` by default. Do not expose it directly to the internet. Put any LAN
or remote deployment behind an authenticated reverse proxy and trusted network
boundary.

## Reporting a vulnerability

Use GitHub's private security-advisory flow for this repository. Do not include
credentials, databases, or real intake files in a public issue.

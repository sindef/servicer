# Reverse Proxy Contract

Production traffic terminates TLS before reaching the `web` Service. The ingress or reverse proxy must:

- enforce HTTPS redirects
- set `X-Forwarded-Proto=https`
- set `X-Forwarded-Host` to the public host
- preserve `Host`
- cap request bodies at `2m` unless an operator intentionally raises the limit
- rate-limit login and API writes at the edge
- forward `/api/*` only to the BFF through the web nginx proxy

Set `SERVICER_AUTH_EXTERNAL_BASE_URL` to the canonical external URL for auth redirects (for example, `https://servicer.example.com`). This is the preferred source for OAuth redirect URI host/scheme generation.

When `SERVICER_AUTH_EXTERNAL_BASE_URL` is unset, the BFF trusts forwarded security headers only when `SERVICER_TRUSTED_PROXY_HEADERS=true`. In that mode, `X-Forwarded-Host` is accepted only when it passes host validation.

The web image adds HSTS, CSP, frame, content-type, referrer, and permissions-policy headers.

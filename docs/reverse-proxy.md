# Reverse Proxy Contract

Production traffic terminates TLS before reaching the `web` Service. The ingress or reverse proxy must:

- enforce HTTPS redirects
- set `X-Forwarded-Proto=https`
- set `X-Forwarded-Host` to the public host
- preserve `Host`
- cap request bodies at `2m` unless an operator intentionally raises the limit
- rate-limit login and API writes at the edge
- forward `/api/*` only to the BFF through the web nginx proxy

The BFF trusts forwarded security headers only when `SERVICER_TRUSTED_PROXY_HEADERS=true`, which is set in the production manifest. Without that setting, cookies are marked `Secure` only for direct TLS requests.

The web image adds HSTS, CSP, frame, content-type, referrer, and permissions-policy headers.

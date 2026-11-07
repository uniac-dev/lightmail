# LightMail

LightMail is a single-installation, sender-only email service. The backend
accepts authenticated HTTP requests, signs messages for verified domains, and
delivers directly to recipient MX servers. The dashboard is an independent
static application with a small HTTP server. Both run without Node.js in
production. The TypeScript SDK uses the same public HTTP API as the dashboard.

## Contract

- One administrator per installation, authenticated with an administrator token.
  Browser sign-in exchanges that token for an HttpOnly, SameSite session cookie.
- Sending keys can submit mail and read only the messages submitted with that
  key. Optional domain restrictions constrain the key's From addresses.
- Domains receive RSA-2048 DKIM keys and explicit DNS instructions. A domain
  must have its public key verified in DNS before the service admits mail.
- Email submission commits the message and recipient queue before returning
  HTTP 202. Idempotency keys are scoped to the submitting credential; reusing
  a key with different content returns a conflict.
- Delivery state is tracked per recipient. Accepted means the remote SMTP
  server accepted DATA, not that the message reached an inbox. Permanent SMTP
  errors fail only the affected recipient. Temporary errors receive bounded
  backoff. A process interruption can leave an ambiguous remote outcome, so
  recovery can produce a duplicate; exactly-once delivery is not promised.
- Sending is direct SMTP on destination port 25 with certificate-verified
  STARTTLS required by default. DNS-resolved private, loopback, link-local and
  multicast destinations are refused. No incoming SMTP or mailbox service runs.
- Failed recipients can be retried explicitly; queued recipients can be
  canceled. Successful recipients are never selected by a manual retry.
- The backend sleeps until submission or the next queued retry. Retention is
  evaluated at startup and on activity, with no idle housekeeping poll loop.
- Message bodies, keys, and sessions are stored on a private data volume using
  bbolt. A backend instance holds the store exclusively. Sent-message history
  defaults to 30 days, and old freed pages are reused by the store.

## Modules

| Module | Inputs | Outputs and state |
| --- | --- | --- |
| domains | Domain names, DNS responses | Private signing keys, public DNS instructions, verification |
| messages | Validated submission, credential identity | Immutable content, recipient states, attempts, idempotency |
| delivery | Signed MIME, recipient, MX/DNS | SMTP acceptance or classified failure |
| queue | Durable recipient intent, next retry time | Claimed work, committed outcomes; one sleeping worker |
| HTTP API | Bearer credentials or browser session | Domain, email, key, and installation resources |
| dashboard | HTTP API resources and user actions | Static browser UI, same-origin API proxy |
| TypeScript SDK | Base URL, credential, typed requests | Typed results and structured HTTP errors |

## Layout and dependencies

The Go backend lives in cmd/lightmail and internal/sender. bbolt supplies
durability and transaction exclusivity; go-msgauth supplies DKIM; go-message
supplies MIME construction. All are permissively licensed. Transport uses Go's
standard SMTP, TLS, DNS, and HTTP implementations. No external queue, database,
identity service, or authoritative DNS server is required.

dashboard contains the Svelte application and its independent Go static server.
sdk contains a dependency-free TypeScript client. The independent
lightmail-uniac repository owns Uniac configuration and source pinning.

## Verification

Exercise the HTTP admission and authorization boundary, DKIM DNS verification,
idempotency, durable restart recovery, per-recipient retry/cancel behavior, MIME
and signatures, and direct SMTP responses using local fixtures. Exercise the
SDK against the HTTP contract and the actual backend. Build both containers,
check the dashboard in a browser at desktop and mobile widths, and measure
idle process memory and CPU. Live internet delivery requires the operator's
domain DNS, SMTP egress, and intended sending IP/PTR configuration.

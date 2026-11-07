# HTTP API

The backend serves /v1. The dashboard reverse proxy exposes it under /api/v1.
Use HTTPS for public access.

## Authentication

Send **Authorization: Bearer TOKEN**. The administrator token can manage the
installation. Sending keys can submit mail, list and retrieve their own
messages, retry failed recipients, and cancel their queued recipients.
Domain-restricted keys additionally constrain the From domain.

Browser sign-in uses POST /v1/session with a JSON token field. It sets an
HttpOnly, SameSite=Strict session cookie, Secure when accessed through HTTPS.
DELETE /v1/session revokes the current session. Browser session mutations
must be same-origin.

## Resources

| Method | Path | Access | Body / result |
| --- | --- | --- | --- |
| GET | /healthz | Public | Process health |
| GET | /v1/status | Administrator | Configuration, counts, heap and store size |
| GET | /v1/domains | Administrator | Domain array |
| POST | /v1/domains | Administrator | name → domain with DNS records |
| GET | /v1/domains/:id | Administrator | Domain with DNS records |
| POST | /v1/domains/:id/verify | Administrator | Current verification result |
| DELETE | /v1/domains/:id | Administrator | deleted; refuses while queued/in flight |
| GET | /v1/api-keys | Administrator | Key metadata; no token or hash |
| POST | /v1/api-keys | Administrator | name, optional domain_id → key and token |
| DELETE | /v1/api-keys/:id | Administrator | Revoke a key |
| POST | /v1/emails | Administrator or sending key | Message → durable email, HTTP 202 |
| GET | /v1/emails | Administrator or sending key | Lightweight paginated message summaries |
| GET | /v1/emails/:id | Administrator or owning key | Full message, recipients and attempts |
| POST | /v1/emails/:id/retry | Administrator or owning key | Retry failed recipients only |
| POST | /v1/emails/:id/cancel | Administrator or owning key | Cancel queued recipients only |

Normal success responses contain a data property. Errors contain an error
object with code and message, plus a corresponding non-2xx HTTP status.
Health returns a top-level status property.

Example request:

    POST /v1/emails
    Authorization: Bearer sk_...
    Content-Type: application/json
    Idempotency-Key: welcome-123

    {
      "from": "Example <hello@example.com>",
      "to": ["one@example.net"],
      "cc": ["copy@example.org"],
      "bcc": ["hidden@example.org"],
      "reply_to": "support@example.com",
      "subject": "Welcome",
      "text": "Hello",
      "html": "<p>Hello</p>",
      "attachments": [{
        "filename": "hello.txt",
        "content": "SGVsbG8=",
        "content_type": "text/plain"
      }]
    }

The HTTP API requires recipient arrays. The TypeScript SDK also accepts single
addresses and converts them to arrays. At least one To recipient is required.
There can be at most 50 unique delivery recipients across To, CC and BCC
(the request's combined array lengths are limited to 50). Duplicate envelope
addresses are delivered once. BCC is not included in MIME headers.

At least text or html is required. Decoded bodies plus attachments may total
at most 10 MiB. JSON requests may not exceed 15 MiB. At most 10 attachments
are accepted. Attachments use base64 content.

The Idempotency-Key header accepts at most 256 printable bytes. Admission keys
are scoped to the credential and retained for 24 hours. Identical content
returns the existing email; different content returns HTTP 409.

## Pagination

GET /v1/emails accepts limit (1–100, default 50), after (last email ID from a
previous page), and status. Results are newest first:

    {
      "data": [],
      "has_more": false,
      "next_cursor": ""
    }

List items omit text, html, and attachments. Retrieve an individual message
for its full content.

## States

Each recipient is queued, sending, accepted, failed, or canceled. Email status
is derived from recipients: sending while any is in flight, queued while any
waits, accepted when all were accepted, partially_accepted for mixed terminal
outcomes including acceptance, failed if a terminal failure remains without
acceptance, otherwise canceled.

Transient failures use delays of 1 minute, 5 minutes, 15 minutes, 1 hour,
4 hours, and 12 hours, bounded by the message's 48-hour automatic retry window.
An explicit retry admits failed recipients again; accepted recipients remain
untouched. Message-ID is stable across attempts.

The service observes SMTP acceptance and synchronous failures. It does not
claim inbox placement or process asynchronous delivery-status messages.

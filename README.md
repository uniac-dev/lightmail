# LightMail

A small, self-hosted email sending service with a dashboard and a TypeScript
SDK. MIT licensed.

LightMail delivers directly to recipient mail servers. Add a domain, publish
its signing record, create a sending key, and send through your own installation.

## What runs

- **Backend:** one Go process, an embedded bbolt database, and a durable recipient
  queue. It sleeps until there is a message or a scheduled retry.
- **Dashboard:** a compiled Svelte application served by a separate Go process.
  It proxies the backend API under the same origin. There is no Node.js runtime
  in either production container.
- **TypeScript SDK:** typed sending and management operations, structured errors,
  binary attachments, cancellation, pagination, and idempotency keys. No runtime
  dependencies.

There are no subscriptions, organizations, billing, contact lists, or campaigns.
This is a single-installation sender. The administrator token manages the
installation; sending keys are restricted to their own messages and can also be
restricted to a specific domain.

The measured empty idle installation used about 5.6 MiB RSS for the backend
and 4.8 MiB for the dashboard server on macOS arm64. See
[methodology and limits](docs/performance.md).

## Start with Docker

Requirements: Docker Compose with BuildKit, a domain you can edit, a sending server with
outbound TCP access to recipients on destination port 25, and appropriate
forward/reverse DNS for its sending IP. Configure a stable hostname and PTR
through the provider controlling that IP. LightMail cannot change those
network permissions.

    git clone https://github.com/uniac-dev/lightmail.git
    cd lightmail
    export SENDER_ADMIN_TOKEN="$(openssl rand -hex 32)"
    export SENDER_HOSTNAME="mail.example.com"
    export SENDER_PUBLIC_IP="YOUR_SERVER_PUBLIC_IP"
    docker compose up -d --build

Open <http://localhost:3000> and sign in with your administrator token. Keep that
token in your password manager. On a remote server, place the dashboard behind
an HTTPS reverse proxy or use an SSH tunnel. The Compose port is bound to
loopback by default; the backend has no published host port.

1. Open **Domains**, add your domain, and publish the DKIM TXT record shown.
2. Add the suggested SPF authorization to your existing SPF record and review
   your DMARC policy. Recommended records marked Present are not full policy
   evaluations.
3. Select **Verify records**. DKIM must verify before messages can be queued.
4. Open **API Keys** and create a sending key. Copy it when it is shown.
5. Use the SDK or HTTP API, or select **Send email** in the dashboard.

## TypeScript

Install the release package:

    npm install https://github.com/uniac-dev/lightmail/releases/download/v0.2.0/uniac-dev-lightmail-0.2.0.tgz

Or build a package from the source checkout:

    npm ci
    npm run build --workspace sdk
    npm pack --workspace sdk
    # In your application:
    npm install /path/to/uniac-dev-lightmail-0.2.0.tgz

Usage:

    import { LightMail } from "@uniac-dev/lightmail";

    const sender = new LightMail(process.env.LIGHTMAIL_API_KEY!, {
      baseUrl: "https://mail.example.com/api",
    });

    const { data, error } = await sender.emails.send(
      {
        from: "Example <hello@example.com>",
        to: "person@example.net",
        subject: "Hello from your server",
        text: "Your message goes directly to the recipient's mail server.",
      },
      { idempotencyKey: "welcome-person-123" },
    );

    if (error) {
      console.error(error.code, error.message);
    } else {
      console.log(data.id, data.status);
    }

The dashboard origin uses the /api proxy prefix. A client reaching the backend
directly uses its origin without /api. See [SDK documentation](sdk/README.md)
and the [HTTP API](docs/api.md).

## Sending behavior

- Submission returns HTTP 202 after the message and recipients are committed.
- Each recipient has its own state and attempts. Retries never select a
  recipient already accepted by its mail server.
- Temporary failures receive backoff; permanent SMTP failures remain visible
  and can be retried explicitly.
- STARTTLS with certificate verification is required by default.
- **Accepted** means the recipient mail server accepted the SMTP DATA command.
  It does not establish inbox placement or later delivery.
- An interrupted connection can leave an ambiguous acceptance outcome. A retry
  can then duplicate a message, even with the same Message-ID. API idempotency
  prevents duplicate admission, not every possible SMTP duplicate.
- There is no incoming SMTP listener. Mailboxes, incoming messages, later
  bounces, open tracking, and click tracking are outside this application.
  Use a working return address with your existing receiving mail service.

## Configuration

| Environment variable | Default | Purpose |
| --- | --- | --- |
| SENDER_ADMIN_TOKEN | Required | At least 32 characters; full installation access |
| SENDER_HOSTNAME | mail.example.com | Hostname sent in SMTP EHLO and Message-ID |
| SENDER_PUBLIC_IP | Unset | Public sending IP used in SPF instructions |
| SENDER_ENVELOPE_FROM | Message From address | Optional working bounce address |
| SENDER_REQUIRE_TLS | true | Require certificate-verified STARTTLS |
| SENDER_DATA_DIR | ./data | Private persistent store directory |
| SENDER_ADDRESS | :8080 | Backend HTTP listener |
| SENDER_RETENTION_DAYS | 30 | Completed-message history retention |
| SENDER_API_URL | http://127.0.0.1:8080 | Dashboard server's backend origin |
| DASHBOARD_ADDRESS | :3000 | Dashboard listener; container uses :8080 |
| DASHBOARD_ROOT | ./dashboard/dist | Static dashboard build directory |

Store permissions are 0600, with a 0700 data directory. Treat the data volume as
sensitive: it contains message content and private DKIM keys. Sending tokens and
browser session tokens are stored as hashes. Back up the persistent volume
with the backend stopped, and restore it to one backend instance.

Completed history is pruned at startup and at most hourly on API activity.
Queued messages are retained until resolved. Freed database pages are reused;
deleting history does not necessarily shrink the database file immediately.

## Develop and verify

Use Go 1.27.3 and Node.js 22 for the same runtime versions as CI.

    npm ci
    npm run build
    npm run check
    npm test
    go test -race ./...
    (cd dashboard/server && go test -race ./...)

    export SENDER_ADMIN_TOKEN="$(openssl rand -hex 32)"
    export SENDER_HOSTNAME="mail.example.com"
    go run ./cmd/lightmail
    # In another terminal:
    npm run dev --workspace dashboard

The tests include real HTTP calls from the compiled TypeScript SDK, a local
SMTP fixture with cryptographic DKIM verification, recipient retry isolation,
restart recovery, and credential boundaries. They do not send internet email.

## Deploy on Uniac

The separate [lightmail-uniac](https://github.com/uniac-dev/lightmail-uniac)
repository owns the pinned application source and Uniac composition. The
backend runs as a singleton with a data volume; the dashboard runs independently
and exposes the same-origin API proxy.

See [design and contracts](docs/design.md) for module boundaries.

## License

[MIT](LICENSE). Dependency notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

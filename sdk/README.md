# LightMail TypeScript SDK

A dependency-free client for your self-hosted LightMail installation.
Requires Node.js 18+ or a browser with Fetch and AbortController.

Install the release tarball:

    npm install https://github.com/uniac-dev/lightmail/releases/download/v0.2.0/uniac-dev-lightmail-0.2.0.tgz

Build with **npm ci && npm run build --workspace sdk** from the repository root.
Run **npm pack --workspace sdk** and install the generated tarball in your app.
The package is named **@uniac-dev/lightmail**; this repository supplies its
source and build, not an npm registry publication.

    import { LightMail } from "@uniac-dev/lightmail";
    const sender = new LightMail(process.env.LIGHTMAIL_API_KEY!, {
      baseUrl: "https://your-dashboard.example.com/api",
      timeoutMs: 30_000,
    });

Every operation returns a discriminated **{ data, error }** result. HTTP errors
include code, message, and status. Canceled, timed-out, and unreachable
requests return status 0. The client does not automatically repeat requests.

## Sending

    const abortController = new AbortController();
    const result = await sender.emails.send({
      from: "Example <hello@example.com>",
      to: ["one@example.net", "two@example.net"],
      cc: "copy@example.org",
      bcc: "hidden@example.org",
      reply_to: "support@example.com",
      subject: "A document",
      text: "See the attachment.",
      html: "<p>See the attachment.</p>",
      attachments: [{
        filename: "hello.txt",
        content: new TextEncoder().encode("Hello"),
        content_type: "text/plain",
      }],
    }, {
      idempotencyKey: "document-123",
      signal: abortController.signal,
    });

Attachment content accepts Uint8Array or a base64 string. Recipients accept one
address or an array. BCC addresses are excluded from the message headers.

Use the same idempotency key when retrying an ambiguous submission. The server
retains the admission key for 24 hours and rejects different content with the
same key. This is separate from SMTP's potential duplicate delivery after a
connection interruption.

    await sender.emails.get("em_...");
    const page = await sender.emails.list({ limit: 50, status: "failed" });
    if (page.data?.has_more) {
      await sender.emails.list({ after: page.data.next_cursor });
    }
    await sender.emails.retry("em_...");  // failed recipients only
    await sender.emails.cancel("em_..."); // queued recipients only

Sending keys can read only messages they admitted. Lists contain lightweight
metadata; retrieve an individual message for its body and attachments.

## Administration

Use the administrator token in a server-side client for these operations:

    const admin = new LightMail(process.env.LIGHTMAIL_ADMIN_TOKEN!, {
      baseUrl: "https://your-dashboard.example.com/api",
    });
    await admin.domains.create({ name: "example.com" });
    await admin.domains.list();
    await admin.domains.get("dom_...");
    await admin.domains.verify("dom_...");
    await admin.domains.remove("dom_...");
    await admin.apiKeys.create({ name: "Production app", domain_id: "dom_..." });
    await admin.apiKeys.list();
    await admin.apiKeys.revoke("key_...");
    await admin.system.status();

The raw sending token is returned only when the key is created. An empty
client key uses the browser's same-origin session cookie, as the dashboard does.
Keep administrator and sending tokens out of public frontend bundles.

import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { MailSender } from "../dist/index.js";

async function fixture(t, handler) {
  const server = createServer(handler);
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  t.after(async () => {
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  });
  return "http://127.0.0.1:" + server.address().port;
}

test("sends typed content, binary attachments, authentication and idempotency", async t => {
  let received;
  const baseUrl = await fixture(t, async (req, res) => {
    let raw = ""; for await (const chunk of req) raw += chunk;
    received = { path: req.url, headers: req.headers, body: JSON.parse(raw) };
    res.writeHead(202, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ data: { id: "em_test", status: "queued" } }));
  });
  const client = new MailSender("test-key", { baseUrl });
  const result = await client.emails.send({
    from: "hello@example.com", to: "person@example.net", subject: "Hello", text: "Body",
    attachments: [{ filename: "example.txt", content: new Uint8Array([72, 105]) }],
  }, { idempotencyKey: "one-send" });
  assert.equal(result.error, null);
  assert.equal(result.data.id, "em_test");
  assert.equal(received.path, "/v1/emails");
  assert.equal(received.headers.authorization, "Bearer test-key");
  assert.equal(received.headers["idempotency-key"], "one-send");
  assert.deepEqual(received.body.to, ["person@example.net"]);
  assert.equal(received.body.attachments[0].content, "SGk=");
});

test("preserves structured errors without exposing the credential", async t => {
  const baseUrl = await fixture(t, (req, res) => {
    res.writeHead(403, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: { code: "domain_not_allowed", message: "This key cannot send from that domain." } }));
  });
  const result = await new MailSender("credential-sentinel", { baseUrl }).domains.list();
  assert.equal(result.data, null);
  assert.deepEqual(result.error, { code: "domain_not_allowed", message: "This key cannot send from that domain.", status: 403 });
  assert.ok(!JSON.stringify(result).includes("credential-sentinel"));
});

test("binds native Fetch to its global receiver, as browsers require", async t => {
  const baseUrl = await fixture(t, (req, res) => {
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ data: { hostname: "mail.example.com" } }));
  });
  const nativeFetch = globalThis.fetch;
  const browserLikeFetch = function (...args) {
    assert.equal(this, globalThis, "native Fetch received the client instance as its receiver");
    return nativeFetch(...args);
  };
  const result = await new MailSender("key", { baseUrl, fetch: browserLikeFetch }).system.status();
  assert.equal(result.error, null);
  assert.equal(result.data.hostname, "mail.example.com");
});

test("handles pagination and encodes filters and resource identifiers", async t => {
  const paths = [];
  const baseUrl = await fixture(t, (req, res) => {
    paths.push(req.url);
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ data: [], has_more: true, next_cursor: "em_next" }));
  });
  const client = new MailSender("key", { baseUrl: baseUrl + "/" });
  const result = await client.emails.list({ limit: 10, after: "a&b", status: "queued" });
  assert.deepEqual(result.data, { items: [], has_more: true, next_cursor: "em_next" });
  assert.equal(paths[0], "/v1/emails?limit=10&after=a%26b&status=queued");
  await client.emails.get("identifier/with/slashes");
  assert.equal(paths[1], "/v1/emails/identifier%2Fwith%2Fslashes");
});

test("reports invalid success responses and canceled requests without retrying", async t => {
  let requests = 0;
  const baseUrl = await fixture(t, (req, res) => { requests++; res.end("not JSON"); });
  const result = await new MailSender("key", { baseUrl }).system.status();
  assert.equal(result.error.code, "invalid_response");
  assert.equal(requests, 1);
  const canceled = new AbortController(); canceled.abort();
  const abortResult = await new MailSender("key", { baseUrl }).emails.send({
    from: "a@example.com", to: "b@example.net", subject: "Test", text: "Body",
  }, { signal: canceled.signal });
  assert.equal(abortResult.error.code, "request_aborted");
  assert.equal(requests, 1);
});

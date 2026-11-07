export type EmailStatus =
  | "queued"
  | "sending"
  | "accepted"
  | "failed"
  | "partially_accepted"
  | "canceled";
export interface SenderError {
  code: string;
  message: string;
  status: number;
}
export type Result<T> =
  { data: T; error: null } | { data: null; error: SenderError };
export interface DNSRecord {
  type: "TXT";
  name: string;
  value: string;
  required: boolean;
  status: "pending" | "verified" | "present";
}
export interface Domain {
  id: string;
  name: string;
  selector: string;
  status: "pending" | "verified";
  created_at: string;
  verified_at?: string;
  records: DNSRecord[];
}
export interface APIKey {
  id: string;
  name: string;
  prefix: string;
  domain_id?: string;
  created_at: string;
  last_used_at?: string;
}
export interface Attachment {
  filename: string;
  content: string | Uint8Array;
  content_type?: string;
}
export interface SendEmail {
  from: string;
  to: string | string[];
  cc?: string | string[];
  bcc?: string | string[];
  subject: string;
  text?: string;
  html?: string;
  reply_to?: string;
  attachments?: Attachment[];
}
export interface Attempt {
  at: string;
  status: string;
  detail?: string;
}
export interface Recipient {
  address: string;
  status: Exclude<EmailStatus, "partially_accepted">;
  attempts: Attempt[];
  next_attempt: string;
  last_error?: string;
}
export interface Email {
  id: string;
  domain_id: string;
  created_at: string;
  updated_at: string;
  status: EmailStatus;
  message_id: string;
  message: SendEmail;
  recipients: Recipient[];
}
export interface Page<T> {
  items: T[];
  has_more: boolean;
  next_cursor: string;
}
export interface Status {
  version: string;
  hostname: string;
  public_ip: string;
  require_tls: boolean;
  envelope_from: string;
  retention_days: number;
  domains: number;
  api_keys: number;
  emails: Record<EmailStatus, number>;
  heap_bytes: number;
  database_bytes: number;
}
export interface RequestOptions {
  signal?: AbortSignal;
  idempotencyKey?: string;
}
export interface ClientOptions {
  baseUrl: string;
  fetch?: typeof globalThis.fetch;
  timeoutMs?: number;
}

function query(input: Record<string, string | number | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(input))
    if (value !== undefined) params.set(key, String(value));
  const value = params.toString();
  return value ? "?" + value : "";
}
function addresses(value: string | string[] | undefined): string[] | undefined {
  return value === undefined
    ? undefined
    : typeof value === "string"
      ? [value]
      : value;
}
function base64(value: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < value.length; i += 8192)
    binary += String.fromCharCode(...value.subarray(i, i + 8192));
  return btoa(binary);
}

/** A self-hosted client. Sending keys are scoped by the server; administrator
 * operations require its administrator token. An empty key uses browser cookies. */
export class LightMail {
  private readonly baseUrl: string;
  private readonly transport: typeof globalThis.fetch;
  private readonly timeoutMs: number;
  constructor(
    private readonly apiKey: string,
    options: ClientOptions,
  ) {
    this.baseUrl = options.baseUrl.replace(/\/+$/, "");
    this.transport = (options.fetch ?? globalThis.fetch).bind(globalThis);
    this.timeoutMs = options.timeoutMs ?? 30_000;
    if (!this.baseUrl) throw new Error("A self-hosted baseUrl is required.");
    if (!Number.isFinite(this.timeoutMs) || this.timeoutMs <= 0)
      throw new Error("timeoutMs must be positive.");
  }

  private async request<T>(
    path: string,
    method = "GET",
    body?: unknown,
    options: RequestOptions = {},
    paged = false,
  ): Promise<Result<T>> {
    const controller = new AbortController();
    const abort = () => controller.abort(options.signal?.reason);
    if (options.signal?.aborted) abort();
    options.signal?.addEventListener("abort", abort, { once: true });
    const timer = setTimeout(
      () => controller.abort(new Error("Request timed out.")),
      this.timeoutMs,
    );
    try {
      const headers = new Headers({ Accept: "application/json" });
      if (this.apiKey) headers.set("Authorization", "Bearer " + this.apiKey);
      if (body !== undefined) headers.set("Content-Type", "application/json");
      if (options.idempotencyKey)
        headers.set("Idempotency-Key", options.idempotencyKey);
      const response = await this.transport(this.baseUrl + path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: controller.signal,
      });
      const payload: unknown = await response.json().catch(() => null);
      if (!response.ok) {
        const problem =
          payload && typeof payload === "object" && "error" in payload
            ? (payload.error as { code?: string; message?: string })
            : undefined;
        return {
          data: null,
          error: {
            code: problem?.code ?? "http_error",
            message: problem?.message ?? "The server refused the request.",
            status: response.status,
          },
        };
      }
      if (!payload || typeof payload !== "object" || !("data" in payload)) {
        return {
          data: null,
          error: {
            code: "invalid_response",
            message: "The server did not return a LightMail API response.",
            status: response.status,
          },
        };
      }
      const raw = payload as {
        data: unknown;
        has_more?: boolean;
        next_cursor?: string;
      };
      const data = paged
        ? {
            items: raw.data,
            has_more: raw.has_more ?? false,
            next_cursor: raw.next_cursor ?? "",
          }
        : raw.data;
      return { data: data as T, error: null };
    } catch {
      return {
        data: null,
        error: {
          code: controller.signal.aborted ? "request_aborted" : "network_error",
          message: controller.signal.aborted
            ? "The request was canceled or timed out. Use the same idempotency key when retrying a send."
            : "Could not reach the LightMail API.",
          status: 0,
        },
      };
    } finally {
      clearTimeout(timer);
      options.signal?.removeEventListener("abort", abort);
    }
  }

  readonly emails = {
    send: (
      email: SendEmail,
      options?: RequestOptions,
    ): Promise<Result<Email>> =>
      this.request(
        "/v1/emails",
        "POST",
        {
          ...email,
          to: addresses(email.to),
          cc: addresses(email.cc),
          bcc: addresses(email.bcc),
          attachments: email.attachments?.map((a) => ({
            ...a,
            content:
              typeof a.content === "string" ? a.content : base64(a.content),
          })),
        },
        options,
      ),
    get: (id: string, options?: RequestOptions): Promise<Result<Email>> =>
      this.request(
        "/v1/emails/" + encodeURIComponent(id),
        "GET",
        undefined,
        options,
      ),
    list: (
      filter: { limit?: number; after?: string; status?: EmailStatus } = {},
      options?: RequestOptions,
    ): Promise<Result<Page<Email>>> =>
      this.request(
        "/v1/emails" + query(filter),
        "GET",
        undefined,
        options,
        true,
      ),
    retry: (id: string, options?: RequestOptions): Promise<Result<Email>> =>
      this.request(
        "/v1/emails/" + encodeURIComponent(id) + "/retry",
        "POST",
        undefined,
        options,
      ),
    cancel: (id: string, options?: RequestOptions): Promise<Result<Email>> =>
      this.request(
        "/v1/emails/" + encodeURIComponent(id) + "/cancel",
        "POST",
        undefined,
        options,
      ),
  };
  readonly domains = {
    list: (options?: RequestOptions): Promise<Result<Domain[]>> =>
      this.request("/v1/domains", "GET", undefined, options),
    get: (id: string, options?: RequestOptions): Promise<Result<Domain>> =>
      this.request(
        "/v1/domains/" + encodeURIComponent(id),
        "GET",
        undefined,
        options,
      ),
    create: (
      input: { name: string },
      options?: RequestOptions,
    ): Promise<Result<Domain>> =>
      this.request("/v1/domains", "POST", input, options),
    verify: (id: string, options?: RequestOptions): Promise<Result<Domain>> =>
      this.request(
        "/v1/domains/" + encodeURIComponent(id) + "/verify",
        "POST",
        undefined,
        options,
      ),
    remove: (
      id: string,
      options?: RequestOptions,
    ): Promise<Result<{ deleted: boolean }>> =>
      this.request(
        "/v1/domains/" + encodeURIComponent(id),
        "DELETE",
        undefined,
        options,
      ),
  };
  readonly apiKeys = {
    list: (options?: RequestOptions): Promise<Result<APIKey[]>> =>
      this.request("/v1/api-keys", "GET", undefined, options),
    create: (
      input: { name: string; domain_id?: string },
      options?: RequestOptions,
    ): Promise<Result<{ key: APIKey; token: string }>> =>
      this.request("/v1/api-keys", "POST", input, options),
    revoke: (
      id: string,
      options?: RequestOptions,
    ): Promise<Result<{ deleted: boolean }>> =>
      this.request(
        "/v1/api-keys/" + encodeURIComponent(id),
        "DELETE",
        undefined,
        options,
      ),
  };
  readonly system = {
    status: (options?: RequestOptions): Promise<Result<Status>> =>
      this.request("/v1/status", "GET", undefined, options),
  };
}

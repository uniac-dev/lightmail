import { MailSender } from "../../sdk/src/index";
import type {
  APIKey,
  Domain,
  Email,
  Result,
  Status,
} from "../../sdk/src/index";

function requireData<T>(result: Result<T>): T {
  if (result.error)
    throw Object.assign(new Error(result.error.message), {
      status: result.error.status,
    });
  return result.data;
}
export type Page = "emails" | "domains" | "keys" | "settings" | "guide";
export type Modal =
  | "domain"
  | "key"
  | "compose"
  | "domain-detail"
  | "email-detail"
  | "confirm"
  | null;

export class Dashboard {
  readonly api = new MailSender("", { baseUrl: "/api" });
  authenticated = $state(false);
  loading = $state(true);
  busy = $state(false);
  page = $state<Page>("emails");
  modal = $state<Modal>(null);
  error = $state("");
  notice = $state("");
  loginToken = $state("");
  domains = $state<Domain[]>([]);
  emails = $state<Email[]>([]);
  keys = $state<APIKey[]>([]);
  system = $state<Status | null>(null);
  selectedDomain = $state<Domain | null>(null);
  selectedEmail = $state<Email | null>(null);
  search = $state("");
  filter = $state("");
  nextCursor = $state("");
  hasMore = $state(false);
  domainName = $state("");
  keyName = $state("");
  keyDomain = $state("");
  createdToken = $state("");
  draft = $state({ from: "", to: "", subject: "", text: "", html: "" });
  composeHTML = $state(false);
  confirmation = $state({
    title: "",
    detail: "",
    label: "",
    action: async () => {},
  });
  private noticeTimer?: ReturnType<typeof setTimeout>;
  private draftSignature = "";
  private draftIdempotency = "";

  mount() {
    const readPage = () => {
      const page = location.hash.slice(1);
      if (["emails", "domains", "keys", "settings", "guide"].includes(page))
        this.page = page as Page;
    };
    readPage();
    addEventListener("hashchange", readPage);
    void this.initialize();
    return () => {
      removeEventListener("hashchange", readPage);
      clearTimeout(this.noticeTimer);
    };
  }

  async initialize() {
    try {
      const status = await this.api.system.status();
      if (status.error?.status === 401) return;
      this.system = requireData(status);
      this.authenticated = true;
      await this.refresh();
    } catch (e) {
      this.fail(e);
    } finally {
      this.loading = false;
    }
  }
  private fail(e: unknown) {
    if (e && typeof e === "object" && "status" in e && e.status === 401)
      this.authenticated = false;
    this.error =
      e instanceof Error ? e.message : "The operation could not be completed.";
  }
  flash(message: string) {
    this.notice = message;
    clearTimeout(this.noticeTimer);
    this.noticeTimer = setTimeout(() => (this.notice = ""), 4000);
  }
  async run(action: () => Promise<void>) {
    if (this.busy) return;
    this.busy = true;
    this.error = "";
    try {
      await action();
    } catch (e) {
      this.fail(e);
    } finally {
      this.busy = false;
    }
  }
  async login() {
    await this.run(async () => {
      const response = await fetch("/api/v1/session", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token: this.loginToken }),
      });
      const result = await response.json();
      if (!response.ok)
        throw new Error(result.error?.message ?? "Sign-in failed.");
      this.loginToken = "";
      this.authenticated = true;
      await this.refresh();
    });
  }
  async logout() {
    await this.run(async () => {
      const response = await fetch("/api/v1/session", { method: "DELETE" });
      if (!response.ok) throw new Error("Sign-out failed.");
      this.authenticated = false;
      this.emails = [];
      this.domains = [];
      this.keys = [];
      this.selectedEmail = null;
      this.selectedDomain = null;
      this.modal = null;
    });
  }
  async refresh() {
    const [domains, emails, keys, status] = await Promise.all([
      this.api.domains.list(),
      this.api.emails.list({ limit: 50 }),
      this.api.apiKeys.list(),
      this.api.system.status(),
    ]);
    this.domains = requireData(domains);
    this.keys = requireData(keys);
    this.system = requireData(status);
    const page = requireData(emails);
    this.emails = page.items;
    this.hasMore = page.has_more;
    this.nextCursor = page.next_cursor;
  }
  navigate(page: Page) {
    this.page = page;
    location.hash = page;
    this.search = "";
    this.filter = "";
    this.error = "";
    this.close();
  }
  close() {
    this.modal = null;
    this.createdToken = "";
  }
  open(modal: Modal) {
    this.error = "";
    this.createdToken = "";
    this.modal = modal;
    if (modal === "domain") this.domainName = "";
    if (modal === "key") {
      this.keyName = "";
      this.keyDomain = "";
    }
    if (modal === "compose") {
      this.draftSignature = "";
      this.draftIdempotency = "";
      const domain = this.domains.find((d) => d.status === "verified");
      this.draft = {
        from: domain ? "hello@" + domain.name : "",
        to: "",
        subject: "",
        text: "",
        html: "",
      };
    }
  }
  async addDomain() {
    await this.run(async () => {
      const domain = requireData(
        await this.api.domains.create({ name: this.domainName }),
      );
      this.domains = [domain, ...this.domains];
      this.selectedDomain = domain;
      this.modal = "domain-detail";
      this.page = "domains";
      this.flash("Domain added");
    });
  }
  showDomain(domain: Domain) {
    this.selectedDomain = domain;
    this.error = "";
    this.modal = "domain-detail";
  }
  async verifyDomain() {
    if (!this.selectedDomain) return;
    await this.run(async () => {
      const domain = requireData(
        await this.api.domains.verify(this.selectedDomain!.id),
      );
      this.selectedDomain = domain;
      this.domains = this.domains.map((d) => (d.id === domain.id ? domain : d));
      this.flash(
        domain.status === "verified"
          ? "Domain verified"
          : "DNS checked. The signing record is not verified yet.",
      );
    });
  }
  removeDomain(domain: Domain) {
    this.confirmation = {
      title: "Remove " + domain.name + "?",
      detail:
        "Messages already accepted by recipient servers stay in your history. Queued messages must be canceled first.",
      label: "Remove domain",
      action: async () => {
        requireData(await this.api.domains.remove(domain.id));
        this.close();
        await this.refresh();
        this.flash("Domain removed");
      },
    };
    this.modal = "confirm";
  }
  async createKey() {
    await this.run(async () => {
      const result = requireData(
        await this.api.apiKeys.create({
          name: this.keyName,
          domain_id: this.keyDomain || undefined,
        }),
      );
      this.createdToken = result.token;
      this.keys = [result.key, ...this.keys];
      this.flash("API key created");
    });
  }
  revokeKey(key: APIKey) {
    this.confirmation = {
      title: "Revoke " + key.name + "?",
      detail:
        "Applications using this key will lose API access. Already queued messages continue sending.",
      label: "Revoke key",
      action: async () => {
        requireData(await this.api.apiKeys.revoke(key.id));
        this.close();
        await this.refresh();
        this.flash("API key revoked");
      },
    };
    this.modal = "confirm";
  }
  async sendEmail() {
    await this.run(async () => {
      const input = {
        from: this.draft.from,
        to: this.draft.to
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        subject: this.draft.subject,
        text: this.composeHTML ? undefined : this.draft.text,
        html: this.composeHTML ? this.draft.html : undefined,
      };
      const signature = JSON.stringify(input);
      if (signature !== this.draftSignature) {
        this.draftSignature = signature;
        this.draftIdempotency = crypto.randomUUID();
      }
      const email = requireData(
        await this.api.emails.send(input, {
          idempotencyKey: this.draftIdempotency,
        }),
      );
      this.selectedEmail = email;
      this.modal = "email-detail";
      this.page = "emails";
      await this.refresh();
      this.flash("Email queued");
    });
  }
  async showEmail(email: Email) {
    this.selectedEmail = email;
    this.modal = "email-detail";
    this.error = "";
    await this.run(async () => {
      this.selectedEmail = requireData(await this.api.emails.get(email.id));
    });
  }
  async reloadEmail() {
    if (!this.selectedEmail) return;
    await this.run(async () => {
      this.selectedEmail = requireData(
        await this.api.emails.get(this.selectedEmail!.id),
      );
      await this.refresh();
    });
  }
  async changeEmail(action: "retry" | "cancel") {
    if (!this.selectedEmail) return;
    await this.run(async () => {
      this.selectedEmail = requireData(
        await this.api.emails[action](this.selectedEmail!.id),
      );
      await this.refresh();
      this.flash(
        action === "retry"
          ? "Failed recipients queued for retry"
          : "Queued recipients canceled",
      );
    });
  }
  async loadMore() {
    await this.run(async () => {
      const page = requireData(
        await this.api.emails.list({ limit: 50, after: this.nextCursor }),
      );
      this.emails = [...this.emails, ...page.items];
      this.hasMore = page.has_more;
      this.nextCursor = page.next_cursor;
    });
  }
  async copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      this.flash("Copied to clipboard");
    } catch {
      this.error =
        "Clipboard access is unavailable. Select and copy the value.";
    }
  }
}

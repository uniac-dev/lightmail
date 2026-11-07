<script lang="ts">
  import { onMount } from "svelte";
  import {
    Send,
    Mail,
    Globe,
    KeyRound,
    Settings,
    Plus,
    Search,
    RefreshCw,
    Copy,
    Check,
    ChevronRight,
    X,
    Trash2,
    LogOut,
    BookOpen,
    ArrowUpRight,
    Terminal,
    ShieldCheck,
    Activity,
    LoaderCircle,
    Menu,
    CircleHelp,
  } from "@lucide/svelte";
  import { Dashboard } from "./control.svelte";
  import type { EmailStatus } from "../../sdk/src/index";

  const c = new Dashboard();
  let dialog: HTMLDialogElement;
  let sidebar = $state(false);
  let detailTab = $state<"preview" | "text" | "json">("preview");
  const titles = {
    emails: "Emails",
    domains: "Domains",
    keys: "API Keys",
    settings: "Settings",
    guide: "Quickstart",
  };
  const navigation = [
    { id: "emails" as const, title: "Emails", icon: Mail },
    { id: "domains" as const, title: "Domains", icon: Globe },
    { id: "keys" as const, title: "API Keys", icon: KeyRound },
  ];
  const labels: Record<EmailStatus, string> = {
    queued: "Queued",
    sending: "Sending",
    accepted: "Accepted",
    failed: "Failed",
    partially_accepted: "Partially accepted",
    canceled: "Canceled",
  };
  const date = (value: string) =>
    new Date(value).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    });
  const bytes = (value: number) =>
    value >= 1024 * 1024
      ? (value / 1024 / 1024).toFixed(1) + " MiB"
      : (value / 1024).toFixed(0) + " KiB";
  const recipients = (value: string | string[]) =>
    Array.isArray(value) ? value.join(", ") : value;
  const preview = (value: string) =>
    "<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; img-src data:\">" +
    value;
  let visibleEmails = $derived(
    c.emails.filter(
      (e) =>
        (!c.filter || e.status === c.filter) &&
        [e.message.subject, e.message.from, recipients(e.message.to)]
          .join(" ")
          .toLowerCase()
          .includes(c.search.toLowerCase()),
    ),
  );
  let visibleDomains = $derived(
    c.domains.filter((d) => d.name.includes(c.search.toLowerCase())),
  );
  let total = $derived(
    c.system ? Object.values(c.system.emails).reduce((a, b) => a + b, 0) : 0,
  );
  onMount(() => {
    return c.mount();
  });
  $effect(() => {
    if (!dialog) return;
    if (c.modal && !dialog.open) dialog.showModal();
    else if (!c.modal && dialog.open) dialog.close();
  });
  $effect(() => {
    if (c.selectedEmail?.id) detailTab = "preview";
  });
  function navigate(page: typeof c.page) {
    c.navigate(page);
    sidebar = false;
  }
  const quickstart =
    'import { MailSender } from "@uniac-dev/mail-sender";\n\nconst sender = new MailSender(process.env.MAIL_SENDER_API_KEY!, {\n  baseUrl: "https://mail.your-domain.com/api",\n});\n\nconst { data, error } = await sender.emails.send({\n  from: "Hello <hello@your-domain.com>",\n  to: "you@example.com",\n  subject: "Your first email",\n  text: "Sent from your own server.",\n}, { idempotencyKey: "welcome-123" });';
</script>

{#if c.loading}
  <div class="loading-screen">
    <div class="brand-mark"><Send size={22} /></div>
    <LoaderCircle size={20} class="spin" /><span>Connecting to Mail Sender</span
    >
  </div>
{:else if !c.authenticated}
  <main class="login">
    <div class="login-grid"></div>
    <form
      class="login-card"
      onsubmit={(e) => {
        e.preventDefault();
        void c.login();
      }}
    >
      <div class="brand-mark large"><Send size={27} /></div>
      <div class="eyebrow">YOUR SERVER. YOUR EMAIL.</div>
      <h1>Welcome to Mail Sender</h1>
      <p>Sign in to your installation.</p>
      {#if c.error}<div class="alert error" role="alert">{c.error}</div>{/if}
      <label for="admin-token">Administrator token</label>
      <input
        id="admin-token"
        type="password"
        bind:value={c.loginToken}
        placeholder="Enter your administrator token"
        autocomplete="current-password"
        required
        minlength="32"
      />
      <button class="primary full" disabled={c.busy}
        >{#if c.busy}<LoaderCircle size={15} class="spin" />{/if}Sign in <ArrowUpRight
          size={15}
        /></button
      >
      <div class="login-note">
        <ShieldCheck size={14} /> Your token stays with this installation.
      </div>
    </form>
    <div class="login-footer">Mail Sender <span>Open source · MIT</span></div>
  </main>
{:else}
  <div class="app-shell">
    <aside class:mobile-open={sidebar}>
      <a class="brand" href="#emails" onclick={() => navigate("emails")}
        ><span class="brand-mark"><Send size={19} /></span><span
          >Mail Sender</span
        ></a
      >
      <div class="installation">
        <span class="online-dot"></span><span
          >{c.system?.hostname ?? "Your installation"}</span
        ><span class="tag">Self-hosted</span>
      </div>
      <div class="nav-label">MAIL</div>
      <nav aria-label="Main navigation">
        {#each navigation as item}
          <button
            class:active={c.page === item.id}
            aria-current={c.page === item.id ? "page" : undefined}
            onclick={() => navigate(item.id)}
            ><item.icon
              size={17}
            />{item.title}{#if item.id === "domains" && c.domains.length}<span
                class="nav-count">{c.domains.length}</span
              >{/if}</button
          >
        {/each}
      </nav>
      <div class="sidebar-bottom">
        <button
          class:active={c.page === "guide"}
          onclick={() => navigate("guide")}
          ><BookOpen size={17} />Quickstart<ArrowUpRight
            size={13}
            class="muted right"
          /></button
        >
        <button
          class:active={c.page === "settings"}
          onclick={() => navigate("settings")}
          ><Settings size={17} />Settings</button
        >
        <div class="sidebar-rule"></div>
        <div class="profile">
          <span class="avatar">A</span>
          <div>
            <strong>Administrator</strong><span>Local installation</span>
          </div>
          <button
            class="icon-button"
            aria-label="Sign out"
            onclick={() => c.logout()}><LogOut size={15} /></button
          >
        </div>
      </div>
    </aside>
    {#if sidebar}<button
        class="mobile-scrim"
        aria-label="Close navigation"
        onclick={() => (sidebar = false)}
      ></button>{/if}
    <main class="workspace">
      <header class="topbar">
        <div class="breadcrumb">
          <button
            class="icon-button mobile-only"
            aria-label="Open navigation"
            onclick={() => (sidebar = true)}><Menu size={19} /></button
          ><span>Installation</span><ChevronRight size={13} /><strong
            >{titles[c.page]}</strong
          >
        </div>
        <a
          class="top-link"
          href="https://github.com/uniac-dev/mail-sender"
          target="_blank"
          rel="noreferrer">GitHub<ArrowUpRight size={13} /></a
        >
      </header>
      <div class="page">
        <div class="page-heading">
          <div>
            <h1>{titles[c.page]}</h1>
            <p>
              {c.page === "emails"
                ? "Your outgoing messages, from queue to acceptance."
                : c.page === "domains"
                  ? "Send email from domains you own."
                  : c.page === "keys"
                    ? "Connect your applications to this installation."
                    : c.page === "settings"
                      ? "Configuration and storage for this installation."
                      : "Send your first email with the TypeScript SDK."}
            </p>
          </div>
          <div class="actions">
            {#if ["emails", "domains", "keys"].includes(c.page)}<button
                class="icon-button bordered"
                aria-label="Refresh"
                disabled={c.busy}
                onclick={() => c.run(() => c.refresh())}
                ><RefreshCw size={16} class={c.busy ? "spin" : ""} /></button
              >{/if}
            {#if c.page === "emails"}<button
                class="primary"
                onclick={() => c.open("compose")}
                ><Plus size={16} />Send email</button
              >
            {:else if c.page === "domains"}<button
                class="primary"
                onclick={() => c.open("domain")}
                ><Plus size={16} />Add domain</button
              >
            {:else if c.page === "keys"}<button
                class="primary"
                onclick={() => c.open("key")}
                ><Plus size={16} />Create API key</button
              >{/if}
          </div>
        </div>
        {#if c.error && !c.modal}<div class="alert error" role="alert">
            {c.error}<button
              class="icon-button"
              aria-label="Dismiss error"
              onclick={() => (c.error = "")}><X size={14} /></button
            >
          </div>{/if}
        {#if c.page === "emails"}
          <div class="stats">
            <div>
              <span>Total emails</span><strong>{total.toLocaleString()}</strong
              ><span class="stat-note">Within retention period</span>
            </div>
            <div>
              <span><span class="status-dot accepted"></span>Accepted</span
              ><strong>{c.system?.emails.accepted ?? 0}</strong><span
                class="stat-note">By recipient mail servers</span
              >
            </div>
            <div>
              <span><span class="status-dot queued"></span>In queue</span
              ><strong
                >{(c.system?.emails.queued ?? 0) +
                  (c.system?.emails.sending ?? 0)}</strong
              ><span class="stat-note">Waiting or sending</span>
            </div>
            <div>
              <span><span class="status-dot failed"></span>Failed</span><strong
                >{(c.system?.emails.failed ?? 0) +
                  (c.system?.emails.partially_accepted ?? 0)}</strong
              ><span class="stat-note">At least one recipient failed</span>
            </div>
          </div>
          <div class="table-panel">
            <div class="table-toolbar">
              <div class="search">
                <Search size={15} /><input
                  aria-label="Search loaded emails"
                  placeholder="Search loaded emails…"
                  bind:value={c.search}
                />
              </div>
              <select aria-label="Filter email status" bind:value={c.filter}
                ><option value="">All statuses</option
                >{#each Object.entries(labels) as [status, label]}<option
                    value={status}>{label}</option
                  >{/each}</select
              >
            </div>
            {#if !c.emails.length}
              <div class="empty">
                <div class="empty-art">
                  <span></span>
                  <div><Mail size={28} strokeWidth={1.4} /></div>
                  <span></span>
                </div>
                <h2>Your first email starts here</h2>
                <p>Add a domain, create an API key, and send something good.</p>
                <div class="actions">
                  <button class="primary" onclick={() => navigate("domains")}
                    >Add your domain<ChevronRight size={15} /></button
                  ><button class="secondary" onclick={() => navigate("guide")}
                    ><Terminal size={15} />View quickstart</button
                  >
                </div>
                <div class="empty-steps">
                  <span><span>1</span>Add domain</span><ChevronRight
                    size={12}
                  /><span><span>2</span>Verify DNS</span><ChevronRight
                    size={12}
                  /><span><span>3</span>Send email</span>
                </div>
              </div>
            {:else}
              <div class="table-scroll">
                <table class="email-table">
                  <thead
                    ><tr
                      ><th>Recipient / Subject</th><th>From</th><th>Status</th
                      ><th>Created</th><th></th></tr
                    ></thead
                  ><tbody>
                    {#each visibleEmails as email}<tr
                        ><td
                          ><button
                            class="row-link"
                            onclick={() => c.showEmail(email)}
                            ><strong>{recipients(email.message.to)}</strong
                            ><span
                              >{email.message.subject || "(No subject)"}</span
                            ></button
                          ></td
                        ><td class="muted cell-truncate"
                          >{email.message.from}</td
                        ><td
                          ><span class="badge {email.status}"
                            ><span class="status-dot {email.status}"
                            ></span>{labels[email.status]}</span
                          ></td
                        ><td class="muted nowrap">{date(email.created_at)}</td
                        ><td
                          ><button
                            class="icon-button"
                            aria-label={"Open " + email.message.subject}
                            onclick={() => c.showEmail(email)}
                            ><ChevronRight size={15} /></button
                          ></td
                        ></tr
                      >{/each}
                  </tbody>
                </table>
              </div>
              {#if !visibleEmails.length}<div class="small-empty">
                  No messages match this filter.
                </div>{/if}
              <div class="table-footer">
                <span
                  >{visibleEmails.length} of {c.emails.length} loaded messages</span
                >{#if c.hasMore}<button
                    class="secondary small"
                    disabled={c.busy}
                    onclick={() => c.loadMore()}>Load more</button
                  >{/if}
              </div>
            {/if}
          </div>
        {:else if c.page === "domains"}
          <div class="table-panel">
            <div class="table-toolbar">
              <div class="search">
                <Search size={15} /><input
                  aria-label="Search domains"
                  placeholder="Search domains…"
                  bind:value={c.search}
                />
              </div>
              <span class="muted"
                >{c.domains.length} domain{c.domains.length === 1
                  ? ""
                  : "s"}</span
              >
            </div>
            {#if !c.domains.length}<div class="empty">
                <div class="empty-art">
                  <span></span>
                  <div><Globe size={28} strokeWidth={1.4} /></div>
                  <span></span>
                </div>
                <h2>Make it your own</h2>
                <p>Verify a domain to send from any address under it.</p>
                <button class="primary" onclick={() => c.open("domain")}
                  ><Plus size={15} />Add domain</button
                >
              </div>
            {:else}<div class="table-scroll">
                <table>
                  <thead
                    ><tr
                      ><th>Domain</th><th>Status</th><th>Created</th><th
                      ></th></tr
                    ></thead
                  ><tbody
                    >{#each visibleDomains as domain}<tr
                        ><td
                          ><button
                            class="domain-link"
                            onclick={() => c.showDomain(domain)}
                            ><span class="domain-icon"><Globe size={18} /></span
                            ><strong>{domain.name}</strong></button
                          ></td
                        ><td
                          ><span class="badge {domain.status}"
                            ><span class="status-dot {domain.status}"
                            ></span>{domain.status === "verified"
                              ? "Verified"
                              : "Pending verification"}</span
                          ></td
                        ><td class="muted">{date(domain.created_at)}</td><td
                          ><button
                            class="icon-button"
                            aria-label={"Configure " + domain.name}
                            onclick={() => c.showDomain(domain)}
                            ><ChevronRight size={15} /></button
                          ></td
                        ></tr
                      >{/each}</tbody
                  >
                </table>
              </div>{/if}
          </div>
          <div class="help-row">
            <CircleHelp size={15} /><span
              >Your existing inboxes and incoming mail routing stay with your
              mail provider.</span
            >
          </div>
        {:else if c.page === "keys"}
          <div class="table-panel">
            {#if !c.keys.length}<div class="empty">
                <div class="empty-art">
                  <span></span>
                  <div><KeyRound size={28} strokeWidth={1.4} /></div>
                  <span></span>
                </div>
                <h2>A key for every application</h2>
                <p>Create a sending key and connect your app.</p>
                <button class="primary" onclick={() => c.open("key")}
                  ><Plus size={15} />Create API key</button
                >
              </div>
            {:else}<div class="table-scroll">
                <table>
                  <thead
                    ><tr
                      ><th>Name</th><th>Token</th><th>Domain access</th><th
                        >Created</th
                      ><th></th></tr
                    ></thead
                  ><tbody
                    >{#each c.keys as key}<tr
                        ><td
                          ><div class="name-with-icon">
                            <KeyRound size={16} /><strong>{key.name}</strong>
                          </div></td
                        ><td><code>{key.prefix}••••••••</code></td><td
                          class="muted"
                          >{key.domain_id
                            ? (c.domains.find((d) => d.id === key.domain_id)
                                ?.name ?? "Removed domain")
                            : "All verified domains"}</td
                        ><td class="muted nowrap">{date(key.created_at)}</td><td
                          ><button
                            class="icon-button danger"
                            aria-label={"Revoke " + key.name}
                            onclick={() => c.revokeKey(key)}
                            ><Trash2 size={15} /></button
                          ></td
                        ></tr
                      >{/each}</tbody
                  >
                </table>
              </div>{/if}
          </div>
          <div class="help-row">
            <ShieldCheck size={15} /><span
              >Sending keys can access only their own messages. Administrator
              access stays separate.</span
            >
          </div>
        {:else if c.page === "settings" && c.system}
          <section class="settings-panel">
            <div class="section-heading">
              <Activity size={18} />
              <h2>Installation</h2>
              <span class="tag right">v{c.system.version}</span>
            </div>
            <dl class="settings-list">
              <div>
                <dt>Sending hostname</dt>
                <dd><code>{c.system.hostname}</code></dd>
              </div>
              <div>
                <dt>Public sending IP</dt>
                <dd>{c.system.public_ip || "Not configured"}</dd>
              </div>
              <div>
                <dt>Transport security</dt>
                <dd>
                  <span
                    class="badge {c.system.require_tls
                      ? 'verified'
                      : 'pending'}"
                    >{c.system.require_tls
                      ? "Verified STARTTLS required"
                      : "Opportunistic STARTTLS"}</span
                  >
                </dd>
              </div>
              <div>
                <dt>Envelope sender</dt>
                <dd>{c.system.envelope_from || "Same as From address"}</dd>
              </div>
              <div>
                <dt>Message retention</dt>
                <dd>{c.system.retention_days} days</dd>
              </div>
              <div>
                <dt>Go heap</dt>
                <dd>{bytes(c.system.heap_bytes)}</dd>
              </div>
              <div>
                <dt>Database file</dt>
                <dd>{bytes(c.system.database_bytes)}</dd>
              </div>
            </dl>
          </section>
          <div class="info-card">
            <ShieldCheck size={19} />
            <div>
              <h3>Configuration belongs to your server</h3>
              <p>
                Set the sending hostname, public IP, administrator token, and
                retention through the backend environment. Restart the backend
                to apply changes.
              </p>
            </div>
          </div>
        {:else if c.page === "guide"}
          <div class="guide-grid">
            <section class="settings-panel">
              <div class="section-heading">
                <Terminal size={18} />
                <h2>TypeScript quickstart</h2>
                <button
                  class="icon-button right"
                  aria-label="Copy TypeScript example"
                  onclick={() => c.copy(quickstart)}><Copy size={15} /></button
                >
              </div>
              <pre class="code-example"><code>{quickstart}</code></pre>
            </section>
            <div class="guide-notes">
              <div>
                <span class="step-number">1</span>
                <h3>Verify your domain</h3>
                <p>
                  Publish the DKIM record from Domains. Your signing key stays
                  on your server.
                </p>
                <button class="text-button" onclick={() => navigate("domains")}
                  >Open domains<ArrowUpRight size={14} /></button
                >
              </div>
              <div>
                <span class="step-number">2</span>
                <h3>Create a sending key</h3>
                <p>
                  Use a separate key for each application, with optional domain
                  restrictions.
                </p>
                <button class="text-button" onclick={() => navigate("keys")}
                  >Open API keys<ArrowUpRight size={14} /></button
                >
              </div>
              <div>
                <span class="step-number">3</span>
                <h3>Follow the message</h3>
                <p>
                  Queued messages are durable. Accepted means the recipient's
                  server accepted the email.
                </p>
                <a
                  class="text-button"
                  href="https://github.com/uniac-dev/mail-sender/tree/main/sdk"
                  target="_blank"
                  rel="noreferrer">SDK installation<ArrowUpRight size={14} /></a
                >
              </div>
            </div>
          </div>
        {/if}
        <footer class="page-footer">
          <span
            ><span class="online-dot"></span>Your infrastructure. Your messages.</span
          ><span>Mail Sender · MIT</span>
        </footer>
      </div>
    </main>
  </div>
{/if}

<dialog
  bind:this={dialog}
  class:wide={c.modal === "domain-detail" || c.modal === "email-detail"}
  oncancel={() => c.close()}
  onclose={() => {
    if (c.modal) c.close();
  }}
>
  <div class="dialog-inner">
    <button
      class="icon-button dialog-close"
      aria-label="Close dialog"
      onclick={() => c.close()}><X size={18} /></button
    >
    {#if c.error && c.modal}<div class="alert error" role="alert">
        {c.error}
      </div>{/if}
    {#if c.modal === "domain"}
      <form
        onsubmit={(e) => {
          e.preventDefault();
          void c.addDomain();
        }}
      >
        <span class="modal-icon"><Globe size={21} /></span>
        <h2>Add a domain</h2>
        <p class="modal-description">The domain you want to send email from.</p>
        <label for="domain-name">Domain name</label><input
          id="domain-name"
          bind:value={c.domainName}
          placeholder="example.com"
          required
          autocomplete="off"
        />
        <div class="modal-tip">
          You can also use a subdomain, such as mail.example.com.
        </div>
        <div class="modal-actions">
          <button type="button" class="secondary" onclick={() => c.close()}
            >Cancel</button
          ><button class="primary" disabled={c.busy}
            >{#if c.busy}<LoaderCircle size={14} class="spin" />{/if}Add domain</button
          >
        </div>
      </form>
    {:else if c.modal === "key"}
      <span class="modal-icon"><KeyRound size={21} /></span>
      <h2>{c.createdToken ? "Your API key is ready" : "Create an API key"}</h2>
      {#if c.createdToken}<p class="modal-description">
          Copy this key now. It will not be shown again.
        </p>
        <div class="secret-box">
          <code>{c.createdToken}</code><button
            class="icon-button"
            aria-label="Copy API key"
            onclick={() => c.copy(c.createdToken)}><Copy size={16} /></button
          >
        </div>
        <div class="modal-actions">
          <button class="primary" onclick={() => c.close()}>Done</button>
        </div>
      {:else}<p class="modal-description">
          Give your application permission to send email.
        </p>
        <form
          onsubmit={(e) => {
            e.preventDefault();
            void c.createKey();
          }}
        >
          <label for="key-name">Name</label><input
            id="key-name"
            bind:value={c.keyName}
            placeholder="Production app"
            required
            maxlength="80"
          /><label for="key-domain">Domain access</label><select
            id="key-domain"
            bind:value={c.keyDomain}
            ><option value="">All verified domains</option
            >{#each c.domains as domain}<option value={domain.id}
                >{domain.name}</option
              >{/each}</select
          >
          <div class="modal-actions">
            <button type="button" class="secondary" onclick={() => c.close()}
              >Cancel</button
            ><button class="primary" disabled={c.busy}>Create key</button>
          </div>
        </form>{/if}
    {:else if c.modal === "compose"}
      <span class="modal-icon"><Send size={21} /></span>
      <h2>Send an email</h2>
      <p class="modal-description">Send from one of your verified domains.</p>
      {#if !c.domains.some((d) => d.status === "verified")}<div
          class="alert warning"
        >
          Verify a domain before sending. <button
            class="text-button"
            onclick={() => {
              c.close();
              navigate("domains");
            }}>Open domains<ArrowUpRight size={13} /></button
          >
        </div>{/if}
      <form
        onsubmit={(e) => {
          e.preventDefault();
          void c.sendEmail();
        }}
      >
        <label for="from">From</label><input
          id="from"
          bind:value={c.draft.from}
          placeholder="Hello <hello@example.com>"
          required
        /><label for="to">To</label><input
          id="to"
          bind:value={c.draft.to}
          placeholder="you@example.com"
          required
        /><label for="subject">Subject</label><input
          id="subject"
          bind:value={c.draft.subject}
          placeholder="A small hello"
          maxlength="998"
        />
        <div class="label-row">
          <label for="body">Message</label>
          <div class="segmented">
            <button
              type="button"
              class:chosen={!c.composeHTML}
              onclick={() => (c.composeHTML = false)}>Text</button
            ><button
              type="button"
              class:chosen={c.composeHTML}
              onclick={() => (c.composeHTML = true)}>HTML</button
            >
          </div>
        </div>
        {#if c.composeHTML}<textarea
            id="body"
            rows="7"
            bind:value={c.draft.html}
            placeholder="<h1>Hello!</h1>"
            required></textarea>{:else}<textarea
            id="body"
            rows="7"
            bind:value={c.draft.text}
            placeholder="Write your message…"
            required></textarea>{/if}
        <div class="modal-actions">
          <button type="button" class="secondary" onclick={() => c.close()}
            >Cancel</button
          ><button
            class="primary"
            disabled={c.busy || !c.domains.some((d) => d.status === "verified")}
            >{#if c.busy}<LoaderCircle size={14} class="spin" />{:else}<Send
                size={14}
              />{/if}Send email</button
          >
        </div>
      </form>
    {:else if c.modal === "domain-detail" && c.selectedDomain}
      {@const domain = c.selectedDomain}
      <div class="detail-heading">
        <span class="modal-icon"><Globe size={21} /></span>
        <div>
          <h2>{domain.name}</h2>
          <span class="badge {domain.status}"
            ><span class="status-dot {domain.status}"></span>{domain.status ===
            "verified"
              ? "Verified"
              : "Pending verification"}</span
          >
        </div>
      </div>
      <div class="section-divider"></div>
      <div class="dns-heading">
        <div>
          <h3>DNS records</h3>
          <p>Add these records at your DNS provider.</p>
        </div>
        <button
          class="secondary"
          disabled={c.busy}
          onclick={() => c.verifyDomain()}
          ><RefreshCw size={14} class={c.busy ? "spin" : ""} />Verify records</button
        >
      </div>
      {#each domain.records as record}<section class="dns-record">
          <div class="dns-record-heading">
            <span class="record-type"
              >{record.required
                ? "DKIM"
                : record.value.startsWith("v=spf1")
                  ? "SPF"
                  : "DMARC"}</span
            ><span class="tag"
              >{record.required ? "Required" : "Recommended"}</span
            ><span class="record-status {record.status}"
              >{#if record.status !== "pending"}<Check
                  size={13}
                />{/if}{record.status === "verified"
                ? "Verified"
                : record.status === "present"
                  ? "Present"
                  : "Not verified"}</span
            >
          </div>
          <div class="dns-fields">
            <div><span>Type</span><code>{record.type}</code></div>
            <div>
              <span>Name</span>
              <div class="copy-field">
                <code>{record.name}</code><button
                  class="icon-button"
                  aria-label={"Copy record name " + record.name}
                  onclick={() => c.copy(record.name)}><Copy size={13} /></button
                >
              </div>
            </div>
            <div class="dns-value">
              <span>Value</span>
              <div class="copy-field">
                <code>{record.value}</code><button
                  class="icon-button"
                  aria-label={"Copy record value for " + record.name}
                  onclick={() => c.copy(record.value)}
                  ><Copy size={13} /></button
                >
              </div>
            </div>
          </div>
        </section>{/each}
      <p class="fine-print">
        Keep existing SPF senders in one SPF record. “Present” confirms a record
        exists; it does not validate all SPF or DMARC policies. DNS changes can
        take time to propagate.
      </p>
      <div class="modal-actions between">
        <button
          class="text-button danger"
          onclick={() => c.removeDomain(domain)}
          ><Trash2 size={14} />Remove domain</button
        ><button class="primary" onclick={() => c.close()}>Done</button>
      </div>
    {:else if c.modal === "email-detail" && c.selectedEmail}
      {@const email = c.selectedEmail}
      <div class="detail-heading">
        <span class="modal-icon"><Mail size={21} /></span>
        <div>
          <h2>{email.message.subject || "(No subject)"}</h2>
          <span class="badge {email.status}"
            ><span class="status-dot {email.status}"></span>{labels[
              email.status
            ]}</span
          >
        </div>
      </div>
      <dl class="email-metadata">
        <div>
          <dt>From</dt>
          <dd>{email.message.from}</dd>
        </div>
        <div>
          <dt>To</dt>
          <dd>{recipients(email.message.to)}</dd>
        </div>
        <div>
          <dt>Created</dt>
          <dd>{date(email.created_at)}</dd>
        </div>
        <div>
          <dt>Email ID</dt>
          <dd>
            <code>{email.id}</code><button
              class="icon-button"
              aria-label="Copy email ID"
              onclick={() => c.copy(email.id)}><Copy size={12} /></button
            >
          </dd>
        </div>
      </dl>
      <div class="detail-actions">
        <button
          class="secondary small"
          disabled={c.busy}
          onclick={() => c.reloadEmail()}
          ><RefreshCw size={13} class={c.busy ? "spin" : ""} />Refresh</button
        >{#if email.recipients.some((r) => r.status === "failed")}<button
            class="secondary small"
            disabled={c.busy}
            onclick={() => c.changeEmail("retry")}
            >Retry failed recipients</button
          >{/if}{#if email.recipients.some((r) => r.status === "queued")}<button
            class="secondary small"
            disabled={c.busy}
            onclick={() => c.changeEmail("cancel")}
            >Cancel queued recipients</button
          >{/if}
      </div>
      <div class="detail-tabs">
        <button
          class:chosen={detailTab === "preview"}
          onclick={() => (detailTab = "preview")}>Preview</button
        ><button
          class:chosen={detailTab === "text"}
          onclick={() => (detailTab = "text")}>Plain text</button
        ><button
          class:chosen={detailTab === "json"}
          onclick={() => (detailTab = "json")}>JSON</button
        >
      </div>
      <div class="message-content">
        {#if detailTab === "preview" && email.message.html}<iframe
            title="Email HTML preview"
            sandbox=""
            srcdoc={preview(email.message.html)}
          ></iframe>{:else if detailTab === "json"}<pre>{JSON.stringify(
              email,
              null,
              2,
            )}</pre>{:else}<pre class="plain-message">{email.message.text ||
              (detailTab === "text"
                ? "No plain text part."
                : "No message content.")}</pre>{/if}
      </div>
      <h3 class="timeline-title">Delivery activity</h3>
      <div class="timeline">
        {#each email.recipients as recipient}<div class="recipient-activity">
            <div class="recipient-title">
              <strong>{recipient.address}</strong><span
                class="badge {recipient.status}"
                >{labels[recipient.status]}</span
              >
            </div>
            {#if !recipient.attempts.length}<div class="activity">
                <span class="status-dot queued"></span>
                <div>
                  <strong>Queued</strong><span>{date(email.created_at)}</span>
                </div>
              </div>{/if}{#each recipient.attempts as attempt}<div
                class="activity"
              >
                <span class="status-dot {attempt.status}"></span>
                <div>
                  <strong
                    >{attempt.status === "queued"
                      ? "Retry scheduled"
                      : attempt.status === "interrupted"
                        ? "Interrupted"
                        : (labels[attempt.status as EmailStatus] ??
                          attempt.status)}</strong
                  >{#if attempt.detail}<p>{attempt.detail}</p>{/if}<span
                    >{date(attempt.at)}</span
                  >
                </div>
              </div>{/each}
          </div>{/each}
      </div>
      <p class="fine-print">
        Accepted means the recipient mail server accepted the message. Inbox
        placement and later bounces are not tracked.
      </p>
    {:else if c.modal === "confirm"}
      <span class="modal-icon"><Trash2 size={21} /></span>
      <h2>{c.confirmation.title}</h2>
      <p class="modal-description">{c.confirmation.detail}</p>
      <div class="modal-actions">
        <button class="secondary" onclick={() => c.close()}>Cancel</button
        ><button
          class="destructive"
          disabled={c.busy}
          onclick={() => c.run(c.confirmation.action)}
          >{c.confirmation.label}</button
        >
      </div>
    {/if}
  </div>
</dialog>
{#if c.notice}<div class="toast" role="status">
    <span><Check size={14} /></span>{c.notice}
  </div>{/if}

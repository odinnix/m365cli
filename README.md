# m365

**The [`gog`](https://gogcli.sh)-style CLI alternative for Microsoft 365.**
A single static Go binary for mail, calendar, contacts, OneDrive, and SharePoint
— against **Microsoft 365 in the cloud (Graph)** or an **on-premise Exchange
server (EWS)** — built for terminals, shell scripts, CI, and coding agents.

Made by [makerslab.ai](https://makerslab.ai).

```bash
m365 mail list --mailbox agent@contoso.com --json
m365 calendar create --subject "Sync" --start 2026-06-10T10:00:00 --end 2026-06-10T10:30:00
m365 calendar create --subject "Urlaub" --all-day --start 2026-06-10 --end 2026-06-12 --timezone Europe/Vienna
m365 drive ls --json
```

## What makes it different

- **Two backends, one interface.** Talk to Microsoft 365 in the cloud (Graph) or
  to an on-premise Exchange server (EWS, NTLM) — chosen with a single `backend`
  field in config. Commands, allowlists, the send guardrail, and the JSON output
  are identical either way; callers never learn which transport answered.
- **App-only, no login (cloud).** On the Graph backend, authentication is
  certificate-based client credentials — no browser consent, no user session; an
  administrator sets up one Azure app once. The EWS backend uses a domain service
  account (NTLM) with impersonation instead. Ideal for unattended agents and CI.
- **Hard allowlists, fail-closed.** Every mailbox and every SharePoint site the
  binary may touch is listed in config. Anything not listed is refused **before**
  any network call. An empty list denies everything.
- **Send guardrail.** Mail to a recipient outside your `send_allow` list is never
  sent silently — it becomes a **draft** for human review. You opt out
  explicitly, and `doctor` warns when you do.
- **Agent-friendly output.** `--json` emits a stable envelope on stdout; human
  hints and progress go to stderr, so pipelines stay parseable.

## Install

```bash
# Install the latest from source (produces the `m365` binary on your PATH):
go install github.com/MakersLab-ai/m365cli/cmd/m365@latest

# Or build from a checkout:
git clone https://github.com/MakersLab-ai/m365cli
cd m365cli && go build -o bin/m365 ./cmd/m365
```

Requires Go 1.26+.

## Setup

`m365` needs an Azure app registration (with a certificate) and access scoped to
the mailboxes/sites you intend to use. The full administrator guide —
app registration, certificate, RBAC for Applications, and `Sites.Selected` — is
in **[docs/azure-setup.md](docs/azure-setup.md)**.

Then create `~/.config/m365cli/config.toml` (mode `600`):

```toml
tenant_id  = "<directory-tenant-id>"
client_id  = "<application-client-id>"
cert_path  = "/path/to/m365-app.pem"   # certificate + private key, one PEM

default_mailbox   = "agent@contoso.com"
allowed_mailboxes = ["agent@contoso.com", "*@contoso.com"]  # exact or glob
allowed_sites     = ["contoso.sharepoint.com,*"]

send_allow        = ["*@partner.com"]   # direct external send; others → draft
# send_unrestricted = true              # disable the send guardrail (doctor warns)
```

Verify:

```bash
m365 doctor          # offline checks: config, certificate, allowlists
m365 doctor --live   # acquires a real Graph token (verifies cert + tenant)
```

### On-premise Exchange (EWS)

For mail that lives on a local Exchange server instead of the cloud, set
`backend = "ews"` and point at your EWS endpoint with an NTLM service account:

```toml
backend           = "ews"
ews_url           = "https://mail.example.com/EWS/Exchange.asmx"
ews_user          = "EXAMPLE\\svc-agent"     # DOMAIN\user, or a UPN
ews_password_file = "/etc/m365/ews.pass"     # 0600 file holding only the password

default_mailbox   = "agent@example.com"
allowed_mailboxes = ["agent@example.com", "*@example.com"]
send_allow        = ["*@partner.com"]
```

The full guide (service account, ApplicationImpersonation, what's supported) is
in **[docs/ews-setup.md](docs/ews-setup.md)**.

## Commands

```
m365 doctor    [--live]                                     # config + token health
m365 mail      list read search send draft reply
               attachments get-attachment
               folders categories move categorize mark      # organize existing mail
               watch poll                                   # scoped by allowed_mailboxes
m365 calendar  list get create update delete
               freebusy find-times                          # scoped by allowed_mailboxes
m365 contacts  list get add                                 # scoped by allowed_mailboxes
m365 drive     ls search get download upload                # OneDrive, scoped by allowed_mailboxes
m365 sp        sites list items download                    # SharePoint, scoped by allowed_sites
```

Run `m365 <domain> --help` or `m365 <domain> <verb> --help` for flags.

The full command set above runs against the **cloud (Graph)** backend. The
**on-premise (EWS)** backend covers mail (incl. reply/attachments), calendar
list/get/create/update/delete, and `mail watch poll`; `calendar freebusy`/
`find-times`, `contacts`, and `drive`/`sp` are cloud-only and report
`operation not supported by this backend` there.

### Composing mail safely

Message bodies are read from a file (`--body-file`) rather than a flag, so there
is no shell-escaping of mail text:

```bash
m365 mail send --mailbox agent@contoso.com \
  --to person@contoso.com --subject "Update" --body-file ./body.txt

# A recipient outside send_allow → saved as a draft, not sent:
m365 mail send --to stranger@example.com --subject Hi --body-file ./body.txt
# stderr: send guardrail: [stranger@example.com] not in send_allow — saving as draft
```

Bodies are plain text by default, and the formatting survives on every path:

- **Replies keep their line breaks.** Graph splices a reply into the HTML body
  of the original message, where newlines are insignificant, so `m365 mail
  reply` converts the plain text to HTML first (blank line → paragraph, single
  newline → `<br>`).
- **An HTML body file is sent as HTML.** Pass `--html` to state it explicitly.
  If you forget, a body that *starts* with a tag (`<html`, `<body`, `<p>`, …)
  is recognised anyway and a note goes to stderr — otherwise the recipient
  would read the markup instead of the mail. Prose is never touched: detection
  looks at the opening of the body only, so a mail that merely mentions a tag
  stays plain text.

- **A reply can be prepared instead of sent.** `m365 mail reply --draft` (with
  `--reply-all` for Outlook's "Reply All") never sends: it creates the draft
  Graph's `createReply`/`createReplyAll` builds, writes the answer **on top of**
  the quoted original, and leaves thread, signatures and inline images below it
  untouched. Without `--draft`, a reply whose recipients are all inside
  `send_allow` goes out immediately — the draft is then only the guardrail's
  fallback for outside recipients.
- **Inline images survive.** `--inline-image cid=path` (repeatable, on `reply`
  and `draft`) attaches a picture as an inline attachment, so an HTML body can
  reference it as `<img src="cid:…">` — that is how a signature keeps its logos.

Both backends honour this, each in its own terms: Graph needs the conversion
described above, while EWS types every body explicitly and keeps a plain-text
body plain (its line breaks survive as-is) — there it is the `BodyType` that has
to match what you wrote.

### Watching a mailbox

`m365 mail watch poll` delta-polls one or more mailboxes and forwards new mail
to a webhook (gog-compatible payload) — no public inbound endpoint required.
See **[docs/watch.md](docs/watch.md)** for usage, payload, and the reliability
contract.

## Output

- `--json` — a stable envelope on stdout: `{"ok":true,"data":…}` or
  `{"ok":false,"error":{"message":…}}`.
- Human-facing messages and progress go to **stderr**; data goes to **stdout**.

## Security model

`m365`'s allowlists are defense-in-depth **on top of** the access you grant in
Azure (RBAC for Applications for mailboxes, `Sites.Selected` for sites). Even if
those grants were too broad, `m365` refuses any mailbox or site not in your
config. Secrets (the certificate) live on disk at `cert_path` and are never
passed on the command line; the token cache is written `600`.

## Not in scope

Teams chat, full Tasks/To-Do parity, and personal Microsoft accounts are out of
scope — `m365` targets Microsoft 365 work tenants (cloud, app-only) and
on-premise Exchange. For delegated, user-login scenarios on Google Workspace,
see [`gog`](https://gogcli.sh).

## Disclaimer & trademarks

This is an independent, third-party project. It is **not** affiliated with,
endorsed by, or sponsored by Microsoft. "Microsoft" and "Microsoft 365" are
trademarks of the Microsoft group of companies; they are used here only to
describe interoperability (nominative use). All other trademarks are the
property of their respective owners.

## License

See [LICENSE](LICENSE).

---

Made by [makerslab.ai](https://makerslab.ai) — the `gog` alternative for Microsoft 365.

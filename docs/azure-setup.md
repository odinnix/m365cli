# Azure / Microsoft 365 setup

> 🇩🇪 Deutsche Version: **[azure-setup.de.md](azure-setup.de.md)**

`m365` authenticates **app-only** (client credentials with a certificate). There
is **no user login and no consent prompt** — an administrator sets up one Entra
app registration once, and scopes its access to specific mailboxes and sites.

This guide is for the administrator who prepares the tenant. It covers the one
Azure app, its certificate, the permissions, and — crucially — how to **scope**
those permissions so the app only ever touches the mailboxes and sites you
intend.

> ### ✅ For IT admins: your tasks at a glance
>
> If you received this guide because an **AI assistant** is being connected
> (e.g. it emailed you this link with a certificate attached):
>
> - **Your tasks are sections 1–4 only** (register the app, upload the
>   certificate, scope mailbox access, optionally file access).
> - **Then reply with just two values** (see the box after section 4): the
>   **Directory (tenant) ID** and the **Application (client) ID**.
> - **Sections 5–6 happen automatically on the assistant's side** — nothing to
>   do there. They are documented so you can see what happens on the other end.

> **Two scoping models, on purpose.** Mailbox data (mail, calendar, contacts) is
> scoped with **RBAC for Applications** in Exchange Online. Files/SharePoint is
> scoped with **`Sites.Selected`** in Microsoft Graph. They are configured
> differently — see the relevant sections below.

---

## 1. Register the application (Microsoft Entra ID)

1. Entra admin center → **Identity → Applications → App registrations → New
   registration**.
2. Name it (e.g. `m365cli`), single-tenant, no redirect URI.
3. Note the **Application (client) ID** and **Directory (tenant) ID** from the
   Overview blade — these go into `config.toml`.

## 2. Add a certificate

App-only auth uses a certificate assertion (preferred over a client secret:
longer-lived, not a bearer secret on disk).

Generate a self-signed certificate (or use one from your PKI):

```bash
# Private key + public certificate, valid 365 days
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout m365-key.pem -out m365-cert.pem \
  -days 365 -subj "/CN=m365cli"

# m365 expects ONE PEM file containing the certificate AND the private key:
cat m365-cert.pem m365-key.pem > m365-app.pem
chmod 600 m365-app.pem
```

Upload the **public** certificate only:

- App registration → **Certificates & secrets → Certificates → Upload
  certificate** → upload `m365-cert.pem`.

Keep `m365-app.pem` (cert + key) on the machine that runs `m365`, mode `600`.
**Never** upload the private key or commit it.

## 3. Scope mailbox access — RBAC for Applications

This is how mail, calendar, and contacts are restricted to specific mailboxes.

> ⚠️ **Do not also grant the tenant-wide permission in Entra ID.** RBAC grants
> are *independent and additive* to Entra app-permission grants. If you grant
> `Mail.ReadWrite` tenant-wide in Entra **and** scope it via RBAC, the union is
> effectively unscoped (full-tenant) access. For scoped access, grant the
> permission **only** through RBAC below — leave the app's Entra **API
> permissions empty** for mailbox scopes.

Connect as an Exchange admin (member of **Organization Management**):

```powershell
Connect-ExchangeOnline
```

**a) Create a pointer to the app's service principal.** Use the IDs from the
Entra **Enterprise applications** page (App ID + the *enterprise app* Object ID —
not the App registration object ID):

```powershell
New-ServicePrincipal -AppId <client-id> -ObjectId <enterprise-app-object-id> -DisplayName "m365cli"
```

**b) Define a resource scope** (which mailboxes). Easiest: a mail-enabled
security group whose members are the allowed mailboxes, then a management scope
that points at it via its distinguished name:

```powershell
$dn = (Get-Group "m365cli-mailboxes").DistinguishedName
New-ManagementScope -Name "m365cli scope" -RecipientRestrictionFilter "MemberOfGroup -eq '$dn'"
```

(You can instead scope by any [recipient filter property](https://learn.microsoft.com/en-us/powershell/exchange/recipientfilter-properties),
or use a Microsoft Entra **administrative unit** via `-RecipientAdministrativeUnitScope`.)

**c) Assign the application roles, scoped.** For Phase 1 (mail + calendar):

```powershell
New-ManagementRoleAssignment -App <enterprise-app-object-id> -Role "Application Mail.ReadWrite"     -CustomResourceScope "m365cli scope"
New-ManagementRoleAssignment -App <enterprise-app-object-id> -Role "Application Mail.Send"          -CustomResourceScope "m365cli scope"
New-ManagementRoleAssignment -App <enterprise-app-object-id> -Role "Application Calendars.ReadWrite" -CustomResourceScope "m365cli scope"
# Phase 2 (contacts):
New-ManagementRoleAssignment -App <enterprise-app-object-id> -Role "Application Contacts.ReadWrite"  -CustomResourceScope "m365cli scope"
# Optional, for `m365 mail categories` (listing the mailbox's Outlook categories):
New-ManagementRoleAssignment -App <enterprise-app-object-id> -Role "Application MailboxSettings.Read" -CustomResourceScope "m365cli scope"
```

> `Application Mail.ReadWrite` does **not** include sending — `Application
> Mail.Send` is separate. (`Application Mail Full Access` bundles both.)

**d) Verify the scope.** This bypasses the permission cache:

```powershell
Test-ServicePrincipalAuthorization -Identity "m365cli" -Resource allowed@contoso.com | Format-Table
# InScope = True for an allowed mailbox, False for one outside the scope.
```

> **Cache:** RBAC changes take **30 minutes to 2 hours** to apply to live calls
> (the test cmdlet bypasses this). If `m365` calls fail right after a change,
> wait and retry.

## 4. Scope file access — `Sites.Selected` (Phase 2)

Files (SharePoint **and** OneDrive) do **not** use Exchange RBAC. Instead:

> ⚠️ **Avoid `Files.ReadWrite.All` / `Files.Read.All`.** Those are *tenant-wide*:
> the app can reach **every** user's OneDrive and **every** site, and Azure
> offers **no way to restrict them to individual drives**. `Sites.Selected` is
> the scoped alternative — default-deny, with explicit per-site/per-drive grants.

1. App registration → **API permissions → Add → Microsoft Graph → Application
   permissions** → add **`Sites.Selected`** → **Grant admin consent**.
   With only this consent the app has access to **nothing** until you grant
   sites individually below.
2. Grant the app access to each target site individually (as an admin, via
   Graph):

   ```http
   POST https://graph.microsoft.com/v1.0/sites/{site-id}/permissions
   {
     "roles": ["read"],                          // or "write"
     "grantedToIdentities": [{
       "application": { "id": "<client-id>", "displayName": "m365cli" }
     }]
   }
   ```

Only sites listed in `allowed_sites` (config) **and** granted here are reachable.

### Scoping individual OneDrives

A user's OneDrive is technically a **personal SharePoint site**
(`contoso-my.sharepoint.com/personal/user_contoso_com`), so the same
`Sites.Selected` grant works per OneDrive — the app gets access to exactly the
drives you grant, and nothing else.

1. Find the site ID of the user's OneDrive (as an admin with sufficient rights,
   e.g. via Graph Explorer):

   ```http
   GET https://graph.microsoft.com/v1.0/users/{user}/drive?$select=sharePointIds
   # → sharePointIds.siteId is the {site-id} for the permissions call
   ```

2. Grant access with the same `POST /sites/{site-id}/permissions` call as above.

Notes:

- **One grant per drive.** There is no group/bulk grant ("all OneDrives of
  department X") — each drive is an individual grant. The calls are easy to
  script for a list of users.
- **Finer than a whole drive:** the newer `Files.SelectedOperations.Selected`
  and `Lists.SelectedOperations.Selected` application permissions support
  grants down to individual **folders, files, or document libraries** instead
  of a whole site/drive, via the corresponding `permissions` endpoints.
- **No grant → no access.** Without an explicit grant, every `m365 drive` call
  fails with 403 from Graph, regardless of `allowed_mailboxes`.

---

## ✅ For IT admins: done! Send these two values back

With section 3 (and 4, if file access is wanted) complete, your part is done.
Reply to the AI assistant's email with these two values from the app
registration's Overview blade:

1. **Directory (tenant) ID**
2. **Application (client) ID**

Both are non-sensitive without the private key and fine to send by email. The
assistant handles the rest (sections 5–6) automatically.

> **Note:** RBAC changes take up to 2 hours to apply to live calls. If the
> assistant reports the connection isn't working yet, that's normal within the
> first 2 hours — it retries on its own. You can verify immediately with
> `Test-ServicePrincipalAuthorization` (section 3d).

---

## 5. Write `config.toml` — done by the assistant

> **Nothing to do here for IT.** This section documents what happens
> automatically on the assistant's side once your two IDs arrive. (If you run
> `m365` manually without an assistant, this step is yours.)

`~/.config/m365cli/config.toml` (mode `600`):

```toml
tenant_id  = "<directory-tenant-id>"
client_id  = "<application-client-id>"
cert_path  = "/path/to/m365-app.pem"   # cert + private key, one PEM, mode 600

default_mailbox   = "agent@contoso.com"
allowed_mailboxes = ["agent@contoso.com", "*@contoso.com"]  # exact or glob
allowed_sites     = ["contoso.sharepoint.com,*"]            # Phase 2

# Direct external send is OFF by default: recipients not listed become drafts.
send_allow        = ["*@partner.com"]
# send_unrestricted = true   # disables the external-send guardrail (doctor warns)
```

`m365`'s allowlists are an **extra** guardrail on top of RBAC/`Sites.Selected`:
even if RBAC were misconfigured too broadly, `m365` refuses any mailbox or site
not in the config (fail-closed — an empty list denies all).

## 6. Verify — done by the assistant

> **Nothing to do here for IT** — the assistant verifies the connection itself
> and reports back to its user.

```bash
m365 doctor          # offline: config, cert presence, allowlists
m365 doctor --live   # acquires a real Graph token (verifies cert + tenant)
m365 mail list --mailbox agent@contoso.com --json
```

A green `doctor --live` plus a successful `mail list` against an allowed mailbox
confirms the full chain.

---

## Permission reference

| Domain | Graph permission | How it is granted/scoped |
| --- | --- | --- |
| Mail read/write | `Mail.ReadWrite` | RBAC role `Application Mail.ReadWrite`, scoped |
| Mail send | `Mail.Send` | RBAC role `Application Mail.Send`, scoped |
| Calendar | `Calendars.ReadWrite` | RBAC role `Application Calendars.ReadWrite`, scoped |
| Contacts | `Contacts.ReadWrite` | RBAC role `Application Contacts.ReadWrite`, scoped |
| Mail folders, move, categorize, mark | `Mail.ReadWrite` | Same RBAC role as mail read/write, scoped |
| Outlook category list (`mail categories`) | `MailboxSettings.Read` | RBAC role `Application MailboxSettings.Read`, scoped |
| SharePoint/OneDrive | `Sites.Selected` | Entra consent + per-site/per-drive grant (a OneDrive is a personal site) |
| Single folders/files (optional) | `Files.SelectedOperations.Selected`, `Lists.SelectedOperations.Selected` | Entra consent + per-item grant |

## Troubleshooting

| Symptom | Cause / fix |
| --- | --- |
| `AADSTS700027` / cert errors | Uploaded cert doesn't match the key in your PEM, or it expired. Re-generate and re-upload. |
| `AADSTS90002: Tenant not found` | Wrong `tenant_id`. |
| Token works, mailbox calls return 403 | RBAC scope not applied yet (wait up to 2h), or the mailbox isn't in your management scope. Check with `Test-ServicePrincipalAuthorization`. |
| App can read mailboxes outside the scope | A tenant-wide Entra grant is still present — remove it (see the warning in §3). |
| `drive`/`sp` calls return 403 | No `Sites.Selected` grant for that site/OneDrive yet — grant it per §4. |

## References

- [RBAC for Applications in Exchange Online](https://learn.microsoft.com/en-us/exchange/permissions-exo/application-rbac)
- [Sites.Selected / per-site permissions](https://learn.microsoft.com/en-us/graph/api/site-post-permissions)
- [Overview of Selected permissions in OneDrive and SharePoint](https://learn.microsoft.com/en-us/graph/permissions-selected-overview)
- [Graph app-only auth (client credentials)](https://learn.microsoft.com/en-us/graph/auth-v2-service)

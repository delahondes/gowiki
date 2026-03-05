# Microsoft 365 / Azure AD Authentication Setup

This guide explains how to configure Gowiki to allow users to sign in with their Microsoft 365 (Azure AD / Entra ID) accounts.

## Prerequisites

- A Microsoft 365 tenant (or Azure AD tenant)
- An Azure account with permission to register applications (Global Administrator, Application Administrator, or Cloud Application Administrator)
- A running Gowiki instance with admin access

## Step 1: Register an Application in Azure Portal

1. Go to [Azure Portal](https://portal.azure.com)
2. Navigate to **Azure Active Directory** > **App registrations** > **New registration**
3. Fill in:
   - **Name**: `Gowiki` (or whatever you prefer)
   - **Supported account types**: Choose based on your needs:
     - *Single tenant* — only users from your organization
     - *Multi-tenant* — users from any Azure AD organization
   - **Redirect URI**: Select **Web** and enter:
     - For development: `http://localhost:8080/api/auth/oauth/callback`
     - For production: `https://your-wiki-domain.com/api/auth/oauth/callback`
4. Click **Register**

## Step 2: Note the Application IDs

After registration, you'll see the app overview page. Note these values:

- **Application (client) ID** — this is the `Client ID`
- **Directory (tenant) ID** — this is the `Tenant ID`

## Step 3: Create a Client Secret

1. In the app registration, go to **Certificates & secrets** > **Client secrets** > **New client secret**
2. Add a description (e.g., "Gowiki") and choose an expiry period
3. Click **Add**
4. **Copy the Value immediately** (it won't be shown again). This is the `Client Secret`
   - Important: copy the **Value**, not the **Secret ID**

## Step 4: Configure API Permissions

1. Go to **API permissions** > **Add a permission**
2. Select **Microsoft Graph** > **Delegated permissions**
3. Add these permissions:
   - `openid` (Sign users in)
   - `email` (View users' email address)
   - `profile` (View users' basic profile)
4. Click **Add permissions**
5. If you see "Admin consent required", click **Grant admin consent for [your org]**

These are minimal, read-only permissions — Gowiki only needs to identify the user.

## Step 5: Configure Gowiki

### Option A: Via Admin UI

1. Log into Gowiki as an admin
2. Go to the **Admin** page > **Configuration** tab
3. In the **OAuth / Microsoft 365** section, fill in:
   - **Provider**: Azure AD / Microsoft 365
   - **Tenant ID**: the Directory (tenant) ID from Step 2
   - **Client ID**: the Application (client) ID from Step 2
   - **Client Secret**: the Value from Step 3
   - **Auto-create users**: check if you want users to be created automatically on first login
   - **Default groups**: comma-separated list of groups for auto-created users (e.g., `editors`)
4. Click **Save Configuration**

### Option B: Via config.yaml

Edit `data/config.yaml`:

```yaml
auth:
  session_ttl: "24h"
  oauth:
    provider: "azure"
    tenant_id: "your-tenant-id-here"
    client_id: "your-client-id-here"
    client_secret: "your-client-secret-here"
    auto_create_users: false
    default_groups:
      - editors
```

Restart the backend after editing the file.

## Step 6: Test

1. Open Gowiki and click the **Login** link
2. You should see a **"Sign in with Microsoft 365"** button above the username/password form
3. Click it — you'll be redirected to Microsoft's login page
4. After authenticating, you'll be redirected back to Gowiki

## How User Mapping Works

Gowiki uses **email** as the join key between local accounts and OAuth:

- When a user logs in via Microsoft 365, Gowiki extracts their email from the ID token
- It then looks up a local user with the same email (case-insensitive)
- If found: the user gets a session as that local user (same username, groups, permissions)
- If not found and **auto-create** is enabled: a new user is created with username derived from the email prefix and the configured default groups
- If not found and auto-create is disabled: login is rejected

This means you can:
- **Pre-provision users**: create users in Admin > Users with their Microsoft email, assign groups — when they log in via 365, they get those permissions
- **Use as fallback**: users with both a local password and a Microsoft account can log in either way
- **Mix authentication**: some users local-only, some OAuth-only, some both

## Redirect URIs

You can register multiple redirect URIs in Azure Portal. Common setup:

- `http://localhost:8080/api/auth/oauth/callback` — for development
- `https://wiki.yourcompany.com/api/auth/oauth/callback` — for production

Gowiki uses a relative callback path (`/api/auth/oauth/callback`), so it works regardless of the host. The OAuth library resolves it against the request's origin.

## Troubleshooting

**"OAuth is not configured"**: Check that provider is set to "azure" and client_id is filled in the config.

**"No account found for user@example.com"**: The user's email doesn't match any local account and auto-create is disabled. Either create the user in Admin > Users with that email, or enable auto-create.

**"Authentication failed: invalid_client"**: The client secret is wrong or expired. Generate a new one in Azure Portal.

**"Authentication failed: redirect_uri_mismatch"**: The callback URL doesn't match what's registered in Azure. Make sure `http://localhost:8080/api/auth/oauth/callback` (or your production URL) is listed in the app's redirect URIs.

**Azure returns AADSTS50011**: Same as above — redirect URI mismatch.

**No email in token**: Ensure the `email` API permission is granted and admin consent is given. Some Azure configurations put the email in `preferred_username` or `upn` instead — Gowiki checks all three.

# API Tokens

## 1. For users

Click **API Tokens** in the top-right banner (next to your username) to manage your tokens.

- **Create** — give the token a name (e.g. "Claude assistant"), copy the `gwk_...` value. It is shown once and never stored.
- **Revoke** — delete a token immediately. Any client using it will be rejected.

## 1. For admins

Access: Admin > Tokens

The admin panel shows all tokens across all users. Admins can revoke any token.

## 1. Using tokens

**Preferred — HTTP header:**

```
Authorization: Bearer gwk_<your_token>
```

**Fallback — query parameter** (for platforms that cannot set custom headers):

```
https://wiki.example.com/api/ai/v1/meta/some/page?token=gwk_<your_token>
```

The query parameter method is less secure (tokens may appear in server logs). Use the header method when possible.

## 1. Security

- Tokens authenticate as the user who created them — same permissions, same ACL
- A disabled user's tokens are immediately rejected
- Tokens cannot create other tokens (session auth required for token management)
- Rate limits apply per token (configurable in Admin > Configuration)

## 1. Configuration

The AI API must be enabled in Admin > Configuration > AI Content API before tokens can be used for API access. Token creation is always available regardless of this setting.

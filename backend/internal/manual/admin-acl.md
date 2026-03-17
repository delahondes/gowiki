# ACL (Access Control)

Access: Admin > ACL

## 1. How ACL works

ACL rules control who can view, edit, and delete pages. Each rule has:
- **Pattern** — a regex matched against the page path
- **Subject** — a user, group, or special keyword
- **Permissions** — view, edit, delete (any combination)

## 1. Evaluation order

1. All rules whose pattern matches the page path are collected
2. Filtered to rules whose subject applies to the current user
3. Sorted by pattern length (longest = most specific)
4. The most specific tier wins — permissions are unioned within that tier
5. If no rules match: **deny by default**

## 1. Special subjects

| Subject | Matches |
| --- | --- |
| @all | Everyone, including anonymous users |
| @authenticated | Any logged-in user |
| @self | The user whose username appears in the path |

## 1. @self rules

The `@self` placeholder in patterns allows per-user namespaces:

```
Pattern: staff/@self/.*
Subject: @self
Permissions: view, edit, delete
```

This gives each user full control over `staff/<their-username>/...` pages.

## 1. Default rules

On first start, three rules are created:
- `.*` + group `admin` → view, edit, delete
- `.*` + group `editors` → view, edit
- `.*` + `@all` → view

## 1. Tips

- More specific patterns override broader ones
- Use `regulatory/.*` to restrict an entire namespace
- Test ACL changes by logging in as a non-admin user

#!/usr/bin/env python3
"""
Migrate DokuWiki validation status to Gowiki reviewflow.

Two categories:
  A) Pages with existing {reviewflow} directive → import validation status only
  B) Pages with "Auteurs et relecteurs" table → convert table to {reviewflow} + import status

Usage:
  python3 migrate_validations.py \
    --gowiki-content /opt/gowiki/data/content \
    --gowiki-meta /opt/gowiki/data/meta \
    --dokuwiki-meta /tmp/dokuwiki-meta \
    [--dry-run]
"""

import argparse
import json
import os
import re
import sys
import time
from pathlib import Path
from datetime import datetime, timezone

import phpserialize

# ── Display name → login mapping ──

DISPLAY_TO_LOGIN = {
    "R.de Lahondès": "raynald.delahondes",
    "R. de Lahondes": "raynald.delahondes",
    "R. de Lahondès": "raynald.delahondes",
    "M.Laborde": "michel.laborde",
    "M. Laborde": "michel.laborde",
    "E.Formstecher": "etienne.formstecher",
    "E. Formstecher": "etienne.formstecher",
    "B.Duplan": "benjamin.duplan",
    "B. Duplan": "benjamin.duplan",
    "A.Laporte": "alice.laporte",
    "A. Laporte": "alice.laporte",
    "T.Moncion": "thomas.moncion",
    "T. Moncion": "thomas.moncion",
    "L.Lock": "laurent.lock",
    "L. Lock": "laurent.lock",
    "V.Puller": "vadim.puller",
    "V. Puller": "vadim.puller",
    "L.Lesage": "louison.lesage",
    "L. Lesage": "louison.lesage",
    "F.Plaza Oñate": "fplazaonate",
    "F. Plaza Oñate": "fplazaonate",
    "S.Nicolas": "simon.nicolas",
    "S. Nicolas": "simon.nicolas",
}

# ── Role name translations (FR → EN) ──

ROLE_TRANSLATIONS = {
    "auteur": "author",
    "relecteur": "reviewer",
    "validation": "validation",
    "author": "author",
    "reviewer": "reviewer",
}

# ── Namespace rename mapping (Gowiki → DokuWiki old names) ──
# For each Gowiki namespace under smq/, list of DokuWiki meta paths to try
NAMESPACE_ALIASES = {
    "dir": ["dir", "ps01"],
    "qara": ["qara", "ps02"],
    "soft": ["soft", "ps03", "ps05"],
    "cpm": ["cpm", "ps04"],
    "res": ["res", "ps06", "ps07"],
}


def resolve_login(display_name: str) -> str | None:
    """Resolve a display name to a login name."""
    name = display_name.strip()
    if name in DISPLAY_TO_LOGIN:
        return DISPLAY_TO_LOGIN[name]
    # Try case-insensitive
    for k, v in DISPLAY_TO_LOGIN.items():
        if k.lower() == name.lower():
            return v
    return None


def find_dokuwiki_meta(gowiki_relpath: str, dokuwiki_meta_root: str) -> str | None:
    """Find the DokuWiki .meta file for a given Gowiki content path.

    Gowiki: regulatory/smq/soft/sop01/index.md
    DokuWiki meta possibilities:
      - regulatory/smq/soft/sop01.meta (parent-level)
      - regulatory/smq/soft/sop01/start.meta (namespace start page)
      - regulatory/smq/ps03/sop01.meta (old namespace name)
      - regulatory/smq/ps03/sop01/start.meta
    """
    # Strip .md extension and handle index.md
    rel = gowiki_relpath
    if rel.endswith("/index.md"):
        # Namespace page: could be parent.meta or dir/start.meta
        ns_path = rel[:-len("/index.md")]
        candidates = [
            ns_path + ".meta",           # parent-level .meta
            ns_path + "/start.meta",     # start page inside dir
        ]
    elif rel.endswith(".md"):
        page_path = rel[:-3]
        candidates = [page_path + ".meta"]
    else:
        return None

    # Generate namespace alias variations
    all_candidates = []
    for candidate in candidates:
        all_candidates.append(candidate)
        # Try namespace aliases for smq/ subdirectories
        parts = candidate.split("/")
        if len(parts) >= 4 and parts[0] == "regulatory" and parts[1] == "smq":
            ns_name = parts[2]
            if ns_name in NAMESPACE_ALIASES:
                for alias in NAMESPACE_ALIASES[ns_name]:
                    if alias != ns_name:
                        alt = "/".join([parts[0], parts[1], alias] + parts[3:])
                        all_candidates.append(alt)

    for candidate in all_candidates:
        full_path = os.path.join(dokuwiki_meta_root, candidate)
        if os.path.isfile(full_path):
            return full_path

    return None


def parse_dokuwiki_meta(meta_path: str) -> dict:
    """Parse a DokuWiki .meta file (PHP serialized)."""
    with open(meta_path, "rb") as f:
        raw = phpserialize.loads(f.read())
    return decode_bytes(raw)


def decode_bytes(obj):
    """Recursively decode bytes to strings in phpserialize output."""
    if isinstance(obj, bytes):
        return obj.decode("utf-8", errors="replace")
    if isinstance(obj, dict):
        return {decode_bytes(k): decode_bytes(v) for k, v in obj.items()}
    if isinstance(obj, (list, tuple)):
        return [decode_bytes(x) for x in obj]
    return obj


def get_publish_approval_status(meta: dict) -> dict | None:
    """Check if the latest revision has ≥3 approvals (publish plugin).

    Returns {user: timestamp, ...} for the latest fully-approved revision,
    or None if not validated.
    """
    persistent = meta.get("persistent", {})
    approval = persistent.get("approval", {})
    if not approval:
        return None

    # Find the latest revision with ≥3 approvals
    best_rev = None
    best_approvals = None
    for rev, approvals in approval.items():
        if isinstance(approvals, dict) and len(approvals) >= 3:
            rev_int = int(rev) if not isinstance(rev, int) else rev
            if best_rev is None or rev_int > best_rev:
                best_rev = rev_int
                best_approvals = approvals

    if best_approvals is None:
        return None

    # Return user→timestamp mapping
    result = {}
    for user, info in best_approvals.items():
        if isinstance(info, dict):
            ts = info.get(3, 0)
        elif isinstance(info, (list, tuple)):
            ts = info[3] if len(info) > 3 else 0
        else:
            ts = 0
        result[user] = int(ts)
    return result


def get_reviewflow_validation(meta: dict) -> dict | None:
    """Check if reviewflow was validated in DokuWiki.

    Returns {role: user, ...} from the latest version_history entry,
    or None if not validated.
    """
    persistent = meta.get("persistent", {})
    plugin = persistent.get("plugin", {})
    rf = plugin.get("reviewflow", {})
    if not rf:
        return None

    version_history = rf.get("_version_history")
    if not version_history:
        return None

    # Find the latest entry
    latest = None
    for entry in (version_history.values() if isinstance(version_history, dict) else []):
        if isinstance(entry, dict):
            if latest is None or entry.get("timestamp", 0) > latest.get("timestamp", 0):
                latest = entry

    if latest and "confirmed_by" in latest:
        return latest["confirmed_by"]

    return None


def parse_author_table(content: str) -> tuple[dict[str, str], int, int] | None:
    """Find and parse the 2-column author/relecteur table.

    Returns (roles_dict, start_line, end_line) where roles_dict maps
    translated role names to login usernames, and start/end lines
    indicate the table block to replace (including {table headers=1c}).
    """
    lines = content.split("\n")
    i = 0
    while i < len(lines):
        line = lines[i].strip()

        # Look for {table headers=1c} followed by a 2-column table with role names
        if line == "{table headers=1c}":
            table_start = i
            # Check if next lines are a 2-column table with roles
            roles = {}
            j = i + 1
            # Skip separator rows and parse data rows
            while j < len(lines):
                row = lines[j].strip()
                if not row.startswith("|"):
                    break
                # Parse | Key | Value |
                if re.match(r"^\|\s*---", row):
                    j += 1
                    continue
                m = re.match(r"^\|\s*(.+?)\s*\|\s*(.*?)\s*\|$", row)
                if m:
                    key = m.group(1).strip()
                    val = m.group(2).strip()
                    key_lower = key.lower()
                    if key_lower in ROLE_TRANSLATIONS:
                        login = resolve_login(val)
                        if login:
                            role = ROLE_TRANSLATIONS[key_lower]
                            roles[role] = login
                        elif val:
                            # Unknown display name, keep as-is but warn
                            role = ROLE_TRANSLATIONS[key_lower]
                            roles[role] = val
                    else:
                        # Not a role row — this table is probably not the author table
                        break
                j += 1

            # Must have at least 2 role entries to be the author table
            if len(roles) >= 2:
                # end_line is the first line after the table (exclusive)
                return roles, table_start, j

        i += 1

    return None


def find_last_version(content: str) -> str:
    """Find the version from the last row of the history table.

    Looks for "Historique des modifications" or "Change history" heading,
    then parses the table below it to get the last row's Version column.
    """
    lines = content.split("\n")
    i = 0
    while i < len(lines):
        line = lines[i].strip().lower()
        # Match history heading (any level)
        if re.search(r"historique des modifications|change history", line):
            # Find the table after this heading
            j = i + 1
            # Skip blank lines
            while j < len(lines) and not lines[j].strip():
                j += 1
            # Should be a table header row
            if j < len(lines) and lines[j].strip().startswith("|"):
                # Skip header and separator
                j += 1  # header
                if j < len(lines) and re.match(r"^\s*\|[\s\-|]+\|", lines[j]):
                    j += 1  # separator
                # Read data rows, keep last one
                last_version = ""
                while j < len(lines):
                    row = lines[j].strip()
                    if not row.startswith("|"):
                        break
                    m = re.match(r"^\|\s*(.+?)\s*\|", row)
                    if m:
                        v = m.group(1).strip()
                        if v and v != "---":
                            last_version = v
                    j += 1
                if last_version:
                    return last_version.rstrip(".")
        i += 1

    return "1.0"  # fallback


def create_reviewflow_state(
    roles: dict[str, str],
    version_tag: str,
    confirmed_by: dict[str, str] | None,
    page_version: int,
) -> dict:
    """Build a .reviewflow.json state dict."""
    now = datetime.now(timezone.utc).isoformat()

    state = {
        "roles": roles,
        "version_tag": version_tag,
        "current_page_version": page_version,
        "confirmations": [],
        "validated_page_version": 0,
    }

    if confirmed_by:
        # All roles confirmed — create confirmation entries
        confirmations = []
        for role, user in roles.items():
            confirmations.append({
                "page_version": page_version,
                "role": role,
                "user": user,
                "timestamp": now,
                "version_tag": version_tag,
            })
        state["confirmations"] = confirmations
        state["validated_page_version"] = page_version
        state["version_history"] = [{
            "page_version": page_version,
            "timestamp": now,
            "confirmed_by": {r: u for r, u in roles.items()},
            "version_tag": version_tag,
        }]

    return state


def get_page_version(gowiki_meta_root: str, page_relpath: str) -> int:
    """Get the current page version from Gowiki metadata."""
    # Page metadata is at meta/{path}.json (replacing .md with .json)
    meta_rel = page_relpath.replace(".md", ".json")
    meta_path = os.path.join(gowiki_meta_root, meta_rel)
    if os.path.isfile(meta_path):
        try:
            with open(meta_path) as f:
                data = json.load(f)
            return data.get("version", 1)
        except (json.JSONDecodeError, KeyError):
            pass
    return 1


def process_page(
    page_relpath: str,
    gowiki_content_root: str,
    gowiki_meta_root: str,
    dokuwiki_meta_root: str,
    dry_run: bool,
) -> str | None:
    """Process a single page. Returns a status message or None if skipped."""
    full_path = os.path.join(gowiki_content_root, page_relpath)
    with open(full_path, "r") as f:
        content = f.read()

    has_reviewflow = bool(re.search(r"^\{reviewflow\s", content, re.MULTILINE))

    # Find DokuWiki meta
    dw_meta_path = find_dokuwiki_meta(page_relpath, dokuwiki_meta_root)
    dw_meta = None
    if dw_meta_path:
        try:
            dw_meta = parse_dokuwiki_meta(dw_meta_path)
        except Exception as e:
            return f"WARN  {page_relpath}: failed to parse DokuWiki meta: {e}"

    if has_reviewflow:
        # Category A: existing reviewflow — import validation status only
        if not dw_meta:
            return None  # no DokuWiki meta to import from

        confirmed_by = get_reviewflow_validation(dw_meta)
        if not confirmed_by:
            return None  # not validated in DokuWiki

        # Parse the existing {reviewflow} directive to get roles and version
        m = re.search(r"^\{reviewflow\s+(.+)\}$", content, re.MULTILINE)
        if not m:
            return None
        directive_attrs = m.group(1)
        roles = {}
        version_tag = ""
        for km in re.finditer(r"(\S+?)=(\S+)", directive_attrs):
            k, v = km.group(1), km.group(2)
            if k == "version":
                version_tag = v
            else:
                roles[k] = v

        if not roles:
            return None

        page_version = get_page_version(gowiki_meta_root, page_relpath)
        state = create_reviewflow_state(roles, version_tag, confirmed_by, page_version)

        # Write .reviewflow.json
        rf_rel = page_relpath.replace(".md", ".reviewflow.json")
        rf_path = os.path.join(gowiki_meta_root, rf_rel)
        if dry_run:
            return f"DRY-A {page_relpath}: would create {rf_rel} (validated)"
        os.makedirs(os.path.dirname(rf_path), exist_ok=True)
        with open(rf_path, "w") as f:
            json.dump(state, f, indent=2)
        return f"CAT-A {page_relpath}: created {rf_rel} (validated)"

    else:
        # Category B: publish → reviewflow
        result = parse_author_table(content)
        if not result:
            return None  # no author table found

        roles, table_start, table_end = result
        version_tag = find_last_version(content)

        # Build the {reviewflow} directive
        parts = [f"version={version_tag}"]
        for role in ["author", "reviewer", "validation"]:
            if role in roles:
                parts.append(f"{role}={roles[role]}")
        # Any extra roles
        for role, user in roles.items():
            if role not in ("author", "reviewer", "validation"):
                parts.append(f"{role}={user}")

        directive = "{reviewflow " + " ".join(parts) + "}"

        # Replace the table block with the directive
        lines = content.split("\n")
        new_lines = lines[:table_start] + [directive] + lines[table_end:]
        new_content = "\n".join(new_lines)

        # Check publish validation status
        confirmed_by = None
        if dw_meta:
            approval = get_publish_approval_status(dw_meta)
            if approval:
                confirmed_by = roles  # all roles validated

        # Write modified page
        if dry_run:
            status = "validated" if confirmed_by else "not validated"
            return f"DRY-B {page_relpath}: would replace author table → {directive} ({status})"

        with open(full_path, "w") as f:
            f.write(new_content)

        msg = f"CAT-B {page_relpath}: replaced author table → {directive}"

        # Create .reviewflow.json if validated
        if confirmed_by:
            page_version = get_page_version(gowiki_meta_root, page_relpath)
            state = create_reviewflow_state(roles, version_tag, confirmed_by, page_version)
            rf_rel = page_relpath.replace(".md", ".reviewflow.json")
            rf_path = os.path.join(gowiki_meta_root, rf_rel)
            os.makedirs(os.path.dirname(rf_path), exist_ok=True)
            with open(rf_path, "w") as f:
                json.dump(state, f, indent=2)
            msg += " + created validation state"

        return msg


def main():
    parser = argparse.ArgumentParser(description="Migrate DokuWiki validations to Gowiki reviewflow")
    parser.add_argument("--gowiki-content", required=True, help="Path to Gowiki data/content/")
    parser.add_argument("--gowiki-meta", required=True, help="Path to Gowiki data/meta/")
    parser.add_argument("--dokuwiki-meta", required=True, help="Path to DokuWiki import/data/meta/")
    parser.add_argument("--dry-run", action="store_true", help="Preview changes without writing")
    args = parser.parse_args()

    content_root = args.gowiki_content
    meta_root = args.gowiki_meta
    dw_meta_root = args.dokuwiki_meta

    regulatory_dir = os.path.join(content_root, "regulatory")
    if not os.path.isdir(regulatory_dir):
        print(f"ERROR: {regulatory_dir} does not exist", file=sys.stderr)
        sys.exit(1)

    stats = {"cat_a": 0, "cat_b": 0, "skipped": 0, "warned": 0}

    for root, dirs, files in os.walk(regulatory_dir):
        # Exclude biomscope
        rel_root = os.path.relpath(root, content_root)
        if "biomscope" in rel_root.split(os.sep):
            dirs.clear()
            continue

        for fname in sorted(files):
            if not fname.endswith(".md"):
                continue
            page_relpath = os.path.relpath(os.path.join(root, fname), content_root)
            try:
                msg = process_page(page_relpath, content_root, meta_root, dw_meta_root, args.dry_run)
            except Exception as e:
                msg = f"ERROR {page_relpath}: {e}"

            if msg:
                print(msg)
                if msg.startswith("CAT-A") or msg.startswith("DRY-A"):
                    stats["cat_a"] += 1
                elif msg.startswith("CAT-B") or msg.startswith("DRY-B"):
                    stats["cat_b"] += 1
                elif msg.startswith("WARN") or msg.startswith("ERROR"):
                    stats["warned"] += 1
            else:
                stats["skipped"] += 1

    print(f"\n{'DRY RUN ' if args.dry_run else ''}Summary:")
    print(f"  Category A (reviewflow status import): {stats['cat_a']}")
    print(f"  Category B (table → reviewflow):       {stats['cat_b']}")
    print(f"  Skipped (no action needed):            {stats['skipped']}")
    print(f"  Warnings/errors:                       {stats['warned']}")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""
Database migration: renumber system IDs, rename pages, add {database-row} blocks.

For tables with integer index fields and <prefix><N> page names:
  - Renumber system id to match the original number N
  - Rename pages from <prefix><N> to <N>
  - Update page_path in DB
  - Update FK references (lookup/tag) in other tables
  - Update junction tables (multi_enum)
  - Archive the old index field
  - Reset the sequence

For all page-bound tables:
  - Add {database-row table=...} block at the bottom of each page

Must be run on the production server as root (or user with sudo).

Usage:
  python3 migrate_database_ids.py --dry-run   # preview changes
  python3 migrate_database_ids.py             # execute
"""

import argparse
import os
import re
import subprocess
import sys

# ── Configuration ──

CONTENT_DIR = "/opt/gowiki/data/content"

# Tables with integer index fields where pages are named <prefix><N>.
# Maps table_name -> (page_name_prefix, index_field_name)
# prefix=None means pages are already named with plain numbers.
INTEGER_ID_TABLES = {
    "capa":                 ("capa",                  "capaid"),
    "changecontrol":        ("change",                "changeid"),
    "infra_incident":       (None,                    "incidentid"),  # pages already numeric
    "integrationchecklist": ("integrationchecklist",  "integrationchecklistid"),
    "interviews":           ("interviews",            "interviewsid"),
    "nonconformity":        ("nc",                    "idnc"),
    "provider":             ("provider",              "providerid"),
}
# customercomplaint and provider_incident have 0 rows — skip them.

# FK references: (source_table, source_field) -> target_table
FK_REFERENCES = {
    ("customercomplaint", "capaid"):        "capa",
    ("nonconformity",     "capa"):          "capa",
    ("changecontrol",     "impactedchange"): "changecontrol",
    ("provider_incident", "provider"):      "provider",
}

# Junction tables for multi_enum fields: (table_name, field_name)
JUNCTION_TABLES = [
    ("capa", "origine_de_l_action"),
    ("provider", "category"),
]

OFFSET = 100000  # temporary ID offset to avoid conflicts


def psql(sql, fetch=True, superuser=False):
    """Run a SQL command via psql."""
    user = "postgres" if superuser else "gowiki"
    cmd = ["sudo", "-u", user, "psql", "-d", "gowiki", "-t", "-A", "-c", sql]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"SQL ERROR: {result.stderr.strip()}", file=sys.stderr)
        print(f"  SQL: {sql}", file=sys.stderr)
        sys.exit(1)
    if not fetch:
        return []
    lines = result.stdout.strip().split("\n") if result.stdout.strip() else []
    return lines


def psql_exec(sql, superuser=False):
    """Execute SQL without fetching results."""
    psql(sql, fetch=False, superuser=superuser)


def get_rows(table_name):
    """Get all rows (id, page_path) for a table."""
    rows = psql(f'SELECT id, page_path FROM "_{table_name}" ORDER BY id;')
    result = []
    for line in rows:
        if "|" not in line:
            continue
        parts = line.split("|", 1)
        result.append((int(parts[0]), parts[1]))
    return result


def extract_number(page_path, prefix):
    """Extract the numeric ID from a page path like /foo/bar/capa7 -> 7."""
    page_name = page_path.rstrip("/").rsplit("/", 1)[-1]
    if prefix is None:
        try:
            return int(page_name)
        except ValueError:
            return None
    if page_name.startswith(prefix):
        try:
            return int(page_name[len(prefix):])
        except ValueError:
            return None
    return None


# ── Main migration steps ──

def step_1_build_mappings():
    """Build old_system_id -> new_id mappings for all integer-indexed tables."""
    print("Step 1: Build ID mappings")
    mappings = {}  # table_name -> {old_id: new_id}

    for table_name, (prefix, index_field) in INTEGER_ID_TABLES.items():
        rows = get_rows(table_name)
        if not rows:
            print(f"  {table_name}: no rows, skipping")
            continue

        mapping = {}
        for old_id, page_path in rows:
            new_id = extract_number(page_path, prefix)
            if new_id is not None:
                mapping[old_id] = new_id
            else:
                print(f"  WARNING: cannot extract number from {page_path}")

        needs_change = sum(1 for k, v in mapping.items() if k != v)
        print(f"  {table_name}: {len(mapping)} rows, {needs_change} need renumbering")
        mappings[table_name] = mapping

    return mappings


def step_2_build_link_map(mappings):
    """Build old_page_path -> new_page_path mapping BEFORE any changes."""
    print("\nStep 2: Build link map")
    link_map = {}  # old_path -> new_path

    for table_name, mapping in mappings.items():
        prefix = INTEGER_ID_TABLES[table_name][0]
        rows = get_rows(table_name)

        for old_id, page_path in rows:
            if not page_path:
                continue
            new_id = mapping.get(old_id, old_id)
            folder = page_path.rsplit("/", 1)[0]
            new_path = f"{folder}/{new_id}"
            if page_path != new_path:
                link_map[page_path] = new_path

    print(f"  {len(link_map)} pages will be renamed")
    return link_map


def step_3_renumber_ids(mappings, dry_run):
    """Renumber system IDs and update FK references + junction tables."""
    print("\nStep 3: Renumber system IDs")

    for table_name, mapping in mappings.items():
        needs_change = {k: v for k, v in mapping.items() if k != v}
        if not needs_change:
            print(f"  {table_name}: no changes needed")
            continue

        print(f"  {table_name}: renumbering {len(needs_change)} rows")
        for old_id, new_id in sorted(needs_change.items()):
            print(f"    {old_id} -> {new_id}")

        if dry_run:
            continue

        # Disable triggers on main table and junction tables (requires superuser)
        psql_exec(f'ALTER TABLE "_{table_name}" DISABLE TRIGGER ALL;', superuser=True)
        for jt_table, jt_field in JUNCTION_TABLES:
            if jt_table == table_name:
                psql_exec(f'ALTER TABLE "_{table_name}__{jt_field}" DISABLE TRIGGER ALL;', superuser=True)

        # Pass 1: shift to temp IDs
        for old_id in sorted(needs_change.keys()):
            tmp_id = old_id + OFFSET
            psql_exec(f'UPDATE "_{table_name}" SET id = {tmp_id} WHERE id = {old_id};')
            # Junction tables
            for jt_table, jt_field in JUNCTION_TABLES:
                if jt_table == table_name:
                    psql_exec(f'UPDATE "_{table_name}__{jt_field}" SET row_id = {tmp_id} WHERE row_id = {old_id};')
            # FK references in other tables
            for (src_table, src_field), tgt_table in FK_REFERENCES.items():
                if tgt_table == table_name:
                    psql_exec(f'UPDATE "_{src_table}" SET "{src_field}" = {tmp_id} WHERE "{src_field}" = {old_id};')

        # Pass 2: set final IDs
        for old_id, new_id in sorted(needs_change.items()):
            tmp_id = old_id + OFFSET
            psql_exec(f'UPDATE "_{table_name}" SET id = {new_id} WHERE id = {tmp_id};')
            for jt_table, jt_field in JUNCTION_TABLES:
                if jt_table == table_name:
                    psql_exec(f'UPDATE "_{table_name}__{jt_field}" SET row_id = {new_id} WHERE row_id = {tmp_id};')
            for (src_table, src_field), tgt_table in FK_REFERENCES.items():
                if tgt_table == table_name:
                    psql_exec(f'UPDATE "_{src_table}" SET "{src_field}" = {new_id} WHERE "{src_field}" = {tmp_id};')

        # Re-enable triggers
        psql_exec(f'ALTER TABLE "_{table_name}" ENABLE TRIGGER ALL;', superuser=True)
        for jt_table, jt_field in JUNCTION_TABLES:
            if jt_table == table_name:
                psql_exec(f'ALTER TABLE "_{table_name}__{jt_field}" ENABLE TRIGGER ALL;', superuser=True)

        # Reset sequence
        max_id = max(mapping.values())
        psql_exec(f"SELECT setval('\"_{table_name}_id_seq\"', {max_id});")


def step_4_update_page_paths(mappings, dry_run):
    """Update page_path in DB and rename page files on disk."""
    print("\nStep 4: Rename pages")

    for table_name, mapping in mappings.items():
        prefix = INTEGER_ID_TABLES[table_name][0]
        rows = get_rows(table_name)

        for row_id, old_page_path in rows:
            if not old_page_path:
                continue

            folder = old_page_path.rsplit("/", 1)[0]
            new_page_path = f"{folder}/{row_id}"

            if old_page_path == new_page_path:
                continue

            print(f"  {old_page_path} -> {new_page_path}")

            if dry_run:
                continue

            # Update DB
            psql_exec(f"UPDATE \"_{table_name}\" SET page_path = '{new_page_path}' WHERE id = {row_id};")

            # Rename on disk
            old_disk = os.path.join(CONTENT_DIR, old_page_path.lstrip("/"))
            new_disk = os.path.join(CONTENT_DIR, new_page_path.lstrip("/"))
            old_md = old_disk + ".md"
            new_md = new_disk + ".md"

            if os.path.isdir(old_disk):
                os.makedirs(os.path.dirname(new_disk), exist_ok=True)
                os.rename(old_disk, new_disk)
            elif os.path.exists(old_md):
                os.makedirs(os.path.dirname(new_md), exist_ok=True)
                os.rename(old_md, new_md)
            else:
                print(f"    WARNING: file not found: {old_md} or {old_disk}/")


def step_5_update_links(link_map, dry_run):
    """Update internal links in all .md files."""
    print("\nStep 5: Update internal links")

    if not link_map:
        print("  No links to update")
        return

    changes = 0
    for dirpath, dirnames, filenames in os.walk(CONTENT_DIR):
        for fn in filenames:
            if not fn.endswith(".md"):
                continue
            filepath = os.path.join(dirpath, fn)
            with open(filepath, "r", encoding="utf-8") as f:
                content = f.read()

            new_content = content
            for old_path, new_path in link_map.items():
                if old_path in new_content:
                    new_content = new_content.replace(old_path, new_path)

            if new_content != content:
                rel = os.path.relpath(filepath, CONTENT_DIR)
                changes += 1
                print(f"  Updated: {rel}")
                if not dry_run:
                    with open(filepath, "w", encoding="utf-8") as f:
                        f.write(new_content)

    print(f"  {changes} files updated")


def step_6_archive_fields(mappings, dry_run):
    """Archive old index fields."""
    print("\nStep 6: Archive old index fields")

    for table_name in mappings:
        index_field = INTEGER_ID_TABLES[table_name][1]
        print(f"  Archiving: {table_name}.{index_field}")
        if dry_run:
            continue
        psql_exec(f"""
            UPDATE database_fields SET archived_at = NOW()
            WHERE table_id = (SELECT id FROM database_tables WHERE name = '{table_name}')
              AND name = '{index_field}'
              AND archived_at IS NULL;
        """)


def step_7_add_database_row_blocks(dry_run):
    """Add {database-row table=...} blocks to all page-bound pages."""
    print("\nStep 7: Add {database-row} blocks")

    # Get all page-bound tables
    tables = psql("SELECT name FROM database_tables WHERE page_folder <> '' ORDER BY name;")

    for table_name in [t.strip() for t in tables if t.strip()]:
        # Get non-archived fields with their types
        field_lines = psql(f"""
            SELECT name, type FROM database_fields
            WHERE table_id = (SELECT id FROM database_tables WHERE name = '{table_name}')
              AND archived_at IS NULL
            ORDER BY display_order, id;
        """)
        fields = []  # (name, type)
        for fl in field_lines:
            if "|" not in fl:
                continue
            fn, ft = fl.strip().split("|", 1)
            fields.append((fn, ft))

        regular_fields = [(fn, ft) for fn, ft in fields if ft != "multi_enum"]
        multi_enum_fields = [(fn, ft) for fn, ft in fields if ft == "multi_enum"]

        rows = get_rows(table_name)
        for row_id, page_path in rows:
            if not page_path:
                continue

            # Find page file
            disk_path = os.path.join(CONTENT_DIR, page_path.lstrip("/"))
            md_file = disk_path + ".md"
            if os.path.isdir(disk_path):
                md_file = os.path.join(disk_path, "index.md")

            if not os.path.exists(md_file):
                print(f"  WARNING: {md_file} not found")
                continue

            with open(md_file, "r", encoding="utf-8") as f:
                content = f.read()

            if f"{{database-row table={table_name}}}" in content:
                continue

            # Get regular field values
            field_values = {}
            if regular_fields:
                cols = ", ".join(f'"{fn}"' for fn, _ in regular_fields)
                val_lines = psql(f'SELECT {cols} FROM "_{table_name}" WHERE id = {row_id};')
                if val_lines:
                    values = val_lines[0].split("|")
                    for i, (fn, _) in enumerate(regular_fields):
                        if i < len(values):
                            field_values[fn] = values[i] if values[i] else ""

            # Get multi_enum values from junction tables
            for fn, _ in multi_enum_fields:
                jt = f"_{table_name}__{fn}"
                me_lines = psql(f'SELECT value FROM "{jt}" WHERE row_id = {row_id} ORDER BY value;')
                field_values[fn] = ", ".join(v.strip() for v in me_lines if v.strip())

            # Build block
            block = f"{{database-row table={table_name}}}\n"
            block += "| Field | Value |\n"
            block += "| --- | --- |\n"
            for fn, _ in fields:
                val = field_values.get(fn, "")
                val = val.replace("|", "\\|")
                block += f"| {fn} | {val} |\n"

            print(f"  {table_name}: {page_path}")

            if dry_run:
                continue

            if not content.endswith("\n"):
                content += "\n"
            content += "\n" + block

            with open(md_file, "w", encoding="utf-8") as f:
                f.write(content)


def main():
    parser = argparse.ArgumentParser(description="Migrate database IDs and pages")
    parser.add_argument("--dry-run", action="store_true", help="Preview changes")
    args = parser.parse_args()

    dry_run = args.dry_run
    print(f"=== Database ID Migration ({'DRY RUN' if dry_run else 'EXECUTING'}) ===\n")

    mappings = step_1_build_mappings()
    link_map = step_2_build_link_map(mappings)
    step_3_renumber_ids(mappings, dry_run)
    step_4_update_page_paths(mappings, dry_run)
    step_5_update_links(link_map, dry_run)
    step_6_archive_fields(mappings, dry_run)
    step_7_add_database_row_blocks(dry_run)

    print("\n=== Done ===")


if __name__ == "__main__":
    main()

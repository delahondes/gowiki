#!/usr/bin/env python3
"""Convert DokuWiki struct table/lookup blocks and form blocks to Gowiki database directives.

Handles:
  - ---- struct table ---- ... ---- → {database-query table=... fields="..." ...}
  - ---- struct lookup ---- ... ---- → {database-query table=...}
  - <form>...</form> → {database-newrow table=...}

Also unwraps code fences (```) that wrap these blocks (artifact of the import).

Extracts table configuration (page_folder, index_field, page_template_path) from
<form> blocks and prints them as SQL UPDATE statements for manual review.

Usage:
  python3 convert_struct_blocks.py <content_dir> [--dry-run]
"""

import os
import re
import sys

DRY_RUN = "--dry-run" in sys.argv


def convert_struct_block(lines):
    """Convert a struct table/lookup block to a database-query directive."""
    attrs = {}
    for line in lines:
        line = line.strip()
        if line.startswith("schema:"):
            attrs["table"] = line.split(":", 1)[1].strip()
        elif line.startswith("cols:"):
            raw_cols = line.split(":", 1)[1].strip()
            # Convert %pageid% to %title%
            raw_cols = raw_cols.replace("%pageid%", "%title%")
            # If cols is just "%title%, *" or "*, ..." with wildcard, skip fields attr
            # (show all fields is the default)
            parts = [c.strip() for c in raw_cols.split(",")]
            # Remove standalone * entries
            parts = [p for p in parts if p != "*"]
            if parts:
                attrs["fields"] = ", ".join(parts)
        elif line.startswith("sort:"):
            raw_sort = line.split(":", 1)[1].strip()
            # DokuWiki sort can be multi-field: category,^provider,%title%
            # We only support single sort in Gowiki, take the first one
            sort_fields = [s.strip() for s in raw_sort.split(",") if s.strip()]
            if sort_fields:
                first = sort_fields[0]
                if first.startswith("^"):
                    attrs["sort"] = first[1:]
                    attrs["order"] = "desc"
                else:
                    attrs["sort"] = first
        elif line.startswith("filter:"):
            raw_filter = line.split(":", 1)[1].strip()
            if "filter" in attrs:
                attrs["filter"] += "&" + raw_filter
            else:
                attrs["filter"] = raw_filter
        # Ignore dynfilters and other unknown attrs

    if "table" not in attrs:
        return None

    # Build directive
    parts = [f'table={attrs["table"]}']
    if "fields" in attrs:
        fields_val = attrs["fields"]
        parts.append(f'fields="{fields_val}"')
    if "filter" in attrs:
        f = attrs["filter"]
        parts.append(f'filter="{f}"')
    if "sort" in attrs:
        parts.append(f'sort={attrs["sort"]}')
    if attrs.get("order") == "desc":
        parts.append("order=desc")

    return "{database-query " + " ".join(parts) + "}"


def parse_form_block(lines):
    """Parse a <form> block and return (table_name, template_info_dict)."""
    schema = None
    template_path = None
    page_folder = None
    index_field = None
    submit_label = None

    for line in lines:
        line = line.strip()
        if line.startswith("struct_schema"):
            # struct_schema "schemaname" !
            m = re.search(r'"([^"]+)"', line)
            if m:
                schema = m.group(1)
        elif line.startswith("Action template"):
            # Action template <template_path> <page_folder>:<schema>[<schema>.<index>]
            # or with link syntax: ...[schema.index](url)
            rest = line[len("Action template"):].strip()
            parts = rest.split(None, 1)
            if parts:
                # Convert DokuWiki path (colons) to Gowiki path (slashes)
                template_path = "/" + parts[0].replace(":", "/")
            if len(parts) > 1:
                # Parse page_folder and index_field from second part
                second = parts[1]
                # Pattern: regulatory:smq:ps03:server:[server.server_name](url)
                # or: regulatory:smq:ps03:server:@@schemaname.field@@
                # Find the schema.field reference
                m = re.search(r'\[(\w+)\.(\w+)\]', second)
                if not m:
                    m = re.search(r'@@(\w+)\.(\w+)@@', second)
                if m:
                    index_field = m.group(2)
                    # page_folder is everything before the [schema.field] part
                    # Extract the DokuWiki path prefix
                    bracket_pos = second.find("[")
                    if bracket_pos == -1:
                        bracket_pos = second.find("@@")
                    if bracket_pos > 0:
                        folder_part = second[:bracket_pos].rstrip(":")
                        page_folder = "/" + folder_part.replace(":", "/")
        elif line.startswith("submit"):
            m = re.search(r'"([^"]+)"', line)
            if m:
                submit_label = m.group(1)

    return schema, {
        "template_path": template_path,
        "page_folder": page_folder,
        "index_field": index_field,
        "submit_label": submit_label,
    }


def process_file(filepath):
    """Process a single .md file, converting struct/form blocks."""
    with open(filepath, "r", encoding="utf-8") as f:
        content = f.read()

    original = content
    table_configs = []

    # 1. Convert struct table/lookup blocks (possibly wrapped in code fences)
    # Pattern: optional ``` before, then ---- struct (table|lookup) ----, content, ----, optional ```
    def replace_struct_block(m):
        block_text = m.group(0)
        # Extract the lines between the ---- markers
        inner_match = re.search(
            r'----\s+struct\s+(?:table|lookup)\s+----\s*\n(.*?)-{3,4}',
            block_text, re.DOTALL
        )
        if not inner_match:
            return block_text

        inner_lines = inner_match.group(1).strip().split("\n")
        directive = convert_struct_block(inner_lines)
        if directive is None:
            return block_text

        return directive + "\n"

    # Match: optional ```\n before, ---- struct ... ----, optional \n```
    # Also handle bare blocks (no code fence) and blocks with trailing ---
    pattern = re.compile(
        r'(?:```\s*\n)?'               # optional opening code fence
        r'----\s+struct\s+(?:table|lookup)\s+----\s*\n'
        r'(.*?)'                        # block content
        r'-{3,4}\s*\n?'                  # closing --- or ----
        r'(?:```\s*\n?)?',             # optional closing code fence
        re.DOTALL
    )
    content = pattern.sub(replace_struct_block, content)

    # 2. Convert <form> blocks
    def replace_form_block(m):
        block_text = m.group(1)
        form_lines = block_text.strip().split("\n")
        schema, config = parse_form_block(form_lines)
        if schema:
            table_configs.append((schema, config))
            return "{database-newrow table=" + schema + "}\n"
        return m.group(0)  # leave unchanged if we can't parse

    form_pattern = re.compile(r'<form>\s*\n(.*?)</form>\s*\n?', re.DOTALL)
    content = form_pattern.sub(replace_form_block, content)

    changed = content != original
    if changed:
        rel = os.path.relpath(filepath)
        if DRY_RUN:
            print(f"WOULD MODIFY: {rel}")
        else:
            with open(filepath, "w", encoding="utf-8") as f:
                f.write(content)
            print(f"MODIFIED: {rel}")

    return table_configs


def main():
    if len(sys.argv) < 2:
        print("Usage: convert_struct_blocks.py <content_dir> [--dry-run]")
        sys.exit(1)

    content_dir = sys.argv[1]
    all_configs = []

    for root, dirs, files in os.walk(content_dir):
        for fname in sorted(files):
            if not fname.endswith(".md"):
                continue
            filepath = os.path.join(root, fname)
            configs = process_file(filepath)
            all_configs.extend(configs)

    if all_configs:
        print("\n--- Table configurations extracted from <form> blocks ---")
        print("-- These should be set via the admin UI or SQL:")
        for schema, cfg in all_configs:
            print(f"\n-- Table: {schema}")
            if cfg["page_folder"]:
                print(f"--   page_folder:        {cfg['page_folder']}")
            if cfg["index_field"]:
                print(f"--   index_field:         {cfg['index_field']}")
            if cfg["template_path"]:
                print(f"--   page_template_path:  {cfg['template_path']}")
            if cfg["submit_label"]:
                print(f"--   submit_label:        {cfg['submit_label']}")


if __name__ == "__main__":
    main()

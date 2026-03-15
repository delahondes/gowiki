#!/usr/bin/env python3
"""Replace DokuWiki-style colon namespace separators with slashes in identifiers.

Converts patterns like MQ01:SOP01 → MQ01/SOP01, QARA:SOP05:REC01 → QARA/SOP05/REC01
in page content (headings, link text, inline references).

Only processes files under the specified directory.
Skips IMPORT:FLAG markers.
"""

import os
import re
import sys

# Match colon between uppercase identifier segments:
# e.g. MQ01:SOP01, IFU:SOFT01:SOP, GSPR:SOFT01:v2.1
# Pattern: UPPER+DIGITS colon UPPER/lower+digits, possibly chained
IDENT_COLON_RE = re.compile(r'([A-Z][A-Z0-9]*):([A-Za-z][A-Za-z0-9]*)')

SKIP_PATTERNS = {'IMPORT:FLAG'}


def fix_colons(content):
    """Replace colon separators in identifiers with slashes."""
    def replacer(m):
        full = m.group(0)
        if full in SKIP_PATTERNS:
            return full
        return m.group(1) + '/' + m.group(2)

    # Apply repeatedly since chained colons (A:B:C) need multiple passes
    # (first pass converts A:B:C → A/B:C, second converts A/B:C... no)
    # Actually the regex matches non-overlapping pairs, so A:B:C matches
    # A:B first, leaving :C. We need to loop.
    prev = None
    while prev != content:
        prev = content
        content = IDENT_COLON_RE.sub(replacer, content)
    return content


def process_file(filepath, dry_run=False):
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()

    new_content = fix_colons(content)

    if new_content == content:
        return 0

    # Count changes
    old_count = len(IDENT_COLON_RE.findall(content))
    # Rough count: number of colons replaced
    changes = content.count(':') - new_content.count(':')

    if dry_run:
        print(f"  {filepath}: {changes} colon(s) to replace")
        # Show a few examples
        for line_no, (old_line, new_line) in enumerate(
            zip(content.splitlines(), new_content.splitlines()), 1
        ):
            if old_line != new_line:
                print(f"    L{line_no}: {old_line.strip()[:120]}")
                print(f"      → {new_line.strip()[:120]}")
                if line_no > 10:
                    print(f"    ... (more changes)")
                    break
        return changes

    with open(filepath, 'w', encoding='utf-8') as f:
        f.write(new_content)
    print(f"  Fixed {filepath}: {changes} colon(s)")
    return changes


def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <content_dir> [--dry-run]")
        sys.exit(1)

    content_dir = sys.argv[1]
    dry_run = '--dry-run' in sys.argv

    if dry_run:
        print("DRY RUN — no files will be modified\n")

    total_files = 0
    total_changes = 0

    # Skip TF (Technical File) documents — they reference the legacy doc system
    skip_dirs = {'biomscope'}

    for root, dirs, files in os.walk(content_dir):
        dirs[:] = [d for d in dirs if d not in skip_dirs]
        for fname in files:
            if not fname.endswith('.md'):
                continue
            filepath = os.path.join(root, fname)
            changes = process_file(filepath, dry_run)
            if changes:
                total_files += 1
                total_changes += changes

    print(f"\n{'Would replace' if dry_run else 'Replaced'} {total_changes} colon(s) in {total_files} file(s)")


if __name__ == '__main__':
    main()

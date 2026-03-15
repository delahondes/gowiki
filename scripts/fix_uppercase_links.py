#!/usr/bin/env python3
"""Fix uppercase internal page links in Gowiki markdown files.

Lowercases the path portion of internal page links like [text](/PATH/TO/PAGE)
while preserving:
- Link display text (before the `](`)
- External URLs (http://, https://, mailto:, etc.)
- Attachment links (paths with file extensions)
- Anchors (#fragment) — lowercased too since headings are lowercase
"""

import os
import re
import sys

# Internal page link: ](/path) or ](/path#anchor)
# Must NOT have a file extension (that would be an attachment)
LINK_RE = re.compile(r'(\]\()(/[^)]+?)(\))')

# File extensions that indicate an attachment, not a page link
ATTACHMENT_EXTS = {
    '.png', '.jpg', '.jpeg', '.gif', '.svg', '.webp', '.bmp',
    '.pdf', '.doc', '.docx', '.xls', '.xlsx', '.ppt', '.pptx',
    '.odt', '.ods', '.zip', '.tar', '.gz', '.7z', '.rar',
    '.csv', '.txt', '.rtf', '.mp3', '.mp4', '.avi', '.mov',
    '.wav', '.ogg', '.json', '.xml', '.html', '.md', '.epub',
}


def has_extension(path):
    """Check if path has a file extension (attachment)."""
    # Strip anchor
    p = path.split('#')[0]
    _, ext = os.path.splitext(p)
    return ext.lower() in ATTACHMENT_EXTS


def fix_link(m):
    prefix = m.group(1)  # ](
    path = m.group(2)    # /PATH/TO/PAGE or /PATH/TO/PAGE#anchor
    suffix = m.group(3)  # )

    # Skip attachments
    if has_extension(path):
        return m.group(0)

    # Lowercase the entire path (including anchor)
    return prefix + path.lower() + suffix


def process_file(filepath, dry_run=False):
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()

    new_content = LINK_RE.sub(fix_link, content)

    if new_content == content:
        return 0

    # Count changes
    changes = sum(1 for a, b in zip(
        LINK_RE.findall(content), LINK_RE.findall(new_content)
    ) if a != b)

    if dry_run:
        print(f"  {filepath}: {changes} link(s) to fix")
        # Show diffs
        for old, new in zip(LINK_RE.findall(content), LINK_RE.findall(new_content)):
            if old != new:
                print(f"    {old[0]}{old[1]}{old[2]} -> {new[0]}{new[1]}{new[2]}")
        return changes

    with open(filepath, 'w', encoding='utf-8') as f:
        f.write(new_content)
    print(f"  Fixed {filepath}: {changes} link(s)")
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
    total_links = 0

    for root, _, files in os.walk(content_dir):
        for fname in files:
            if not fname.endswith('.md'):
                continue
            filepath = os.path.join(root, fname)
            changes = process_file(filepath, dry_run)
            if changes:
                total_files += 1
                total_links += changes

    print(f"\n{'Would fix' if dry_run else 'Fixed'} {total_links} link(s) in {total_files} file(s)")


if __name__ == '__main__':
    main()

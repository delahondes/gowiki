#!/usr/bin/env python3
"""Transform <code LANG>...</code> and <codeprism> DokuWiki remnants into fenced code blocks."""

import re
import sys
import os

DRY_RUN = "--dry-run" in sys.argv

# Files to skip (documentation about DokuWiki syntax)
SKIP_FILES = {"wiki/syntax.md"}

# Language normalization
LANG_MAP = {
    "sudo": "bash",
    "dos": "bat",
    "R": "r",
    "cron": "bash",
    "dokuwiki": "",
}

# Regex for opening and closing tags
OPEN_RE = re.compile(r'<code(?:prism)?\b([^>]*)\\?>')
CLOSE_RE = re.compile(r'</code(?:prism)?\s*\\?>')


def extract_lang(attrs):
    """Extract language from tag attributes."""
    attrs = attrs.strip().rstrip('\\')
    if not attrs:
        return ""

    # <codeprism lang=r> or <codeprism params.yaml lang=yaml el=true>
    m = re.search(r'\blang=(\w+)', attrs)
    if m:
        lang = m.group(1)
        return LANG_MAP.get(lang, lang)

    # <code - myfile.foo> = no highlighting
    if attrs.startswith('-') or attrs.startswith(' -'):
        return ""

    # <code bash>, <code ini zabbix_agent2.conf>, <code python>
    # Take only the first word as language
    first_word = attrs.split()[0] if attrs.split() else ""
    return LANG_MAP.get(first_word, first_word)


def process_file(filepath, base_dir):
    rel = os.path.relpath(filepath, base_dir)
    if rel in SKIP_FILES:
        return 0

    with open(filepath, 'r') as f:
        content = f.read()

    original = content
    lines = content.split('\n')
    result = []
    i = 0
    changes = 0

    while i < len(lines):
        line = lines[i]

        m = OPEN_RE.search(line)
        if not m:
            result.append(line)
            i += 1
            continue

        attrs = m.group(1)
        lang = extract_lang(attrs)
        before_tag = line[:m.start()]
        after_tag = line[m.end():]

        # Check if closing tag is on the same line
        cm = CLOSE_RE.search(after_tag)
        if cm:
            # Single-line: <code bash>content</code> possibly with text after
            code_content = after_tag[:cm.start()]
            after_close = after_tag[cm.end():]

            # Check for ANOTHER open tag after this close (multiple on one line)
            # e.g. "text <code>X</code> more <code>Y</code>"
            # Handle by just doing one at a time - the next iteration will catch the rest

            before_stripped = before_tag.rstrip()
            # Detect indentation from the line
            indent = re.match(r'^(\s*)', line).group(1)

            # If within a list item, use deeper indentation for the fence
            # Detect list context: before_tag starts with spaces + "- " or "1. "
            list_match = re.match(r'^(\s*(?:[-*]|\d+\.)\s+)', before_tag)
            if list_match:
                fence_indent = ' ' * len(list_match.group(1))
            elif before_stripped:
                fence_indent = indent + '  '
            else:
                fence_indent = indent

            fence_open = f"{fence_indent}```{lang}"
            fence_close = f"{fence_indent}```"

            if code_content.strip():
                # Code has content
                if before_stripped:
                    result.append(before_stripped)
                result.append(fence_open)
                # Multi-line content within single-line tags
                for cline in code_content.split('\n'):
                    result.append(f"{fence_indent}{cline.strip()}")
                result.append(fence_close)
            else:
                # Empty code block (rare)
                if before_stripped:
                    result.append(before_stripped)
                result.append(fence_open)
                result.append(fence_close)

            if after_close.strip():
                result.append(f"{fence_indent}{after_close.strip()}")

            changes += 1
            i += 1
            continue

        # Multiline: find closing tag on a subsequent line
        code_lines = []
        if after_tag.strip():
            code_lines.append(after_tag)

        j = i + 1
        found_close = False
        while j < len(lines):
            cm = CLOSE_RE.search(lines[j])
            if cm:
                before_close = lines[j][:cm.start()]
                if before_close.strip():
                    code_lines.append(before_close)
                found_close = True
                break
            else:
                code_lines.append(lines[j])
            j += 1

        if not found_close:
            # No closing tag found, leave line as-is
            result.append(line)
            i += 1
            continue

        # Determine indentation
        before_stripped = before_tag.rstrip()
        indent = re.match(r'^(\s*)', line).group(1)

        # Detect the indentation of the code content to set fence indentation
        # Use the indentation of the closing tag line as reference
        close_indent = re.match(r'^(\s*)', lines[j]).group(1)

        # For list items: fence should be at the content indentation level
        list_match = re.match(r'^(\s*(?:[-*]|\d+\.)\s+)', before_tag)
        if list_match:
            fence_indent = ' ' * len(list_match.group(1))
        elif close_indent:
            fence_indent = close_indent
        else:
            fence_indent = indent

        fence_open = f"{fence_indent}```{lang}"
        fence_close = f"{fence_indent}```"

        if before_stripped:
            result.append(before_stripped)
        result.append(fence_open)
        # Re-indent content lines if they're less indented than the fence
        fence_len = len(fence_indent)
        if fence_len > 0 and code_lines:
            # Find minimum indentation of non-empty content lines
            min_indent = None
            for cl in code_lines:
                if cl.strip():
                    cl_indent = len(cl) - len(cl.lstrip())
                    if min_indent is None or cl_indent < min_indent:
                        min_indent = cl_indent
            if min_indent is not None and min_indent < fence_len:
                delta = fence_len - min_indent
                padding = ' ' * delta
                for cl in code_lines:
                    if cl.strip():
                        result.append(padding + cl)
                    else:
                        result.append(cl)
            else:
                for cl in code_lines:
                    result.append(cl)
        else:
            for cl in code_lines:
                result.append(cl)
        result.append(fence_close)

        changes += 1
        i = j + 1
        continue

    new_content = '\n'.join(result)

    if new_content != original:
        if DRY_RUN:
            print(f"WOULD FIX {rel}: {changes} code block(s)")
        else:
            with open(filepath, 'w') as f:
                f.write(new_content)
            print(f"FIXED {rel}: {changes} code block(s)")
        return changes
    return 0


def main():
    if len(sys.argv) < 2 or sys.argv[1].startswith('-'):
        print(f"Usage: {sys.argv[0]} <content-dir> [--dry-run]")
        sys.exit(1)

    content_dir = sys.argv[1]
    total_files = 0
    total_blocks = 0

    for root, dirs, files in os.walk(content_dir):
        for fname in files:
            if not fname.endswith('.md'):
                continue
            filepath = os.path.join(root, fname)
            n = process_file(filepath, content_dir)
            if n:
                total_files += 1
                total_blocks += n

    print(f"\nSummary: {total_blocks} code block(s) in {total_files} file(s)")
    if DRY_RUN:
        print("(dry run, no changes made)")


if __name__ == '__main__':
    main()

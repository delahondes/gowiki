#!/usr/bin/env python3
"""Fix DokuWiki table cell syntax that was lost during import.

Handles two issues:
  1. !!text!! vertical text — still present in converted files, needs {vtext=upward} prefix
  2. @color: cell colors — stripped during import, needs restoration from DokuWiki sources

Usage:
  # Fix !!text!! in converted files (no DokuWiki source needed)
  python3 fix_table_cells.py /path/to/content --fix-vtext [--dry-run]

  # Fix @color: by reading from DokuWiki source
  python3 fix_table_cells.py /path/to/content --fix-colors --dw-pages /path/to/import/data/pages [--dry-run]

  # Fix both
  python3 fix_table_cells.py /path/to/content --fix-vtext --fix-colors --dw-pages /path/to/import/data/pages [--dry-run]
"""

import os
import re
import sys

DRY_RUN = "--dry-run" in sys.argv
FIX_VTEXT = "--fix-vtext" in sys.argv
FIX_COLORS = "--fix-colors" in sys.argv

# Regex for !!text!! — vertical text in table cells
# Match !!content!! but not !!! (which is just emphasis in DokuWiki)
RE_VTEXT = re.compile(r'!!([^!]+)!!')

# Regex for @color: in DokuWiki source
RE_DW_COLOR = re.compile(r'@([A-Za-z]+|#[0-9a-fA-F]{3,6}):')


def fix_vtext_in_line(line):
    """Convert !!text!! to {vtext=upward} text in a table line."""
    if not line.startswith("|"):
        return line

    def replace_vtext(m):
        inner = m.group(1).strip()
        return "{vtext=upward} " + inner

    return RE_VTEXT.sub(replace_vtext, line)


def get_dw_page_path(content_path, content_dir, dw_pages_dir):
    """Convert a Gowiki content path to the corresponding DokuWiki page path."""
    rel = os.path.relpath(content_path, content_dir)
    # content/a/b/c.md → pages/a/b/c.txt
    if rel.endswith(".md"):
        rel = rel[:-3] + ".txt"
    # index.md → start.txt
    if rel.endswith("start.txt"):
        pass  # already correct if it was start
    elif rel.endswith("/index.txt") or rel == "index.txt":
        rel = rel.rsplit("index.txt", 1)[0] + "start.txt"

    # Handle namespace renames (reverse mapping)
    # These are the renames that were applied during import
    reverse_renames = {
        "dir": "ps01",
        "qara": "ps02",
        "soft": "ps03",  # ps03 and ps05 both map to soft — try both
        "cpm": "ps04",
        "res": "ps06",  # ps06 and ps07 both map to res — try both
    }

    dw_path = os.path.join(dw_pages_dir, rel)
    if os.path.exists(dw_path):
        return dw_path

    # Try reverse namespace renames
    parts = rel.split(os.sep)
    for i, part in enumerate(parts):
        if part in reverse_renames:
            # Try the primary reverse rename
            alt_parts = parts[:i] + [reverse_renames[part]] + parts[i + 1:]
            alt_path = os.path.join(dw_pages_dir, os.sep.join(alt_parts))
            if os.path.exists(alt_path):
                return alt_path
            # Try secondary mappings (ps05→soft, ps07→res)
            if part == "soft":
                alt_parts = parts[:i] + ["ps05"] + parts[i + 1:]
                alt_path = os.path.join(dw_pages_dir, os.sep.join(alt_parts))
                if os.path.exists(alt_path):
                    return alt_path
            if part == "res":
                alt_parts = parts[:i] + ["ps07"] + parts[i + 1:]
                alt_path = os.path.join(dw_pages_dir, os.sep.join(alt_parts))
                if os.path.exists(alt_path):
                    return alt_path

    return None


def split_table_cells(line):
    """Split a pipe table line into cells, respecting links and code spans."""
    if not line.startswith("|"):
        return None
    # Remove leading and trailing |
    inner = line[1:]
    if inner.endswith("|"):
        inner = inner[:-1]

    cells = []
    current = []
    in_code = False
    in_link = 0
    i = 0
    while i < len(inner):
        ch = inner[i]
        if ch == '`':
            in_code = not in_code
            current.append(ch)
        elif not in_code and ch == '[' and i + 1 < len(inner) and inner[i + 1] == '(':
            # markdown link text[(...)]
            current.append(ch)
        elif not in_code and ch == '|' and in_link == 0:
            cells.append("".join(current))
            current = []
        else:
            current.append(ch)
        i += 1
    cells.append("".join(current))
    return cells


def extract_dw_cell_colors(dw_line):
    """Extract @color: prefixes from a DokuWiki table line, return list of (color, stripped_text)."""
    if not dw_line.strip() or dw_line.strip()[0] not in ("|", "^"):
        return None

    # Simple split by | and ^ for DokuWiki
    parts = re.split(r'[|^]', dw_line.strip())
    # First and last are usually empty
    colors = []
    for part in parts[1:]:  # skip first empty part
        part = part.strip()
        m = RE_DW_COLOR.match(part)
        if m:
            color = m.group(1).lower()
            text = part[m.end():].strip()
            colors.append((color, text))
        else:
            colors.append((None, part))
    return colors


def find_table_lines(lines):
    """Find table line groups (consecutive lines starting with |)."""
    groups = []
    current = []
    for i, line in enumerate(lines):
        if line.startswith("|"):
            current.append(i)
        else:
            if current:
                groups.append(current)
                current = []
    if current:
        groups.append(current)
    return groups


def restore_colors_in_file(filepath, content_dir, dw_pages_dir):
    """Restore @color: cell colors by comparing with DokuWiki source."""
    dw_path = get_dw_page_path(filepath, content_dir, dw_pages_dir)
    if dw_path is None or not os.path.exists(dw_path):
        return False

    with open(filepath, "r", encoding="utf-8") as f:
        gw_lines = f.readlines()

    with open(dw_path, "r", encoding="utf-8") as f:
        dw_content = f.read()

    # Check if DokuWiki source has any @color: in table cells
    dw_lines = dw_content.split("\n")
    dw_table_lines = [l for l in dw_lines if l.strip() and l.strip()[0] in ("|", "^") and RE_DW_COLOR.search(l)]
    if not dw_table_lines:
        return False

    # Build a map of DokuWiki table lines with colors, keyed by a fingerprint
    # of their non-color text content (to match with converted lines)
    changed = False

    # Process each Gowiki table line and try to find matching DokuWiki line
    for i, gw_line in enumerate(gw_lines):
        if not gw_line.startswith("|"):
            continue
        # Skip separator lines
        if re.match(r'^\|(\s*---\s*\|)+\s*$', gw_line):
            continue

        gw_cells = split_table_cells(gw_line.rstrip("\n"))
        if gw_cells is None:
            continue

        # Try to find a matching DokuWiki line
        best_match = None
        best_score = 0

        for dw_line in dw_table_lines:
            dw_colors = extract_dw_cell_colors(dw_line)
            if dw_colors is None:
                continue

            # Check if any cell has a color
            has_color = any(c[0] is not None for c in dw_colors)
            if not has_color:
                continue

            # Score: count cells where stripped DW text appears in GW cell
            score = 0
            matchable = min(len(gw_cells), len(dw_colors))
            for j in range(matchable):
                gw_text = gw_cells[j].strip()
                dw_text = dw_colors[j][1]
                # Normalize for comparison: strip inline markup differences
                gw_norm = re.sub(r'[*_`~\[\](){}]', '', gw_text).strip().lower()
                dw_norm = re.sub(r'[*/\'_~\[\](){}\\<>]', '', dw_text).strip().lower()
                if gw_norm and dw_norm and (gw_norm in dw_norm or dw_norm in gw_norm):
                    score += 1

            if score > best_score and score >= matchable * 0.4:
                best_score = score
                best_match = dw_colors

        if best_match is None:
            continue

        # Apply colors to the Gowiki cells
        new_cells = []
        for j, gw_cell in enumerate(gw_cells):
            stripped = gw_cell.strip()
            if j < len(best_match) and best_match[j][0] is not None:
                color = best_match[j][0]
                # Check if cell already has a {color=...} directive
                if not stripped.startswith("{"):
                    # Check if cell already has {vtext=...} — merge directives
                    if stripped.startswith("{vtext="):
                        # Insert color into existing directive
                        stripped = stripped.replace("{vtext=", "{color=" + color + " vtext=", 1)
                    else:
                        stripped = "{color=" + color + "} " + stripped
                    changed = True

            # Preserve original spacing
            leading = len(gw_cell) - len(gw_cell.lstrip())
            trailing = len(gw_cell) - len(gw_cell.rstrip())
            new_cells.append(gw_cell[:leading] + stripped + gw_cell[len(gw_cell) - trailing:] if trailing else gw_cell[:leading] + stripped)

        if changed:
            new_line = "|" + "|".join(new_cells) + "|\n"
            gw_lines[i] = new_line

    if changed:
        rel = os.path.relpath(filepath)
        if DRY_RUN:
            print(f"WOULD MODIFY (colors): {rel}")
        else:
            with open(filepath, "w", encoding="utf-8") as f:
                f.writelines(gw_lines)
            print(f"MODIFIED (colors): {rel}")

    return changed


def fix_vtext_in_file(filepath):
    """Fix !!text!! vertical text markers in a file."""
    with open(filepath, "r", encoding="utf-8") as f:
        content = f.read()

    if "!!" not in content:
        return False

    lines = content.split("\n")
    changed = False

    for i, line in enumerate(lines):
        if not line.startswith("|"):
            continue
        new_line = fix_vtext_in_line(line)
        if new_line != line:
            lines[i] = new_line
            changed = True

    if changed:
        rel = os.path.relpath(filepath)
        if DRY_RUN:
            print(f"WOULD MODIFY (vtext): {rel}")
        else:
            with open(filepath, "w", encoding="utf-8") as f:
                f.write("\n".join(lines))
            print(f"MODIFIED (vtext): {rel}")

    return changed


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    if not args:
        print("Usage: fix_table_cells.py <content_dir> [--fix-vtext] [--fix-colors --dw-pages <dir>] [--dry-run]")
        sys.exit(1)

    content_dir = args[0]
    dw_pages_dir = None

    if FIX_COLORS:
        try:
            idx = sys.argv.index("--dw-pages")
            dw_pages_dir = sys.argv[idx + 1]
        except (ValueError, IndexError):
            print("--fix-colors requires --dw-pages <path>")
            sys.exit(1)

    if not FIX_VTEXT and not FIX_COLORS:
        print("Specify at least one of --fix-vtext or --fix-colors")
        sys.exit(1)

    vtext_count = 0
    color_count = 0

    for root, dirs, files in os.walk(content_dir):
        for fname in sorted(files):
            if not fname.endswith(".md"):
                continue
            filepath = os.path.join(root, fname)

            if FIX_VTEXT:
                if fix_vtext_in_file(filepath):
                    vtext_count += 1

            if FIX_COLORS and dw_pages_dir:
                if restore_colors_in_file(filepath, content_dir, dw_pages_dir):
                    color_count += 1

    print(f"\n--- Summary ---")
    if FIX_VTEXT:
        print(f"Vertical text fixes: {vtext_count} files")
    if FIX_COLORS:
        print(f"Color fixes: {color_count} files")


if __name__ == "__main__":
    main()

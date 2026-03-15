#!/usr/bin/env python3
"""Reintroduce DokuWiki folded blocks as Gowiki spoiler blocks.

Uses distinctive text anchors to find content boundaries in each page,
then wraps the content in ```spoiler Title ... ``` fences.
"""

import re
import sys
import os

DRY_RUN = "--dry-run" in sys.argv


def find_line(lines, pattern, start=0):
    """Find line index containing pattern (substring match)."""
    for i in range(start, len(lines)):
        if pattern in lines[i]:
            return i
    return -1


def find_heading(lines, text, start=0):
    """Find a markdown heading containing text."""
    for i in range(start, len(lines)):
        if re.match(r'^#+\s', lines[i]) and text in lines[i]:
            return i
    return -1


def find_code_fence_end(lines, start):
    """Find closing ``` after an opening ``` at start."""
    for i in range(start + 1, len(lines)):
        if lines[i].strip() == '```':
            return i
    return -1


def wrap_in_spoiler(lines, title, start, end):
    """Insert spoiler fences around lines[start:end]."""
    # Trim empty lines at boundaries
    while start < end and not lines[start].strip():
        start += 1
    while end > start and not lines[end - 1].strip():
        end -= 1
    if start >= end:
        return lines, False

    # Check if already wrapped
    if start > 0 and '```spoiler' in lines[start - 1]:
        return lines, False

    result = lines[:start] + [f'```spoiler {title}'] + lines[start:end] + ['```'] + lines[end:]
    return result, True


def process_file(filepath, rules):
    """Apply spoiler wrapping rules to a file."""
    with open(filepath) as f:
        lines = f.read().split('\n')

    changes = 0
    # Apply rules in reverse order to preserve line numbers
    applied = []
    for rule in rules:
        title = rule['title']
        # Find start anchor
        start = -1
        if 'start_after_text' in rule:
            idx = find_line(lines, rule['start_after_text'])
            if idx >= 0:
                start = idx + 1
        elif 'start_after_heading' in rule:
            idx = find_heading(lines, rule['start_after_heading'])
            if idx >= 0:
                start = idx + 1
        elif 'start_at_text' in rule:
            start = find_line(lines, rule['start_at_text'])

        if start < 0:
            print(f"  WARNING: cannot find start for '{title}'")
            continue

        # Find end anchor
        end = len(lines)
        if 'end_before_text' in rule:
            idx = find_line(lines, rule['end_before_text'], start)
            if idx >= 0:
                end = idx
        elif 'end_before_heading' in rule:
            idx = find_heading(lines, rule['end_before_heading'], start)
            if idx >= 0:
                end = idx
        elif 'end_after_code_fence' in rule:
            # Find the closing ``` of a code block starting at/after start
            fence_start = find_line(lines, '```', start)
            if fence_start >= 0:
                fence_end = find_code_fence_end(lines, fence_start)
                if fence_end >= 0:
                    end = fence_end + 1

        if end <= start:
            print(f"  WARNING: empty range for '{title}' (start={start}, end={end})")
            continue

        applied.append((title, start, end))

    # Apply in reverse order
    for title, start, end in sorted(applied, key=lambda x: -x[1]):
        lines, ok = wrap_in_spoiler(lines, title, start, end)
        if ok:
            changes += 1
            print(f"  WRAPPED '{title}' (lines {start+1}-{end})")

    return lines, changes


# Define rules for each page
PAGES = {
    "bioit/cr/2024-09-10.md": [
        {
            "title": "extract mpa4 genomes",
            "start_after_text": "Le code est ci après",
            "end_before_text": "Vadim est en train",
        },
    ],
    "bioit/cr/2024-12-03.md": [
        {
            "title": "sortie typique",
            "start_after_text": "k_penalty permet de durcir",
            "end_before_text": "Le but est d'être très efficace",
        },
    ],
    "bioit/cr/2025-04-29.md": [
        {
            "title": "Les autres donneurs",
            "start_after_text": "pattern n'est pas observé",  # after the caption
            "end_before_text": "Species presents in at least",
        },
    ],
    "bioit/cr/2025-10-28.md": [
        {
            "title": "Détail des 21 labos",
            "start_at_text": "![](/bioit/cr/pasted/20251028-162258.png)",
            "end_before_text": "Même patient non reconnaissable",
        },
    ],
    "bioit/cr/2025-12-09.md": [
        {
            "title": "Code utilisé",
            "start_after_text": "absence de de pénalité à l'overfit",
            "end_before_heading": "Todo",
        },
    ],
    "regulatory/smq/kpi.md": [
        {
            "title": "Gestion des valeurs de statut",
            "start_at_text": "{database-query table=smqkpi_status}",
            "end_before_heading": "Archivage",
        },
    ],
    "regulatory/smq/res/sop04/rec01.md": [
        {
            "title": "Fiches entretien annuel cloturées",
            "start_after_heading": "cloturées et archivées",
            "end_before_heading": "Ajout d'une fiche",
        },
    ],
    "regulatory/smq/soft/sop06/rec02.md": [
        {
            "title": "This solution is no longer used, click here to view the archive",
            "start_after_heading": "Flow-archiver (.7z archive)",
            # End at the Archives-section Multicloud heading (not the Primary Backup one)
            "end_before_text": "former archiving solution",
        },
        {
            "title": "This solution is no longer recommended outside HDS backups, click here to view the archive",
            # Use distinctive text from the Archives Multicloud section
            "start_after_text": "former archiving solution",
            "end_before_heading": "Rclone",
        },
    ],
    "regulatory/smq/soft/sop06/rec03.md": [
        {
            "title": "Gestion des statuts",
            "start_at_text": "#### Statuts",
            "end_before_heading": "Archivage",
        },
    ],
    "regulatory/smq/qara/sop01/ins01.md": [
        {
            "title": "Cliquer pour dévoiler",
            "start_at_text": "Ce texte est masqué.",
            "end_before_text": "This text is hidden.",
        },
        {
            "title": "Click to reveal",
            "start_at_text": "This text is hidden.",
            "end_before_text": "include plugin:",
        },
    ],
    "si/software/zabbix/client.md": [
        {
            "title": "smart_forensic.py",
            "start_after_text": "Add this script:",
            "end_before_text": "Add this in zabbix_agent2.conf:",
        },
    ],
}


def add_ioscope_rules():
    """Generate rules for ioscope3 — 7 code blocks before specific headings."""
    # Each folded block wraps a ```python code block before a heading
    # The headings after each code block (from DW source analysis):
    headings_after = [
        "Confusion Matrix",
        "Kaplan Meier",
        "Toposcore",     # after Kaplan Meier > IOScope 3
        "PFS",           # after Kaplan Meier > Toposcore
        "Toposcore",     # after PFS > IOScope 3
        "Comparison with PD-L1",
        None,            # last one goes to EOF
    ]
    # Rather than matching each individually (tricky with repeated "Toposcore"),
    # we find all ```python blocks and wrap each one
    PAGES["biomarker/ioscope3/index.md"] = "IOSCOPE_SPECIAL"


add_ioscope_rules()


def process_ioscope(filepath):
    """Special handling for ioscope3 — wrap each standalone python code block in spoiler."""
    with open(filepath) as f:
        lines = f.read().split('\n')

    # Find all ```python code blocks that are NOT in the first section
    # (the first two code blocks at lines ~49 and ~60 are part of the dataset section,
    # not folded blocks)
    code_blocks = []
    i = 0
    while i < len(lines):
        if lines[i].strip().startswith('```python'):
            end = find_code_fence_end(lines, i)
            if end >= 0:
                code_blocks.append((i, end + 1))
                i = end + 1
                continue
        i += 1

    # The first two code blocks (around lines 49 and 60) are NOT folded blocks
    # The folded blocks start from the 3rd code block onwards
    folded_blocks = [cb for cb in code_blocks if cb[0] > 100]

    changes = 0
    # Apply in reverse
    for start, end in reversed(folded_blocks):
        # Check if already wrapped
        if start > 0 and '```spoiler' in lines[start - 1]:
            continue
        lines = lines[:start] + ['```spoiler Source code'] + lines[start:end] + ['```'] + lines[end:]
        changes += 1
        print(f"  WRAPPED 'Source code' (lines {start+1}-{end})")

    return lines, changes


def main():
    if len(sys.argv) < 2 or sys.argv[1].startswith('-'):
        print(f"Usage: {sys.argv[0]} <gowiki-content-dir> [--dry-run]")
        sys.exit(1)

    content_dir = sys.argv[1]
    total_files = 0
    total_blocks = 0

    for rel, rules in sorted(PAGES.items()):
        filepath = os.path.join(content_dir, rel)
        if not os.path.exists(filepath):
            print(f"SKIP {rel} (not found)")
            continue

        print(f"\n{'=' * 60}")
        print(f"FILE: {rel}")

        if rules == "IOSCOPE_SPECIAL":
            new_lines, n_changes = process_ioscope(filepath)
        else:
            new_lines, n_changes = process_file(filepath, rules)

        if n_changes > 0:
            if DRY_RUN:
                print(f"  ({n_changes} change(s), dry run)")
            else:
                with open(filepath, 'w') as f:
                    f.write('\n'.join(new_lines))
                print(f"  WRITTEN: {n_changes} spoiler(s)")
            total_files += 1
            total_blocks += n_changes
        else:
            print(f"  (no changes)")

    print(f"\n{'=' * 60}")
    print(f"Summary: {total_blocks} spoiler(s) in {total_files} file(s)")
    if DRY_RUN:
        print("(dry run, no changes made)")


if __name__ == '__main__':
    main()

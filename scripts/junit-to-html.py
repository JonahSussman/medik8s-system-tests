#!/usr/bin/env python3
"""Generate test-summary.html from JUnit XML files in $ARTIFACT_DIR.

Handles Ginkgo's malformed <system-err> blocks (unescaped '<' characters)
by stripping them with regex before XML parsing.
"""

import glob
import html
import os
import re
import sys
import defusedxml.ElementTree as ET
from datetime import datetime, timezone


def strip_system_err(content):
    return re.sub(r'<system-err>.*?</system-err>', '', content, flags=re.DOTALL)


def parse_testcase(raw_name):
    """Extract (suite, test, tid) from a Ginkgo verbose testcase name.

    Strips the leading '[It]' prefix and trailing label block, then splits
    the remainder into a suite group and a test description.  Returns
    (None, None, None) for synthetic entries such as ReportAfterSuite.
    """
    if 'ReportAfterSuite' in raw_name or 'ReportBeforeSuite' in raw_name:
        return None, None, None

    tid_match = re.search(r'test_id:(\d+)', raw_name)
    tid = tid_match.group(1) if tid_match else '—'

    clean = re.sub(r'^\[It\]\s+', '', raw_name)
    clean = re.sub(r'(?:\s*\[[^\]]+\])+\s*$', '', clean).strip()

    m = re.match(r'^(.+?)\s+((?:Verify|Discover|Check|Test|Ensure|Assert)\s+.+)$', clean)
    if m:
        return m.group(1).strip(), m.group(2).strip(), tid
    return '—', clean, tid


def status_cell(tc):
    """Return (html_badge, failure_message) for a testcase element.

    Inspects child elements (<failure>, <error>, <skipped>) to determine
    outcome.  The failure message is HTML-escaped and truncated to 400 chars.
    Passing tests return an empty failure message.
    """
    f = tc.find('failure')
    e = tc.find('error')
    if f is not None or e is not None:
        node = f if f is not None else e
        raw = (node.attrib.get('message') or node.text or '').strip()
        msg = html.escape(raw[:400])
        return '<span class="fail">✗ FAIL</span>', msg
    if tc.attrib.get('status') == 'skipped' or tc.find('skipped') is not None:
        return '<span class="skip">⊘ SKIP</span>', ''
    return '<span class="pass">✓ PASS</span>', ''


TEMPLATE = """\
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Test Summary — {suite_name}</title>
<style>
  body  {{ font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
           margin: 2rem; color: #1a1a1a; }}
  h1   {{ font-size: 1.4rem; margin-bottom: 0.2rem; }}
  .meta {{ color: #666; font-size: 0.85rem; margin-bottom: 1rem; }}
  .summary {{ margin-bottom: 1.2rem; font-size: 0.95rem; }}
  .summary b {{ font-weight: 700; }}
  table {{ border-collapse: collapse; width: 100%; font-size: 0.88rem; }}
  th  {{ background: #f4f4f4; text-align: left; padding: 0.5rem 0.75rem;
         border-bottom: 2px solid #ddd; white-space: nowrap; }}
  td  {{ padding: 0.45rem 0.75rem; border-bottom: 1px solid #eee; vertical-align: top; }}
  tr:hover td {{ background: #fafafa; }}
  .pass {{ color: #1a7f37; font-weight: 600; }}
  .fail {{ color: #cf222e; font-weight: 600; }}
  .skip {{ color: #9a6700; font-weight: 600; }}
  .dur  {{ color: #555; font-size: 0.82rem; white-space: nowrap; }}
  .tid  {{ color: #555; font-size: 0.82rem; font-family: monospace; }}
  .msg  {{ color: #cf222e; font-size: 0.8rem; margin-top: 0.3rem; white-space: pre-wrap; }}
</style>
</head>
<body>
<h1>Test Summary — {suite_name}</h1>
<p class="meta">Generated {generated} &nbsp;|&nbsp; {total} tests &nbsp;|&nbsp; {duration}s total</p>
<p class="summary">
  <b class="pass">{passed} passed</b> &nbsp;&nbsp;
  <b class="fail">{failed} failed</b> &nbsp;&nbsp;
  <b class="skip">{skipped} skipped</b>
</p>
<table>
<thead><tr>
  <th>#</th><th>Suite</th><th>Test</th><th>ID</th><th>Status</th><th>Duration</th>
</tr></thead>
<tbody>
{rows}
</tbody>
</table>
</body>
</html>
"""


def build_html(xml_files):
    """Parse xml_files and return a complete HTML page as a string.

    Each file is stripped of <system-err> blocks before parsing to handle
    Ginkgo's unescaped '<' characters.  Files that cannot be parsed are
    skipped with a warning.  The returned HTML is self-contained and requires
    no external assets.
    """
    rows = []
    total = passed = failed = skipped = 0
    total_dur = 0.0
    suite_names = set()
    n = 0

    for path in xml_files:
        with open(path, encoding='utf-8', errors='replace') as fh:
            content = fh.read()

        content = strip_system_err(content)

        try:
            root = ET.fromstring(content)
        except ET.ParseError as exc:
            print(f"Warning: skipping {path}: {exc}", file=sys.stderr)
            continue

        for ts in root.iter('testsuite'):
            suite_names.add(ts.attrib.get('name', ''))

        for tc in root.iter('testcase'):
            suite, test, tid = parse_testcase(tc.attrib.get('name', ''))
            if suite is None:
                continue

            n += 1
            total += 1
            try:
                dur = float(tc.attrib.get('time') or 0)
            except (TypeError, ValueError):
                dur = 0.0
            total_dur += dur

            badge, msg = status_cell(tc)
            if 'FAIL' in badge:
                failed += 1
            elif 'SKIP' in badge:
                skipped += 1
            else:
                passed += 1

            fail_html = f'<div class="msg">{msg}</div>' if msg else ''
            rows.append(
                f'<tr>'
                f'<td>{n}</td>'
                f'<td>{html.escape(suite)}</td>'
                f'<td>{html.escape(test)}{fail_html}</td>'
                f'<td class="tid">{html.escape(tid)}</td>'
                f'<td>{badge}</td>'
                f'<td class="dur">{dur:.2f}s</td>'
                f'</tr>'
            )

    body = '\n'.join(rows) if rows else '<tr><td colspan="6">No test cases found.</td></tr>'
    return TEMPLATE.format(
        suite_name=html.escape(', '.join(sorted(suite_names)) or 'Unknown'),
        generated=datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M UTC'),
        total=total,
        duration=f'{total_dur:.1f}',
        passed=passed,
        failed=failed,
        skipped=skipped,
        rows=body,
    )


def main():
    """Entry point: locate JUnit XML files and write test-summary.html to ARTIFACT_DIR.

    Scans ECO_REPORTS_DUMP_DIR (where the runner writes XMLs), falling back to
    ARTIFACT_DIR.  Always writes the HTML output to ARTIFACT_DIR so Prow picks
    it up, even when the caller has overridden ECO_REPORTS_DUMP_DIR.  Exits
    with code 0 if no scan root is available or no XML files are found so the
    caller's exit code is not affected.
    """
    report_dir = os.environ.get('ECO_REPORTS_DUMP_DIR') or os.environ.get('ARTIFACT_DIR', '')
    if not report_dir:
        print("Neither ECO_REPORTS_DUMP_DIR nor ARTIFACT_DIR set — skipping.", file=sys.stderr)
        sys.exit(0)

    xml_files = list({
        *glob.glob(os.path.join(report_dir, '*_junit.xml')),
        *glob.glob(os.path.join(report_dir, '**', '*_junit.xml'), recursive=True),
    })

    if not xml_files:
        print(f"No *_junit.xml files found in {report_dir}", file=sys.stderr)
        sys.exit(0)

    page = build_html(xml_files)

    out_dir = os.environ.get('ARTIFACT_DIR', report_dir)
    out = os.path.join(out_dir, 'test-summary.html')
    with open(out, 'w', encoding='utf-8') as fh:
        fh.write(page)

    print(f"Test summary written to {out}")


if __name__ == '__main__':
    main()

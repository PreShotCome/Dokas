"""Enrich a list of candidate companies with signals that map to the cold
outreach segments A-E in docs/outreach/.

Input:  a CSV with at least a `domain` column (one row per candidate).
Output: the same CSV plus derived columns — the strongest matching segment,
        a confidence score, the specific signals detected, and where they
        were found (URL).

Design constraints:
    * Standard-library + requests + beautifulsoup4 only. No API keys required.
    * Politeness: single-threaded, 2s inter-domain delay, custom User-Agent,
      hard timeouts, robots.txt respected.
    * Rules-based scoring. Every rule is inspectable in RULES below; you can
      add more without touching the fetch logic.

Usage:
    pip install requests beautifulsoup4
    python docs/outreach/crawl_prospects.py --in candidates.csv --out enriched.csv

If you want person-level contact emails, layer Hunter.io or Apollo on
top of the enriched CSV — this script deliberately does NOT do
contact-email discovery, which needs a paid data source to be useful.
"""

from __future__ import annotations

import argparse
import csv
import re
import sys
import time
import urllib.parse
import urllib.robotparser
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from typing import Iterable

try:
    import requests
    from bs4 import BeautifulSoup
except ImportError:
    print("This script needs `requests` and `beautifulsoup4`:", file=sys.stderr)
    print("  pip install requests beautifulsoup4", file=sys.stderr)
    sys.exit(2)


USER_AGENT = (
    "Dokaz-Outreach-Research/0.1 "
    "(compliance discovery; contact: sales@dokaz.net) "
    "requests"
)
TIMEOUT = 10                # per-request wall-clock cap
POLITE_DELAY_SEC = 2.0      # between distinct domains
PATHS = ["/", "/security", "/trust", "/compliance", "/legal",
         "/careers", "/jobs", "/company", "/about", "/blog"]


# =============================================================================
# Signal rules — each rule contributes to a segment score. Keep them narrow;
# false positives cost more than missed matches in outbound.
# =============================================================================

SEG_A = "A - SOC 2 in flight"
SEG_B = "B - Cyber-insurance renewal window"
SEG_C = "C - PagerDuty user"
SEG_D = "D - Recent SOC 2 announcement"
SEG_E = "E - Hiring security / compliance engineer"


@dataclass
class Rule:
    segment: str
    pattern: re.Pattern
    weight: int
    label: str  # short human-readable label shown in the output CSV


def r(pattern: str, *, flags=re.IGNORECASE) -> re.Pattern:
    return re.compile(pattern, flags)


RULES: list[Rule] = [
    # --- Segment A: SOC 2 in flight ---
    Rule(SEG_A, r(r"vanta\.com/trust|vanta trust"),                 3, "vanta trust page"),
    Rule(SEG_A, r(r"drata\.com/trust|drata trust"),                 3, "drata trust page"),
    Rule(SEG_A, r(r"secureframe\.com/trust|secureframe trust"),     3, "secureframe trust page"),
    Rule(SEG_A, r(r"SOC ?2\b.{0,40}(in progress|underway|pending|type ?I|coming)"), 4,
         "SOC 2 in progress copy"),
    Rule(SEG_A, r(r"working (toward|towards) (our )?SOC ?2"),       3, "'working toward SOC 2'"),

    # --- Segment B: cyber-insurance renewal ---
    Rule(SEG_B, r(r"cyber[- ]?insurance"),                          2, "cyber-insurance mention"),
    Rule(SEG_B, r(r"coalition|at[- ]bay|corvus|resilience|cowbell"), 1, "cyber-carrier name"),
    Rule(SEG_B, r(r"ransomware.{0,80}(recover|restore|backup)"),    2, "ransomware/restore context"),

    # --- Segment C: PagerDuty user ---
    Rule(SEG_C, r(r"pagerduty\.com|pagerduty|pdt-|Powered by PagerDuty"), 4, "PagerDuty reference"),
    Rule(SEG_C, r(r"status\.[a-z0-9.-]+"),                          1, "status page (weak signal)"),

    # --- Segment D: recent SOC 2 announcement ---
    Rule(SEG_D, r(r"achiev(ed|es|ing) SOC ?2 Type ?II"),            5, "achieved SOC 2 Type II"),
    Rule(SEG_D, r(r"we (are )?SOC ?2 (Type ?II )?certified"),       4, "SOC 2 certified claim"),
    Rule(SEG_D, r(r"SOC ?2 (Type ?II )?compliant"),                 3, "SOC 2 compliant claim"),

    # --- Segment E: hiring security / compliance eng ---
    Rule(SEG_E, r(r"security engineer|security eng\b"),             4, "hiring: security engineer"),
    Rule(SEG_E, r(r"compliance engineer|grc engineer"),             4, "hiring: compliance engineer"),
    Rule(SEG_E, r(r"platform eng.{0,40}security"),                  2, "hiring: platform eng + security"),
]


# =============================================================================
# Fetch layer
# =============================================================================

@dataclass
class SiteCorpus:
    """Everything we read from one domain, keyed by URL."""
    domain: str
    pages: dict[str, str] = field(default_factory=dict)  # url -> text (already stripped)

    def all_text(self) -> str:
        return "\n\n".join(self.pages.values())


def fetch(url: str) -> str | None:
    try:
        resp = requests.get(url, timeout=TIMEOUT, allow_redirects=True,
                            headers={"User-Agent": USER_AGENT})
    except requests.RequestException:
        return None
    if resp.status_code >= 400 or "text/html" not in resp.headers.get("Content-Type", ""):
        return None
    # Strip scripts + styles, then take visible text.
    soup = BeautifulSoup(resp.text, "html.parser")
    for tag in soup(["script", "style", "noscript"]):
        tag.decompose()
    return re.sub(r"\s+", " ", soup.get_text(separator=" "))[:200_000]


def load_robots(base: str) -> urllib.robotparser.RobotFileParser | None:
    rp = urllib.robotparser.RobotFileParser()
    rp.set_url(urllib.parse.urljoin(base, "/robots.txt"))
    try:
        rp.read()
    except Exception:
        return None
    return rp


def crawl_domain(domain: str) -> SiteCorpus:
    base = domain if domain.startswith("http") else f"https://{domain}"
    corpus = SiteCorpus(domain=domain)

    rp = load_robots(base)
    for path in PATHS:
        url = urllib.parse.urljoin(base, path)
        if rp is not None and not rp.can_fetch(USER_AGENT, url):
            continue
        text = fetch(url)
        if text:
            corpus.pages[url] = text
    return corpus


# =============================================================================
# Score + label
# =============================================================================

@dataclass
class Verdict:
    segment: str
    score: int
    signals: list[str]         # human labels
    evidence_urls: list[str]   # where each signal was found


def score(corpus: SiteCorpus) -> dict[str, Verdict]:
    by_segment: dict[str, Verdict] = {}
    for url, text in corpus.pages.items():
        for rule in RULES:
            if rule.pattern.search(text):
                v = by_segment.setdefault(
                    rule.segment,
                    Verdict(segment=rule.segment, score=0, signals=[], evidence_urls=[]),
                )
                if rule.label not in v.signals:
                    v.signals.append(rule.label)
                    v.evidence_urls.append(url)
                    v.score += rule.weight
    return by_segment


def best_segment(verdicts: dict[str, Verdict]) -> Verdict | None:
    if not verdicts:
        return None
    return max(verdicts.values(), key=lambda v: v.score)


# =============================================================================
# CLI
# =============================================================================

def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--in", dest="input", required=True, help="input CSV with a `domain` column")
    ap.add_argument("--out", dest="output", required=True, help="output CSV path")
    ap.add_argument("--limit", type=int, default=0, help="stop after N candidates (0 = no limit)")
    ap.add_argument("--min-score", type=int, default=3,
                    help="drop candidates whose best segment scored under this (default: 3)")
    args = ap.parse_args(argv)

    with open(args.input, newline="") as f:
        reader = csv.DictReader(f)
        if not reader.fieldnames or "domain" not in reader.fieldnames:
            print("input CSV needs a `domain` column", file=sys.stderr)
            return 2
        rows = list(reader)

    if args.limit:
        rows = rows[: args.limit]

    out_fields = list(reader.fieldnames) + [
        "best_segment", "score", "signals", "evidence_urls", "crawled_at",
    ]

    with open(args.output, "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=out_fields)
        writer.writeheader()

        for i, row in enumerate(rows, 1):
            domain = row["domain"].strip().lower()
            if not domain:
                continue
            print(f"[{i}/{len(rows)}] {domain}", file=sys.stderr)
            corpus = crawl_domain(domain)
            verdicts = score(corpus)
            top = best_segment(verdicts)

            row["crawled_at"] = datetime.utcnow().isoformat(timespec="seconds") + "Z"
            if top is None or top.score < args.min_score:
                row["best_segment"] = ""
                row["score"] = ""
                row["signals"] = ""
                row["evidence_urls"] = ""
            else:
                row["best_segment"] = top.segment
                row["score"] = str(top.score)
                row["signals"] = " · ".join(top.signals)
                row["evidence_urls"] = " · ".join(top.evidence_urls)
            writer.writerow(row)

            time.sleep(POLITE_DELAY_SEC)

    return 0


if __name__ == "__main__":
    sys.exit(main())

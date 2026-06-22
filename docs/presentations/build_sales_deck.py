"""Generate Dokaz_Sales_Deck.pptx — a 6-slide deck to send to a business
prospect. Ends with a sign-up CTA. Same Dokaz brand palette as the
investor deck (red + gold + lapis touches), different audience.
"""

from pptx import Presentation
from pptx.util import Inches, Pt, Emu
from pptx.dml.color import RGBColor
from pptx.enum.shapes import MSO_SHAPE
from pptx.enum.text import PP_ALIGN, MSO_ANCHOR
from pathlib import Path

BRAND_900 = RGBColor(0x7F, 0x1D, 0x1D)
BRAND_800 = RGBColor(0x99, 0x1B, 0x1B)
BRAND_700 = RGBColor(0xB9, 0x1C, 0x1C)
BRAND_500 = RGBColor(0xEF, 0x44, 0x44)
BRAND_300 = RGBColor(0xFC, 0xA5, 0xA5)
BRAND_100 = RGBColor(0xFE, 0xE2, 0xE2)
BRAND_50  = RGBColor(0xFE, 0xF2, 0xF2)

GOLD_700  = RGBColor(0xA1, 0x62, 0x07)
GOLD_500  = RGBColor(0xEA, 0xB3, 0x08)
GOLD_300  = RGBColor(0xFD, 0xE0, 0x47)
GOLD_100  = RGBColor(0xFE, 0xF9, 0xC3)

LAPIS_900 = RGBColor(0x1E, 0x3A, 0x8A)
LAPIS_700 = RGBColor(0x1D, 0x4D, 0xD8)

EMERALD     = RGBColor(0x10, 0xB9, 0x81)
EMERALD_700 = RGBColor(0x04, 0x78, 0x57)
EMERALD_50  = RGBColor(0xEC, 0xFD, 0xF5)

ZINC_900 = RGBColor(0x18, 0x18, 0x1B)
ZINC_700 = RGBColor(0x3F, 0x3F, 0x46)
ZINC_500 = RGBColor(0x71, 0x71, 0x7A)
ZINC_400 = RGBColor(0xA1, 0xA1, 0xAA)
ZINC_200 = RGBColor(0xE4, 0xE4, 0xE7)
ZINC_50  = RGBColor(0xFA, 0xFA, 0xFA)

RED_700 = RGBColor(0xB9, 0x1C, 0x1C)
WHITE   = RGBColor(0xFF, 0xFF, 0xFF)


prs = Presentation()
prs.slide_width  = Inches(13.333)
prs.slide_height = Inches(7.5)
BLANK = prs.slide_layouts[6]


def add_slide():
    return prs.slides.add_slide(BLANK)


def textbox(slide, left, top, width, height, text, *,
            size=18, bold=False, color=ZINC_900, align=PP_ALIGN.LEFT,
            anchor=MSO_ANCHOR.TOP, font='Calibri'):
    tb = slide.shapes.add_textbox(left, top, width, height)
    tf = tb.text_frame
    tf.word_wrap = True
    tf.vertical_anchor = anchor
    tf.margin_left = tf.margin_right = tf.margin_top = tf.margin_bottom = 0
    p = tf.paragraphs[0]
    p.alignment = align
    r = p.add_run()
    r.text = text
    r.font.size = Pt(size)
    r.font.bold = bold
    r.font.color.rgb = color
    r.font.name = font
    return tb


def rect(slide, left, top, width, height, fill, line=None):
    shp = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, left, top, width, height)
    shp.fill.solid()
    shp.fill.fore_color.rgb = fill
    if line is None:
        shp.line.fill.background()
    else:
        shp.line.color.rgb = line
        shp.line.width = Pt(0.75)
    shp.shadow.inherit = False
    return shp


def rounded(slide, left, top, width, height, fill, line=None, radius=0.05):
    shp = slide.shapes.add_shape(MSO_SHAPE.ROUNDED_RECTANGLE, left, top, width, height)
    shp.adjustments[0] = radius
    shp.fill.solid()
    shp.fill.fore_color.rgb = fill
    if line is None:
        shp.line.fill.background()
    else:
        shp.line.color.rgb = line
        shp.line.width = Pt(0.75)
    shp.shadow.inherit = False
    return shp


def page_chrome(slide, eyebrow, title, slide_num, total):
    rect(slide, Inches(0), Inches(0), prs.slide_width, Inches(0.08), BRAND_700)
    textbox(slide, Inches(0.6), Inches(0.32), Inches(8), Inches(0.3),
            eyebrow.upper(), size=10, bold=True, color=BRAND_700)
    textbox(slide, Inches(0.6), Inches(0.58), Inches(12), Inches(0.7),
            title, size=30, bold=True, color=ZINC_900)
    textbox(slide, Inches(12.2), Inches(7.05), Inches(1), Inches(0.3),
            f'{slide_num} / {total}', size=9, color=ZINC_400, align=PP_ALIGN.RIGHT)
    textbox(slide, Inches(0.6), Inches(7.05), Inches(6), Inches(0.3),
            'DOKAZ  ·  app.dokaz.net', size=9, color=ZINC_400)


TOTAL = 6


# =========================================================================
# Slide 1 — Cover
# =========================================================================
s = add_slide()
rect(s, Inches(0), Inches(0), prs.slide_width, prs.slide_height, BRAND_900)
rect(s, Inches(4.5), Inches(0), prs.slide_width - Inches(4.5), prs.slide_height, BRAND_800)

# S-mark
rounded(s, Inches(0.9), Inches(0.9), Inches(0.6), Inches(0.6), GOLD_300, radius=0.25)
textbox(s, Inches(0.95), Inches(0.96), Inches(0.5), Inches(0.5),
        'S', size=22, bold=True, color=BRAND_900, align=PP_ALIGN.CENTER)
textbox(s, Inches(1.65), Inches(1.0), Inches(5), Inches(0.4),
        'DOKAZ', size=14, bold=True, color=WHITE)

# Status pill
rounded(s, Inches(0.9), Inches(2.55), Inches(3.4), Inches(0.4), GOLD_500, radius=0.5)
textbox(s, Inches(0.9), Inches(2.57), Inches(3.4), Inches(0.4),
        'BACKUP VERIFICATION AS A SERVICE', size=10, bold=True, color=BRAND_900,
        align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)

textbox(s, Inches(0.9), Inches(3.15), Inches(11.5), Inches(1.6),
        'Backup verification\nyou can prove.', size=54, bold=True, color=WHITE)

textbox(s, Inches(0.9), Inches(5.3), Inches(11), Inches(0.8),
        'Stop finding out about broken backups during the outage.\nWe drill them every day and sign the result.',
        size=18, color=BRAND_100)

# CTA strip
rounded(s, Inches(0.9), Inches(6.45), Inches(5.2), Inches(0.55), GOLD_500, radius=0.4)
textbox(s, Inches(0.9), Inches(6.46), Inches(5.2), Inches(0.55),
        '→  Start free at  app.dokaz.net/signup', size=14, bold=True,
        color=BRAND_900, align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)


# =========================================================================
# Slide 2 — The risk
# =========================================================================
s = add_slide()
page_chrome(s, 'The risk',
            'Your backups are untested until something restores them.', 2, TOTAL)

# Big stat card
rounded(s, Inches(0.6), Inches(1.7), Inches(5.6), Inches(3.6),
        BRAND_50, line=BRAND_300, radius=0.06)
rect(s, Inches(0.6), Inches(1.7), Inches(0.08), Inches(3.6), BRAND_700)
textbox(s, Inches(0.95), Inches(2.0), Inches(5), Inches(0.3),
        'INDUSTRY BENCHMARK', size=11, bold=True, color=BRAND_700)
textbox(s, Inches(0.95), Inches(2.4), Inches(5), Inches(1.6),
        '1 in 3', size=88, bold=True, color=BRAND_800)
textbox(s, Inches(0.95), Inches(4.25), Inches(5.0), Inches(0.6),
        'restore attempts fail on the first try.', size=16, color=ZINC_700)
textbox(s, Inches(0.95), Inches(4.7), Inches(5.0), Inches(0.4),
        'Industry survey average across mid-market teams.',
        size=10, color=ZINC_500)

# Right column: 3 bullet rows
def risk_row(slide, top, head, body):
    rounded(slide, Inches(6.6), top, Inches(6.2), Inches(1.05),
            WHITE, line=ZINC_200, radius=0.05)
    rect(slide, Inches(6.6), top, Inches(0.06), Inches(1.05), GOLD_500)
    textbox(slide, Inches(6.9), top + Inches(0.18), Inches(5.8), Inches(0.3),
            head, size=13, bold=True, color=ZINC_900)
    textbox(slide, Inches(6.9), top + Inches(0.5), Inches(5.8), Inches(0.55),
            body, size=11, color=ZINC_700)

risk_row(s, Inches(1.7),
         'Backup software reports "success" too early.',
         'It writes the bytes. It does not verify the bytes can be restored into a working database.')
risk_row(s, Inches(2.95),
         'Failure modes are invisible until the outage.',
         'Schema drift, corrupted blocks, missing WAL segments, key rotation gone wrong — silent.')
risk_row(s, Inches(4.2),
         'Downtime costs $5,600 per minute.',
         'Gartner industry average. By the time you discover the gap, you are already in the outage.')

# Bottom callout
rounded(s, Inches(0.6), Inches(5.55), Inches(12.2), Inches(1.2),
        ZINC_50, line=ZINC_200, radius=0.05)
rect(s, Inches(0.6), Inches(5.55), Inches(0.08), Inches(1.2), BRAND_700)
textbox(s, Inches(0.9), Inches(5.75), Inches(11.8), Inches(0.35),
        'THE QUESTION YOUR BOARD WILL ASK', size=11, bold=True, color=BRAND_700)
textbox(s, Inches(0.9), Inches(6.1), Inches(11.8), Inches(0.6),
        '"When was the last time someone actually restored from a backup, end to end, and proved it worked?"',
        size=14, bold=True, color=ZINC_900)


# =========================================================================
# Slide 3 — The fix: a drill
# =========================================================================
s = add_slide()
page_chrome(s, 'The fix', 'We run a drill on your backup every day — and sign the result.', 3, TOTAL)

# 5-step pipeline as numbered cards
steps = [
    ('1', 'Provision',
     'Spin up an isolated\nPostgres sandbox.'),
    ('2', 'Fetch',
     'Pull the latest dump from\nyour storage (S3/GCS/R2).'),
    ('3', 'Restore',
     'Restore the dump cleanly\ninto the sandbox.'),
    ('4', 'Assert',
     'Run your SQL invariants:\nrow counts, FK integrity.'),
    ('5', 'Sign',
     'Produce an Ed25519-signed\nPDF evidence report.'),
]
card_w = Inches(2.36)
card_h = Inches(2.7)
card_top = Inches(1.95)
gap = Inches(0.12)
left = Inches(0.6)

for i, (num, name, desc) in enumerate(steps):
    rounded(s, left, card_top, card_w, card_h, WHITE, line=ZINC_200, radius=0.06)
    rect(s, left, card_top, card_w, Inches(0.08), BRAND_700)
    # Number circle
    shp = s.shapes.add_shape(MSO_SHAPE.OVAL,
                             left + Inches(0.28), card_top + Inches(0.3),
                             Inches(0.55), Inches(0.55))
    shp.fill.solid(); shp.fill.fore_color.rgb = BRAND_700; shp.line.fill.background()
    shp.shadow.inherit = False
    textbox(s, left + Inches(0.28), card_top + Inches(0.31),
            Inches(0.55), Inches(0.55),
            num, size=18, bold=True, color=WHITE,
            align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
    textbox(s, left + Inches(0.3), card_top + Inches(1.0),
            card_w - Inches(0.4), Inches(0.4),
            name, size=15, bold=True, color=ZINC_900)
    textbox(s, left + Inches(0.3), card_top + Inches(1.45),
            card_w - Inches(0.4), Inches(1.1),
            desc, size=10, color=ZINC_700)
    left += card_w + gap

# Outcome strip — pass and fail
rounded(s, Inches(0.6), Inches(5.0), Inches(6.0), Inches(1.5),
        EMERALD_50, line=EMERALD, radius=0.06)
rect(s, Inches(0.6), Inches(5.0), Inches(0.08), Inches(1.5), EMERALD_700)
textbox(s, Inches(0.85), Inches(5.2), Inches(5.5), Inches(0.4),
        '✓  PASS', size=14, bold=True, color=EMERALD_700)
textbox(s, Inches(0.85), Inches(5.55), Inches(5.5), Inches(0.85),
        'Signed PDF evidence in your dashboard.\nReady for SOC 2 auditors, board, and customers.',
        size=12, color=ZINC_700)

rounded(s, Inches(6.8), Inches(5.0), Inches(6.0), Inches(1.5),
        BRAND_50, line=BRAND_700, radius=0.06)
rect(s, Inches(6.8), Inches(5.0), Inches(0.08), Inches(1.5), BRAND_700)
textbox(s, Inches(7.05), Inches(5.2), Inches(5.5), Inches(0.4),
        '✗  FAIL', size=14, bold=True, color=BRAND_700)
textbox(s, Inches(7.05), Inches(5.55), Inches(5.5), Inches(0.85),
        'Paged engineer, Slack alert, root cause in\nthe report. You learn in minutes, not days.',
        size=12, color=ZINC_700)


# =========================================================================
# Slide 4 — What you get
# =========================================================================
s = add_slide()
page_chrome(s, 'What you get', 'Proof — not a green checkmark.', 4, TOTAL)

def value_card(slide, left, top, w, h, head, body, tag=None):
    rounded(slide, left, top, w, h, WHITE, line=ZINC_200, radius=0.06)
    rect(slide, left, top, w, Inches(0.08), GOLD_500)
    textbox(slide, left + Inches(0.35), top + Inches(0.4), w - Inches(0.7), Inches(0.5),
            head, size=16, bold=True, color=ZINC_900)
    textbox(slide, left + Inches(0.35), top + Inches(1.05), w - Inches(0.7), Inches(1.3),
            body, size=11, color=ZINC_700)
    if tag:
        rounded(slide, left + Inches(0.35), top + h - Inches(0.6),
                Inches(2.4), Inches(0.32), BRAND_50, radius=0.5)
        textbox(slide, left + Inches(0.35), top + h - Inches(0.59),
                Inches(2.4), Inches(0.32),
                tag, size=9, bold=True, color=BRAND_700,
                align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)

card_w = Inches(6.0)
card_h = Inches(2.5)
top1 = Inches(1.9)
top2 = top1 + card_h + Inches(0.25)
l1 = Inches(0.6)
l2 = l1 + card_w + Inches(0.2)

value_card(s, l1, top1, card_w, card_h,
           'Signed PDF evidence',
           'Every drill produces a tamper-evident PDF with an Ed25519 signature, '
           '7-year retention, and an audit-ready hash chain. Hand it to your auditor.',
           tag='FOR SOC 2 / COMPLIANCE')

value_card(s, l2, top1, card_w, card_h,
           'JSON API + signed webhooks',
           'Pull drill results into your status page, your reliability dashboard, '
           'your data-warehouse. Webhooks signed so downstream can verify origin.',
           tag='FOR ENGINEERING')

value_card(s, l1, top2, card_w, card_h,
           'Plain-SQL invariants',
           'Define checks in the language your team already writes — row counts, '
           'FK integrity, freshness windows, anything queryable. No new DSL.',
           tag='FOR YOUR DBAS')

value_card(s, l2, top2, card_w, card_h,
           'Slack / email alerts',
           'Failures page the on-call. Successes get a one-line confirmation in '
           'the team channel. No noisy dashboards, no missed failures.',
           tag='FOR OPERATIONS')

# Bottom footer note
textbox(s, Inches(0.6), Inches(7.05), Inches(12), Inches(0.3),
        'Postgres supported today  ·  MySQL and SQL Server next  ·  Self-hosted runners on Enterprise',
        size=10, color=ZINC_500, align=PP_ALIGN.CENTER)


# =========================================================================
# Slide 5 — Pricing
# =========================================================================
s = add_slide()
page_chrome(s, 'Pricing', 'Pricing by how often you verify.', 5, TOTAL)

def pricing_card(slide, left, top, w, h, name, price, period, cadence_label,
                 cadence_value, features, popular=False):
    border = GOLD_500 if popular else ZINC_200
    border_w = Pt(2) if popular else Pt(0.75)
    card = rounded(slide, left, top, w, h, WHITE, line=border, radius=0.04)
    card.line.width = border_w
    if popular:
        rounded(slide, left + Inches(0.6), top - Inches(0.15),
                Inches(2.0), Inches(0.3), GOLD_500, radius=0.5)
        textbox(slide, left + Inches(0.6), top - Inches(0.13),
                Inches(2.0), Inches(0.28),
                'MOST POPULAR', size=8, bold=True, color=BRAND_900,
                align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
    textbox(slide, left + Inches(0.3), top + Inches(0.3), w - Inches(0.6), Inches(0.4),
            name, size=16, bold=True, color=ZINC_900)
    textbox(slide, left + Inches(0.3), top + Inches(0.75), w - Inches(0.6), Inches(0.6),
            price, size=34, bold=True, color=ZINC_900)
    if period:
        textbox(slide, left + Inches(0.3) + Inches(1.4 if len(price) <= 4 else 2.0),
                top + Inches(1.0), Inches(1.5), Inches(0.4),
                period, size=11, color=ZINC_500)
    rounded(slide, left + Inches(0.3), top + Inches(1.6),
            w - Inches(0.6), Inches(0.65), BRAND_50, radius=0.08)
    textbox(slide, left + Inches(0.3), top + Inches(1.65),
            w - Inches(0.6), Inches(0.25),
            cadence_label, size=9, bold=True, color=BRAND_700, align=PP_ALIGN.CENTER)
    textbox(slide, left + Inches(0.3), top + Inches(1.88),
            w - Inches(0.6), Inches(0.3),
            cadence_value, size=14, bold=True, color=BRAND_800, align=PP_ALIGN.CENTER)
    fy = top + Inches(2.45)
    for f in features:
        chk = slide.shapes.add_shape(MSO_SHAPE.OVAL,
                                     left + Inches(0.3), fy + Inches(0.08),
                                     Inches(0.1), Inches(0.1))
        chk.fill.solid(); chk.fill.fore_color.rgb = EMERALD; chk.line.fill.background()
        textbox(slide, left + Inches(0.5), fy, w - Inches(0.8), Inches(0.3),
                f, size=10, color=ZINC_700)
        fy += Inches(0.35)

card_top = Inches(2.0)
card_h   = Inches(5.0)
card_w   = Inches(4.0)
card_gap = Inches(0.15)
left0    = Inches(0.6)

pricing_card(s, left0, card_top, card_w, card_h,
             'Trial', 'Free', '', 'FREE FOR 14 DAYS', 'Daily drills, full access',
             ['10 databases', '10 team seats', 'Signed PDF evidence',
              '7-year retention', 'API & webhooks', 'Community support'])

pricing_card(s, left0 + card_w + card_gap, card_top, card_w, card_h,
             'Standard', '$99', '/mo', 'DRILL FREQUENCY', 'Weekly',
             ['10 databases', '10 team seats', 'Signed PDF evidence',
              '7-year retention', 'API & webhooks · Slack alerts', 'Email support'])

pricing_card(s, left0 + (card_w + card_gap) * 2, card_top, card_w, card_h,
             'VIP', '$299', '/mo', 'DRILL FREQUENCY', 'Daily',
             ['Unlimited databases', 'Unlimited team seats', 'Signed PDF evidence',
              '7-year retention', 'API & webhooks · Slack', 'SSO/SAML · Priority support · SLA'],
             popular=True)

textbox(s, Inches(0.6), Inches(7.05), Inches(12), Inches(0.3),
        'Enterprise: SOC 2 docs, custom retention, self-hosted runners in your VPC — contact sales.',
        size=10, color=ZINC_500, align=PP_ALIGN.CENTER)


# =========================================================================
# Slide 6 — Get started
# =========================================================================
s = add_slide()
rect(s, Inches(0), Inches(0), prs.slide_width, prs.slide_height, BRAND_900)
rect(s, Inches(0), Inches(0), prs.slide_width, Inches(0.08), GOLD_500)

# Eyebrow
textbox(s, Inches(0.9), Inches(0.6), Inches(8), Inches(0.3),
        'GET STARTED', size=11, bold=True, color=GOLD_300)
textbox(s, Inches(0.9), Inches(1.0), Inches(12), Inches(1.4),
        'Your first signed PDF\nin under 15 minutes.',
        size=44, bold=True, color=WHITE)

# Three-step strip
def step_chip(slide, left, top, num, head, body):
    rounded(slide, left, top, Inches(3.8), Inches(2.4),
            BRAND_800, line=BRAND_700, radius=0.06)
    shp = slide.shapes.add_shape(MSO_SHAPE.OVAL,
                                 left + Inches(0.3), top + Inches(0.3),
                                 Inches(0.55), Inches(0.55))
    shp.fill.solid(); shp.fill.fore_color.rgb = GOLD_500; shp.line.fill.background()
    shp.shadow.inherit = False
    textbox(slide, left + Inches(0.3), top + Inches(0.31),
            Inches(0.55), Inches(0.55),
            num, size=18, bold=True, color=BRAND_900,
            align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
    textbox(slide, left + Inches(0.3), top + Inches(1.0),
            Inches(3.2), Inches(0.4),
            head, size=14, bold=True, color=WHITE)
    textbox(slide, left + Inches(0.3), top + Inches(1.4),
            Inches(3.2), Inches(0.95),
            body, size=11, color=BRAND_100)

step_chip(s, Inches(0.6),  Inches(3.4), '1',
          'Sign up',
          'Email + password. No card. 14-day free trial.')
step_chip(s, Inches(4.7),  Inches(3.4), '2',
          'Connect storage',
          'Point us at your S3 / GCS / R2 dump location.\nRead-only access is enough.')
step_chip(s, Inches(8.8),  Inches(3.4), '3',
          'See the PDF',
          'Run your first drill on demand. Download the\nsigned evidence in minutes.')

# Big CTA
rounded(s, Inches(0.9), Inches(6.2), Inches(7.0), Inches(0.7), GOLD_500, radius=0.35)
textbox(s, Inches(0.9), Inches(6.21), Inches(7.0), Inches(0.7),
        '→  app.dokaz.net/signup', size=20, bold=True,
        color=BRAND_900, align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)

textbox(s, Inches(8.2), Inches(6.32), Inches(4.8), Inches(0.5),
        'Questions?  hello@dokaz.net', size=12, color=BRAND_100)


out = Path(__file__).parent / 'Dokaz_Sales_Deck.pptx'
prs.save(str(out))
print(f"Wrote {out}")

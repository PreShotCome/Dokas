"""Generate Vesta_Investor_Deck.pptx — a tight 9-slide investor brief.

Matches the Vesta brand (deep ceremonial red + Egyptian gold, with
lapis-blue touches). Run once; commit the output.
"""

from pptx import Presentation
from pptx.util import Inches, Pt, Emu
from pptx.dml.color import RGBColor
from pptx.enum.shapes import MSO_SHAPE
from pptx.enum.text import PP_ALIGN, MSO_ANCHOR
from pathlib import Path

# Brand palette mirrors tailwind.config.js — Vesta red (primary) with
# gold (secondary) and lapis (accent). Emerald stays reserved for the
# verified/passed state.
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

RED_700   = RGBColor(0xB9, 0x1C, 0x1C)
AMBER_700 = RGBColor(0xB4, 0x53, 0x09)
WHITE     = RGBColor(0xFF, 0xFF, 0xFF)


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
    # Thin brand accent stripe at the top.
    rect(slide, Inches(0), Inches(0), prs.slide_width, Inches(0.08), BRAND_700)
    # Eyebrow
    textbox(slide, Inches(0.6), Inches(0.32), Inches(8), Inches(0.3),
            eyebrow.upper(), size=10, bold=True, color=BRAND_700)
    # Title
    textbox(slide, Inches(0.6), Inches(0.58), Inches(12), Inches(0.7),
            title, size=30, bold=True, color=ZINC_900)
    # Footer page number
    textbox(slide, Inches(12.2), Inches(7.05), Inches(1), Inches(0.3),
            f'{slide_num} / {total}', size=9, color=ZINC_400, align=PP_ALIGN.RIGHT)
    # Footer brand mark
    textbox(slide, Inches(0.6), Inches(7.05), Inches(6), Inches(0.3),
            'VESTA  ·  Backup verification you can prove', size=9, color=ZINC_400)


TOTAL = 11


# =========================================================================
# Slide 1 — Cover
# =========================================================================
s = add_slide()
# Full-bleed brand background
rect(s, Inches(0), Inches(0), prs.slide_width, prs.slide_height, BRAND_900)
# A softer brand-700 panel on the right two-thirds for depth
rect(s, Inches(4.5), Inches(0), prs.slide_width - Inches(4.5), prs.slide_height, BRAND_800)

# Shield-mark in white (just a rounded square — keeps the deck asset-free)
rounded(s, Inches(0.9), Inches(0.9), Inches(0.6), Inches(0.6), WHITE, radius=0.25)
textbox(s, Inches(0.95), Inches(0.96), Inches(0.5), Inches(0.5),
        'S', size=22, bold=True, color=BRAND_700, align=PP_ALIGN.CENTER)
textbox(s, Inches(1.65), Inches(1.0), Inches(5), Inches(0.4),
        'VESTA', size=14, bold=True, color=WHITE)

# Status pill
rounded(s, Inches(0.9), Inches(2.6), Inches(2.6), Inches(0.35), BRAND_700, radius=0.5)
textbox(s, Inches(0.9), Inches(2.62), Inches(2.6), Inches(0.35),
        '●  Pre-launch · Code complete', size=10, bold=True, color=BRAND_100,
        align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)

textbox(s, Inches(0.9), Inches(3.15), Inches(11.5), Inches(1.4),
        'Backup verification\nyou can prove.', size=54, bold=True, color=WHITE)

textbox(s, Inches(0.9), Inches(5.2), Inches(11), Inches(0.8),
        'A B2B SaaS that drills your backups every day and signs the result —\nso a broken backup shows up in a report, not an outage.',
        size=18, color=BRAND_100)

textbox(s, Inches(0.9), Inches(6.7), Inches(8), Inches(0.35),
        'Investor brief  ·  May 2026', size=11, color=BRAND_300)


# =========================================================================
# Slide 2 — The problem
# =========================================================================
s = add_slide()
page_chrome(s, 'The problem', 'Backups fail silently — you find out during the outage.', 2, TOTAL)

# Three stat cards
def stat_card(slide, left, top, w, h, label, value, sub, accent=BRAND_700, value_color=ZINC_900):
    rounded(slide, left, top, w, h, WHITE, line=ZINC_200, radius=0.06)
    rect(slide, left, top, Inches(0.06), h, accent)  # accent stripe
    textbox(slide, left + Inches(0.3), top + Inches(0.3), w - Inches(0.4), Inches(0.3),
            label.upper(), size=10, bold=True, color=ZINC_500)
    textbox(slide, left + Inches(0.3), top + Inches(0.65), w - Inches(0.4), Inches(0.9),
            value, size=36, bold=True, color=value_color)
    textbox(slide, left + Inches(0.3), top + Inches(1.65), w - Inches(0.4), Inches(0.4),
            sub, size=11, color=ZINC_500)

stat_card(s, Inches(0.6),  Inches(1.7), Inches(4.0), Inches(2.2),
          'Restores that fail', '~1 in 3', 'on first attempt',
          accent=RED_700, value_color=RED_700)
stat_card(s, Inches(4.7),  Inches(1.7), Inches(4.0), Inches(2.2),
          'Downtime cost', '$5,600 / min', 'Gartner industry average')
stat_card(s, Inches(8.8),  Inches(1.7), Inches(4.0), Inches(2.2),
          'Teams testing weekly', '< 1 in 5', 'most test annually or never')

# Body callout
rounded(s, Inches(0.6), Inches(4.2), Inches(12.2), Inches(2.4), ZINC_50, line=ZINC_200, radius=0.05)
rect(s, Inches(0.6), Inches(4.2), Inches(0.06), Inches(2.4), AMBER_700)
textbox(s, Inches(0.9), Inches(4.4), Inches(11.6), Inches(0.4),
        'WHY THIS HAPPENS', size=11, bold=True, color=AMBER_700)
textbox(s, Inches(0.9), Inches(4.75), Inches(11.6), Inches(1.7),
        'Backup software reports "success" the moment the bytes are written. It does not verify\n'
        'the bytes can be restored into a working database. Schema drift, corrupted blocks, missing\n'
        'WAL segments, key-rotation gone wrong — invisible until restore time. The first real\n'
        'restore most teams ever do is during the outage. That is when they discover the gap.',
        size=15, color=ZINC_700)


# =========================================================================
# Slide 3 — The solution: a drill
# =========================================================================
s = add_slide()
page_chrome(s, 'The solution', 'A drill is a controlled restore — daily, automated, signed.', 3, TOTAL)

# Headline statement
rounded(s, Inches(0.6), Inches(1.7), Inches(12.2), Inches(1.2), BRAND_50, line=BRAND_100, radius=0.05)
rect(s, Inches(0.6), Inches(1.7), Inches(0.06), Inches(1.2), BRAND_700)
textbox(s, Inches(0.9), Inches(1.85), Inches(11.6), Inches(0.95),
        'Vesta pulls your latest backup into a clean sandbox, runs your assertions, and produces a\n'
        'cryptographically signed PDF. Run it every day. Catch the broken backup on a Tuesday.',
        size=16, color=ZINC_900, bold=True)

# 5-step pipeline
step_top = Inches(3.5)
step_h   = Inches(1.6)
gap      = Inches(0.16)
total_w  = prs.slide_width - Inches(1.2)
arrow_w  = Inches(0.35)
box_w    = (total_w - gap * 4 - arrow_w * 4) / 5
steps = [
    ('1', 'Fetch',     'Read the latest\ndump from S3 / GCS / etc.'),
    ('2', 'Restore',   'Load it into a fresh,\nisolated Postgres.'),
    ('3', 'Assert',    'Run your SQL checks:\nrows, schema, freshness.'),
    ('4', 'Sign',      'Produce an Ed25519-\nsigned PDF report.'),
    ('5', 'Tear down', 'Destroy the sandbox.\nAlert on any red.'),
]
x = Inches(0.6)
for i, (n, title, body) in enumerate(steps):
    rounded(s, x, step_top, box_w, step_h, WHITE, line=ZINC_200, radius=0.08)
    # number circle
    cx, cy = x + Inches(0.3), step_top + Inches(0.25)
    rounded(s, cx, cy, Inches(0.4), Inches(0.4), BRAND_700, radius=0.5)
    textbox(s, cx, cy, Inches(0.4), Inches(0.4),
            n, size=14, bold=True, color=WHITE, align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
    textbox(s, x + Inches(0.85), step_top + Inches(0.28), box_w - Inches(0.95), Inches(0.4),
            title, size=15, bold=True, color=ZINC_900)
    textbox(s, x + Inches(0.3), step_top + Inches(0.85), box_w - Inches(0.4), step_h - Inches(0.9),
            body, size=10, color=ZINC_500)
    x += box_w
    if i < 4:
        # arrow
        ar = s.shapes.add_shape(MSO_SHAPE.RIGHT_ARROW, x + gap, step_top + Inches(0.65), arrow_w, Inches(0.3))
        ar.fill.solid(); ar.fill.fore_color.rgb = BRAND_300
        ar.line.fill.background()
        x += arrow_w + gap * 2

# Outcome strip
rounded(s, Inches(0.6), Inches(5.6), Inches(12.2), Inches(1.0), EMERALD_50, line=EMERALD, radius=0.06)
textbox(s, Inches(0.9), Inches(5.75), Inches(11.6), Inches(0.4),
        'THE OUTCOME', size=11, bold=True, color=EMERALD_700)
textbox(s, Inches(0.9), Inches(6.05), Inches(11.6), Inches(0.55),
        '"We have backups" becomes "we have signed proof, from this morning, that we can restore."',
        size=14, bold=True, color=ZINC_900)


# =========================================================================
# Slide 4 — Onboarding (signup → first signed PDF in 15 min)
# =========================================================================
s = add_slide()
page_chrome(s, 'Onboarding', 'From signup to first signed PDF in 15 minutes.', 4, TOTAL)

# 4-step onboarding pipeline
ob_top    = Inches(1.85)
ob_h      = Inches(2.6)
ob_gap    = Inches(0.15)
ob_arrow  = Inches(0.35)
ob_total  = prs.slide_width - Inches(1.2)
ob_box_w  = (ob_total - ob_arrow*3 - ob_gap*6) / 4
ob_steps = [
    ('1', 'Sign up', '~ 2 min',
     'Email + password.\nThe 14-day free trial starts automatically.\nStripe customer record created in the background.'),
    ('2', 'Connect AWS', '~ 3 min',
     'Click "Connect AWS" in the dashboard.\nA pre-filled CloudFormation template opens\nin the customer\'s AWS console — they\nclick "Create stack". No keys to copy.'),
    ('3', 'Define checks', '~ 5 min',
     'Pick from SQL assertion templates —\nrow-count floors, freshness windows,\nNOT NULL checks. Set a daily schedule.'),
    ('4', 'First drill', '~ 5 min',
     'Runs while you watch.\nA signed PDF lands in the evidence vault.\nThat moment IS the sales close.'),
]
ob_x = Inches(0.6)
for i, (n, title, time_label, body) in enumerate(ob_steps):
    rounded(s, ob_x, ob_top, ob_box_w, ob_h, WHITE, line=ZINC_200, radius=0.06)
    cx, cy = ob_x + Inches(0.25), ob_top + Inches(0.25)
    rounded(s, cx, cy, Inches(0.45), Inches(0.45), BRAND_700, radius=0.5)
    textbox(s, cx, cy, Inches(0.45), Inches(0.45),
            n, size=16, bold=True, color=WHITE, align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
    textbox(s, ob_x + Inches(0.85), ob_top + Inches(0.25), ob_box_w - Inches(0.95), Inches(0.35),
            title, size=15, bold=True, color=ZINC_900)
    textbox(s, ob_x + Inches(0.85), ob_top + Inches(0.6), ob_box_w - Inches(0.95), Inches(0.3),
            time_label, size=10, bold=True, color=BRAND_500)
    textbox(s, ob_x + Inches(0.3), ob_top + Inches(1.1), ob_box_w - Inches(0.5), ob_h - Inches(1.2),
            body, size=10, color=ZINC_700)
    ob_x += ob_box_w
    if i < 3:
        ar = s.shapes.add_shape(MSO_SHAPE.RIGHT_ARROW, ob_x + ob_gap, ob_top + Inches(1.05), ob_arrow, Inches(0.35))
        ar.fill.solid(); ar.fill.fore_color.rgb = BRAND_300
        ar.line.fill.background()
        ob_x += ob_arrow + ob_gap*2

# Brand callout: how the first 10 customers experience this
strip1_top = Inches(4.7)
rounded(s, Inches(0.6), strip1_top, Inches(12.2), Inches(1.15), BRAND_50, line=BRAND_100, radius=0.06)
textbox(s, Inches(0.9), strip1_top + Inches(0.15), Inches(11.6), Inches(0.35),
        'FIRST 10 DESIGN PARTNERS', size=10, bold=True, color=BRAND_700)
textbox(s, Inches(0.9), strip1_top + Inches(0.45), Inches(11.6), Inches(0.65),
        '15-minute white-glove Zoom — you screen-share through the CloudFormation step. The first signed PDF lands while you\'re still on the call: that moment IS the sales close.',
        size=13, color=ZINC_700)

# Emerald callout: the trust line for the security review question
strip2_top = Inches(6.0)
rounded(s, Inches(0.6), strip2_top, Inches(12.2), Inches(0.95), EMERALD_50, line=EMERALD, radius=0.06)
textbox(s, Inches(0.9), strip2_top + Inches(0.15), Inches(11.6), Inches(0.3),
        'WHAT THE CUSTOMER\'S SECURITY TEAM SEES', size=10, bold=True, color=EMERALD_700)
textbox(s, Inches(0.9), strip2_top + Inches(0.4), Inches(11.6), Inches(0.5),
        'Same pattern as Datadog, Snowflake, Vanta, Drata: a read-only IAM role scoped to one bucket. No agent install. No production-DB access. Revocable in one click.',
        size=12, color=ZINC_900)


# =========================================================================
# Slide 5 — Product (what's built)
# =========================================================================
s = add_slide()
page_chrome(s, 'Product', 'What we have built.', 5, TOTAL)

# Left: live mock of the drill detail screen
mock_left = Inches(0.6)
mock_top  = Inches(1.6)
mock_w    = Inches(6.6)
mock_h    = Inches(5.2)
rounded(s, mock_left, mock_top, mock_w, mock_h, WHITE, line=ZINC_200, radius=0.04)
# browser chrome
rect(s, mock_left, mock_top, mock_w, Inches(0.45), ZINC_50)
for i, c in enumerate([RGBColor(0xEF, 0x44, 0x44), RGBColor(0xF5, 0x9E, 0x0B), EMERALD]):
    dot = s.shapes.add_shape(MSO_SHAPE.OVAL,
                             mock_left + Inches(0.18 + i * 0.22), mock_top + Inches(0.15),
                             Inches(0.13), Inches(0.13))
    dot.fill.solid(); dot.fill.fore_color.rgb = c; dot.line.fill.background()
textbox(s, mock_left + Inches(0.95), mock_top + Inches(0.12), Inches(5), Inches(0.25),
        'app.vesta.io/drills/a1f9c2', size=9, color=ZINC_400, font='Consolas')

# Content
textbox(s, mock_left + Inches(0.3), mock_top + Inches(0.7), Inches(4), Inches(0.35),
        'production-primary', size=14, bold=True, color=ZINC_900)
textbox(s, mock_left + Inches(0.3), mock_top + Inches(1.0), Inches(4), Inches(0.3),
        'Daily drill  ·  pg_dump -Fc  ·  4.2 GB', size=10, color=ZINC_500)
# passed pill
pill_left = mock_left + mock_w - Inches(1.3)
rounded(s, pill_left, mock_top + Inches(0.75), Inches(1.0), Inches(0.35), EMERALD_50, radius=0.5)
textbox(s, pill_left, mock_top + Inches(0.78), Inches(1.0), Inches(0.3),
        '● passed', size=11, bold=True, color=EMERALD_700, align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)

# 4 mock stat tiles
tile_top = mock_top + Inches(1.55)
tile_h   = Inches(0.95)
tile_gap = Inches(0.12)
tile_w   = (mock_w - Inches(0.6) - tile_gap * 3) / 4
for i, (lbl, val) in enumerate([('Restore time', '2m 14s'), ('Rows verified', '4.2M'), ('Assertions', '6 / 6'), ('Evidence', 'Signed')]):
    tx = mock_left + Inches(0.3) + (tile_w + tile_gap) * i
    rounded(s, tx, tile_top, tile_w, tile_h, ZINC_50, line=ZINC_200, radius=0.08)
    textbox(s, tx + Inches(0.15), tile_top + Inches(0.13), tile_w - Inches(0.2), Inches(0.25),
            lbl.upper(), size=8, bold=True, color=ZINC_500)
    textbox(s, tx + Inches(0.15), tile_top + Inches(0.4), tile_w - Inches(0.2), Inches(0.5),
            val, size=14, bold=True, color=ZINC_900)

# Step pills
step_pill_top = tile_top + tile_h + Inches(0.25)
pill_x = mock_left + Inches(0.3)
for name in ['provision', 'fetch', 'restore', 'assert', 'report', 'teardown']:
    rounded(s, pill_x, step_pill_top, Inches(0.95), Inches(0.32), EMERALD_50, radius=0.5)
    textbox(s, pill_x, step_pill_top + Inches(0.02), Inches(0.95), Inches(0.28),
            '✓ ' + name, size=9, color=EMERALD_700, align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
    pill_x += Inches(1.05)

# Evidence note
ev_top = step_pill_top + Inches(0.6)
rounded(s, mock_left + Inches(0.3), ev_top, mock_w - Inches(0.6), Inches(0.95), ZINC_50, line=ZINC_200, radius=0.06)
textbox(s, mock_left + Inches(0.45), ev_top + Inches(0.1), mock_w - Inches(0.9), Inches(0.3),
        'SIGNED EVIDENCE  ·  ed25519:9f2c4b…a17b  ·  valid',
        size=9, bold=True, color=ZINC_500, font='Consolas')
textbox(s, mock_left + Inches(0.45), ev_top + Inches(0.4), mock_w - Inches(0.9), Inches(0.5),
        'Encrypted at rest · 7-year retention · tamper-evident chain',
        size=10, color=ZINC_700)

# Right: feature list
cap_left = Inches(7.5)
cap_w    = Inches(5.3)
textbox(s, cap_left, Inches(1.6), cap_w, Inches(0.35),
        'CAPABILITIES SHIPPED', size=11, bold=True, color=BRAND_700)

features = [
    ('Connector library', 'S3, GCS, Azure Blob, R2, local, SFTP. Postgres first.'),
    ('Isolated restore runner', 'Ephemeral sandbox per drill. Zero production touch.'),
    ('Assertion engine', 'Plain SQL checks: rows, schema, freshness, invariants.'),
    ('Evidence vault', 'Ed25519-signed PDFs, AES-GCM encrypted, 7-year retention.'),
    ('Scheduling', 'Cron cadences per database + on-demand drills.'),
    ('Failure alerts', 'Email, Slack, signed webhooks. Pages only on real signal.'),
    ('Audit-ready reports', 'Per-DB history, pass-rate trends, exportable evidence packs.'),
    ('JSON API & multi-tenant SaaS', 'Versioned API, billing, MFA, RBAC, audit log.'),
]
y = Inches(2.05)
for title, desc in features:
    # bullet dot
    bd = s.shapes.add_shape(MSO_SHAPE.OVAL, cap_left, y + Inches(0.08), Inches(0.1), Inches(0.1))
    bd.fill.solid(); bd.fill.fore_color.rgb = EMERALD; bd.line.fill.background()
    textbox(s, cap_left + Inches(0.22), y, cap_w - Inches(0.22), Inches(0.3),
            title, size=12, bold=True, color=ZINC_900)
    textbox(s, cap_left + Inches(0.22), y + Inches(0.28), cap_w - Inches(0.22), Inches(0.3),
            desc, size=10, color=ZINC_500)
    y += Inches(0.6)


# =========================================================================
# Slide 6 — Failed vs Successful drill
# =========================================================================
s = add_slide()
page_chrome(s, 'Comparison', 'A failed drill vs a successful one.', 6, TOTAL)

RED_100 = RGBColor(0xFE, 0xE2, 0xE2)
RED_50  = RGBColor(0xFE, 0xF2, 0xF2)
RED_200 = RGBColor(0xFE, 0xCA, 0xCA)

def drill_card(slide, left, top, w, h, *, status, pill_bg, pill_fg, stats, steps,
               callout_label, callout_label_color, callout_lines,
               callout_bg, callout_border):
    rounded(slide, left, top, w, h, WHITE, line=ZINC_200, radius=0.04)
    # browser chrome
    rect(slide, left, top, w, Inches(0.4), ZINC_50)
    for i, c in enumerate([RGBColor(0xEF, 0x44, 0x44), RGBColor(0xF5, 0x9E, 0x0B), EMERALD]):
        dot = slide.shapes.add_shape(MSO_SHAPE.OVAL,
                                     left + Inches(0.15 + i * 0.2),
                                     top + Inches(0.13),
                                     Inches(0.12), Inches(0.12))
        dot.fill.solid(); dot.fill.fore_color.rgb = c; dot.line.fill.background()
    textbox(slide, left + Inches(0.85), top + Inches(0.1), Inches(5), Inches(0.25),
            'app.vesta.io/drills/...', size=9, color=ZINC_400, font='Consolas')
    # header
    textbox(slide, left + Inches(0.3), top + Inches(0.6), Inches(3.5), Inches(0.3),
            'production-primary', size=13, bold=True, color=ZINC_900)
    textbox(slide, left + Inches(0.3), top + Inches(0.9), Inches(4), Inches(0.25),
            'Daily drill  ·  pg_dump -Fc  ·  4.2 GB', size=9, color=ZINC_500)
    # status pill
    pill_left = left + w - Inches(1.4)
    rounded(slide, pill_left, top + Inches(0.6), Inches(1.15), Inches(0.35), pill_bg, radius=0.5)
    textbox(slide, pill_left, top + Inches(0.63), Inches(1.15), Inches(0.3),
            '● ' + status, size=10, bold=True, color=pill_fg,
            align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
    # 4 stat tiles
    tile_top = top + Inches(1.4)
    tile_h   = Inches(0.85)
    tile_gap = Inches(0.1)
    tile_w   = (w - Inches(0.6) - tile_gap * 3) / 4
    for i, (lbl, val, val_color) in enumerate(stats):
        tx = left + Inches(0.3) + (tile_w + tile_gap) * i
        rounded(slide, tx, tile_top, tile_w, tile_h, ZINC_50, line=ZINC_200, radius=0.08)
        textbox(slide, tx + Inches(0.1), tile_top + Inches(0.1), tile_w - Inches(0.2), Inches(0.25),
                lbl.upper(), size=7, bold=True, color=ZINC_500)
        textbox(slide, tx + Inches(0.1), tile_top + Inches(0.32), tile_w - Inches(0.2), Inches(0.45),
                val, size=12, bold=True, color=val_color)
    # step pills
    sp_top = tile_top + tile_h + Inches(0.2)
    px = left + Inches(0.3)
    for name, ok in steps:
        bg = EMERALD_50 if ok else RED_100
        fg = EMERALD_700 if ok else RED_700
        mark = '✓' if ok else '✗'
        rounded(slide, px, sp_top, Inches(0.85), Inches(0.3), bg, radius=0.5)
        textbox(slide, px, sp_top + Inches(0.02), Inches(0.85), Inches(0.26),
                mark + ' ' + name, size=8, color=fg,
                align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
        px += Inches(0.95)
    # callout at bottom
    co_top = sp_top + Inches(0.5)
    co_h   = top + h - co_top - Inches(0.2)
    rounded(slide, left + Inches(0.3), co_top, w - Inches(0.6), co_h,
            callout_bg, line=callout_border, radius=0.05)
    textbox(slide, left + Inches(0.45), co_top + Inches(0.12), w - Inches(0.9), Inches(0.3),
            callout_label, size=9, bold=True, color=callout_label_color)
    line_y = co_top + Inches(0.42)
    for line in callout_lines:
        textbox(slide, left + Inches(0.45), line_y, w - Inches(0.9), Inches(0.3),
                line, size=10, color=ZINC_700)
        line_y += Inches(0.28)

card_top = Inches(1.65)
card_h   = Inches(4.95)
card_w   = Inches(6.05)
card_gap = Inches(0.15)

drill_card(s, Inches(0.6), card_top, card_w, card_h,
           status='failed', pill_bg=RED_100, pill_fg=RED_700,
           stats=[('Restore time', '14m 02s', AMBER_700),
                  ('Rows verified', '3.8M', RED_700),
                  ('Assertions',    '4 / 6',  RED_700),
                  ('Evidence',      'Signed', ZINC_900)],
           steps=[('provision', True), ('fetch', True), ('restore', True),
                  ('assert', False), ('report', True), ('teardown', True)],
           callout_label='FAILED ASSERTIONS  ·  ALERTED 04:11 UTC',
           callout_label_color=RED_700,
           callout_lines=[
               '• orders.created_at freshness — latest row is 5d 11h old (max 26h)',
               '• payments.amount — 421 NULLs in a NOT NULL column',
               '• Paged oncall@yours.co and #ops Slack — broken backup, before the outage.'],
           callout_bg=RED_50, callout_border=RED_200)

drill_card(s, Inches(0.6) + card_w + card_gap, card_top, card_w, card_h,
           status='passed', pill_bg=EMERALD_50, pill_fg=EMERALD_700,
           stats=[('Restore time', '2m 14s', EMERALD_700),
                  ('Rows verified', '4.2M',  ZINC_900),
                  ('Assertions',    '6 / 6', EMERALD_700),
                  ('Evidence',      'Signed', ZINC_900)],
           steps=[('provision', True), ('fetch', True), ('restore', True),
                  ('assert', True), ('report', True), ('teardown', True)],
           callout_label='ALL 6 ASSERTIONS PASSED  ·  SIGNED AND ARCHIVED',
           callout_label_color=EMERALD_700,
           callout_lines=[
               '• Restore time within SLO. Fresh data within window.',
               '• Schema match. Row counts within tolerance.',
               '• Ed25519-signed PDF in the evidence vault. No human in the loop.'],
           callout_bg=EMERALD_50, callout_border=EMERALD)

textbox(s, Inches(0.6), Inches(6.75), Inches(12.2), Inches(0.4),
        'The difference: one of these gets fixed Tuesday morning. The other becomes an incident.',
        size=13, bold=True, color=ZINC_900, align=PP_ALIGN.CENTER)


# =========================================================================
# Slide 7 — Market & business model
# =========================================================================
s = add_slide()
page_chrome(s, 'Market', 'Every regulated B2B company is a buyer.', 7, TOTAL)

# Three buyer columns
col_top = Inches(1.7)
col_h   = Inches(4.6)
col_w   = Inches(3.95)
col_gap = Inches(0.2)
cols = [
    ('Compliance-driven',
     'SOC 2, ISO 27001, HIPAA, PCI all require restore testing.\nThe auditor wants signed proof — not a policy doc.',
     'PRIMARY BEACHHEAD',
     ['Series A → C SaaS', 'Fintech, healthtech, legaltech', '20-500 employees']),
    ('Trauma-driven',
     'Teams that have lived through a failed restore (or watched\na peer live through one) buy fast and pay full price.',
     'WORD-OF-MOUTH ENGINE',
     ['Anyone who lost data', 'Anyone whose CTO did', 'Sleep-driven purchase']),
    ('Devops-mature',
     'Teams that already run chaos drills and game-days but\nhave never automated backup verification.',
     'INBOUND VIA CONTENT',
     ['Strong infra culture', 'Already pay for Datadog tier', 'Self-serve onboarding']),
]
for i, (title, body, label, bullets) in enumerate(cols):
    x = Inches(0.6) + (col_w + col_gap) * i
    rounded(s, x, col_top, col_w, col_h, WHITE, line=ZINC_200, radius=0.04)
    rect(s, x, col_top, col_w, Inches(0.08), BRAND_700)
    textbox(s, x + Inches(0.3), col_top + Inches(0.3), col_w - Inches(0.4), Inches(0.4),
            title, size=18, bold=True, color=ZINC_900)
    textbox(s, x + Inches(0.3), col_top + Inches(0.85), col_w - Inches(0.4), Inches(1.2),
            body, size=11, color=ZINC_700)
    textbox(s, x + Inches(0.3), col_top + Inches(2.3), col_w - Inches(0.4), Inches(0.3),
            label, size=9, bold=True, color=BRAND_700)
    by = col_top + Inches(2.65)
    for b in bullets:
        bd = s.shapes.add_shape(MSO_SHAPE.OVAL, x + Inches(0.3), by + Inches(0.08), Inches(0.08), Inches(0.08))
        bd.fill.solid(); bd.fill.fore_color.rgb = EMERALD; bd.line.fill.background()
        textbox(s, x + Inches(0.5), by, col_w - Inches(0.6), Inches(0.3),
                b, size=11, color=ZINC_700)
        by += Inches(0.4)

# Bottom note
textbox(s, Inches(0.6), Inches(6.6), Inches(12.2), Inches(0.4),
        'Bottom-up via SOC 2 prep → expansion into the rest of the company. Land at $99, expand to $299, retain on the audit trail.',
        size=11, color=ZINC_500, align=PP_ALIGN.CENTER)


# =========================================================================
# Slide 8 — Pricing
# =========================================================================
s = add_slide()
page_chrome(s, 'Pricing', 'Pricing by how often you verify.', 8, TOTAL)

# Three pricing cards
def pricing_card(slide, left, top, w, h, name, price, period, cadence_label, cadence_value,
                 features, popular=False):
    border = BRAND_700 if popular else ZINC_200
    border_w = Pt(2) if popular else Pt(0.75)
    card = rounded(slide, left, top, w, h, WHITE, line=border, radius=0.04)
    card.line.width = border_w
    if popular:
        rounded(slide, left + Inches(0.6), top - Inches(0.15), Inches(2.0), Inches(0.3), BRAND_700, radius=0.5)
        textbox(slide, left + Inches(0.6), top - Inches(0.13), Inches(2.0), Inches(0.28),
                'MOST POPULAR', size=8, bold=True, color=WHITE, align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
    textbox(slide, left + Inches(0.3), top + Inches(0.3), w - Inches(0.6), Inches(0.4),
            name, size=16, bold=True, color=ZINC_900)
    textbox(slide, left + Inches(0.3), top + Inches(0.75), w - Inches(0.6), Inches(0.6),
            price, size=34, bold=True, color=ZINC_900)
    if period:
        textbox(slide, left + Inches(0.3) + Inches(1.4 if len(price) <= 4 else 2.0),
                top + Inches(1.0), Inches(1.5), Inches(0.4),
                period, size=11, color=ZINC_500)
    # cadence badge
    rounded(slide, left + Inches(0.3), top + Inches(1.6), w - Inches(0.6), Inches(0.65), BRAND_50, radius=0.08)
    textbox(slide, left + Inches(0.3), top + Inches(1.65), w - Inches(0.6), Inches(0.25),
            cadence_label, size=9, bold=True, color=BRAND_700, align=PP_ALIGN.CENTER)
    textbox(slide, left + Inches(0.3), top + Inches(1.88), w - Inches(0.6), Inches(0.3),
            cadence_value, size=14, bold=True, color=BRAND_800, align=PP_ALIGN.CENTER)
    # features
    fy = top + Inches(2.45)
    for f in features:
        chk = slide.shapes.add_shape(MSO_SHAPE.OVAL, left + Inches(0.3), fy + Inches(0.08), Inches(0.1), Inches(0.1))
        chk.fill.solid(); chk.fill.fore_color.rgb = EMERALD; chk.line.fill.background()
        textbox(slide, left + Inches(0.5), fy, w - Inches(0.8), Inches(0.3),
                f, size=10, color=ZINC_700)
        fy += Inches(0.35)

card_top = Inches(2.0)
card_h   = Inches(5.0)
card_w   = Inches(4.0)
card_gap = Inches(0.15)
left0 = Inches(0.6)

pricing_card(s, left0, card_top, card_w, card_h,
             'Trial', 'Free', '', 'FREE FOR 14 DAYS', 'Daily drills, full access',
             ['10 databases', '10 team seats', 'Signed PDF evidence', '7-year retention',
              'API & webhooks', 'Community support'])

pricing_card(s, left0 + card_w + card_gap, card_top, card_w, card_h,
             'Standard', '$99', '/mo', 'DRILL FREQUENCY', 'Weekly',
             ['10 databases', '10 team seats', 'Signed PDF evidence', '7-year retention',
              'API & webhooks · Slack alerts', 'Email support'])

pricing_card(s, left0 + (card_w + card_gap) * 2, card_top, card_w, card_h,
             'VIP', '$299', '/mo', 'DRILL FREQUENCY', 'Daily',
             ['Unlimited databases', 'Unlimited team seats', 'Signed PDF evidence',
              '7-year retention', 'API & webhooks · Slack', 'SSO/SAML · Priority support · SLA'],
             popular=True)

# Footer note about Enterprise
textbox(s, Inches(0.6), Inches(7.05), Inches(12), Inches(0.3),
        'Enterprise: SOC 2 docs, custom retention, self-hosted runner in your VPC — contact sales.',
        size=10, color=ZINC_500, align=PP_ALIGN.CENTER)


# =========================================================================
# Slide 9 — Where we stand today
# =========================================================================
s = add_slide()
page_chrome(s, 'Status', 'Where we stand today.', 9, TOTAL)

# Status pills row
def status_pill(slide, left, top, w, h, status, label, color_bg, color_fg):
    rounded(slide, left, top, w, h, color_bg, radius=0.08)
    textbox(slide, left, top + Inches(0.15), w, Inches(0.3),
            status, size=10, bold=True, color=color_fg, align=PP_ALIGN.CENTER)
    textbox(slide, left, top + Inches(0.5), w, Inches(0.4),
            label, size=14, bold=True, color=color_fg, align=PP_ALIGN.CENTER)

status_pill(s, Inches(0.6),  Inches(1.7), Inches(4.0), Inches(1.1),
            '✓ DONE', 'Product code-complete', EMERALD_50, EMERALD_700)
status_pill(s, Inches(4.7),  Inches(1.7), Inches(4.0), Inches(1.1),
            '✓ DONE', 'Deployment pipeline live', EMERALD_50, EMERALD_700)
status_pill(s, Inches(8.8),  Inches(1.7), Inches(4.0), Inches(1.1),
            '○ NEXT', 'Onboarding 5 design partners', BRAND_50, BRAND_700)

# Two-column: shipped / shipping next
def section_card(slide, left, top, w, h, header, lines, accent):
    rounded(slide, left, top, w, h, WHITE, line=ZINC_200, radius=0.04)
    rect(slide, left, top, Inches(0.06), h, accent)
    textbox(slide, left + Inches(0.3), top + Inches(0.25), w - Inches(0.5), Inches(0.35),
            header.upper(), size=11, bold=True, color=accent)
    ly = top + Inches(0.7)
    for line in lines:
        chk = slide.shapes.add_shape(MSO_SHAPE.OVAL, left + Inches(0.3), ly + Inches(0.1), Inches(0.1), Inches(0.1))
        chk.fill.solid(); chk.fill.fore_color.rgb = accent; chk.line.fill.background()
        textbox(slide, left + Inches(0.5), ly, w - Inches(0.7), Inches(0.4),
                line, size=12, color=ZINC_700)
        ly += Inches(0.42)

shipped = [
    'Drill engine: fetch · restore · assert · sign · teardown',
    'Multi-tenant SaaS: auth, MFA, RBAC, billing (Stripe), audit log',
    'Evidence vault: Ed25519 signed PDFs, AES-GCM, 7-year retention',
    'Versioned JSON API + signed webhooks',
    'Fly.io deployment with auto-deploy on green CI',
    'Persistent evidence storage, monitoring hooks',
    'Pricing, signup, onboarding flows',
    'Compliance-ready: SOC 2 evidence model, immutable audit trail',
]
shipping = [
    'Onboard 5 design-partner customers',
    'Drill real production Postgres dumps end-to-end',
    'First $99 / $299 paid subscription via Stripe',
    'SOC 2 Type I evidence pack (using our own product)',
    'Marketing site + docs (Astro)',
    'Observability + first SLA',
    'MySQL & SQL Server connectors',
    'Self-hosted runner option for enterprise pilots',
]
section_card(s, Inches(0.6), Inches(3.05), Inches(6.05), Inches(4.0),
             'Shipped — in the repo today', shipped, EMERALD)
section_card(s, Inches(6.75), Inches(3.05), Inches(6.05), Inches(4.0),
             'Next 90 days', shipping, BRAND_700)


# =========================================================================
# Slide 10 — Roadmap / what's next
# =========================================================================
s = add_slide()
page_chrome(s, 'Plan', 'The next 90 days.', 10, TOTAL)

# Timeline boxes — 30/60/90
def timeline_card(slide, left, top, w, h, when, title, items, color):
    rounded(slide, left, top, w, h, WHITE, line=ZINC_200, radius=0.04)
    # day-strip header
    rounded(slide, left, top, w, Inches(0.85), color, radius=0.04)
    textbox(slide, left, top + Inches(0.12), w, Inches(0.3),
            when, size=11, bold=True, color=WHITE, align=PP_ALIGN.CENTER)
    textbox(slide, left, top + Inches(0.4), w, Inches(0.5),
            title, size=18, bold=True, color=WHITE, align=PP_ALIGN.CENTER)
    iy = top + Inches(1.1)
    for it in items:
        chk = slide.shapes.add_shape(MSO_SHAPE.OVAL, left + Inches(0.3), iy + Inches(0.1), Inches(0.1), Inches(0.1))
        chk.fill.solid(); chk.fill.fore_color.rgb = color; chk.line.fill.background()
        textbox(slide, left + Inches(0.5), iy, w - Inches(0.7), Inches(0.4),
                it, size=12, color=ZINC_700)
        iy += Inches(0.42)

tl_top = Inches(1.7)
tl_h   = Inches(5.0)
tl_w   = Inches(4.0)
tl_gap = Inches(0.15)
tl_left = Inches(0.6)

timeline_card(s, tl_left, tl_top, tl_w, tl_h, '0 – 30 DAYS', 'Launch',
              ['Fly.io production live', 'Stripe + Postmark wired', 'First 5 design partners onboarded',
               'Real drills running on real DBs', 'Daily evidence PDFs delivered'], BRAND_700)
timeline_card(s, tl_left + tl_w + tl_gap, tl_top, tl_w, tl_h, '30 – 60 DAYS', 'Convert',
              ['Design partners → paying ($99–$299)', 'First case study published',
               'MySQL connector beta', 'Slack integration GA', 'SOC 2 Type I kickoff'], BRAND_500)
timeline_card(s, tl_left + (tl_w + tl_gap) * 2, tl_top, tl_w, tl_h, '60 – 90 DAYS', 'Scale',
              ['10 paying customers', 'Inbound from content + SEO', 'SQL Server connector beta',
               'First enterprise pilot', 'First $1k MRR'], BRAND_300)


# =========================================================================
# Slide 11 — Contact / Q&A
# =========================================================================
s = add_slide()
rect(s, Inches(0), Inches(0), prs.slide_width, prs.slide_height, BRAND_900)
rect(s, Inches(4.5), Inches(0), prs.slide_width - Inches(4.5), prs.slide_height, BRAND_800)

textbox(s, Inches(0.9), Inches(1.5), Inches(11.5), Inches(0.5),
        'THANK YOU', size=14, bold=True, color=BRAND_300)

textbox(s, Inches(0.9), Inches(2.2), Inches(11.5), Inches(1.5),
        'Let’s build the proof layer\nfor every backup.', size=48, bold=True, color=WHITE)

textbox(s, Inches(0.9), Inches(4.5), Inches(11.5), Inches(0.6),
        'Vesta — backup verification you can prove.', size=18, color=BRAND_100)

# Contact card
rounded(s, Inches(0.9), Inches(5.6), Inches(6.5), Inches(1.4), BRAND_700, radius=0.06)
textbox(s, Inches(1.1), Inches(5.75), Inches(6), Inches(0.4),
        'CONTACT', size=10, bold=True, color=BRAND_300)
textbox(s, Inches(1.1), Inches(6.05), Inches(6), Inches(0.4),
        'ianrocks62@gmail.com', size=16, bold=True, color=WHITE)
textbox(s, Inches(1.1), Inches(6.45), Inches(6), Inches(0.4),
        'github.com/preshotcome/anything', size=12, color=BRAND_100, font='Consolas')


# ---- save ----
out = Path(__file__).parent / 'Vesta_Investor_Deck.pptx'
prs.save(out)
print(f'Wrote {out}')

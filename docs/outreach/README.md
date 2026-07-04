# Cold outbound templates

Five short templates, one per ICP segment. Each is designed to be sent from
`ian@dokaz.net` (or a founder's real address, not a role account — response
rates are 3–5× worse from `sales@`), with the recipient's first name
substituted for `{{first_name}}` and their company for `{{company}}`.

## Rules of the road

1. **Keep the subject line short.** Under 45 characters, ideally under 30.
   No emoji, no ALL CAPS, no `[Re:]`. Cold-email filters key on those.
2. **Never lead with the product.** The first line names the recipient's
   world; the second line names a specific pain; only the third line
   introduces us.
3. **One CTA per email.** Either "Reply if [X]" or a specific URL. Not both.
4. **The best CTA is `dokaz.net/verify`.** It lets them verify a real signed
   PDF in their browser before they even talk to you. That's the moat
   demonstrating itself — send them the sample PDF + signature and let them
   verify it independently. Nothing else in this category can do that.
5. **Do not include a calendar link in email #1.** It signals "I want to
   sell you something." Only offer a call once they reply.
6. **Follow-up cadence:** send #2 four business days after #1 if no reply;
   drop after #2. Higher pressure lowers response rates in security-tool
   sales.
7. **Signature line names the moat.** "Powered by an open-source verifier
   at github.com/preshotcome/dokaz-verify" is a stronger sig than a
   photo.

## Segments

Files map one-to-one to segments. Send the template whose trigger the
prospect matches most tightly — a company that matches two triggers should
get the tighter-matching one, not the more generic one.

- `segment-a-soc2-in-flight.md` — Publicly hiring "SOC 2 audit" or has a
  Vanta/Drata trust page badge indicating an in-flight audit.
- `segment-b-cyber-insurance-renewal.md` — Insurance renewal season for
  their carrier (Jan–Mar for most carriers) or a recent Coalition/At-Bay
  Pcap announcement.
- `segment-c-pagerduty-user.md` — Uses PagerDuty (visible via job posts,
  status page integrations, or PublicWWW searches on JS embeds).
- `segment-d-recent-soc2-announcement.md` — Announced SOC 2 in the last
  30 days on their blog / LinkedIn. Sharpest close.
- `segment-e-hiring-security-eng.md` — Open req for "security engineer" or
  "compliance engineer" — no dedicated headcount yet.

## Data hygiene

- Every send must be against a personal (not group) email address.
- Never send to `security@`, `legal@`, `contact@` — those go to
  ticketing systems and hurt sender reputation.
- Track opens + replies in a lightweight CSV. If open rate is under 25%
  the subject line is dead; iterate before the body.

## After the reply

Reply from your phone within 15 minutes if humanly possible. The chapter of
the pitch that closes deals is *how fast you follow up*. The reply should
propose one specific 20-minute slot in the next 48 hours — not a Calendly
link. A calendar link on the second message is fine; on the reply it
signals you don't care.

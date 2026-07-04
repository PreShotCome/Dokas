# Segment B — Cyber-insurance renewal window

**Trigger:** Renewal season for their carrier — Coalition/At-Bay/Corvus policies
mostly renew Jan–Mar; Chubb/AIG Q4; carrier is inferable from LinkedIn posts,
Announcements press releases, or SEC filings for public companies. Also
triggered by a Pcap/ransomware announcement in the segment: everyone renews
tighter after a nearby incident.

**Why this works:** Every 2024+ carrier now asks for
"restore-tested-in-last-12-months" evidence at renewal. Buyers have a hard
deadline, and nothing else on the market emits the exact artifact the
carrier's underwriter accepts.

---

## Email 1

**Subject:** Restore evidence before {{company}}'s renewal

Hi {{first_name}},

Cyber-insurance renewals since 2024 have been asking for one specific
thing: proof your backups have been *restored* in the last 12 months —
not the backup job's log, an actual restore result.

Underwriters got tired of paying out ransomware claims where the customer
had backups the whole time but the archives were unopenable.

Dokaz emits that artifact automatically — a signed PDF with the SHA-256
of the input dump, the restore duration, the assertions run, and an
Ed25519 signature the underwriter can verify against our public key.
No trust in us required.

If your renewal is inside the next 90 days, the signup takes 30 seconds
and produces a real signed report on your own dump in about 5 minutes:
dokaz.net.

Worth 15 minutes to compare against what your broker is asking for?

Ian
Dokaz — dokaz.net

---

## Email 2 (send 4 business days later, no reply)

**Subject:** re: {{company}}'s renewal

{{first_name}} — the piece I forgot in the last note: cost.

$99/mo covers weekly drills on up to 5 databases with 7-year evidence
retention. Cheaper than one hour of your compliance eng's time; produces
the artifact your broker is going to ask for anyway.

Happy to send you a sample PDF you can hand your underwriter as a
reference — reply "sample" and it'll be in your inbox in 5 minutes.

Ian

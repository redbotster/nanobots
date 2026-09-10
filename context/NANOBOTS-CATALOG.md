# Nanobots — MVP Catalog

*The launch box of bricks. 28 nanobots (22 job bricks + 6 utility bricks) and 12 nanoswarms.*

Companion to `NANOBOTS-BLUEPRINT.md`. Every brick below declares ports, so any swarm here can be re-snapped by a user into something we didn't think of. That is the point.

---

## Who these are for

| Persona | What they want | Their measure of success |
|---|---|---|
| **The busy person** — anyone with an inbox, a calendar, and too many tabs | Give me back an hour a day without learning anything new | "It just ran and I approved one thing on my phone." |
| **The solo founder** — building, selling, and marketing alone | Look like a team of five on social and in the inbox | Consistent posting, no lead left cold, invoices paid |
| **The small business** (2–50 people) — owner or ops lead | Run repeatable back-office work without hiring for it | Fewer hours on reporting, support, bookkeeping, onboarding |

Design rules that follow from the personas:

1. **Every brick that sends, posts, pays, or deletes has an approval gate by default.** Users turn it off; we never ship it off.
2. **Every brick that reads private data runs through 1Claw Shroud with PII redaction on.** Same locally and in the cloud.
3. **Bricks have a one-sentence job.** If it needs a second sentence, it's two bricks.
4. **Plain names.** A user snaps "Recap inbox to PDF", not `gmail_summarizer_v2`.

Port type key: `string` · `list<string>` · `json` · `file` · `datetime` · `boolean` · `event`

---

## Job bricks (22)

### Inbox and calendar

| # | Brick | Job (one sentence) | For | Harness | Services | Inputs → Outputs | Default guardrails |
|---|---|---|---|---|---|---|---|
| 1 | **Triage my inbox** `inbox-triage` | Sort new mail into *reply today / read later / ignore* and label it. | Busy person, founder | openclaw | Gmail or Microsoft (read + label) | `since:datetime`, `rules:string` → `urgent:list<json>`, `later:list<json>`, `triaged_count:json` | PII redact; label-only, never archives |
| 2 | **Recap inbox to PDF** `recap-emails-to-pdf` | Summarise mail since last run into a PDF recap saved to Drive. | Busy person, SMB owner | openclaw | Gmail (read), Drive (write) | `since:datetime`, `label:string` → `recap_pdf:file`, `recap_json:json`, `drive_file_id:string` | Read-only on Gmail |
| 3 | **Draft replies** `draft-replies` | Write a reply draft in my voice for each thread you're given; never send. | Everyone | openclaw | Gmail (drafts) | `threads:list<json>`, `voice_sample:file?` → `draft_ids:list<string>`, `drafts:list<json>` | Drafts only; sending is a separate brick |
| 4 | **Send when I approve** `email-send-approved` | Send an existing draft after a human taps approve. | Everyone | bare | Gmail (send) | `draft_id:string`, `summary:string` → `message_id:string`, `sent_at:datetime` | Approval gate, cannot be disabled on this brick |
| 5 | **Chase follow-ups** `follow-up-chaser` | Find threads I sent that got no reply in N days and draft a polite nudge. | Founder, sales | openclaw | Gmail | `days_silent:string`, `exclude_labels:list<string>` → `stale_threads:list<json>`, `draft_ids:list<string>` | Drafts only |
| 6 | **Prep my meetings** `meeting-prep` | For each meeting today, pull related mail and docs and write a one-page brief. | Busy person, SMB | openclaw | Calendar, Gmail (read), Drive (read) | `date:datetime` → `briefs:list<json>`, `brief_md:file` | Read-only everywhere |
| 7 | **File meeting notes** `meeting-notes-filer` | Turn a transcript into notes, decisions, and action items; file them to Drive. | SMB, agency | openclaw | Drive (read/write) | `transcript:file` → `notes_md:file`, `action_items:list<json>`, `decisions:list<string>` | PII redact |
| 8 | **Propose meeting times** `calendar-scheduler` | Read a scheduling request and draft a reply with three open slots. | Busy person, founder | openclaw | Calendar (read), Gmail (drafts) | `thread:json`, `working_hours:string` → `slots:list<datetime>`, `draft_id:string` | Never creates events without approval |

### Social and marketing (solo founder)

| # | Brick | Job | For | Harness | Services | Inputs → Outputs | Default guardrails |
|---|---|---|---|---|---|---|---|
| 9 | **Find post ideas** `content-ideas` | Suggest ten post ideas for my niche based on what I've written and what's trending. | Founder | openclaw | Drive (read, past posts), web fetch | `niche:string`, `past_posts:file?` → `ideas:list<json>` | No publishing capability |
| 10 | **Write posts** `post-writer` | Turn one idea into platform-ready posts for X, LinkedIn, and Threads in my voice. | Founder | openclaw | Drive (voice samples) | `idea:json`, `voice_sample:file?`, `platforms:list<string>` → `posts:json` | Drafts only |
| 11 | **Publish posts** `post-publisher` | Publish or schedule approved posts to X and LinkedIn. | Founder | bare | X, LinkedIn | `posts:json`, `schedule:datetime?` → `post_ids:list<string>` | Approval gate; daily post cap (default 3) |
| 12 | **Repurpose content** `repurposer` | Turn one long piece (blog, transcript, video notes) into a thread, a LinkedIn post, and a newsletter blurb. | Founder, SMB marketing | openclaw | Drive (read) | `source:file` → `thread:json`, `linkedin_post:string`, `newsletter_blurb:string` | — |
| 13 | **Draft a newsletter** `newsletter-drafter` | Assemble a newsletter draft from links, notes, and recent posts. | Founder | openclaw | Gmail (drafts), Drive (read) | `sources:list<string>`, `blurbs:list<string>`, `template:file?` → `draft_html:file`, `draft_id:string` | Drafts only |
| 14 | **Watch competitors** `competitor-watch` | Check a list of pages weekly and report what changed. | Founder, SMB | bare + ai.generate | web fetch, memory | `urls:list<string>` → `changes:list<json>`, `summary_md:string` | Egress limited to listed hosts |
| 15 | **Reply to reviews** `review-responder` | Draft replies to new customer reviews in the business's tone. | SMB (local business) | openclaw | Google Business Profile (HTTP binding), Slack | `since:datetime`, `tone:string` → `reply_drafts:list<json>` | Drafts only; approval before posting |

### Sales and customers

| # | Brick | Job | For | Harness | Services | Inputs → Outputs | Default guardrails |
|---|---|---|---|---|---|---|---|
| 16 | **Enrich a lead** `lead-enricher` | Given a name, email, or company, find public context and score fit against my ICP. | Founder, sales | openclaw | web fetch, HubSpot (read) | `lead:json`, `icp:string` → `enriched:json`, `fit_score:string` | Public sources only; no data brokers; no personal-life lookups |
| 17 | **Route a lead** `lead-router` | Create or update the CRM contact and alert the right person. | SMB sales | bare | HubSpot or Salesforce, Slack | `enriched:json`, `owner_rules:string` → `contact_id:string`, `alert_id:string` | — |
| 18 | **Triage support** `support-triage` | Categorise new support mail, draft a reply, and flag anything needing a human. | SMB | openclaw | Gmail (read/drafts), Slack | `since:datetime`, `kb:file?` → `tickets:list<json>`, `draft_ids:list<string>`, `escalations:list<json>` | Drafts only; refund/legal keywords always escalate |
| 19 | **Build a quote** `quote-builder` | Turn a request into a priced quote PDF from my price sheet. | SMB (services, trades) | openclaw | Sheets (read), Drive (write) | `request:json`, `price_sheet:string` → `quote_pdf:file`, `quote_json:json`, `drive_file_id:string` | Prices only from the sheet, never invented |
| 20 | **Chase invoices** `invoice-chaser` | Find overdue invoices and draft reminders that escalate in tone by age. | Founder, SMB | openclaw | Sheets or Stripe (HTTP), Gmail (drafts) | `overdue_days:string` → `overdue:list<json>`, `draft_ids:list<string>` | Drafts only; approval before sending |

### Operations and finance

| # | Brick | Job | For | Harness | Services | Inputs → Outputs | Default guardrails |
|---|---|---|---|---|---|---|---|
| 21 | **File receipts** `receipt-filer` | Pull receipts and invoices out of mail, save them to Drive, and log a row per receipt. | Founder, SMB | openclaw | Gmail (read), Drive (write), Sheets (append) | `since:datetime`, `folder:string` → `receipts:list<json>`, `rows_added:string` | Read-only on Gmail |
| 22 | **Report on a sheet** `sheet-reporter` | Answer a plain-English question about a spreadsheet and produce a short report with a chart. | SMB owner, agency | openclaw | Sheets (read) | `sheet:string`, `question:string`, `period:string` → `report_md:string`, `report_json:json`, `chart_png:file` | Read-only; numbers cited to cell ranges |

*Two bricks considered and deliberately not shipped in MVP:* a hiring/resume screener (fairness and legal exposure need more than a guardrail toggle) and a contract reviewer (too easy to mistake for legal advice). Both go on the roadmap with a proper design.

---

## Utility bricks (6, all `bare`)

These are the studs everyone snaps to. No model, tiny images, sub-second runs.

| # | Brick | Job | Inputs → Outputs |
|---|---|---|---|
| 23 | **Watch a Drive folder** `drive-watch` | Fire when a new file lands in a folder. | `folder:string` → `file:file`, `file_id:string`, `event:event` |
| 24 | **Form to sheet** `form-to-sheet` | Take a webhook payload (Typeform, Tally, website form) and append a row. | `payload:json`, `sheet:string` → `row:json`, `row_number:string` |
| 25 | **Make a PDF** `render-pdf` | Render Markdown or HTML to a PDF file. | `content:string`, `template:file?` → `pdf:file` |
| 26 | **Save to Drive** `drive-save` | Save a file to a folder and return the link. | `file:file`, `folder:string` → `file_id:string`, `link:string` |
| 27 | **Tell me** `notify` | Post a message to Slack, email, or SMS. | `message:string`, `channel:string` → `delivered:boolean` |
| 28 | **Ask me first** `approve` | Pause the swarm until a human approves on web or phone. | `summary:string`, `risk:string` → `approved:boolean`, `decided_by:string` |

---

## Launch nanoswarms (12)

Each is a saved YAML in `examples/swarms/`. The first column is the name a user sees in the gallery.

| # | Swarm | Persona | Trigger | Bricks snapped | The one approval |
|---|---|---|---|---|---|
| S1 | **Daily inbox recap** | Busy person | Weekdays 7:00 | recap-emails-to-pdf → drive-save → notify | none (read-only) |
| S2 | **Morning brief** | Busy person, SMB owner | Weekdays 6:30 | inbox-triage + meeting-prep → render-pdf → notify | none |
| S3 | **Inbox autopilot** | Everyone | Every 2h, work hours | inbox-triage → draft-replies → approve → email-send-approved | send each reply |
| S4 | **Never drop a thread** | Founder, sales | Daily 16:00 | follow-up-chaser → approve → email-send-approved | send each nudge |
| S5 | **Content engine** | Founder | Monday 9:00 | content-ideas → post-writer → approve → post-publisher (scheduled across the week) | the week's batch |
| S6 | **Repurpose everything** | Founder, marketing | New file in `Drive/Published` | drive-watch → repurposer → newsletter-drafter + post-writer → approve → post-publisher | the derived posts |
| S7 | **Lead to meeting** | Founder, SMB sales | Website form webhook | form-to-sheet → lead-enricher → lead-router → calendar-scheduler → approve → email-send-approved | the reply with time slots |
| S8 | **Support desk lite** | SMB | Every 30 min | support-triage → draft-replies → notify (escalations to Slack) → approve → email-send-approved | each customer reply |
| S9 | **Bookkeeping assistant** | Founder, SMB | Daily 18:00 + Friday report | receipt-filer → sheet-reporter → render-pdf → drive-save → notify | none |
| S10 | **Get paid** | Founder, SMB | Monday 8:00 | invoice-chaser → approve → email-send-approved → notify | the batch of reminders |
| S11 | **Meeting to action** | SMB, agency | New transcript in Drive | drive-watch → meeting-notes-filer → notify (action items to Slack) → draft-replies (follow-ups) | none (drafts only) |
| S12 | **Weekly client report** | Agency | Friday 15:00, one values file per client | sheet-reporter → render-pdf → drive-save → approve → email-send-approved | send to client |

### Two swarms, drawn

**S7 Lead to meeting** — shows a utility brick, a job brick, and a gate in one line:

```
website form ──webhook──▶ form-to-sheet ──row──▶ lead-enricher ──enriched──▶ lead-router
                                                      │                          │
                                                      └─enriched.email──▶ calendar-scheduler ──draft_id──▶ approve ──▶ email-send-approved
```

**S9 Bookkeeping assistant** — shows two triggers in one swarm (daily filing, weekly report):

```
daily 18:00 ──▶ receipt-filer ──rows_added──▶ notify("Filed 6 receipts")
friday 15:00 ─▶ sheet-reporter("spend by category this week") ──report_md──▶ render-pdf ──pdf──▶ drive-save ──link──▶ notify
```

### Priority order for shipping

Ship in this order; each tranche is demoable on its own.

1. **Tranche A (week 9):** utility bricks 23–28, bricks 1–4, swarms S1, S2, S3. This proves the whole loop: read → think → gate → act.
2. **Tranche B (week 10):** bricks 9–13, 5, 8; swarms S4, S5, S6. The founder story.
3. **Tranche C (post-launch):** bricks 16–22, 6–7, 14–15; swarms S7–S12. The SMB story.

---

## What this catalog needs from 1Claw

Additive to the ten asks in the blueprint (§4). These come straight from the services column above.

| Need | Today | Ask |
|---|---|---|
| **Gmail scopes** on the Google provider (`gmail.readonly`, `gmail.compose`, `gmail.send`, `gmail.labels`) | Google registry: `openid, email, profile, calendar, drive` | Blocks 12 of 22 job bricks. Highest priority. |
| **Google Sheets** scope (`spreadsheets`) | Not listed | Needed by 19, 21, 22, 24 |
| **Google Business Profile** provider | Not in registry | Brick 15 falls back to a user-supplied OAuth app via `app-credentials` |
| **X and LinkedIn write scopes** | Registry lists `tweet.write` and `w_member_social`, so this is likely fine | Confirm posting works through `execute` with an OAuth binding |
| **Stripe** provider | Not in registry | Brick 20 uses an API-key binding stored in the vault; an OAuth provider would be nicer for non-technical users |
| **SMS channel** on `notify` | `notify` supports `webhook`, `slack`, `email` | Brick 27 offers SMS via a Twilio HTTP binding until native |
| **Per-brick rate caps** (e.g. max 3 posts/day) | Shroud has `max_requests_per_minute` and `daily_budget_usd` | A generic "max executions of binding X per day" guardrail would let the *user* set the cap in the UI rather than us hard-coding it |

---

## Bootstrap prompt addendum

Append this as step 7 of the prompt in `NANOBOTS-BLUEPRINT.md` §6.

```
7. Build the launch catalog from NANOBOTS-CATALOG.md, in tranche order (A, then B, then C).
   For each brick:
   - Create ./bots/<id>/nanobot.yaml matching the schema, with the ports, services,
     harness, and default guardrails exactly as listed in the catalog table.
   - Write ./bots/<id>/bot.md (the harness instructions) in plain second person,
     under 300 words, with the one-sentence job as the first line.
   - Write one conformance test that feeds fixture inputs and asserts every declared
     output port is produced with the declared type.
   - Any brick whose job includes send / post / pay / delete must include an `approve`
     step before the side effect, and the swarm-level `approval_required_for` default
     must not be able to remove it for bricks 4 and 11.
   For each swarm in the catalog:
   - Create ./examples/swarms/<id>.yaml with the snaps shown in the table, and run
     `nanobots plan` to prove every snap type-checks. Fix the ports, not the planner.
   - Add a gallery entry (name, one-line description, persona, screenshot placeholder)
     to web/src/gallery.json so it appears under "Start from a swarm".
   Where a service in the catalog is missing from 1Claw's OAuth provider registry,
   implement the documented fallback (user-supplied OAuth app credentials, or an
   API-key binding in the vault), and add a TODO(1claw#catalog) comment naming the
   provider so we can swap to native later. Do not skip the brick.
```

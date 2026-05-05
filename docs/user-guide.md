# User Guide

## Overview

jobifai automates the repetitive part of job hunting. You describe yourself once — your background, preferred roles, and work preferences — and the tool searches job boards, decides which listings are a good match, fills in application forms, and keeps a record of everything it did.

It works for any profession. Whether you are looking for work in healthcare, finance, education, construction, or any other field, the tool does not make assumptions about your industry.

---

## Initial Setup

Complete these steps once before running the bot for the first time.

### 1. Create an account

Open `http://localhost:8080`, click **Register**, and create a username and password.

### 2. Enter your LLM API key

Go to **Settings → Secrets**. Under **LLM API Keys**, click **Set** next to API Key and enter your key (Claude / Anthropic recommended, or any OpenAI-compatible endpoint). The key is stored encrypted on disk and never returned in plaintext.

### 3. Select your LLM provider

Go to **Settings → General → LLM Configuration**. Choose your provider (Claude, OpenAI, or Ollama) and enter the model name. Click **Save Settings**.

### 4. Build your profile

Go to **Settings → Profile**.

- **Import from file**: click the upload area, choose a PDF, DOCX, or TXT resume, then click **Extract with AI**. The tool parses your file and populates all the fields below. Review the result and click **Save Profile**.
- **Manual entry**: fill in the sections directly (Personal Information, Experience, Education, Skills, etc.) and save.

You can export your saved profile at any time as YAML using the **Export YAML** link.

### 5. Set your preferences

Go to **Settings → Preferences**. Configure:

- **Work Type**: remote, hybrid, or on-site
- **Experience Level**: select the levels that match your background
- **Job Types**: full-time, contract, part-time, etc.
- **Date Filter**: restrict to listings posted within a chosen window
- **Job Titles / Positions**: the search keywords the bot will use (e.g. "Project Manager", "Data Analyst")
- **Locations**: cities or regions to search in
- **Blacklists**: companies, job titles, or locations to always skip

Click **Save Preferences**.

### 6. Connect to job platforms

Go to **Settings → Secrets → Platform Connections**. For each platform (LinkedIn, Seek):

1. Click **Connect browser**. A browser window opens (shown via noVNC in the page if running in Docker, or as a visible window if running locally).
2. Log in to the platform as you normally would.
3. Click **Save session**. Your login cookies are saved; the bot will use them without asking you to log in again.

Optionally you can also save your email/password credentials per platform so the bot can re-authenticate if the session expires.

### 7. Start the bot

Go to the **Dashboard**, select a platform (LinkedIn or Seek), and click **Start**.

---

## Dashboard

The Dashboard is the main control panel.

### Bot status

A status card at the top shows the current bot state:

| State | Meaning |
|---|---|
| Idle | Bot is not running |
| Running | Actively searching and applying |
| Pending Review | Paused, waiting for you to approve an application |
| Stopped | Stopped manually |
| Error | Stopped due to an error (message shown in the card) |

While running, the card shows the current platform, search keyword, location, and the specific role being processed.

### Daily progress

If a daily application limit is set (under General settings), a progress bar shows how many applications have been submitted today versus the limit.

### Review banner

When applications are waiting for review, an amber banner appears. Click it to go directly to the Review Queue.

### Quick actions

Two cards link to **Generate Resume** and **View Applications** for faster navigation.

### Live Logs

A scrolling log panel at the bottom shows real-time output from the running bot — what it is searching, evaluating, and submitting. The log is delivered over a WebSocket connection. Logs appear even if you navigate away and return.

---

## Review Queue

When **Require Review Before Submit** is enabled in General settings, the bot pauses before each application and puts the job here instead of submitting immediately.

Each card shows:
- Company, role, and location
- Platform and suitability score
- Links to the tailored resume and cover letter PDFs (if generated)
- Islamic ethics verdict (if HalalJobFilter is enabled and the verdict is DOUBTFUL)
- Time since it was queued

**To approve**: click **Approve** (or press `A` on desktop). The bot submits the application.  
**To reject**: click **Reject** (or press `R`). The application is discarded.  
On mobile, swipe right to approve, left to reject.

After you act, the bot resumes processing the next item automatically.

---

## Top Matches

Top Matches shows roles that scored at or above your suitability threshold but could not be applied to automatically — typically because they redirect to an external applicant tracking system or Easy Apply is not available.

These are your best matches; apply to them manually from the job listing link.

**Columns**: Easy Apply indicator (⚡ if the platform flagged it), posted date, company, role, location, platform, application deadline, suitability score, link, and action buttons.

**Actions per row**:
- **Mark Applied** (checkmark icon): records the application in your history, as if the bot had applied.
- **Blacklist Company** (ban icon): adds the company to your blacklist and removes all their listings from this view. A second button removes all their other listings too.
- **Delete** (trash icon): removes this entry without applying or blacklisting.

Click any row to expand the LLM's reasoning and, if HalalJobFilter is on, the Islamic ethics assessment.

---

## Application History

Three tabs track what the bot has done with every listing it evaluated.

### Applied

Every application the bot (or you, via Mark Applied) has submitted. Columns: company, role, location, platform, score, document links, date applied. You can filter by platform and search by company or role. Document links open the tailored resume or cover letter PDF in a new tab. Delete an entry with the trash icon (two-click confirmation).

### Skipped

Listings the bot evaluated and deliberately passed over. Each entry records the skip reason:

- **Below suitability threshold** — score was below your configured minimum
- **Blacklisted company / title / location** — matched a blacklist entry
- **Already applied** — same role seen again
- **Company re-apply limit** — `Apply Once at Company` is enabled
- **Manual skip** — skipped interactively during review

You can filter by platform and by skip reason. Click a row to expand the LLM's score reasoning and halal verdict.

### Cannot Apply

Roles that scored well but the bot could not submit automatically (e.g. external ATS redirect, no Easy Apply button).

- **Requeue** (circular arrow): moves the job to the pending review queue so you can apply through the bot's guided flow.
- **Open link**: opens the original job posting to apply manually.

---

## Generate Documents

The Generate page lets you create documents on demand for any role, independent of the bot.

**Tabs**:

| Tab | What it does |
|---|---|
| Job Fit | Scores how well a posting matches your saved profile (0–10) with a written explanation |
| Tailored Resume | Generates a PDF resume rewritten by the LLM to match the job description |
| Cover Letter | Writes and renders a cover letter PDF for the role |
| Questions | Answers a list of application or interview questions using your profile |
| Base Resume | Renders your saved profile as a PDF without any tailoring |

For any tab that takes a job as input, you can provide:
- **Job Posting URL** — the tool fetches and parses the description automatically
- **Job Description** — paste it manually if the URL is behind a login wall

**Optional overrides** (collapsed by default): upload a different resume file, add LinkedIn/GitHub URLs, or add free-text instructions to steer the LLM (e.g. "emphasise project management experience").

**Target Market**: select a market preset (e.g. Australia) to apply region-specific resume formatting and prompting rules.

If Job Fit is enabled together with HalalJobFilter, evaluating a role shows both the score and an Islamic ethics assessment side by side.

For the Questions tab, type each question into a field (add more with the **+** button) and click **Answer Questions**. Each answer is shown with a one-click copy button.

---

## Settings

### General

**LLM Configuration**

| Field | Description |
|---|---|
| Provider | `Claude (Anthropic)`, `OpenAI`, or `Ollama (local)` |
| Model | Model name, e.g. `claude-sonnet-4-6` |
| Use Proxy | Route LLM calls through a proxy URL |
| Max Tokens | Maximum output tokens per call (0 = model default) |

**Per-Task Model Overrides** — assign a different model to individual tasks (suitability scoring, halal filter, resume tailoring, cover letter, form Q&A, interview questions). Useful for cost optimisation: lightweight models for scoring, more capable models for writing.

**Resume Defaults**

| Field | Description |
|---|---|
| Market | Default target market for generated resumes |
| Generate New Resume / Cover Letter | When enabled, the bot generates a tailored resume (and cover letter where applicable) for every application. When disabled, your existing uploaded resume on the job site is used — saves tokens for high-volume runs |

**Job Filtering**

| Field | Description |
|---|---|
| Suitability Threshold | Minimum score (0–10) a role must achieve to proceed. Jobs below this are skipped |
| Max Jobs Per Keyword | How many listings to collect per search term before processing |
| Require Review Before Submit | Pause for manual approval before each application |
| Halal Job Filter | Automatically skip roles rated HARAM or flag DOUBTFUL roles for review |
| Interview Questions | Show the Questions tab in Generate |

**Browser**

| Field | Description |
|---|---|
| Show Browser Window | Make the Chromium window visible while the bot runs (local only) |
| Use Chrome Profile | Reuse an existing Chrome user profile to stay logged in |
| Profile Path | Filesystem path to the Chrome profile directory |
| Remote Debug Port | Attach to an already-running Chrome instance via DevTools protocol |

**Human Behaviour** — tune the bot's pacing to avoid triggering rate limits:

| Field | Default | Description |
|---|---|---|
| Daily Application Limit | 40 | Stop for the day after this many applications |
| Job Read Time (s) | 10–30 | Random pause while "reading" each listing |
| Pause Between Jobs (s) | 5–15 | Random pause between listings |
| Business Hours | 0–0 (disabled) | Restrict the bot to certain hours of the day |

---

### Preferences

Configures what the bot searches for on each platform.

- **Work Type**: Remote / Hybrid / On-site checkboxes
- **Experience Level**: Internship through Executive
- **Job Types**: Full-time, Contract, Part-time, Temporary, Internship, Volunteer, Other
- **Date Filter**: All time / Last month / Last week / Last 24 hours
- **Job Titles / Positions**: Tag input — each entry is used as a search keyword
- **Locations**: Tag input with autocomplete — each entry becomes a search location
- **Blacklisted Companies**: any listing from these organisations is automatically skipped
- **Blacklisted Job Titles**: listings with these words in the title are skipped
- **Blacklisted Locations**: listings in these locations are skipped

---

### Profile

Stores your professional background, used as the source for scoring and document generation.

Sections: Personal Information, Professional Summary, Skills/Expertise, Education, Experience, Projects/Portfolio, Certifications & Professional Development, Publications, Conference Presentations, Grants & Funding, Languages, Interests, Additional Instructions.

**Additional Instructions** is a free-text field included in every LLM prompt. Use it for standing guidance: things to always emphasise, things to avoid mentioning, tone preferences.

**Import**: upload a PDF/DOCX/TXT and click **Extract with AI** to auto-populate all fields. Review and save after extraction.  
**Export**: click **Export YAML** to download your profile as a portable YAML file.

---

### Secrets

Stores sensitive credentials. All values are encrypted at rest using AES-GCM.

**LLM API Keys**
- **API Key**: your primary LLM provider key (Anthropic, OpenAI, etc.)
- **Proxy Key**: optional key for a proxy sitting in front of the LLM API

**Platform Connections**  
One card per platform (LinkedIn, Seek):
- Session status (active or none) is shown
- **Connect browser**: opens the platform in a browser for you to log in manually, then saves the session
- **Credentials**: optionally store email/password so the bot can re-authenticate automatically

---

## Troubleshooting

**Bot starts then immediately stops**  
Check the live log panel for the error message. Common causes: no saved session for the chosen platform, no LLM API key set, or daily application limit already reached today.

**Browser connection fails**  
The browser launch button times out or shows an error. If running locally, check that no other Chrome process is locking the profile. If running in Docker, the noVNC frame in the page should show the browser — try refreshing the page.

**LLM errors / empty responses**  
Verify your API key in Settings → Secrets. Check that the provider and model name in General settings are correct. If using a proxy, confirm the proxy URL and proxy key are set. The live log will include the LLM error message.

**Application stuck in pending review**  
The bot is paused waiting for you to approve or reject in the Review Queue. No new applications proceed until you act on every pending item.

**Score threshold too strict**  
Many roles are being skipped as "Below suitability threshold". Lower the threshold slider in Settings → General → Job Filtering. A setting of 5–6 is typical for broader coverage; 7–8 is selective.

**Score threshold too loose**  
You are getting applications to roles that are not relevant. Raise the threshold, or add specific unwanted job titles or companies to your blacklists in Settings → Preferences.

#!/usr/bin/env python3
"""Move explicitly selected historical identity failures to manual review.

Dry-run by default. Stop automation before --apply, and restart afterwards so the
seen cache is reloaded. Attempt history stays untouched. --backup is an exclusive
JSON archive of the original skipped rows; it must not already exist.
"""

import argparse
import json
from pathlib import Path
import sqlite3


HOLD = "Listing identity could not be verified in a previous run. Check the original listing manually before applying."


def eligible(db, user, job_ids):
    result = []
    for job_id in dict.fromkeys(job_ids):
        row = db.execute(
            "SELECT * FROM jobs_skipped WHERE user_id=? AND id=? AND platform='linkedin'",
            (user, job_id),
        ).fetchone()
        if row is None or not row["skip_reason"].lower().startswith("easy apply: job identity"):
            continue
        if db.execute(
            "SELECT 1 FROM jobs_applied WHERE user_id=? AND (id=? OR link=?)",
            (user, job_id, row["link"]),
        ).fetchone():
            continue
        if db.execute(
            "SELECT 1 FROM jobs_approved_queue WHERE user_id=? AND job_id=?",
            (user, job_id),
        ).fetchone():
            continue
        if db.execute(
            "SELECT 1 FROM apply_attempts WHERE user_id=? AND job_id=? AND "
            "outcome IN ('submitted_confirmed','submission_unknown','already_applied','in_progress')",
            (user, job_id),
        ).fetchone():
            continue
        # The legacy schema has a globally unique job_id. Never change another
        # user's pending row if it occupies the same key.
        if db.execute(
            "SELECT 1 FROM jobs_pending_review WHERE job_id=? AND user_id<>?",
            (job_id, user),
        ).fetchone():
            continue
        result.append(dict(row))
    return result


def recover(db, rows, user):
    for row in rows:
        existing = db.execute(
            "SELECT document_refs_json FROM jobs_pending_review WHERE job_id=? AND user_id=?",
            (row["id"], user),
        ).fetchone()
        try:
            pack = json.loads(existing[0] or "{}") if existing else {}
        except (ValueError, TypeError):
            pack = {}
        if not isinstance(pack, dict):
            pack = {}
        pack.update(hold_reason=HOLD, listing_review_required=True)
        if existing:
            db.execute(
                "UPDATE jobs_pending_review SET easy_apply=0,document_refs_json=? "
                "WHERE job_id=? AND user_id=?",
                (json.dumps(pack), row["id"], user),
            )
        else:
            db.execute(
                "INSERT INTO jobs_pending_review(job_id,user_id,company,role,location,platform,link,"
                "suitability_score,suitability_reasoning,easy_apply,document_refs_json,created_at) "
                "VALUES(?,?,?,?,?,'linkedin',?,?,?,0,?,datetime('now'))",
                (row["id"], user, row["company"], row["role"], row.get("location", ""),
                 row["link"], row.get("suitability_score", 0), row.get("suitability_reasoning", ""),
                 json.dumps(pack)),
            )
        db.execute("DELETE FROM jobs_skipped WHERE id=? AND user_id=?", (row["id"], user))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--db", required=True, type=Path)
    parser.add_argument("--user", required=True)
    parser.add_argument("--job-id", required=True, action="append")
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--backup", type=Path)
    args = parser.parse_args()
    if args.apply and args.backup is None:
        parser.error("--apply requires --backup to archive the original skipped rows")
    path = args.db.resolve(strict=True)
    with sqlite3.connect(path.as_uri() + ("?mode=rw" if args.apply else "?mode=ro"), uri=True) as db:
        db.row_factory = sqlite3.Row
        if args.apply:
            db.execute("BEGIN IMMEDIATE")
        rows = eligible(db, args.user, args.job_id)
        if args.apply:
            with args.backup.open("x", encoding="utf-8") as archive:
                json.dump({"user_id": args.user, "skipped_rows": rows}, archive, indent=2)
            recover(db, rows, args.user)
        print(json.dumps({"mode": "applied" if args.apply else "dry-run", "eligible": rows}, indent=2))


if __name__ == "__main__":
    main()

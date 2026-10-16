#!/usr/bin/env python3
"""Queue explicitly selected historical identity failures for fresh evaluation.

Dry-run by default. Stop automation before --apply, and restart afterwards so the
seen cache is reloaded. Attempt history stays untouched. --backup is an exclusive
JSON archive of the original skipped rows; it must not already exist.
Requires migration 050. A later bot run may apply a qualifying native job after
fresh evaluation; external jobs must qualify before entering Top Matches.
"""

import argparse
import json
from pathlib import Path
import sqlite3


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
            "SELECT 1 FROM jobs_approved_queue WHERE job_id=?",
            (job_id,),
        ).fetchone():
            continue
        if db.execute(
            "SELECT 1 FROM apply_attempts WHERE user_id=? AND (job_id=? OR url=?) AND "
            "outcome IN ('submitted_confirmed','submission_unknown','already_applied','in_progress')",
            (user, job_id, row["link"]),
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
        # Keep any prepared documents intact; this request re-enters discovery,
        # classification and scoring rather than approving a blind submission.
        db.execute(
            "INSERT INTO jobs_approved_queue(job_id,user_id,company,role,location,platform,link,needs_evaluation) "
            "VALUES(?,?,?,?,?,'linkedin',?,1)",
            (row["id"], user, row["company"], row["role"], row.get("location", ""), row["link"]),
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

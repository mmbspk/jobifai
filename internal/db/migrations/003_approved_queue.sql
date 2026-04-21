-- +goose Up
ALTER TABLE jobs_pending_review ADD COLUMN link TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS jobs_approved_queue (
    job_id            TEXT PRIMARY KEY,
    company           TEXT NOT NULL,
    role              TEXT NOT NULL,
    location          TEXT NOT NULL DEFAULT '',
    platform          TEXT NOT NULL,
    link              TEXT NOT NULL DEFAULT '',
    resume_path       TEXT,
    cover_letter_path TEXT,
    approved_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE IF EXISTS jobs_approved_queue;

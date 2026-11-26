-- +goose Up
-- Version 060 is a registered Go migration repairing the apply-engine schema.
-- Candidate facts remain in settings. These records retain source and review
-- evidence separately from the trusted technical mapping cache.
CREATE TABLE application_answer_sources (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 topic_id TEXT NOT NULL,
 fact_digest TEXT NOT NULL,
 source TEXT NOT NULL,
 evidence_json TEXT NOT NULL DEFAULT '[]',
 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(user_id,topic_id)
);
-- Latest validated draft evidence only; it cannot alter candidate facts until
-- an explicit profile/topic save matches the exact proposed topic value.
CREATE TABLE application_answer_imports (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 import_id TEXT NOT NULL,
 topic_id TEXT NOT NULL,
 fact_digest TEXT NOT NULL,
 evidence_json TEXT NOT NULL DEFAULT '[]',
 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(user_id,import_id,topic_id)
);
CREATE TABLE application_answer_reviews (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 group_key TEXT NOT NULL,
 question TEXT NOT NULL,
 status TEXT NOT NULL,
 topic_id TEXT NOT NULL DEFAULT '',
 country TEXT NOT NULL DEFAULT '',
 employer TEXT NOT NULL DEFAULT '',
 platform TEXT NOT NULL DEFAULT '',
 context_key TEXT NOT NULL DEFAULT '',
 job_scope TEXT NOT NULL DEFAULT '',
 lifecycle TEXT NOT NULL DEFAULT 'active',
 revision INTEGER NOT NULL DEFAULT 1,
 profile_revision TEXT NOT NULL,
 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 resolved_at TEXT,
 UNIQUE(user_id,group_key)
);
CREATE INDEX idx_answer_reviews_user_active ON application_answer_reviews(user_id,lifecycle,updated_at DESC,id DESC);
CREATE TABLE application_answer_encounters (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 review_id TEXT NOT NULL REFERENCES application_answer_reviews(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 mapping_id TEXT NOT NULL DEFAULT '',
 field_signature TEXT NOT NULL DEFAULT '',
 field_instance_key TEXT NOT NULL DEFAULT '',
 attempt_id TEXT NOT NULL DEFAULT '',
 job_id TEXT NOT NULL DEFAULT '',
 job_title TEXT NOT NULL DEFAULT '',
 question TEXT NOT NULL,
 proposed_answer TEXT NOT NULL DEFAULT '',
 answer_used TEXT NOT NULL DEFAULT '',
 evidence_json TEXT NOT NULL DEFAULT '[]',
 reason TEXT NOT NULL DEFAULT '',
 source TEXT NOT NULL DEFAULT '',
 profile_revision TEXT NOT NULL,
 use_status TEXT NOT NULL DEFAULT 'not_used',
 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_answer_encounters_review ON application_answer_encounters(user_id,review_id,seq DESC);
CREATE INDEX idx_answer_encounters_attempt ON application_answer_encounters(user_id,attempt_id);
CREATE INDEX idx_answer_encounters_instance ON application_answer_encounters(user_id,attempt_id,field_instance_key);
CREATE TABLE application_answer_notifications (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 generation INTEGER NOT NULL DEFAULT 0,
 read_generation INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE application_answer_notifications;
DROP TABLE application_answer_encounters;
DROP TABLE application_answer_reviews;
DROP TABLE application_answer_imports;
DROP TABLE application_answer_sources;

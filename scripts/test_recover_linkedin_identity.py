import sqlite3
import unittest

from recover_linkedin_identity import eligible, recover


class RecoveryTest(unittest.TestCase):
    def setUp(self):
        self.db = sqlite3.connect(":memory:")
        self.db.row_factory = sqlite3.Row
        self.addCleanup(self.db.close)
        self.db.executescript("""
        CREATE TABLE jobs_skipped(id TEXT PRIMARY KEY,user_id TEXT,platform TEXT,skip_reason TEXT,
          link TEXT,company TEXT,role TEXT,location TEXT,suitability_score INTEGER,suitability_reasoning TEXT);
        CREATE TABLE jobs_pending_review(job_id TEXT PRIMARY KEY,user_id TEXT,company TEXT,role TEXT,
          location TEXT,platform TEXT,link TEXT,suitability_score INTEGER,suitability_reasoning TEXT,
          easy_apply INTEGER,document_refs_json TEXT,created_at TEXT,attempt_count INTEGER DEFAULT 0);
        CREATE TABLE jobs_applied(id TEXT,user_id TEXT,link TEXT);
        CREATE TABLE jobs_approved_queue(job_id TEXT PRIMARY KEY,user_id TEXT,company TEXT,role TEXT,location TEXT,platform TEXT,link TEXT,needs_evaluation INTEGER);
        CREATE TABLE apply_attempts(job_id TEXT,user_id TEXT,outcome TEXT,url TEXT DEFAULT '');
        INSERT INTO jobs_skipped VALUES('123','u1','linkedin','easy apply: job identity: drift',
          'https://www.linkedin.com/jobs/view/123','Company','Role','Remote',8,'match');
        INSERT INTO apply_attempts(job_id,user_id,outcome) VALUES('123','u1','failed');
        """)

    def test_user_scope_and_uncertainty_block_recovery(self):
        self.assertEqual([], eligible(self.db, "u2", ["123"]))
        self.db.execute("INSERT INTO apply_attempts(job_id,user_id,outcome) VALUES('123','u1','submission_unknown')")
        self.assertEqual([], eligible(self.db, "u1", ["123"]))

    def test_recovery_preserves_attempt_history(self):
        rows = eligible(self.db, "u1", ["123", "123"])
        self.assertEqual(1, len(rows))
        recover(self.db, rows, "u1")
        self.assertEqual(0, self.db.execute("SELECT COUNT(*) FROM jobs_skipped").fetchone()[0])
        self.assertEqual("failed", self.db.execute("SELECT outcome FROM apply_attempts").fetchone()[0])
        self.assertEqual(0, self.db.execute("SELECT COUNT(*) FROM jobs_pending_review").fetchone()[0])
        row = self.db.execute("SELECT * FROM jobs_approved_queue").fetchone()
        self.assertEqual(1, row["needs_evaluation"])
        self.assertEqual("123", row["job_id"])

    def test_uncertainty_matches_url_even_with_different_stored_id(self):
        self.db.execute("INSERT INTO apply_attempts VALUES('legacy-id','u1','submission_unknown','https://www.linkedin.com/jobs/view/123')")
        self.assertEqual([], eligible(self.db, "u1", ["123"]))

    def test_recovery_preserves_prepared_documents_and_attempt_count(self):
        self.db.execute("""INSERT INTO jobs_pending_review(job_id,user_id,easy_apply,document_refs_json,attempt_count)
          VALUES('123','u1',1,'{"resume":{"local_path":"existing.pdf"}}',2)""")
        recover(self.db, eligible(self.db, "u1", ["123"]), "u1")
        row = self.db.execute("SELECT * FROM jobs_pending_review").fetchone()
        self.assertEqual(2, row["attempt_count"])
        self.assertIn("existing.pdf", row["document_refs_json"])


if __name__ == "__main__":
    unittest.main()

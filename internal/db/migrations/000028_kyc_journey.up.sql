-- KYC resumable multi-step journey: draft save + identity sub-state tracking.

-- 1. kyc_intake: raw saved answers (for prefill/resume) + draft/submitted status.
ALTER TABLE kyc_intake ADD COLUMN IF NOT EXISTS saved_values JSONB;

-- Migrate the old status vocabulary (pending|completed) to the new one
-- (draft|submitted) before swapping the CHECK constraint.
UPDATE kyc_intake SET status = 'submitted' WHERE status = 'completed';
UPDATE kyc_intake SET status = 'draft'     WHERE status = 'pending';

ALTER TABLE kyc_intake DROP CONSTRAINT IF EXISTS chk_kyc_intake_status;
ALTER TABLE kyc_intake ALTER COLUMN status SET DEFAULT 'draft';
ALTER TABLE kyc_intake ADD CONSTRAINT chk_kyc_intake_status CHECK (status IN ('draft','submitted'));

-- 2. kyc_identity: the Sumsub side of the journey, so GET /kyc/status can report
--    a precise stage and give the app retry context.
CREATE TABLE IF NOT EXISTS kyc_identity (
    user_id            UUID         PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    status             VARCHAR(20)  NOT NULL DEFAULT 'not_started', -- not_started|in_progress|in_review|approved|rejected|resubmit
    applicant_id       VARCHAR(100),
    review_answer      VARCHAR(10),  -- GREEN | RED
    reject_labels      JSONB,        -- Sumsub moderation labels
    reject_type        VARCHAR(20),  -- FINAL | RETRY
    moderation_comment TEXT,
    started_at         TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE notifications (
    id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       VARCHAR(50)  NOT NULL,
    title      VARCHAR(255) NOT NULL,
    body       TEXT         NOT NULL,
    is_read    BOOLEAN      NOT NULL DEFAULT false,
    metadata   JSONB,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_notifications_user   ON notifications (user_id, created_at DESC);
CREATE INDEX idx_notifications_unread ON notifications (user_id) WHERE NOT is_read;

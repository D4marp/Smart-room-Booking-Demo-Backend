CREATE TABLE IF NOT EXISTS feedbacks (
    id                  VARCHAR(36) PRIMARY KEY,
    booking_id          VARCHAR(36) NOT NULL,
    user_id             VARCHAR(36) NOT NULL,
    satisfaction_level  VARCHAR(20) NOT NULL,
    reason              TEXT NOT NULL,
    created_at          BIGINT NOT NULL,
    UNIQUE KEY unique_booking_feedback (booking_id),
    FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_feedbacks_user_id ON feedbacks(user_id);
CREATE INDEX idx_feedbacks_booking_id ON feedbacks(booking_id);

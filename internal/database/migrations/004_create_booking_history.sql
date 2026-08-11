CREATE TABLE IF NOT EXISTS booking_status_history (
    id          VARCHAR(36) PRIMARY KEY,
    booking_id  VARCHAR(36) NOT NULL,
    from_status VARCHAR(20) NOT NULL,
    to_status   VARCHAR(20) NOT NULL,
    changed_by  VARCHAR(36) NOT NULL,
    note        TEXT,
    created_at  BIGINT NOT NULL,
    FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE CASCADE,
    FOREIGN KEY (changed_by) REFERENCES users(id)
);

CREATE INDEX idx_booking_history_booking_id ON booking_status_history(booking_id)

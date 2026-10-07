CREATE TABLE IF NOT EXISTS integration_events (
    id               BIGINT AUTO_INCREMENT PRIMARY KEY,
    event_type       VARCHAR(40) NOT NULL,
    booking_id       VARCHAR(36) NULL,
    room_code        VARCHAR(255) NULL,
    payload          JSON NOT NULL,
    created_at       BIGINT NOT NULL,
    delivered_at     BIGINT NULL,
    attempts         INT NOT NULL DEFAULT 0,
    next_attempt_at  BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX idx_integration_events_pending ON integration_events(delivered_at, next_attempt_at, id);

CREATE INDEX idx_integration_events_room ON integration_events(room_code, id);

CREATE INDEX idx_integration_events_booking ON integration_events(booking_id)

CREATE TABLE IF NOT EXISTS automation_events (
    id               BIGINT AUTO_INCREMENT PRIMARY KEY,
    event_id         VARCHAR(120) NOT NULL,
    event_type       VARCHAR(30) NOT NULL,
    booking_id       VARCHAR(36) NULL,
    room_code        VARCHAR(50) NULL,
    source_event_id  BIGINT NULL,
    status           VARCHAR(30) NOT NULL,
    attempts         INT NOT NULL DEFAULT 0,
    next_attempt_at  BIGINT NOT NULL DEFAULT 0,
    http_status      INT NULL,
    response         TEXT NULL,
    error            TEXT NULL,
    payload          TEXT NULL,
    created_at       BIGINT NOT NULL,
    updated_at       BIGINT NOT NULL,
    UNIQUE KEY uq_automation_event_id (event_id)
);

CREATE INDEX idx_automation_events_status ON automation_events(status, next_attempt_at);

CREATE INDEX idx_automation_events_source ON automation_events(source_event_id)

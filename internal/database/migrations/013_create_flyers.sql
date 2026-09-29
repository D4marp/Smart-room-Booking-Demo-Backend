CREATE TABLE IF NOT EXISTS flyers (
    id                VARCHAR(36) PRIMARY KEY,
    room_id           VARCHAR(36) NULL,
    title             VARCHAR(255) NOT NULL DEFAULT '',
    image_url         VARCHAR(1000) NOT NULL,
    sort_order        INT NOT NULL DEFAULT 0,
    duration_seconds  INT NOT NULL DEFAULT 8,
    is_active         TINYINT(1) NOT NULL DEFAULT 1,
    start_at          BIGINT NULL,
    end_at            BIGINT NULL,
    created_at        BIGINT NOT NULL,
    updated_at        BIGINT NULL,
    FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE
);

CREATE INDEX idx_flyers_room_id ON flyers(room_id);

CREATE INDEX idx_flyers_is_active ON flyers(is_active)

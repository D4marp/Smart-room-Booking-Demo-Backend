CREATE TABLE IF NOT EXISTS rooms (
    id              VARCHAR(36) PRIMARY KEY,
    name            VARCHAR(255) NOT NULL,
    description     TEXT NOT NULL,
    location        VARCHAR(500) NOT NULL,
    city            VARCHAR(100) NOT NULL,
    room_class      VARCHAR(100) NOT NULL,
    image_urls      JSON NOT NULL DEFAULT (JSON_ARRAY()),
    amenities       JSON NOT NULL DEFAULT (JSON_ARRAY()),
    has_ac          TINYINT(1) NOT NULL DEFAULT 0,
    is_available    TINYINT(1) NOT NULL DEFAULT 1,
    max_guests      INT NOT NULL,
    contact_number  VARCHAR(50) NOT NULL,
    floor           VARCHAR(50),
    building        VARCHAR(100),
    created_at      BIGINT NOT NULL,
    updated_at      BIGINT
);

CREATE INDEX idx_rooms_city ON rooms(city);

CREATE INDEX idx_rooms_is_available ON rooms(is_available);

CREATE INDEX idx_rooms_room_class ON rooms(room_class)

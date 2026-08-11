CREATE TABLE IF NOT EXISTS bookings (
    id               VARCHAR(36) PRIMARY KEY,
    user_id          VARCHAR(36) NOT NULL,
    room_id          VARCHAR(36) NOT NULL,
    booking_date     BIGINT NOT NULL,
    check_in_time    VARCHAR(5) NOT NULL,
    check_out_time   VARCHAR(5) NOT NULL,
    number_of_guests INT NOT NULL,
    status           VARCHAR(20) NOT NULL DEFAULT 'pending',
    purpose          TEXT,
    rejection_reason TEXT,
    approved_by      VARCHAR(36),
    approved_at      BIGINT,
    room_name        VARCHAR(255),
    room_location    VARCHAR(500),
    room_image_url   VARCHAR(500),
    user_name        VARCHAR(255),
    user_email       VARCHAR(255),
    created_at       BIGINT NOT NULL,
    updated_at       BIGINT,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE
);

CREATE INDEX idx_bookings_user_id ON bookings(user_id);

CREATE INDEX idx_bookings_room_id ON bookings(room_id);

CREATE INDEX idx_bookings_status ON bookings(status);

CREATE INDEX idx_bookings_booking_date ON bookings(booking_date)

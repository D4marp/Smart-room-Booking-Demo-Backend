ALTER TABLE bookings
    ADD COLUMN booked_for_name VARCHAR(255) NULL AFTER room_image_url,
    ADD COLUMN booked_for_company VARCHAR(255) NULL AFTER booked_for_name;

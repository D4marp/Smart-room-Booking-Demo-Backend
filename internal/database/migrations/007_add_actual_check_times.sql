ALTER TABLE bookings
    ADD COLUMN actual_check_in_time VARCHAR(5) NULL AFTER booked_for_company,
    ADD COLUMN actual_check_out_time VARCHAR(5) NULL AFTER actual_check_in_time,
    ADD COLUMN actual_duration_minutes INT NULL AFTER actual_check_out_time;
CREATE TABLE IF NOT EXISTS classes (
    id             VARCHAR(36) PRIMARY KEY,
    code           VARCHAR(20) NOT NULL,
    study_program  VARCHAR(255) NOT NULL DEFAULT '',
    student_count  INT NOT NULL DEFAULT 0,
    created_at     BIGINT NOT NULL,
    updated_at     BIGINT NULL,
    UNIQUE KEY uq_classes_code_program (code, study_program)
);

ALTER TABLE bookings ADD COLUMN class_id VARCHAR(36) NULL;

ALTER TABLE bookings ADD COLUMN class_code VARCHAR(20) NULL;

CREATE INDEX idx_bookings_class_id ON bookings(class_id)

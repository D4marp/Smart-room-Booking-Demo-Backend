ALTER TABLE feedbacks
    ADD COLUMN complaint_items TEXT NULL AFTER reason,
    ADD COLUMN complaint_other TEXT NULL AFTER complaint_items;
-- Add para_pihak and divisi fields to bookings table
ALTER TABLE bookings 
ADD COLUMN para_pihak VARCHAR(255),
ADD COLUMN divisi VARCHAR(255);

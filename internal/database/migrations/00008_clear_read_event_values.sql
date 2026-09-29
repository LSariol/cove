-- Read events used to store a copy of the secret's encrypted value. They don't
-- need it (restore uses create/update/delete events only), and every copy is
-- one more place a leaked vault key could decrypt. New read events no longer
-- store one; this clears the copies already there.

-- +goose Up
UPDATE cove.event_log
SET old_encrypted_value = NULL
WHERE kind = 'read'
  AND old_encrypted_value IS NOT NULL;

-- Username becomes optional; the identity email lives in Authlier.

ALTER TABLE users
    ALTER COLUMN username DROP NOT NULL;

---- create above / drop below ----

UPDATE users
SET username = coalesce(username, '')
WHERE username IS NULL;

ALTER TABLE users
    ALTER COLUMN username SET NOT NULL;

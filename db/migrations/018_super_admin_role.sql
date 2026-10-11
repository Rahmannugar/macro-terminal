-- Add super_admin role: can demote regular admins; admins cannot demote each other.
ALTER TABLE users DROP CONSTRAINT users_role_valid;
ALTER TABLE users ADD CONSTRAINT users_role_valid
    CHECK (role IN ('super_admin', 'admin', 'user'));

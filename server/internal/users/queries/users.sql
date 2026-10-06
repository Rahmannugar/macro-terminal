-- name: ResolveUserByAuthlierSubjectID :one
WITH inserted AS (
    INSERT INTO users (id, authlier_subject_id, role)
    VALUES ($1, $2, $3)
    ON CONFLICT (authlier_subject_id) DO NOTHING
    RETURNING id, username, authlier_subject_id, role, status, created_at, updated_at
)
SELECT id, username, authlier_subject_id, role, status, created_at, updated_at
FROM inserted
UNION ALL
SELECT id, username, authlier_subject_id, role, status, created_at, updated_at
FROM users
WHERE authlier_subject_id = $2
LIMIT 1;

-- name: GetUserByAuthlierSubjectID :one
SELECT id, username, authlier_subject_id, role, status, created_at, updated_at
FROM users
WHERE authlier_subject_id = $1;

-- name: GetUserByID :one
SELECT id, username, authlier_subject_id, role, status, created_at, updated_at
FROM users
WHERE id = $1;

-- name: UpdateUsername :one
UPDATE users
SET username = $2, updated_at = now()
WHERE id = $1
RETURNING id, username, authlier_subject_id, role, status, created_at, updated_at;

-- name: UpdateRole :one
UPDATE users
SET role = $2, updated_at = now()
WHERE id = $1
RETURNING id, username, authlier_subject_id, role, status, created_at, updated_at;

-- name: ResolveUserByAuthlierSubjectID :one
WITH inserted AS (
    INSERT INTO users (id, username, authlier_subject_id, role)
    VALUES ($1, $2, $3, $4)
    ON CONFLICT (authlier_subject_id) DO NOTHING
    RETURNING id, username, authlier_subject_id, role, created_at, updated_at
)
SELECT id, username, authlier_subject_id, role, created_at, updated_at
FROM inserted
UNION ALL
SELECT id, username, authlier_subject_id, role, created_at, updated_at
FROM users
WHERE authlier_subject_id = $3
LIMIT 1;

-- name: GetUserByAuthlierSubjectID :one
SELECT id, username, authlier_subject_id, role, created_at, updated_at
FROM users
WHERE authlier_subject_id = $1;

-- name: GetUserByID :one
SELECT id, username, authlier_subject_id, role, created_at, updated_at
FROM users
WHERE id = $1;

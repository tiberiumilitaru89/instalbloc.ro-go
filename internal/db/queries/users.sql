-- name: GetUserByID :one
SELECT * FROM users
WHERE id = ? LIMIT 1;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE LOWER(email) = LOWER(?) LIMIT 1;

-- name: CreateUser :one
INSERT INTO users (
    email,
    password_hash,
    name,
    first_name,
    last_name,
    primary_role,
    is_subscribed,
    is_verified,
    phone,
    is_active,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?
)
RETURNING *;

-- name: UpdateUserLastLogin :exec
UPDATE users
SET last_login_at = ?, updated_at = ?
WHERE id = ?;

-- name: UpdateUserSubscriptionStatus :exec
UPDATE users
SET is_subscribed = ?, updated_at = ?
WHERE id = ?;

-- name: GetUserRoles :many
SELECT * FROM user_roles
WHERE user_id = ? AND is_active = 1;

-- name: AddUserRole :one
INSERT INTO user_roles (
    user_id,
    role,
    is_active,
    created_at
) VALUES (
    ?, ?, 1, ?
)
RETURNING *;

-- name: ListAllUsersForAdmin :many
SELECT * FROM users
ORDER BY created_at DESC;

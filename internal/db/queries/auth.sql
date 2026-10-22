-- name: CreateOTPVerification :one
INSERT INTO otp_verifications (
    user_id,
    email,
    code_hash,
    purpose,
    attempts,
    expires_at,
    created_at
) VALUES (
    ?, ?, ?, ?, 0, ?, ?
)
RETURNING *;

-- name: GetLatestActiveOTP :one
SELECT * FROM otp_verifications
WHERE LOWER(email) = LOWER(?)
  AND purpose = ?
  AND consumed_at IS NULL
  AND expires_at > ?
ORDER BY created_at DESC
LIMIT 1;

-- name: IncrementOTPAttempts :exec
UPDATE otp_verifications
SET attempts = attempts + 1
WHERE id = ?;

-- name: ConsumeOTP :exec
UPDATE otp_verifications
SET consumed_at = ?
WHERE id = ?;

-- name: MarkUserVerified :exec
UPDATE users
SET is_verified = 1, updated_at = ?
WHERE id = ?;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = ?, updated_at = ?
WHERE id = ?;

-- name: CreateLocatarProfile :one
INSERT INTO locatar_profiles (
    user_id,
    tip_locuinta,
    este_proprietar,
    association_id,
    address,
    city,
    county,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: CreateProviderProfile :one
INSERT INTO provider_profiles (
    user_id,
    company_name,
    cui,
    county,
    city,
    description,
    phone_public,
    email_public,
    accepts_emergencies,
    travel_radius_km,
    is_active,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?
)
RETURNING *;

-- name: ListUsersForDuplicateCheck :many
SELECT id, email, name, phone FROM users
WHERE is_active = 1;

-- name: ListProvidersForDuplicateCheck :many
SELECT pp.user_id, pp.company_name, pp.cui, u.email
FROM provider_profiles pp
INNER JOIN users u ON u.id = pp.user_id;

-- name: ListAssociationsForDuplicateCheck :many
SELECT a.id, a.name, a.cui, a.address, u.email
FROM associations a
INNER JOIN association_admins aa ON aa.association_id = a.id
INNER JOIN users u ON u.id = aa.user_id;

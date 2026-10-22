-- name: GetAssociationByID :one
SELECT * FROM associations
WHERE id = ? LIMIT 1;

-- name: ListAssociations :many
SELECT * FROM associations
ORDER BY name ASC;

-- name: CreateAssociation :one
INSERT INTO associations (
    name,
    address,
    cui,
    city,
    county,
    apartment_count,
    representative_name,
    is_subscribed,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetAssociationAdmin :one
SELECT * FROM association_admins
WHERE user_id = ? AND association_id = ? AND is_active = 1
LIMIT 1;

-- name: ListUserAssociations :many
SELECT a.* FROM associations a
INNER JOIN association_admins aa ON aa.association_id = a.id
WHERE aa.user_id = ? AND aa.is_active = 1
ORDER BY a.name ASC;

-- name: CreateAssociationAdmin :one
INSERT INTO association_admins (
    user_id,
    association_id,
    role,
    is_active,
    created_at
) VALUES (
    ?, ?, ?, 1, ?
)
RETURNING *;

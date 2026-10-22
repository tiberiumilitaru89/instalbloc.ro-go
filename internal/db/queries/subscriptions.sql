-- name: GetActiveSubscriptionByUserID :one
SELECT * FROM subscriptions
WHERE user_id = ?
ORDER BY expires_at DESC
LIMIT 1;

-- name: CreateSubscription :one
INSERT INTO subscriptions (
    user_id,
    plan_id,
    status,
    started_at,
    expires_at,
    auto_renew,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: ToggleSubscriptionAutoRenew :one
UPDATE subscriptions
SET auto_renew = ?, updated_at = ?
WHERE id = ? AND user_id = ?
RETURNING *;

-- name: AdminToggleSubscriptionAutoRenew :one
UPDATE subscriptions
SET auto_renew = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: ExtendSubscriptionExpiry :one
UPDATE subscriptions
SET expires_at = ?, status = 'active', updated_at = ?
WHERE id = ?
RETURNING *;

-- name: CreatePaymentIdempotent :one
INSERT INTO payments (
    subscription_id,
    user_id,
    amount,
    amount_bani,
    currency,
    status,
    provider,
    external_id,
    idempotency_key,
    raw_payload,
    paid_at,
    created_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

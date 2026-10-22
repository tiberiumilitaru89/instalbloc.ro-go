-- name: UpdateAssociationSubscription :exec
UPDATE associations
SET is_subscribed = ?, updated_at = ?
WHERE id = ?;

-- name: GetPrimaryAssociationAdminUserID :one
SELECT user_id
FROM association_admins
WHERE association_id = ? AND is_active = 1
ORDER BY id ASC
LIMIT 1;

-- name: UpdateAssociationApartmentCount :exec
UPDATE associations
SET apartment_count = ?, updated_at = ?
WHERE id = ?;

-- name: CreateBuilding :one
INSERT INTO buildings (association_id, name)
VALUES (?, ?)
RETURNING id, association_id, name;

-- name: ListBuildingsByAssociation :many
SELECT id, association_id, name
FROM buildings
WHERE association_id = ?
ORDER BY name ASC;

-- name: CreateQrInvite :one
INSERT INTO qr_invites (
    association_id, building_id, apartment_id, type, code, max_uses, used_count, expires_at, is_active, created_at
) VALUES (
    ?, ?, ?, ?, ?, ?, 0, ?, 1, ?
)
RETURNING id, association_id, building_id, apartment_id, type, code, max_uses, used_count, expires_at, is_active, created_at;

-- name: GetQrInviteByCode :one
SELECT id, association_id, building_id, apartment_id, type, code, max_uses, used_count, expires_at, is_active, created_at
FROM qr_invites
WHERE code = ? AND is_active = 1
LIMIT 1;

-- name: ListQrInvitesByAssociation :many
SELECT id, association_id, building_id, apartment_id, type, code, max_uses, used_count, expires_at, is_active, created_at
FROM qr_invites
WHERE association_id = ?
ORDER BY created_at DESC;

-- name: IncrementQrInviteUsedCount :exec
UPDATE qr_invites
SET used_count = used_count + 1
WHERE id = ?;

-- name: DeactivateQrInvite :exec
UPDATE qr_invites
SET is_active = 0
WHERE id = ? AND association_id = ?;

-- name: GetQrRateLimit :one
SELECT id, ip_hash, association_id, request_count, window_start, last_request_at
FROM qr_rate_limits
WHERE ip_hash = ? AND association_id = ?
LIMIT 1;

-- name: CreateQrRateLimit :one
INSERT INTO qr_rate_limits (ip_hash, association_id, request_count, window_start, last_request_at)
VALUES (?, ?, 1, ?, ?)
RETURNING id, ip_hash, association_id, request_count, window_start, last_request_at;

-- name: UpdateQrRateLimitWindow :exec
UPDATE qr_rate_limits
SET request_count = ?, window_start = ?, last_request_at = ?
WHERE id = ?;

-- name: CreateInvoice :one
INSERT INTO invoices (
    user_id, amount, amount_bani, currency, status, type, invoice_number, due_date, paid_at, created_at, updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING id, user_id, amount, amount_bani, currency, status, type, invoice_number, due_date, paid_at, created_at, updated_at;

-- name: GetInvoiceByID :one
SELECT id, user_id, amount, amount_bani, currency, status, type, invoice_number, due_date, paid_at, created_at, updated_at
FROM invoices
WHERE id = ?
LIMIT 1;

-- name: ListInvoicesByUserID :many
SELECT id, user_id, amount, amount_bani, currency, status, type, invoice_number, due_date, paid_at, created_at, updated_at
FROM invoices
WHERE user_id = ?
ORDER BY created_at DESC;

-- name: ListAllInvoicesForAdmin :many
SELECT id, user_id, amount, amount_bani, currency, status, type, invoice_number, due_date, paid_at, created_at, updated_at
FROM invoices
ORDER BY created_at DESC;

-- name: MarkInvoicePaid :one
UPDATE invoices
SET status = 'paid', paid_at = ?, updated_at = ?
WHERE id = ? AND user_id = ?
RETURNING id, user_id, amount, amount_bani, currency, status, type, invoice_number, due_date, paid_at, created_at, updated_at;

-- name: ListPaymentsByUserID :many
SELECT id, subscription_id, user_id, amount, amount_bani, currency, status, provider, external_id, idempotency_key, raw_payload, paid_at, created_at
FROM payments
WHERE user_id = ?
ORDER BY created_at DESC;

-- name: ListAllPaymentsForAdmin :many
SELECT id, subscription_id, user_id, amount, amount_bani, currency, status, provider, external_id, idempotency_key, raw_payload, paid_at, created_at
FROM payments
ORDER BY created_at DESC;

-- name: GetPaymentByIdempotencyKey :one
SELECT id, subscription_id, user_id, amount, amount_bani, currency, status, provider, external_id, idempotency_key, raw_payload, paid_at, created_at
FROM payments
WHERE idempotency_key = ?
LIMIT 1;

-- name: UpdateUserActiveStatus :exec
UPDATE users
SET is_active = ?, updated_at = ?
WHERE id = ?;

-- name: ListAllSubscriptionsForAdmin :many
SELECT id, user_id, plan_id, status, started_at, expires_at, cancelled_at, stripe_subscription_id, stripe_customer_id, auto_renew, created_at, updated_at
FROM subscriptions
ORDER BY updated_at DESC;

-- name: ExpireOverdueSubscriptions :exec
UPDATE subscriptions
SET status = 'expired', updated_at = ?
WHERE status = 'active' AND expires_at < ?;

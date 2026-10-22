-- name: CreateReport :one
INSERT INTO reports (
    title,
    description,
    category,
    status,
    tip_interventie,
    urgency,
    user_id,
    association_id,
    county,
    zone,
    address,
    city,
    client_name,
    client_phone,
    requester_type,
    asociatie,
    apartament,
    tip_spatiu,
    sursa_fonduri,
    aprobat_administrator,
    tracking_token,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at;

-- name: GetReportByID :one
SELECT id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at
FROM reports
WHERE id = ? AND deleted_at IS NULL
LIMIT 1;

-- name: ListActiveReports :many
SELECT id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at
FROM reports
WHERE deleted_at IS NULL AND status != 'archived'
ORDER BY created_at DESC;

-- name: ListReportsByUserID :many
SELECT id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at
FROM reports
WHERE user_id = ? AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: ListUncompletedAcceptedReportsByUser :many
SELECT id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at
FROM reports
WHERE (user_id = ? OR (association_id IS NOT NULL AND association_id = ?))
  AND status IN ('accepted', 'in_progress')
  AND deleted_at IS NULL;

-- name: UpdateReportStatus :one
UPDATE reports
SET status = ?, completed_at = ?, updated_at = ?
WHERE id = ? AND deleted_at IS NULL
RETURNING id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at;

-- name: MarkReportReadByOwner :exec
UPDATE reports
SET is_read = 1, read_at = ?, updated_at = ?
WHERE id = ? AND is_read = 0;

-- name: MarkReportReadByAdmin :exec
UPDATE reports
SET is_read_by_admin = 1, updated_at = ?
WHERE id = ? AND is_read_by_admin = 0;

-- name: UpdateReportFundsAndApproval :one
UPDATE reports
SET sursa_fonduri = ?, aprobat_administrator = ?, invoice_number = ?, updated_at = ?
WHERE id = ? AND deleted_at IS NULL
RETURNING id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at;

-- name: AcceptReportOffer :one
UPDATE reports
SET status = 'accepted', accepted_offer_id = ?, claimed_by_provider_id = ?, accepted_at = ?, updated_at = ?
WHERE id = ? AND deleted_at IS NULL
RETURNING id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at;

-- name: SubmitProviderReport :one
UPDATE reports
SET raport_lucrari_efectuate = ?, raport_materiale = ?, cost_final_manopera = ?, cost_final_materiale = ?, raport_observatii = ?, reported_completed_at = ?, updated_at = ?
WHERE id = ? AND claimed_by_provider_id = ? AND deleted_at IS NULL
RETURNING id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at;

-- name: ExtendReportDeadline :one
UPDATE reports
SET extended_until = ?, extension_reason = ?, updated_at = ?
WHERE id = ? AND deleted_at IS NULL
RETURNING id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at;

-- name: ReopenReportForBidding :one
UPDATE reports
SET status = ?,
    accepted_offer_id = NULL,
    claimed_by_provider_id = NULL,
    accepted_at = NULL,
    extended_until = NULL,
    extension_reason = NULL,
    updated_at = ?
WHERE id = ? AND deleted_at IS NULL
RETURNING id, title, description, category, status, tip_interventie, urgency, user_id, association_id, county, zone, address, city, client_name, client_phone, latitude, longitude, requester_type, asociatie, apartament, tip_spatiu, is_read, read_at, foto_dupa, raport_tehnic, sursa_fonduri, aprobat_administrator, accepted_offer_id, accepted_at, confirmations_count, invoice_number, invoice_url, is_major_emergency, is_read_by_admin, in_portfolio, claimed_by_provider_id, completed_at, tracking_token, extended_until, extension_reason, raport_lucrari_efectuate, raport_materiale, cost_final_manopera, cost_final_materiale, raport_observatii, reported_completed_at, deleted_at, archive_at, created_at, updated_at;

-- name: SoftDeleteReport :exec
UPDATE reports
SET deleted_at = ?, status = 'archived', updated_at = ?
WHERE id = ?;

-- name: IncrementReportConfirmations :exec
UPDATE reports
SET confirmations_count = confirmations_count + 1,
    is_major_emergency = CASE WHEN confirmations_count + 1 >= 3 THEN 1 ELSE is_major_emergency END,
    updated_at = ?
WHERE id = ? AND deleted_at IS NULL;

-- name: CreateTicketFollower :one
INSERT INTO ticket_followers (
    report_id,
    apartament,
    client_phone,
    created_at
) VALUES (
    ?, ?, ?, ?
)
RETURNING id, report_id, apartament, client_phone, created_at;

-- name: ListTicketFollowers :many
SELECT id, report_id, apartament, client_phone, created_at
FROM ticket_followers
WHERE report_id = ?
ORDER BY created_at DESC;

-- name: UpsertTicketView :exec
INSERT INTO ticket_views (
    report_id,
    user_id,
    viewed_at
) VALUES (
    ?, ?, ?
)
ON CONFLICT(report_id, user_id) DO UPDATE SET viewed_at = excluded.viewed_at;

-- name: ListTicketViewsWithUsers :many
SELECT
    tv.id,
    tv.report_id,
    tv.user_id,
    tv.viewed_at,
    u.name AS user_name,
    u.primary_role AS user_role,
    COALESCE(pp.company_name, '') AS company_name
FROM ticket_views tv
INNER JOIN users u ON u.id = tv.user_id
LEFT JOIN provider_profiles pp ON pp.user_id = tv.user_id
WHERE tv.report_id = ?
ORDER BY tv.viewed_at DESC;

-- name: CreateOffer :one
INSERT INTO offers (
    request_id,
    plumber_id,
    price,
    materials_price,
    price_bani,
    materials_price_bani,
    message,
    estimated_time,
    requires_inspection,
    status,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?
)
RETURNING id, request_id, plumber_id, price, materials_price, price_bani, materials_price_bani, message, estimated_time, requires_inspection, status, deleted_at, created_at, updated_at;

-- name: GetOfferByID :one
SELECT id, request_id, plumber_id, price, materials_price, price_bani, materials_price_bani, message, estimated_time, requires_inspection, status, deleted_at, created_at, updated_at
FROM offers
WHERE id = ? AND deleted_at IS NULL
LIMIT 1;

-- name: GetPendingOfferByRequestAndProvider :one
SELECT id, request_id, plumber_id, price, materials_price, price_bani, materials_price_bani, message, estimated_time, requires_inspection, status, deleted_at, created_at, updated_at
FROM offers
WHERE request_id = ? AND plumber_id = ? AND status = 'pending' AND deleted_at IS NULL
LIMIT 1;

-- name: UpdateOfferDetails :one
UPDATE offers
SET price = ?,
    materials_price = ?,
    price_bani = ?,
    materials_price_bani = ?,
    message = ?,
    estimated_time = ?,
    requires_inspection = ?,
    updated_at = ?
WHERE id = ? AND plumber_id = ? AND status = 'pending' AND deleted_at IS NULL
RETURNING id, request_id, plumber_id, price, materials_price, price_bani, materials_price_bani, message, estimated_time, requires_inspection, status, deleted_at, created_at, updated_at;

-- name: UpdateOfferStatus :one
UPDATE offers
SET status = ?, updated_at = ?
WHERE id = ? AND deleted_at IS NULL
RETURNING id, request_id, plumber_id, price, materials_price, price_bani, materials_price_bani, message, estimated_time, requires_inspection, status, deleted_at, created_at, updated_at;

-- name: RejectOtherOffersForRequest :exec
UPDATE offers
SET status = 'rejected', updated_at = ?
WHERE request_id = ? AND id != ? AND status = 'pending' AND deleted_at IS NULL;

-- name: CountPendingOffersForRequest :one
SELECT COUNT(*)
FROM offers
WHERE request_id = ? AND status = 'pending' AND deleted_at IS NULL;

-- name: ListOffersByRequestWithProvider :many
SELECT
    o.id,
    o.request_id,
    o.plumber_id,
    o.price,
    o.materials_price,
    o.price_bani,
    o.materials_price_bani,
    o.message,
    o.estimated_time,
    o.requires_inspection,
    o.status,
    o.created_at,
    o.updated_at,
    u.name AS provider_name,
    COALESCE(u.phone, '') AS provider_phone,
    u.email AS provider_email,
    COALESCE(pp.company_name, '') AS company_name,
    COALESCE(pp.cui, '') AS company_cui,
    COALESCE(pp.rating, 5.0) AS provider_rating,
    COALESCE(pp.total_reviews, 0) AS provider_reviews,
    COALESCE(pp.is_verified, 0) AS provider_verified
FROM offers o
INNER JOIN users u ON u.id = o.plumber_id
LEFT JOIN provider_profiles pp ON pp.user_id = o.plumber_id
WHERE o.request_id = ? AND o.deleted_at IS NULL
ORDER BY o.created_at DESC;

-- name: ListOffersByProviderWithReport :many
SELECT
    o.id,
    o.request_id,
    o.plumber_id,
    o.price,
    o.materials_price,
    o.price_bani,
    o.materials_price_bani,
    o.message,
    o.estimated_time,
    o.requires_inspection,
    o.status,
    o.created_at,
    o.updated_at,
    r.title AS report_title,
    r.category AS report_category,
    r.county AS report_county,
    r.zone AS report_zone,
    r.status AS report_status,
    r.urgency AS report_urgency,
    COALESCE(r.asociatie, '') AS report_asociatie
FROM offers o
INNER JOIN reports r ON r.id = o.request_id
WHERE o.plumber_id = ? AND o.deleted_at IS NULL AND r.deleted_at IS NULL
ORDER BY o.created_at DESC;

-- name: GetProviderProfileByUserID :one
SELECT id, user_id, company_name, cui, county, city, description, phone_public, email_public, website, is_verified, verified_at, rating, total_reviews, total_jobs, accepts_emergencies, travel_radius_km, monthly_modifications, last_modification_reset, is_active, created_at, updated_at
FROM provider_profiles
WHERE user_id = ?
LIMIT 1;

-- name: UpdateProviderModifications :exec
UPDATE provider_profiles
SET monthly_modifications = ?, last_modification_reset = ?, updated_at = ?
WHERE user_id = ?;

-- name: UpdateProviderStatsAfterReview :exec
UPDATE provider_profiles
SET rating = ?, total_reviews = ?, total_jobs = total_jobs + 1, updated_at = ?
WHERE user_id = ?;

-- name: GetLocatarProfileByUserID :one
SELECT id, user_id, tip_locuinta, este_proprietar, association_id, address, city, county, latitude, longitude, created_at, updated_at
FROM locatar_profiles
WHERE user_id = ?
LIMIT 1;

-- name: ListProviderCategorySlugs :many
SELECT DISTINCT c.slug, c.name
FROM provider_specializations ps
INNER JOIN subcategories sc ON sc.id = ps.subcategory_id
INNER JOIN categories c ON c.id = sc.category_id
WHERE ps.provider_id = ?;

-- name: CreateReview :one
INSERT INTO reviews (
    plumber_id,
    client_id,
    request_id,
    rating,
    comment,
    created_at
) VALUES (
    ?, ?, ?, ?, ?, ?
)
RETURNING id, plumber_id, client_id, request_id, rating, comment, created_at;

-- name: GetReviewByRequestID :one
SELECT id, plumber_id, client_id, request_id, rating, comment, created_at
FROM reviews
WHERE request_id = ?
LIMIT 1;

-- name: ListReviewsByPlumberID :many
SELECT id, plumber_id, client_id, request_id, rating, comment, created_at
FROM reviews
WHERE plumber_id = ?
ORDER BY created_at DESC;

-- name: ListReportPhotos :many
SELECT id, report_id, url, "order", uploaded_by
FROM report_photos
WHERE report_id = ?
ORDER BY "order" ASC;

-- name: CreateReportPhoto :one
INSERT INTO report_photos (
    report_id,
    url,
    "order",
    uploaded_by
) VALUES (
    ?, ?, ?, ?
)
RETURNING id, report_id, url, "order", uploaded_by;

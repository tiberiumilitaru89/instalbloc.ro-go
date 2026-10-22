-- InstalBloc Canonical SQLite Schema (Go + sqlc + modernc.org/sqlite)
-- Preserves 100% compatibility with existing Drizzle schema + Integer Subdivisions (Bani)

CREATE TABLE IF NOT EXISTS associations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    address TEXT NOT NULL,
    cui TEXT,
    city TEXT,
    county TEXT,
    apartment_count INTEGER NOT NULL DEFAULT 0 CHECK (apartment_count >= 0),
    representative_name TEXT,
    is_subscribed INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS buildings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    association_id INTEGER NOT NULL REFERENCES associations(id) ON DELETE CASCADE,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS apartments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    building_id INTEGER NOT NULL REFERENCES buildings(id) ON DELETE CASCADE,
    number TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    name TEXT NOT NULL DEFAULT '',
    first_name TEXT,
    last_name TEXT,
    primary_role TEXT NOT NULL DEFAULT 'locatar',
    is_subscribed INTEGER NOT NULL DEFAULT 0,
    is_verified INTEGER NOT NULL DEFAULT 0,
    phone TEXT,
    avatar_url TEXT,
    is_active INTEGER NOT NULL DEFAULT 1,
    last_login_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS otp_verifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    code_hash TEXT NOT NULL,
    purpose TEXT NOT NULL DEFAULT 'login',
    attempts INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL,
    consumed_at INTEGER,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS user_roles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS locatar_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    tip_locuinta TEXT NOT NULL DEFAULT 'bloc',
    este_proprietar INTEGER NOT NULL DEFAULT 0,
    association_id INTEGER REFERENCES associations(id) ON DELETE SET NULL,
    address TEXT,
    city TEXT,
    county TEXT,
    latitude REAL,
    longitude REAL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS provider_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    company_name TEXT,
    cui TEXT,
    county TEXT,
    city TEXT,
    description TEXT,
    phone_public TEXT,
    email_public TEXT,
    website TEXT,
    is_verified INTEGER NOT NULL DEFAULT 0,
    verified_at INTEGER,
    rating REAL NOT NULL DEFAULT 0,
    total_reviews INTEGER NOT NULL DEFAULT 0,
    total_jobs INTEGER NOT NULL DEFAULT 0,
    accepts_emergencies INTEGER NOT NULL DEFAULT 1,
    travel_radius_km INTEGER NOT NULL DEFAULT 20,
    monthly_modifications INTEGER NOT NULL DEFAULT 0 CHECK (monthly_modifications >= 0 AND monthly_modifications <= 3),
    last_modification_reset INTEGER,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS association_admins (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    association_id INTEGER NOT NULL REFERENCES associations(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'asociatie',
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    category TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending_offers',
    tip_interventie TEXT NOT NULL DEFAULT 'programat',
    urgency TEXT NOT NULL DEFAULT 'low',
    user_id INTEGER NOT NULL REFERENCES users(id),
    association_id INTEGER REFERENCES associations(id),
    county TEXT NOT NULL,
    zone TEXT NOT NULL,
    address TEXT,
    city TEXT,
    client_name TEXT,
    client_phone TEXT,
    latitude REAL,
    longitude REAL,
    requester_type TEXT NOT NULL DEFAULT 'locatar',
    asociatie TEXT,
    apartament TEXT,
    tip_spatiu TEXT,
    is_read INTEGER NOT NULL DEFAULT 0,
    read_at INTEGER,
    foto_dupa TEXT,
    raport_tehnic TEXT,
    sursa_fonduri TEXT NOT NULL DEFAULT 'in_asteptare_verificare',
    aprobat_administrator INTEGER NOT NULL DEFAULT 0,
    accepted_offer_id INTEGER,
    accepted_at INTEGER,
    confirmations_count INTEGER NOT NULL DEFAULT 0,
    invoice_number TEXT,
    invoice_url TEXT,
    is_major_emergency INTEGER NOT NULL DEFAULT 0,
    is_read_by_admin INTEGER NOT NULL DEFAULT 0,
    in_portfolio INTEGER NOT NULL DEFAULT 0,
    claimed_by_provider_id INTEGER REFERENCES users(id),
    completed_at INTEGER,
    tracking_token TEXT,
    extended_until INTEGER,
    extension_reason TEXT,
    raport_lucrari_efectuate TEXT,
    raport_materiale TEXT,
    cost_final_manopera INTEGER CHECK (cost_final_manopera IS NULL OR cost_final_manopera >= 0),
    cost_final_materiale INTEGER CHECK (cost_final_materiale IS NULL OR cost_final_materiale >= 0),
    raport_observatii TEXT,
    reported_completed_at INTEGER,
    deleted_at INTEGER,
    archive_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reports_association_id ON reports(association_id);
CREATE INDEX IF NOT EXISTS idx_reports_status ON reports(status);
CREATE INDEX IF NOT EXISTS idx_reports_county ON reports(county);
CREATE INDEX IF NOT EXISTS idx_reports_deleted_at ON reports(deleted_at);
CREATE INDEX IF NOT EXISTS idx_reports_user_id ON reports(user_id);
CREATE INDEX IF NOT EXISTS idx_reports_tracking_token ON reports(tracking_token);

CREATE TABLE IF NOT EXISTS offers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
    plumber_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    price REAL NOT NULL,
    materials_price REAL,
    price_bani INTEGER NOT NULL DEFAULT 0 CHECK (price_bani >= 0),
    materials_price_bani INTEGER NOT NULL DEFAULT 0 CHECK (materials_price_bani >= 0),
    message TEXT,
    estimated_time TEXT NOT NULL,
    requires_inspection INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    deleted_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    description TEXT,
    icon TEXT,
    "order" INTEGER NOT NULL DEFAULT 0,
    is_active INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS subcategories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT,
    "order" INTEGER NOT NULL DEFAULT 0,
    is_active INTEGER NOT NULL DEFAULT 1
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_subcategories_category_slug ON subcategories(category_id, slug);

CREATE TABLE IF NOT EXISTS provider_specializations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id INTEGER NOT NULL REFERENCES provider_profiles(id) ON DELETE CASCADE,
    subcategory_id INTEGER NOT NULL REFERENCES subcategories(id) ON DELETE CASCADE,
    years_experience INTEGER
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_provider_specializations_provider_subcategory ON provider_specializations(provider_id, subcategory_id);

CREATE TABLE IF NOT EXISTS provider_coverage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id INTEGER NOT NULL REFERENCES provider_profiles(id) ON DELETE CASCADE,
    county TEXT NOT NULL,
    city TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_provider_coverage_provider_county_city ON provider_coverage(provider_id, county, city);

CREATE TABLE IF NOT EXISTS report_photos (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    "order" INTEGER NOT NULL DEFAULT 0,
    uploaded_by INTEGER NOT NULL REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS qr_invites (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    association_id INTEGER NOT NULL REFERENCES associations(id) ON DELETE CASCADE,
    building_id INTEGER REFERENCES buildings(id) ON DELETE SET NULL,
    apartment_id INTEGER REFERENCES apartments(id) ON DELETE SET NULL,
    type TEXT NOT NULL DEFAULT 'building',
    code TEXT NOT NULL UNIQUE,
    max_uses INTEGER,
    used_count INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    started_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    cancelled_at INTEGER,
    stripe_subscription_id TEXT,
    stripe_customer_id TEXT,
    auto_renew INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_user_status ON subscriptions(user_id, status);
CREATE INDEX IF NOT EXISTS idx_subscriptions_expires_at ON subscriptions(expires_at);

CREATE TABLE IF NOT EXISTS payments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE RESTRICT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    amount REAL NOT NULL,
    amount_bani INTEGER NOT NULL DEFAULT 0 CHECK (amount_bani >= 0),
    currency TEXT NOT NULL DEFAULT 'RON',
    status TEXT NOT NULL DEFAULT 'pending',
    provider TEXT NOT NULL,
    external_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    raw_payload TEXT NOT NULL,
    paid_at INTEGER,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_payments_subscription_id ON payments(subscription_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);

CREATE TABLE IF NOT EXISTS notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    channel TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    link TEXT,
    target_id INTEGER,
    is_read INTEGER NOT NULL DEFAULT 0,
    read_at INTEGER,
    sent_at INTEGER,
    error TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_read ON notifications(user_id, is_read);

CREATE TABLE IF NOT EXISTS reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plumber_id INTEGER NOT NULL REFERENCES users(id),
    client_id INTEGER NOT NULL REFERENCES users(id),
    request_id INTEGER NOT NULL REFERENCES reports(id),
    rating REAL NOT NULL,
    comment TEXT,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS qr_rate_limits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ip_hash TEXT NOT NULL,
    association_id INTEGER NOT NULL,
    request_count INTEGER NOT NULL DEFAULT 1,
    window_start INTEGER NOT NULL,
    last_request_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_qr_rate_ip_hash ON qr_rate_limits(ip_hash, association_id);
CREATE INDEX IF NOT EXISTS idx_qr_rate_window ON qr_rate_limits(window_start);

CREATE TABLE IF NOT EXISTS invoices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    amount REAL NOT NULL,
    amount_bani INTEGER NOT NULL DEFAULT 0 CHECK (amount_bani >= 0),
    currency TEXT NOT NULL DEFAULT 'RON',
    status TEXT NOT NULL DEFAULT 'unpaid',
    type TEXT NOT NULL DEFAULT 'recurring',
    invoice_number TEXT NOT NULL UNIQUE,
    due_date INTEGER NOT NULL,
    paid_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_invoices_user ON invoices(user_id);
CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status);

CREATE TABLE IF NOT EXISTS ticket_views (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    viewed_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ticket_views_report_id ON ticket_views(report_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_ticket_views_report_user ON ticket_views(report_id, user_id);

CREATE TABLE IF NOT EXISTS ticket_followers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
    apartament TEXT NOT NULL,
    client_phone TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ticket_followers_report_id ON ticket_followers(report_id);

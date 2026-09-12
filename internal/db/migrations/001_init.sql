-- Core schema.

CREATE TABLE users (
    id            TEXT PRIMARY KEY NOT NULL,
    username      TEXT NOT NULL COLLATE NOCASE,
    display_name  TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (username)
);

CREATE TABLE sessions (
    id                  TEXT PRIMARY KEY NOT NULL,
    user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash          TEXT NOT NULL,
    idle_expires_at     TEXT NOT NULL,
    absolute_expires_at TEXT NOT NULL,
    step_up_expires_at  TEXT,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (token_hash)
);

CREATE INDEX sessions_user_id_idx ON sessions(user_id);

CREATE TABLE assets (
    id               TEXT PRIMARY KEY NOT NULL,
    name             TEXT NOT NULL,
    hostname         TEXT NOT NULL,
    asset_type       TEXT NOT NULL CHECK (asset_type IN ('physical', 'vm', 'other')),
    os_family        TEXT NOT NULL CHECK (os_family IN ('linux', 'windows', 'other')),
    os_detail        TEXT NOT NULL DEFAULT '',
    environment      TEXT NOT NULL CHECK (environment IN ('prod', 'staging', 'dev', 'lab', 'other')),
    purpose          TEXT NOT NULL DEFAULT '',
    primary_ip       TEXT NOT NULL DEFAULT '',
    additional_ips   TEXT NOT NULL DEFAULT '[]',
    location         TEXT NOT NULL DEFAULT '',
    hypervisor       TEXT NOT NULL DEFAULT '',
    owner_id         TEXT REFERENCES users(id) ON DELETE SET NULL,
    backup_owner_id  TEXT REFERENCES users(id) ON DELETE SET NULL,
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired', 'unknown')),
    config_notes     TEXT NOT NULL DEFAULT '',
    deleted_at       TEXT,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX assets_hostname_active_uidx ON assets(hostname COLLATE NOCASE) WHERE deleted_at IS NULL;
CREATE INDEX assets_environment_idx ON assets(environment);
CREATE INDEX assets_owner_id_idx ON assets(owner_id);
CREATE INDEX assets_status_idx ON assets(status);

CREATE TABLE tags (
    id   TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL COLLATE NOCASE,
    UNIQUE (name)
);

CREATE TABLE asset_tags (
    asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    tag_id   TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (asset_id, tag_id)
);

CREATE INDEX asset_tags_tag_id_idx ON asset_tags(tag_id);

CREATE TABLE accounts (
    id              TEXT PRIMARY KEY NOT NULL,
    asset_id        TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    username        TEXT NOT NULL,
    auth_type       TEXT NOT NULL CHECK (auth_type IN ('password', 'ssh_private_key', 'api_token', 'other')),
    description     TEXT NOT NULL DEFAULT '',
    last_rotated_at TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX accounts_asset_id_idx ON accounts(asset_id);

CREATE TABLE secret_payloads (
    account_id  TEXT PRIMARY KEY NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    nonce       BLOB NOT NULL,
    ciphertext  BLOB NOT NULL,
    wrapped_dek BLOB NOT NULL,
    key_version TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX secret_payloads_key_version_idx ON secret_payloads(key_version);

CREATE TABLE scheduled_jobs (
    id              TEXT PRIMARY KEY NOT NULL,
    asset_id        TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    scheduler_type  TEXT NOT NULL CHECK (scheduler_type IN ('cron', 'systemd_timer', 'windows_task', 'other')),
    schedule_expr   TEXT NOT NULL DEFAULT '',
    command_or_path TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    enabled_doc     INTEGER NOT NULL DEFAULT 1 CHECK (enabled_doc IN (0, 1)),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX scheduled_jobs_asset_id_idx ON scheduled_jobs(asset_id);

CREATE TABLE asset_notes (
    id         TEXT PRIMARY KEY NOT NULL,
    asset_id   TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    title      TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    author_id  TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX asset_notes_asset_id_idx ON asset_notes(asset_id);

CREATE TABLE audit_events (
    id            TEXT PRIMARY KEY NOT NULL,
    actor_id      TEXT REFERENCES users(id) ON DELETE SET NULL,
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT,
    outcome       TEXT NOT NULL CHECK (outcome IN ('success', 'failure', 'denied')),
    ip            TEXT NOT NULL DEFAULT '',
    user_agent    TEXT NOT NULL DEFAULT '',
    metadata      TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX audit_events_created_at_idx ON audit_events(created_at);
CREATE INDEX audit_events_actor_id_idx ON audit_events(actor_id);
CREATE INDEX audit_events_action_idx ON audit_events(action);

CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY NOT NULL,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

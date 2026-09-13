-- FTS5 asset search index (non-secret fields only).
-- Indexed: asset name/hostname/purpose/os_detail/location/primary_ip/additional_ips/
-- hypervisor/config_notes, tags, account.username, job name+description, note title.
-- Secrets (secret_payloads) and note bodies are intentionally excluded.
-- asset_tags changes are rebuilt once from the app (replaceTagsTx), not per-row triggers.

CREATE VIRTUAL TABLE assets_fts USING fts5(
    asset_id UNINDEXED,
    body,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE VIEW assets_fts_src AS
SELECT
    a.id AS asset_id,
    a.name || ' ' ||
    a.hostname || ' ' ||
    a.purpose || ' ' ||
    a.os_detail || ' ' ||
    a.location || ' ' ||
    a.primary_ip || ' ' ||
    a.additional_ips || ' ' ||
    a.hypervisor || ' ' ||
    a.config_notes || ' ' ||
    IFNULL((
        SELECT group_concat(t.name, ' ')
        FROM asset_tags at
        INNER JOIN tags t ON t.id = at.tag_id
        WHERE at.asset_id = a.id
    ), '') || ' ' ||
    IFNULL((
        SELECT group_concat(ac.username, ' ')
        FROM accounts ac
        WHERE ac.asset_id = a.id
    ), '') || ' ' ||
    IFNULL((
        SELECT group_concat(j.name || ' ' || j.description, ' ')
        FROM scheduled_jobs j
        WHERE j.asset_id = a.id
    ), '') || ' ' ||
    IFNULL((
        SELECT group_concat(n.title, ' ')
        FROM asset_notes n
        WHERE n.asset_id = a.id
    ), '') AS body
FROM assets a;

INSERT INTO assets_fts(asset_id, body)
SELECT asset_id, body FROM assets_fts_src;

CREATE TRIGGER assets_ai_fts AFTER INSERT ON assets BEGIN
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src WHERE asset_id = NEW.id;
END;

CREATE TRIGGER assets_au_fts AFTER UPDATE ON assets BEGIN
    DELETE FROM assets_fts WHERE asset_id = OLD.id;
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src WHERE asset_id = NEW.id;
END;

CREATE TRIGGER assets_ad_fts AFTER DELETE ON assets BEGIN
    DELETE FROM assets_fts WHERE asset_id = OLD.id;
END;

CREATE TRIGGER tags_au_fts AFTER UPDATE OF name ON tags BEGIN
    DELETE FROM assets_fts WHERE asset_id IN (
        SELECT asset_id FROM asset_tags WHERE tag_id = NEW.id
    );
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src
    WHERE asset_id IN (SELECT asset_id FROM asset_tags WHERE tag_id = NEW.id);
END;

CREATE TRIGGER accounts_ai_fts AFTER INSERT ON accounts BEGIN
    DELETE FROM assets_fts WHERE asset_id = NEW.asset_id;
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src WHERE asset_id = NEW.asset_id;
END;

CREATE TRIGGER accounts_au_fts AFTER UPDATE ON accounts BEGIN
    DELETE FROM assets_fts WHERE asset_id IN (OLD.asset_id, NEW.asset_id);
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src
    WHERE asset_id IN (OLD.asset_id, NEW.asset_id);
END;

CREATE TRIGGER accounts_ad_fts AFTER DELETE ON accounts BEGIN
    DELETE FROM assets_fts WHERE asset_id = OLD.asset_id;
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src WHERE asset_id = OLD.asset_id;
END;

CREATE TRIGGER scheduled_jobs_ai_fts AFTER INSERT ON scheduled_jobs BEGIN
    DELETE FROM assets_fts WHERE asset_id = NEW.asset_id;
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src WHERE asset_id = NEW.asset_id;
END;

CREATE TRIGGER scheduled_jobs_au_fts AFTER UPDATE ON scheduled_jobs BEGIN
    DELETE FROM assets_fts WHERE asset_id IN (OLD.asset_id, NEW.asset_id);
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src
    WHERE asset_id IN (OLD.asset_id, NEW.asset_id);
END;

CREATE TRIGGER scheduled_jobs_ad_fts AFTER DELETE ON scheduled_jobs BEGIN
    DELETE FROM assets_fts WHERE asset_id = OLD.asset_id;
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src WHERE asset_id = OLD.asset_id;
END;

CREATE TRIGGER asset_notes_ai_fts AFTER INSERT ON asset_notes BEGIN
    DELETE FROM assets_fts WHERE asset_id = NEW.asset_id;
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src WHERE asset_id = NEW.asset_id;
END;

CREATE TRIGGER asset_notes_au_fts AFTER UPDATE ON asset_notes BEGIN
    DELETE FROM assets_fts WHERE asset_id IN (OLD.asset_id, NEW.asset_id);
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src
    WHERE asset_id IN (OLD.asset_id, NEW.asset_id);
END;

CREATE TRIGGER asset_notes_ad_fts AFTER DELETE ON asset_notes BEGIN
    DELETE FROM assets_fts WHERE asset_id = OLD.asset_id;
    INSERT INTO assets_fts(asset_id, body)
    SELECT asset_id, body FROM assets_fts_src WHERE asset_id = OLD.asset_id;
END;

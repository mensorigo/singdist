-- Config versions gain a user-editable note (renaming a version only
-- changes this label; the archived file itself is never renamed).
ALTER TABLE config_versions ADD COLUMN note TEXT NOT NULL DEFAULT '';

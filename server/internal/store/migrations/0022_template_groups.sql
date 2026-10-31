-- Template proxy groups (Clash proxy-groups): a JSON array stored on the
-- template. Empty means "defaults": Proxy (select) + Auto (url-test) +
-- Fallback (fallback), each over all of the template's inbounds, so
-- pre-existing templates render the new default groups without a data
-- migration. Groups are only ever appended as a column, never dropped.
ALTER TABLE templates ADD COLUMN groups TEXT NOT NULL DEFAULT '';

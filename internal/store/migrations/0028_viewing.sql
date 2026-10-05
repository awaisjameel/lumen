-- Presentation edits never change the source metadata or model inputs.
ALTER TABLE assets ADD COLUMN display_rotation INTEGER NOT NULL DEFAULT 0 CHECK(display_rotation BETWEEN 0 AND 3);
ALTER TABLE assets ADD COLUMN display_flip INTEGER NOT NULL DEFAULT 0 CHECK(display_flip IN (0, 1));
ALTER TABLE assets ADD COLUMN display_version INTEGER NOT NULL DEFAULT 0;
CREATE TABLE playback_history (
    asset_id INTEGER PRIMARY KEY REFERENCES assets(id) ON DELETE CASCADE,
    position_ms INTEGER NOT NULL CHECK(position_ms >= 0)
);

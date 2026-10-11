-- лента событий: watcher пишет, TUI читает, notifier отмечает обработанные
CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id  TEXT    NOT NULL UNIQUE, -- id у GitHub, защищает от дублей
    kind       TEXT    NOT NULL,
    repo       TEXT    NOT NULL,
    actor      TEXT    NOT NULL DEFAULT '',
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '', -- подробности (для пуша — коммиты и файлы)
    url        TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,          -- unix ms, UTC
    notified   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS events_created_idx ON events (created_at DESC);
CREATE INDEX IF NOT EXISTS events_repo_idx ON events (repo, created_at DESC);
CREATE INDEX IF NOT EXISTS events_pending_idx ON events (id) WHERE notified = 0;

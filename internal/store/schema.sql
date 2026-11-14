CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS dusun (
  id   INTEGER PRIMARY KEY AUTOINCREMENT,
  nama TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS timses (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  nama       TEXT NOT NULL UNIQUE,
  kuota      INTEGER,
  aktif      INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  username    TEXT NOT NULL UNIQUE COLLATE NOCASE,
  pass_hash   TEXT NOT NULL,
  role        TEXT NOT NULL CHECK (role IN ('admin','timses')),
  timses_id   INTEGER REFERENCES timses(id) ON DELETE CASCADE,
  aktif       INTEGER NOT NULL DEFAULT 1,
  must_change INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  CHECK (role = 'admin' OR timses_id IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  csrf       TEXT NOT NULL,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS voters (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  timses_id  INTEGER NOT NULL REFERENCES timses(id),
  no_urut    INTEGER NOT NULL,
  nik        TEXT NOT NULL UNIQUE,
  nama       TEXT NOT NULL,
  dusun      TEXT NOT NULL,
  rt         TEXT NOT NULL,
  rw         TEXT NOT NULL,
  created_by INTEGER REFERENCES users(id),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (timses_id, no_urut)
);

CREATE INDEX IF NOT EXISTS idx_voters_timses ON voters(timses_id, no_urut);
CREATE INDEX IF NOT EXISTS idx_voters_nama   ON voters(nama);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS audit_log (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  at       TEXT NOT NULL,
  user_id  INTEGER,
  username TEXT,
  aksi     TEXT NOT NULL,
  voter_id INTEGER,
  ringkas  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_at ON audit_log(id DESC);

CREATE TABLE IF NOT EXISTS integrity_log (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  at        TEXT NOT NULL,
  jumlah    INTEGER NOT NULL,
  hash      TEXT NOT NULL,
  prev_hash TEXT NOT NULL,
  oleh      TEXT
);

-- Kuota per timses dijaga di level database: dua orang menyimpan bersamaan
-- tetap tidak bisa melewati batas.
CREATE TRIGGER IF NOT EXISTS voters_kuota
BEFORE INSERT ON voters
WHEN (SELECT COUNT(*) FROM voters WHERE timses_id = NEW.timses_id)
     >= COALESCE(
          (SELECT kuota FROM timses WHERE id = NEW.timses_id),
          (SELECT CAST(value AS INTEGER) FROM settings WHERE key = 'default_kuota'),
          20
        )
BEGIN
  SELECT RAISE(ABORT, 'KUOTA_PENUH');
END;

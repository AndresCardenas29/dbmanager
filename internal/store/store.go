// Package store persiste la configuración e historial de la app en SQLite (sin CGO).
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store encapsula la base SQLite y la clave de cifrado de secretos.
type Store struct {
	db  *sql.DB
	key []byte
	dir string
}

// DataDir devuelve el directorio de datos de la app.
func DataDir() (string, error) {
	if d := os.Getenv("DBMANAGER_DATA_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "dbmanager"), nil
}

// Open abre (y migra) la base en el directorio de datos.
func Open() (*Store, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	return OpenAt(dir)
}

// OpenAt abre el store en un directorio concreto.
func OpenAt(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key, err := loadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(filepath.Join(dir, "app.db")) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, key: key, dir: dir}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close cierra la base.
func (s *Store) Close() error { return s.db.Close() }

// Dir devuelve el directorio de datos usado.
func (s *Store) Dir() string { return s.dir }

var migrations = []string{
	// 1: esquema inicial
	`CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE servers (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		name             TEXT NOT NULL UNIQUE,
		engine           TEXT NOT NULL,
		mode             TEXT NOT NULL,
		host             TEXT NOT NULL DEFAULT '',
		port             INTEGER NOT NULL DEFAULT 0,
		admin_user       TEXT NOT NULL DEFAULT '',
		admin_password   TEXT NOT NULL DEFAULT '',
		container_name   TEXT NOT NULL DEFAULT '',
		image            TEXT NOT NULL DEFAULT '',
		data_mode        TEXT NOT NULL DEFAULT '',
		data_path        TEXT NOT NULL DEFAULT '',
		memory_mb        INTEGER NOT NULL DEFAULT 0,
		tls              INTEGER NOT NULL DEFAULT 0,
		trust_server_cert INTEGER NOT NULL DEFAULT 0,
		mssql_edition    TEXT NOT NULL DEFAULT '',
		mssql_collation  TEXT NOT NULL DEFAULT '',
		pg_default_db    TEXT NOT NULL DEFAULT '',
		pg_ssl_mode      TEXT NOT NULL DEFAULT '',
		created_at       TEXT NOT NULL
	);
	CREATE TABLE backups (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id   INTEGER NOT NULL,
		server_name TEXT NOT NULL DEFAULT '',
		database    TEXT NOT NULL,
		file_path   TEXT NOT NULL DEFAULT '',
		size_bytes  INTEGER NOT NULL DEFAULT 0,
		status      TEXT NOT NULL,
		message     TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL
	);
	CREATE INDEX idx_backups_server ON backups(server_id);
	CREATE TABLE restores (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id       INTEGER NOT NULL,
		server_name     TEXT NOT NULL DEFAULT '',
		source_file     TEXT NOT NULL,
		target_database TEXT NOT NULL,
		data_dir        TEXT NOT NULL DEFAULT '',
		log_dir         TEXT NOT NULL DEFAULT '',
		status          TEXT NOT NULL,
		message         TEXT NOT NULL DEFAULT '',
		started_at      TEXT NOT NULL,
		finished_at     TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX idx_restores_server ON restores(server_id);
	CREATE TABLE operations (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id  INTEGER NOT NULL DEFAULT 0,
		kind       TEXT NOT NULL,
		target     TEXT NOT NULL DEFAULT '',
		status     TEXT NOT NULL,
		message    TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);`,
	// 2: nombres de servidor únicos sin distinguir mayúsculas
	`CREATE UNIQUE INDEX idx_servers_name_nocase ON servers(name COLLATE NOCASE);`,
	// 3: SFTP y certificados (v2). Nunca se guardan llaves privadas ni contraseñas en claro.
	`CREATE TABLE sftp_services (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		mode TEXT NOT NULL,
		container_name TEXT NOT NULL DEFAULT '',
		image TEXT NOT NULL DEFAULT '',
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 0,
		data_mode TEXT NOT NULL DEFAULT '',
		data_path TEXT NOT NULL DEFAULT '',
		expose_on_network INTEGER NOT NULL DEFAULT 0,
		next_uid INTEGER NOT NULL DEFAULT 1001,
		created_at TEXT NOT NULL
	);
	CREATE UNIQUE INDEX idx_sftp_services_name ON sftp_services(name COLLATE NOCASE);
	CREATE TABLE sftp_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		service_id INTEGER NOT NULL REFERENCES sftp_services(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		uid INTEGER NOT NULL,
		password_hash TEXT NOT NULL DEFAULT '',
		upload_dir TEXT NOT NULL DEFAULT 'upload',
		created_at TEXT NOT NULL,
		UNIQUE(service_id, name)
	);
	CREATE TABLE sftp_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES sftp_users(id) ON DELETE CASCADE,
		comment TEXT NOT NULL DEFAULT '',
		key_type TEXT NOT NULL,
		fingerprint TEXT NOT NULL,
		public_key TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE cert_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		alias TEXT NOT NULL,
		common_name TEXT NOT NULL DEFAULT '',
		not_after TEXT NOT NULL DEFAULT '',
		output_path TEXT NOT NULL DEFAULT '',
		chain_length INTEGER NOT NULL DEFAULT 0,
		verified INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	);`,
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	var cur int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&cur); err != nil {
		return err
	}
	for i := cur; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migración %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?,?)`, i+1, now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion devuelve la versión de esquema aplicada.
func (s *Store) SchemaVersion() (int, error) {
	var v int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v)
	return v, err
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

package store

import (
	"database/sql"
	"errors"
	"strings"
)

// SetLoginSecret guarda (cifrada) la contraseña de un login creado desde la app.
func (s *Store) SetLoginSecret(serverID int64, login, password string) error {
	enc, err := s.Encrypt(password)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO login_secrets(server_id, login, password_enc, updated_at) VALUES(?,?,?,?)
		ON CONFLICT(server_id, login) DO UPDATE SET password_enc=excluded.password_enc, updated_at=excluded.updated_at`,
		serverID, login, enc, now())
	return err
}

// LoginSecret devuelve la contraseña guardada del login (ok=false si no hay).
func (s *Store) LoginSecret(serverID int64, login string) (string, bool, error) {
	var enc string
	err := s.db.QueryRow(`SELECT password_enc FROM login_secrets WHERE server_id=? AND login=?`, serverID, login).Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	pw, err := s.Decrypt(enc)
	if err != nil {
		return "", false, err
	}
	return pw, true, nil
}

// DeleteLoginSecret elimina la contraseña guardada del login.
func (s *Store) DeleteLoginSecret(serverID int64, login string) error {
	_, err := s.db.Exec(`DELETE FROM login_secrets WHERE server_id=? AND login=?`, serverID, login)
	return err
}

// ListLoginSecretLogins devuelve los logins (en minúsculas) con contraseña guardada.
func (s *Store) ListLoginSecretLogins(serverID int64) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT login FROM login_secrets WHERE server_id=?`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out[strings.ToLower(l)] = true
	}
	return out, rows.Err()
}

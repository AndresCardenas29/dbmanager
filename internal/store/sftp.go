package store

import (
	"database/sql"
	"errors"
	"time"
)

// SftpService es un servicio SFTP (managed = creado por la app, external = importado).
type SftpService struct {
	ID              int64
	Name            string
	Mode            string // managed | external
	ContainerName   string
	Image           string
	Host            string
	Port            int
	DataMode        string
	DataPath        string
	ExposeOnNetwork bool
	NextUID         int
	CreatedAt       time.Time
}

const sftpSvcCols = `id,name,mode,container_name,image,host,port,data_mode,data_path,expose_on_network,next_uid,created_at`

func scanSftpSvc(r scanner) (SftpService, error) {
	var s SftpService
	var c string
	err := r.Scan(&s.ID, &s.Name, &s.Mode, &s.ContainerName, &s.Image, &s.Host, &s.Port, &s.DataMode, &s.DataPath, &s.ExposeOnNetwork, &s.NextUID, &c)
	s.CreatedAt = parseTime(c)
	return s, err
}

func (s *Store) CreateSftpService(v SftpService) (SftpService, error) {
	if v.NextUID == 0 {
		v.NextUID = 1001
	}
	res, err := s.db.Exec(`INSERT INTO sftp_services(name,mode,container_name,image,host,port,data_mode,data_path,expose_on_network,next_uid,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, v.Name, v.Mode, v.ContainerName, v.Image, v.Host, v.Port, v.DataMode, v.DataPath, v.ExposeOnNetwork, v.NextUID, now())
	if err != nil {
		return SftpService{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetSftpService(id)
}

func (s *Store) GetSftpService(id int64) (SftpService, error) {
	v, err := scanSftpSvc(s.db.QueryRow(`SELECT `+sftpSvcCols+` FROM sftp_services WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return SftpService{}, ErrNotFound
	}
	return v, err
}

func (s *Store) ListSftpServices() ([]SftpService, error) {
	rows, err := s.db.Query(`SELECT ` + sftpSvcCols + ` FROM sftp_services ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SftpService{}
	for rows.Next() {
		v, err := scanSftpSvc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSftpServiceRuntime(id int64, port int, host string) error {
	_, err := s.db.Exec(`UPDATE sftp_services SET port=?, host=? WHERE id=?`, port, host, id)
	return err
}

func (s *Store) DeleteSftpService(id int64) error {
	res, err := s.db.Exec(`DELETE FROM sftp_services WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SftpUser es un usuario de un servicio gestionado. PasswordHash es un hash crypt SHA-512 (nunca la clave en claro).
type SftpUser struct {
	ID           int64
	ServiceID    int64
	Name         string
	UID          int
	PasswordHash string
	UploadDir    string
	CreatedAt    time.Time
}

type SftpKey struct {
	ID          int64
	UserID      int64
	Comment     string
	KeyType     string
	Fingerprint string
	PublicKey   string
	CreatedAt   time.Time
}

// CreateSftpUser inserta el usuario asignando un uid estable (next_uid del servicio).
func (s *Store) CreateSftpUser(u SftpUser) (SftpUser, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return SftpUser{}, err
	}
	defer tx.Rollback()
	var uid int
	if err := tx.QueryRow(`SELECT next_uid FROM sftp_services WHERE id=?`, u.ServiceID).Scan(&uid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SftpUser{}, ErrNotFound
		}
		return SftpUser{}, err
	}
	res, err := tx.Exec(`INSERT INTO sftp_users(service_id,name,uid,password_hash,upload_dir,created_at) VALUES(?,?,?,?,?,?)`,
		u.ServiceID, u.Name, uid, u.PasswordHash, u.UploadDir, now())
	if err != nil {
		return SftpUser{}, err
	}
	if _, err := tx.Exec(`UPDATE sftp_services SET next_uid=next_uid+1 WHERE id=?`, u.ServiceID); err != nil {
		return SftpUser{}, err
	}
	if err := tx.Commit(); err != nil {
		return SftpUser{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetSftpUser(u.ServiceID, u.Name, id)
}

// GetSftpUser busca por nombre (o por id si id != 0).
func (s *Store) GetSftpUser(serviceID int64, name string, id int64) (SftpUser, error) {
	var u SftpUser
	var c string
	q := `SELECT id,service_id,name,uid,password_hash,upload_dir,created_at FROM sftp_users WHERE service_id=? AND name=?`
	args := []any{serviceID, name}
	if id != 0 {
		q = `SELECT id,service_id,name,uid,password_hash,upload_dir,created_at FROM sftp_users WHERE id=?`
		args = []any{id}
	}
	err := s.db.QueryRow(q, args...).Scan(&u.ID, &u.ServiceID, &u.Name, &u.UID, &u.PasswordHash, &u.UploadDir, &c)
	if errors.Is(err, sql.ErrNoRows) {
		return SftpUser{}, ErrNotFound
	}
	u.CreatedAt = parseTime(c)
	return u, err
}

func (s *Store) ListSftpUsers(serviceID int64) ([]SftpUser, error) {
	rows, err := s.db.Query(`SELECT id,service_id,name,uid,password_hash,upload_dir,created_at FROM sftp_users WHERE service_id=? ORDER BY uid`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SftpUser{}
	for rows.Next() {
		var u SftpUser
		var c string
		if err := rows.Scan(&u.ID, &u.ServiceID, &u.Name, &u.UID, &u.PasswordHash, &u.UploadDir, &c); err != nil {
			return nil, err
		}
		u.CreatedAt = parseTime(c)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetSftpUserPasswordHash(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE sftp_users SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func (s *Store) DeleteSftpUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM sftp_users WHERE id=?`, id)
	return err
}

func (s *Store) AddSftpKey(k SftpKey) (SftpKey, error) {
	res, err := s.db.Exec(`INSERT INTO sftp_keys(user_id,comment,key_type,fingerprint,public_key,created_at) VALUES(?,?,?,?,?,?)`,
		k.UserID, k.Comment, k.KeyType, k.Fingerprint, k.PublicKey, now())
	if err != nil {
		return SftpKey{}, err
	}
	k.ID, _ = res.LastInsertId()
	k.CreatedAt = time.Now().UTC()
	return k, nil
}

func (s *Store) ListSftpKeys(userID int64) ([]SftpKey, error) {
	rows, err := s.db.Query(`SELECT id,user_id,comment,key_type,fingerprint,public_key,created_at FROM sftp_keys WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SftpKey{}
	for rows.Next() {
		var k SftpKey
		var c string
		if err := rows.Scan(&k.ID, &k.UserID, &k.Comment, &k.KeyType, &k.Fingerprint, &k.PublicKey, &c); err != nil {
			return nil, err
		}
		k.CreatedAt = parseTime(c)
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteSftpKey elimina una llave de un usuario; ErrNotFound si no existe.
func (s *Store) DeleteSftpKey(userID, keyID int64) error {
	res, err := s.db.Exec(`DELETE FROM sftp_keys WHERE id=? AND user_id=?`, keyID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Historial de certificados (sin contraseñas) ----------

type CertHistory struct {
	ID          int64
	Alias       string
	CommonName  string
	NotAfter    string
	OutputPath  string
	ChainLength int
	Verified    bool
	CreatedAt   time.Time
}

func (s *Store) AddCertHistory(h CertHistory) (CertHistory, error) {
	res, err := s.db.Exec(`INSERT INTO cert_history(alias,common_name,not_after,output_path,chain_length,verified,created_at) VALUES(?,?,?,?,?,?,?)`,
		h.Alias, h.CommonName, h.NotAfter, h.OutputPath, h.ChainLength, h.Verified, now())
	if err != nil {
		return CertHistory{}, err
	}
	h.ID, _ = res.LastInsertId()
	h.CreatedAt = time.Now().UTC()
	return h, nil
}

func (s *Store) ListCertHistory(limit int) ([]CertHistory, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id,alias,common_name,not_after,output_path,chain_length,verified,created_at FROM cert_history ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CertHistory{}
	for rows.Next() {
		var h CertHistory
		var c string
		if err := rows.Scan(&h.ID, &h.Alias, &h.CommonName, &h.NotAfter, &h.OutputPath, &h.ChainLength, &h.Verified, &c); err != nil {
			return nil, err
		}
		h.CreatedAt = parseTime(c)
		out = append(out, h)
	}
	return out, rows.Err()
}

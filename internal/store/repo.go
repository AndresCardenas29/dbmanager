package store

import (
	"database/sql"
	"errors"
	"time"
)

// ErrNotFound se devuelve cuando un registro no existe.
var ErrNotFound = errors.New("registro no encontrado")

type scanner interface{ Scan(dest ...any) error }

// ---------- Settings ----------

func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) GetSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key,value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ---------- Usuario root predeterminado ----------

const (
	// DefaultRootUser y DefaultRootPassword son el usuario administrador que DB Manager crea en cada servicio nuevo
	// mientras el usuario no los cambie en Ajustes.
	DefaultRootUser     = "root"
	DefaultRootPassword = "root"

	SettingRootUser        = "rootUser"
	SettingRootPasswordEnc = "rootPasswordEnc" // cifrada; nunca se devuelve al frontend
)

// RootDefaults devuelve el usuario y la contraseña root predeterminados (en claro).
func (s *Store) RootDefaults() (user, password string, err error) {
	user, err = s.GetSetting(SettingRootUser)
	if err != nil {
		return "", "", err
	}
	if user == "" {
		user = DefaultRootUser
	}
	enc, err := s.GetSetting(SettingRootPasswordEnc)
	if err != nil {
		return "", "", err
	}
	if enc == "" {
		return user, DefaultRootPassword, nil
	}
	password, err = s.Decrypt(enc)
	return user, password, err
}

// SetRootDefaults guarda el usuario y, si password != "", la contraseña root predeterminada (cifrada).
func (s *Store) SetRootDefaults(user, password string) error {
	if err := s.SetSetting(SettingRootUser, user); err != nil {
		return err
	}
	if password == "" {
		return nil
	}
	enc, err := s.Encrypt(password)
	if err != nil {
		return err
	}
	return s.SetSetting(SettingRootPasswordEnc, enc)
}

// ---------- Servers ----------

// Server es el registro persistido. AdminPasswordEnc está cifrado (AES-GCM, base64);
// use Store.ServerPassword para obtener la contraseña en claro.
type Server struct {
	ID               int64
	Name             string
	Engine           string // mssql | postgres
	Mode             string // local | remote
	Host             string
	Port             int
	AdminUser        string
	AdminPasswordEnc string
	ContainerName    string
	Image            string
	DataMode         string // volume | bind | ""
	DataPath         string
	MemoryMB         int
	TLS              bool
	TrustServerCert  bool
	MssqlEdition     string
	MssqlCollation   string
	PgDefaultDB      string
	PgSSLMode        string
	CreatedAt        time.Time
}

const serverCols = `id,name,engine,mode,host,port,admin_user,admin_password,container_name,image,
	data_mode,data_path,memory_mb,tls,trust_server_cert,mssql_edition,mssql_collation,pg_default_db,pg_ssl_mode,created_at`

func scanServer(r scanner) (Server, error) {
	var sv Server
	var created string
	err := r.Scan(&sv.ID, &sv.Name, &sv.Engine, &sv.Mode, &sv.Host, &sv.Port, &sv.AdminUser, &sv.AdminPasswordEnc,
		&sv.ContainerName, &sv.Image, &sv.DataMode, &sv.DataPath, &sv.MemoryMB, &sv.TLS, &sv.TrustServerCert,
		&sv.MssqlEdition, &sv.MssqlCollation, &sv.PgDefaultDB, &sv.PgSSLMode, &created)
	sv.CreatedAt = parseTime(created)
	return sv, err
}

// CreateServer inserta un servidor. passwordPlain se cifra antes de guardar.
func (s *Store) CreateServer(sv Server, passwordPlain string) (Server, error) {
	enc, err := s.Encrypt(passwordPlain)
	if err != nil {
		return Server{}, err
	}
	res, err := s.db.Exec(`INSERT INTO servers(name,engine,mode,host,port,admin_user,admin_password,container_name,image,
		data_mode,data_path,memory_mb,tls,trust_server_cert,mssql_edition,mssql_collation,pg_default_db,pg_ssl_mode,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sv.Name, sv.Engine, sv.Mode, sv.Host, sv.Port, sv.AdminUser, enc, sv.ContainerName, sv.Image,
		sv.DataMode, sv.DataPath, sv.MemoryMB, sv.TLS, sv.TrustServerCert, sv.MssqlEdition, sv.MssqlCollation,
		sv.PgDefaultDB, sv.PgSSLMode, now())
	if err != nil {
		return Server{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetServer(id)
}

func (s *Store) GetServer(id int64) (Server, error) {
	sv, err := scanServer(s.db.QueryRow(`SELECT `+serverCols+` FROM servers WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Server{}, ErrNotFound
	}
	return sv, err
}

func (s *Store) ListServers() ([]Server, error) {
	rows, err := s.db.Query(`SELECT ` + serverCols + ` FROM servers ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Server{}
	for rows.Next() {
		sv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, rows.Err()
}

// UpdateServer actualiza los campos editables. Si newPassword != "" se re-cifra y guarda;
// si está vacía se conserva la contraseña actual.
func (s *Store) UpdateServer(sv Server, newPassword string) (Server, error) {
	cur, err := s.GetServer(sv.ID)
	if err != nil {
		return Server{}, err
	}
	sv.AdminPasswordEnc = cur.AdminPasswordEnc
	if newPassword != "" {
		enc, err := s.Encrypt(newPassword)
		if err != nil {
			return Server{}, err
		}
		sv.AdminPasswordEnc = enc
	}
	_, err = s.db.Exec(`UPDATE servers SET name=?,engine=?,mode=?,host=?,port=?,admin_user=?,admin_password=?,
		container_name=?,image=?,data_mode=?,data_path=?,memory_mb=?,tls=?,trust_server_cert=?,mssql_edition=?,
		mssql_collation=?,pg_default_db=?,pg_ssl_mode=? WHERE id=?`,
		sv.Name, sv.Engine, sv.Mode, sv.Host, sv.Port, sv.AdminUser, sv.AdminPasswordEnc, sv.ContainerName, sv.Image,
		sv.DataMode, sv.DataPath, sv.MemoryMB, sv.TLS, sv.TrustServerCert, sv.MssqlEdition, sv.MssqlCollation,
		sv.PgDefaultDB, sv.PgSSLMode, sv.ID)
	if err != nil {
		return Server{}, err
	}
	return s.GetServer(sv.ID)
}

func (s *Store) DeleteServer(id int64) error {
	res, err := s.db.Exec(`DELETE FROM servers WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ServerPassword devuelve la contraseña de administrador descifrada.
func (s *Store) ServerPassword(sv Server) (string, error) { return s.Decrypt(sv.AdminPasswordEnc) }

// ---------- Backups ----------

type Backup struct {
	ID         int64
	ServerID   int64
	ServerName string
	Database   string
	FilePath   string
	SizeBytes  int64
	Status     string
	Message    string
	CreatedAt  time.Time
}

const backupCols = `id,server_id,server_name,database,file_path,size_bytes,status,message,created_at`

func scanBackup(r scanner) (Backup, error) {
	var b Backup
	var c string
	err := r.Scan(&b.ID, &b.ServerID, &b.ServerName, &b.Database, &b.FilePath, &b.SizeBytes, &b.Status, &b.Message, &c)
	b.CreatedAt = parseTime(c)
	return b, err
}

func (s *Store) CreateBackup(b Backup) (Backup, error) {
	res, err := s.db.Exec(`INSERT INTO backups(server_id,server_name,database,file_path,size_bytes,status,message,created_at)
		VALUES(?,?,?,?,?,?,?,?)`, b.ServerID, b.ServerName, b.Database, b.FilePath, b.SizeBytes, b.Status, b.Message, now())
	if err != nil {
		return Backup{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetBackup(id)
}

func (s *Store) GetBackup(id int64) (Backup, error) {
	b, err := scanBackup(s.db.QueryRow(`SELECT `+backupCols+` FROM backups WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Backup{}, ErrNotFound
	}
	return b, err
}

func (s *Store) UpdateBackup(b Backup) error {
	_, err := s.db.Exec(`UPDATE backups SET file_path=?,size_bytes=?,status=?,message=? WHERE id=?`,
		b.FilePath, b.SizeBytes, b.Status, b.Message, b.ID)
	return err
}

// ListBackups lista los backups de un servidor (serverID 0 = todos), más recientes primero.
func (s *Store) ListBackups(serverID int64) ([]Backup, error) {
	rows, err := s.db.Query(`SELECT `+backupCols+` FROM backups WHERE (?=0 OR server_id=?) ORDER BY id DESC`, serverID, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Backup{}
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---------- Restores ----------

type Restore struct {
	ID             int64
	ServerID       int64
	ServerName     string
	SourceFile     string
	TargetDatabase string
	DataDir        string
	LogDir         string
	Status         string
	Message        string
	StartedAt      time.Time
	FinishedAt     time.Time // cero si no terminó
}

const restoreCols = `id,server_id,server_name,source_file,target_database,data_dir,log_dir,status,message,started_at,finished_at`

func scanRestore(r scanner) (Restore, error) {
	var x Restore
	var st, fin string
	err := r.Scan(&x.ID, &x.ServerID, &x.ServerName, &x.SourceFile, &x.TargetDatabase, &x.DataDir, &x.LogDir,
		&x.Status, &x.Message, &st, &fin)
	x.StartedAt = parseTime(st)
	if fin != "" {
		x.FinishedAt = parseTime(fin)
	}
	return x, err
}

func (s *Store) CreateRestore(x Restore) (Restore, error) {
	res, err := s.db.Exec(`INSERT INTO restores(server_id,server_name,source_file,target_database,data_dir,log_dir,status,message,started_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, x.ServerID, x.ServerName, x.SourceFile, x.TargetDatabase, x.DataDir, x.LogDir, x.Status, x.Message, now())
	if err != nil {
		return Restore{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetRestore(id)
}

func (s *Store) GetRestore(id int64) (Restore, error) {
	x, err := scanRestore(s.db.QueryRow(`SELECT `+restoreCols+` FROM restores WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Restore{}, ErrNotFound
	}
	return x, err
}

// FinishRestore marca el estado final y la hora de término.
func (s *Store) FinishRestore(id int64, status, message string) error {
	_, err := s.db.Exec(`UPDATE restores SET status=?,message=?,finished_at=? WHERE id=?`, status, message, now(), id)
	return err
}

func (s *Store) UpdateRestoreStatus(id int64, status, message string) error {
	_, err := s.db.Exec(`UPDATE restores SET status=?,message=? WHERE id=?`, status, message, id)
	return err
}

func (s *Store) ListRestores(serverID int64) ([]Restore, error) {
	rows, err := s.db.Query(`SELECT `+restoreCols+` FROM restores WHERE (?=0 OR server_id=?) ORDER BY id DESC`, serverID, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Restore{}
	for rows.Next() {
		x, err := scanRestore(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ---------- Operations ----------

type Operation struct {
	ID        int64
	ServerID  int64
	Kind      string
	Target    string
	Status    string
	Message   string
	CreatedAt time.Time
}

func (s *Store) LogOperation(o Operation) (Operation, error) {
	res, err := s.db.Exec(`INSERT INTO operations(server_id,kind,target,status,message,created_at) VALUES(?,?,?,?,?,?)`,
		o.ServerID, o.Kind, o.Target, o.Status, o.Message, now())
	if err != nil {
		return Operation{}, err
	}
	o.ID, _ = res.LastInsertId()
	o.CreatedAt = time.Now().UTC()
	return o, nil
}

func (s *Store) ListOperations(limit int) ([]Operation, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id,server_id,kind,target,status,message,created_at FROM operations ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Operation{}
	for rows.Next() {
		var o Operation
		var c string
		if err := rows.Scan(&o.ID, &o.ServerID, &o.Kind, &o.Target, &o.Status, &o.Message, &c); err != nil {
			return nil, err
		}
		o.CreatedAt = parseTime(c)
		out = append(out, o)
	}
	return out, rows.Err()
}

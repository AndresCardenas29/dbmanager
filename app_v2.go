package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"dbmanager/internal/certs"
	"dbmanager/internal/sftp"
	"dbmanager/internal/store"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var errNotImplemented = errors.New("no implementado todavía")

func (a *App) sftpManager() (*sftp.Manager, error) {
	st, err := a.db()
	if err != nil {
		return nil, err
	}
	a.sftpOnce.Do(func() {
		a.sftpMgr = &sftp.Manager{Store: st, Docker: a.dockerClient, BaseDir: filepath.Join(st.Dir(), "sftp"),
			Emit: func(event string, id int64, rid string, pct int, msg string, done, isErr bool) {
				if a.emit != nil {
					a.emit(event, ProgressEvent{ID: id, RequestID: rid, Percent: pct, Message: msg, Done: done, Error: isErr})
				}
			}}
	})
	return a.sftpMgr, nil
}

func toSftpService(s store.SftpService) SftpService {
	return SftpService{ID: s.ID, Name: s.Name, Mode: s.Mode, ContainerName: s.ContainerName, Image: s.Image, Host: s.Host, Port: s.Port,
		DataMode: s.DataMode, DataPath: s.DataPath, ExposeOnNetwork: s.ExposeOnNetwork, CreatedAt: fmtTime(s.CreatedAt)}
}

func toSftpUser(v sftp.UserView) SftpUser {
	u := SftpUser{Name: v.User.Name, UID: v.User.UID, HasPassword: v.User.PasswordHash != "", UploadDir: v.User.UploadDir,
		Keys: []SftpKey{}, CreatedAt: fmtTime(v.User.CreatedAt)}
	for _, k := range v.Keys {
		u.Keys = append(u.Keys, SftpKey{ID: k.ID, Comment: k.Comment, Type: k.KeyType, Fingerprint: k.Fingerprint, PublicKey: k.PublicKey, CreatedAt: fmtTime(k.CreatedAt)})
	}
	return u
}

func toSftpResult(r sftp.UserResult) SftpUserResult {
	h := r.ConnectionHints
	if h == nil {
		h = []string{}
	}
	return SftpUserResult{User: toSftpUser(r.View), PrivateKeyPath: r.PrivateKeyPath, PublicKeyPath: r.PublicKeyPath, PpkPath: r.PpkPath, ConnectionHints: h}
}

func toKeyReq(k SftpKeyRequest) sftp.KeyRequest {
	return sftp.KeyRequest{Mode: k.Mode, KeyType: k.KeyType, Passphrase: k.Passphrase, Comment: k.Comment, OutputDir: k.OutputDir,
		ExportPpk: k.ExportPpk, PublicKey: k.PublicKey}
}

// sftpDo ejecuta fn con el manager y un contexto con plazo; traduce errores comunes.
func (a *App) sftpDo(timeout time.Duration, fn func(ctx context.Context, m *sftp.Manager) error) error {
	m, err := a.sftpManager()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, timeout)
	defer cancel()
	return friendlyDBErr(fn(ctx, m))
}

// ---------- SFTP ----------

func (a *App) ListSftpServices() ([]SftpService, error) {
	st, err := a.db()
	if err != nil {
		return nil, err
	}
	list, err := st.ListSftpServices()
	if err != nil {
		return nil, err
	}
	out := make([]SftpService, 0, len(list))
	for _, s := range list {
		out = append(out, toSftpService(s))
	}
	return out, nil
}

func (a *App) CreateSftpService(req CreateSftpRequest) (SftpService, error) {
	var out SftpService
	err := a.sftpDo(20*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		s, err := m.Create(ctx, sftp.CreateRequest{RequestID: req.RequestID, Name: req.Name, Image: req.Image, Port: req.Port,
			DataMode: req.DataMode, DataPath: req.DataPath, ExposeOnNetwork: req.ExposeOnNetwork})
		if err != nil {
			a.logOp(0, "create-sftp", req.Name, err)
			return err
		}
		a.logOp(0, "create-sftp", req.Name, nil)
		out = toSftpService(s)
		return nil
	})
	return out, err
}

func (a *App) ListImportableSftpContainers() ([]ContainerInfo, error) {
	var out []ContainerInfo
	err := a.sftpDo(30*time.Second, func(ctx context.Context, m *sftp.Manager) error {
		list, err := m.ListImportable(ctx)
		if err != nil {
			return err
		}
		out = make([]ContainerInfo, 0, len(list))
		for _, c := range list {
			out = append(out, ContainerInfo{ID: c.ID, Name: c.Name, Image: c.Image, Engine: "sftp", State: c.State, Status: c.Status,
				HostPort: c.HostPort, ContainerPort: c.ContainerPort})
		}
		return nil
	})
	if out == nil {
		out = []ContainerInfo{}
	}
	return out, err
}

func (a *App) ImportSftpContainer(containerName string) (SftpService, error) {
	var out SftpService
	err := a.sftpDo(time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		s, err := m.Import(ctx, containerName)
		a.logOp(0, "import-sftp", containerName, err)
		if err != nil {
			return err
		}
		out = toSftpService(s)
		return nil
	})
	return out, err
}

func (a *App) DeleteSftpService(id int64, removeContainer bool, removeData bool) error {
	return a.sftpDo(5*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		return a.opErr(id, "delete-sftp", "", m.Delete(ctx, id, removeContainer, removeData))
	})
}

func (a *App) StartSftpService(id int64) error {
	return a.sftpDo(2*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		return a.opErr(id, "start-sftp", "", m.Start(ctx, id))
	})
}

func (a *App) StopSftpService(id int64) error {
	return a.sftpDo(2*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		return a.opErr(id, "stop-sftp", "", m.Stop(ctx, id))
	})
}

func (a *App) RestartSftpService(id int64) error {
	return a.sftpDo(2*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		return a.opErr(id, "restart-sftp", "", m.Restart(ctx, id))
	})
}

func (a *App) SftpServiceStatus(id int64) (ServerStatus, error) {
	var out ServerStatus
	err := a.sftpDo(15*time.Second, func(ctx context.Context, m *sftp.Manager) error {
		s, msg, err := m.Status(ctx, id)
		out = ServerStatus{State: s, Message: msg}
		return err
	})
	return out, err
}

func (a *App) SftpServiceLogs(id int64, tail int) (string, error) {
	var out string
	err := a.sftpDo(30*time.Second, func(ctx context.Context, m *sftp.Manager) error {
		var err error
		out, err = m.Logs(ctx, id, tail)
		return err
	})
	return out, err
}

func (a *App) GetSftpHostFingerprints(id int64) ([]string, error) {
	m, err := a.sftpManager()
	if err != nil {
		return nil, err
	}
	_, fps, err := m.HostKeys(id)
	return fps, friendlyDBErr(err)
}

// GetSftpHostKeys devuelve líneas listas para known_hosts: "[localhost]:2224 ssh-ed25519 AAAA...".
func (a *App) GetSftpHostKeys(id int64) ([]string, error) {
	m, err := a.sftpManager()
	if err != nil {
		return nil, err
	}
	kh, _, err := m.HostKeys(id)
	return kh, friendlyDBErr(err)
}

func (a *App) ListSftpUsers(serviceId int64) ([]SftpUser, error) {
	m, err := a.sftpManager()
	if err != nil {
		return nil, err
	}
	list, err := m.ListUsers(serviceId)
	if err != nil {
		return nil, friendlyDBErr(err)
	}
	out := make([]SftpUser, 0, len(list))
	for _, v := range list {
		out = append(out, toSftpUser(v))
	}
	return out, nil
}

func (a *App) CreateSftpUser(serviceId int64, req CreateSftpUserRequest) (SftpUserResult, error) {
	var out SftpUserResult
	err := a.sftpDo(5*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		ur := sftp.UserRequest{Name: req.Name, Password: req.Password, UploadDir: req.UploadDir}
		if req.Key != nil {
			k := toKeyReq(*req.Key)
			ur.Key = &k
		}
		r, err := m.CreateUser(ctx, serviceId, ur)
		a.logOp(serviceId, "create-sftp-user", req.Name, err)
		if err != nil {
			return err
		}
		out = toSftpResult(r)
		return nil
	})
	return out, err
}

func (a *App) DeleteSftpUser(serviceId int64, name string, deleteFiles bool) error {
	return a.sftpDo(5*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		return a.opErr(serviceId, "delete-sftp-user", name, m.DeleteUser(ctx, serviceId, name, deleteFiles))
	})
}

func (a *App) SetSftpUserPassword(serviceId int64, name string, password string) error {
	return a.sftpDo(5*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		return a.opErr(serviceId, "set-sftp-password", name, m.SetPassword(ctx, serviceId, name, password))
	})
}

func (a *App) AddSftpUserKey(serviceId int64, name string, req SftpKeyRequest) (SftpUserResult, error) {
	var out SftpUserResult
	err := a.sftpDo(5*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		r, err := m.AddKey(ctx, serviceId, name, toKeyReq(req))
		a.logOp(serviceId, "add-sftp-key", name, err)
		if err != nil {
			return err
		}
		out = toSftpResult(r)
		return nil
	})
	return out, err
}

func (a *App) RemoveSftpUserKey(serviceId int64, name string, keyId int64) error {
	return a.sftpDo(5*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		return a.opErr(serviceId, "remove-sftp-key", name, m.RemoveKey(ctx, serviceId, name, keyId))
	})
}

func (a *App) TestSftpLogin(req SftpLoginTest) (SftpLoginResult, error) {
	m, err := a.sftpManager()
	if err != nil {
		return SftpLoginResult{}, err
	}
	st, err := a.db()
	if err != nil {
		return SftpLoginResult{}, err
	}
	svc, err := st.GetSftpService(req.ServiceID)
	if err != nil {
		return SftpLoginResult{}, errors.New("el servicio SFTP no existe")
	}
	host := svc.Host
	if host == "" {
		host = "localhost"
	}
	lr := sftp.LoginRequest{Host: host, Port: svc.Port, User: strings.TrimSpace(req.User), Password: req.Password,
		PrivateKeyPath: req.PrivateKeyPath, Passphrase: req.Passphrase}
	if svc.Mode == "managed" {
		if _, fps, err := m.HostKeys(svc.ID); err == nil {
			for _, f := range fps {
				if i := strings.Index(f, " "); i > 0 {
					lr.KnownFingerprints = append(lr.KnownFingerprints, f[i+1:])
				}
			}
		}
	}
	r := sftp.TestLogin(lr)
	if r.Entries == nil {
		r.Entries = []string{}
	}
	return SftpLoginResult{OK: r.OK, Message: r.Message, HostFingerprint: r.HostFingerprint, Entries: r.Entries}, nil
}

// ---------- Certificados ----------

func summaries(in []certs.Summary) []CertSummary {
	out := make([]CertSummary, 0, len(in))
	for _, s := range in {
		sans := s.SANs
		if sans == nil {
			sans = []string{}
		}
		out = append(out, CertSummary{Subject: s.Subject, Issuer: s.Issuer, CommonName: s.CommonName, SANs: sans, NotBefore: fmtTime(s.NotBefore),
			NotAfter: fmtTime(s.NotAfter), Serial: s.Serial, IsCA: s.IsCA, SelfSigned: s.SelfSigned, SHA256: s.SHA256})
	}
	return out
}

func toVerification(v certs.Verification) Pkcs12Verification {
	checks := make([]Pkcs12Check, 0, len(v.Checks))
	for _, c := range v.Checks {
		checks = append(checks, Pkcs12Check{Name: c.Name, OK: c.OK, Detail: c.Detail, Severity: c.Severity})
	}
	return Pkcs12Verification{OK: v.OK, EntryType: v.EntryType, Alias: v.Alias, ChainLength: v.ChainLength, Chain: summaries(v.Chain),
		KeyMatchesCert: v.KeyMatchesCert, Checks: checks, KeytoolAvailable: v.KeytoolAvailable, KeytoolOK: v.KeytoolOK, KeytoolOutput: v.KeytoolOutput}
}

func (a *App) InspectCertFile(path string, passphrase string) (CertFileInfo, error) {
	fi, err := certs.Inspect(path, passphrase)
	if err != nil {
		return CertFileInfo{}, err
	}
	return CertFileInfo{Kind: fi.Kind, KeyType: fi.KeyType, Encrypted: fi.Encrypted, Certs: summaries(fi.Certs)}, nil
}

func (a *App) BuildPkcs12(req Pkcs12Request) (Pkcs12Result, error) {
	st, err := a.db()
	if err != nil {
		return Pkcs12Result{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	res, err := certs.Build(ctx, certs.BuildRequest{PrivateKeyPath: req.PrivateKeyPath, KeyPassphrase: req.KeyPassphrase, CertPath: req.CertPath,
		IntermediatePath: req.IntermediatePath, RootPath: req.RootPath, Alias: req.Alias, Password: req.Password, OutputPath: req.OutputPath, Format: req.Format, Overwrite: req.Overwrite})
	if err != nil {
		a.logOp(0, "build-pkcs12", req.OutputPath, err)
		return Pkcs12Result{}, err
	}
	alias := strings.TrimSpace(req.Alias)
	if alias == "" {
		alias = "tomcat"
	}
	st.AddCertHistory(store.CertHistory{Alias: alias, CommonName: res.Leaf.CommonName, NotAfter: fmtTime(res.Leaf.NotAfter), OutputPath: res.OutputPath,
		ChainLength: res.Verification.ChainLength, Verified: res.Verification.OK}) // nunca se guardan contraseñas
	a.logOp(0, "build-pkcs12", res.OutputPath, nil)
	w := res.Warnings
	if w == nil {
		w = []string{}
	}
	return Pkcs12Result{OutputPath: res.OutputPath, Verification: toVerification(res.Verification), Warnings: w}, nil
}

func (a *App) VerifyPkcs12(path string, password string, expectedAlias string) (Pkcs12Verification, error) {
	ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
	defer cancel()
	v, err := certs.Verify(ctx, path, password, strings.TrimSpace(expectedAlias))
	if err != nil {
		return Pkcs12Verification{}, err
	}
	return toVerification(v), nil
}

func (a *App) ListCertificateHistory(limit int) ([]CertHistory, error) {
	st, err := a.db()
	if err != nil {
		return nil, err
	}
	list, err := st.ListCertHistory(limit)
	if err != nil {
		return nil, err
	}
	out := make([]CertHistory, 0, len(list))
	for _, h := range list {
		out = append(out, CertHistory{ID: h.ID, Alias: h.Alias, CommonName: h.CommonName, NotAfter: h.NotAfter, OutputPath: h.OutputPath,
			ChainLength: h.ChainLength, Verified: h.Verified, CreatedAt: fmtTime(h.CreatedAt)})
	}
	return out, nil
}

// SaveFile abre el diálogo nativo "guardar como" y recuerda la última carpeta. Devuelve "" si se cancela.
func (a *App) SaveFile(title string, defaultName string, filters []FileFilter) (string, error) {
	opts := wruntime.SaveDialogOptions{Title: title, DefaultFilename: defaultName, DefaultDirectory: a.pickerDir(settingLastDir)}
	for _, f := range filters {
		opts.Filters = append(opts.Filters, wruntime.FileFilter{DisplayName: f.DisplayName, Pattern: f.Pattern})
	}
	p, err := wruntime.SaveFileDialog(a.ctx, opts)
	if err != nil {
		return "", err
	}
	if p != "" {
		a.remember(settingLastDir, filepath.Dir(p))
	}
	return p, nil
}

// ---------- SFTP: carpetas compartidas ----------

func (a *App) ListSftpFolders(serviceId int64) ([]SftpFolder, error) {
	m, err := a.sftpManager()
	if err != nil {
		return nil, err
	}
	list, err := m.ListFolders(serviceId)
	if err != nil {
		return nil, friendlyDBErr(err)
	}
	out := make([]SftpFolder, 0, len(list))
	for _, v := range list {
		f := SftpFolder{ID: v.Folder.ID, Name: v.Folder.Name, Access: []SftpFolderAccess{}, CreatedAt: fmtTime(v.Folder.CreatedAt)}
		for _, ac := range v.Access {
			f.Access = append(f.Access, SftpFolderAccess{User: ac.UserName, Level: ac.Level})
		}
		out = append(out, f)
	}
	return out, nil
}

func (a *App) CreateSftpFolder(serviceId int64, name string) error {
	return a.sftpDo(time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		return a.opErr(serviceId, "create-sftp-folder", name, m.CreateFolder(ctx, serviceId, name))
	})
}

func (a *App) DeleteSftpFolder(serviceId int64, name string, deleteFiles bool) error {
	return a.sftpDo(3*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		return a.opErr(serviceId, "delete-sftp-folder", name, m.DeleteFolder(ctx, serviceId, name, deleteFiles))
	})
}

func (a *App) SetSftpFolderAccess(serviceId int64, folder string, user string, level string) error {
	return a.sftpDo(3*time.Minute, func(ctx context.Context, m *sftp.Manager) error {
		return a.opErr(serviceId, "set-sftp-folder-access", folder+":"+user+":"+level, m.SetFolderAccess(ctx, serviceId, folder, user, level))
	})
}

// OpenSftpFolder abre en el explorador del sistema el almacenamiento (kind: service|user|folder) y devuelve la ruta.
func (a *App) OpenSftpFolder(serviceId int64, kind string, name string) (string, error) {
	m, err := a.sftpManager()
	if err != nil {
		return "", err
	}
	p, err := m.OpenFolder(serviceId, kind, name)
	if err != nil {
		return "", friendlyDBErr(err)
	}
	return p, nil
}

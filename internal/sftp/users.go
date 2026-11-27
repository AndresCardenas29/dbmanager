package sftp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/store"
)

var errExternal = errors.New("este servicio fue importado: sus usuarios los gestiona su propia configuración y no se pueden editar desde la app")

// KeyRequest describe cómo obtener la llave de un usuario.
type KeyRequest struct {
	Mode       string // generate | paste
	KeyType    string // ed25519 | rsa4096
	Passphrase string
	Comment    string
	OutputDir  string
	ExportPpk  bool
	PublicKey  string
}

// UserRequest crea un usuario.
type UserRequest struct {
	Name      string
	Password  string
	UploadDir string
	Key       *KeyRequest
}

// UserView es un usuario con sus llaves.
type UserView struct {
	User store.SftpUser
	Keys []store.SftpKey
}

// UserResult es el resultado de crear usuario / agregar llave.
type UserResult struct {
	View            UserView
	PrivateKeyPath  string
	PublicKeyPath   string
	PpkPath         string
	ConnectionHints []string
}

func (m *Manager) managed(id int64) (store.SftpService, error) {
	svc, err := m.Store.GetSftpService(id)
	if err != nil {
		return svc, errors.New("el servicio SFTP no existe")
	}
	if svc.Mode != "managed" {
		return svc, errExternal
	}
	return svc, nil
}

func (m *Manager) view(u store.SftpUser) (UserView, error) {
	ks, err := m.Store.ListSftpKeys(u.ID)
	return UserView{User: u, Keys: ks}, err
}

// ListUsers lista los usuarios gestionados. En servicios importados devuelve lista vacía.
func (m *Manager) ListUsers(serviceID int64) ([]UserView, error) {
	svc, err := m.Store.GetSftpService(serviceID)
	if err != nil {
		return nil, errors.New("el servicio SFTP no existe")
	}
	out := []UserView{}
	if svc.Mode != "managed" {
		return out, nil
	}
	us, err := m.Store.ListSftpUsers(serviceID)
	if err != nil {
		return nil, err
	}
	for _, u := range us {
		v, err := m.view(u)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// registerKey genera o valida la llave y la guarda (solo la pública). Devuelve rutas escritas.
func (m *Manager) registerKey(u store.SftpUser, kr KeyRequest) (store.SftpKey, string, string, string, error) {
	if err := ValidComment(kr.Comment); err != nil {
		return store.SftpKey{}, "", "", "", err
	}
	switch kr.Mode {
	case "paste":
		canon, fp, typ, cmt, err := ParsePublicKey(kr.PublicKey)
		if err != nil {
			return store.SftpKey{}, "", "", "", err
		}
		if kr.Comment != "" && cmt == "" {
			cmt = kr.Comment
		}
		ks, _ := m.Store.ListSftpKeys(u.ID)
		for _, k := range ks {
			if k.Fingerprint == fp {
				return store.SftpKey{}, "", "", "", errors.New("esa llave ya está registrada para el usuario")
			}
		}
		k, err := m.Store.AddSftpKey(store.SftpKey{UserID: u.ID, Comment: cmt, KeyType: typ, Fingerprint: fp, PublicKey: canon})
		return k, "", "", "", err
	case "generate", "":
		if strings.TrimSpace(kr.OutputDir) == "" {
			return store.SftpKey{}, "", "", "", errors.New("elija la carpeta donde guardar la llave privada")
		}
		comment := kr.Comment
		if comment == "" {
			comment = u.Name + "@dbmanager"
		}
		g, err := Generate(kr.KeyType, kr.Passphrase, comment)
		if err != nil {
			return store.SftpKey{}, "", "", "", err
		}
		if err := os.MkdirAll(kr.OutputDir, 0o700); err != nil {
			return store.SftpKey{}, "", "", "", fmt.Errorf("no se pudo crear la carpeta de destino: %w", err)
		}
		base := u.Name + "_id_ed25519"
		if kr.KeyType == KeyRSA4096 {
			base = u.Name + "_id_rsa"
		}
		priv := filepath.Join(kr.OutputDir, base)
		for i := 2; fileExists(priv); i++ { // no se sobrescribe una llave existente
			priv = filepath.Join(kr.OutputDir, fmt.Sprintf("%s_%d", base, i))
		}
		if err := os.WriteFile(priv, g.PrivatePEM, 0o600); err != nil {
			return store.SftpKey{}, "", "", "", fmt.Errorf("no se pudo guardar la llave privada: %w", err)
		}
		restrictFile(priv)
		pubPath := priv + ".pub"
		if err := os.WriteFile(pubPath, []byte(g.PublicLine+"\n"), 0o644); err != nil {
			return store.SftpKey{}, "", "", "", err
		}
		ppkPath := ""
		if kr.ExportPpk {
			ppk, err := MarshalPPK(g.Private, comment, kr.Passphrase)
			if err != nil {
				return store.SftpKey{}, "", "", "", err
			}
			ppkPath = priv + ".ppk"
			if err := os.WriteFile(ppkPath, []byte(ppk), 0o600); err != nil {
				return store.SftpKey{}, "", "", "", err
			}
			restrictFile(ppkPath)
		}
		k, err := m.Store.AddSftpKey(store.SftpKey{UserID: u.ID, Comment: comment, KeyType: g.Type, Fingerprint: g.Fingerprint, PublicKey: g.PublicLine})
		return k, priv, pubPath, ppkPath, err
	}
	return store.SftpKey{}, "", "", "", errors.New("modo de llave inválido: use generate o paste")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func (m *Manager) hints(svc store.SftpService, user, priv string) []string {
	host := svc.Host
	if host == "" {
		host = "localhost"
	}
	h := []string{fmt.Sprintf("Servidor: %s  Puerto: %d  Usuario: %s", host, svc.Port, user)}
	if priv != "" {
		h = append(h, fmt.Sprintf("sftp -P %d -i \"%s\" %s@%s", svc.Port, priv, user, host))
	} else {
		h = append(h, fmt.Sprintf("sftp -P %d %s@%s", svc.Port, user, host))
	}
	return append(h, "Los archivos se suben a la carpeta de carga del usuario (dentro de su home enjaulado).")
}

// CreateUser crea un usuario (contraseña y/o llave), regenera la configuración y recrea el contenedor.
func (m *Manager) CreateUser(ctx context.Context, serviceID int64, r UserRequest) (UserResult, error) {
	svc, err := m.managed(serviceID)
	if err != nil {
		return UserResult{}, err
	}
	r.Name = strings.TrimSpace(r.Name)
	if err := ValidUserName(r.Name); err != nil {
		return UserResult{}, err
	}
	if r.UploadDir == "" {
		r.UploadDir = uploadDirDef
	}
	if err := ValidUploadDir(r.UploadDir); err != nil {
		return UserResult{}, err
	}
	folders, err := m.Store.ListSftpFolders(serviceID)
	if err != nil {
		return UserResult{}, err
	}
	if err := checkUserVsFolders(r.Name, r.UploadDir, folders); err != nil {
		return UserResult{}, err
	}
	if r.Password == "" && r.Key == nil {
		return UserResult{}, errors.New("indique una contraseña, una llave, o ambas (sin ninguna el usuario no podría entrar)")
	}
	if _, err := m.Store.GetSftpUser(serviceID, r.Name, 0); err == nil {
		return UserResult{}, fmt.Errorf("el usuario %q ya existe", r.Name)
	}
	hash := ""
	if r.Password != "" {
		if hash, err = HashPassword(r.Password); err != nil {
			return UserResult{}, err
		}
	}
	u, err := m.Store.CreateSftpUser(store.SftpUser{ServiceID: serviceID, Name: r.Name, PasswordHash: hash, UploadDir: r.UploadDir})
	if err != nil {
		return UserResult{}, err
	}
	var res UserResult
	if r.Key != nil {
		k, priv, pub, ppk, err := m.registerKey(u, *r.Key)
		if err != nil {
			m.Store.DeleteSftpUser(u.ID)
			return UserResult{}, err
		}
		_ = k
		res.PrivateKeyPath, res.PublicKeyPath, res.PpkPath = priv, pub, ppk
	}
	if err := m.recreate(ctx, serviceID); err != nil {
		return UserResult{}, err
	}
	if res.View, err = m.view(u); err != nil {
		return UserResult{}, err
	}
	res.ConnectionHints = m.hints(svc, u.Name, res.PrivateKeyPath)
	return res, nil
}

// DeleteUser elimina el usuario y recrea el contenedor. deleteFiles borra su volumen (en bind no se borran carpetas).
func (m *Manager) DeleteUser(ctx context.Context, serviceID int64, name string, deleteFiles bool) error {
	svc, err := m.managed(serviceID)
	if err != nil {
		return err
	}
	u, err := m.Store.GetSftpUser(serviceID, name, 0)
	if err != nil {
		return fmt.Errorf("el usuario %q no existe", name)
	}
	if err := m.Store.DeleteSftpUser(u.ID); err != nil {
		return err
	}
	if err := m.recreate(ctx, serviceID); err != nil {
		return err
	}
	if deleteFiles && svc.DataMode == "volume" {
		if cli, err := m.Docker(); err == nil {
			if err := cli.RemoveVolume(ctx, volumeName(svc, name)); err != nil && !dockerx.IsNotFound(err) {
				return fmt.Errorf("el usuario se eliminó pero no se pudo borrar su volumen: %w", err)
			}
		}
	}
	return nil
}

// SetPassword cambia la contraseña ("" = quitar; solo permitido si el usuario tiene al menos una llave).
func (m *Manager) SetPassword(ctx context.Context, serviceID int64, name, password string) error {
	if _, err := m.managed(serviceID); err != nil {
		return err
	}
	u, err := m.Store.GetSftpUser(serviceID, name, 0)
	if err != nil {
		return fmt.Errorf("el usuario %q no existe", name)
	}
	hash := ""
	if password == "" {
		ks, _ := m.Store.ListSftpKeys(u.ID)
		if len(ks) == 0 {
			return errors.New("no se puede quitar la contraseña: el usuario no tiene ninguna llave y quedaría sin acceso")
		}
	} else if hash, err = HashPassword(password); err != nil {
		return err
	}
	if err := m.Store.SetSftpUserPasswordHash(u.ID, hash); err != nil {
		return err
	}
	return m.recreate(ctx, serviceID)
}

// AddKey agrega una llave (generada o pegada) a un usuario existente.
func (m *Manager) AddKey(ctx context.Context, serviceID int64, name string, kr KeyRequest) (UserResult, error) {
	svc, err := m.managed(serviceID)
	if err != nil {
		return UserResult{}, err
	}
	u, err := m.Store.GetSftpUser(serviceID, name, 0)
	if err != nil {
		return UserResult{}, fmt.Errorf("el usuario %q no existe", name)
	}
	_, priv, pub, ppk, err := m.registerKey(u, kr)
	if err != nil {
		return UserResult{}, err
	}
	if err := m.recreate(ctx, serviceID); err != nil {
		return UserResult{}, err
	}
	res := UserResult{PrivateKeyPath: priv, PublicKeyPath: pub, PpkPath: ppk, ConnectionHints: m.hints(svc, name, priv)}
	res.View, err = m.view(u)
	return res, err
}

// RemoveKey quita una llave; se rechaza si es la última y el usuario no tiene contraseña.
func (m *Manager) RemoveKey(ctx context.Context, serviceID int64, name string, keyID int64) error {
	if _, err := m.managed(serviceID); err != nil {
		return err
	}
	u, err := m.Store.GetSftpUser(serviceID, name, 0)
	if err != nil {
		return fmt.Errorf("el usuario %q no existe", name)
	}
	ks, err := m.Store.ListSftpKeys(u.ID)
	if err != nil {
		return err
	}
	found := false
	for _, k := range ks {
		if k.ID == keyID {
			found = true
		}
	}
	if !found {
		return errors.New("la llave no existe")
	}
	if len(ks) == 1 && u.PasswordHash == "" {
		return errors.New("no se puede quitar la última llave: el usuario no tiene contraseña y quedaría sin acceso")
	}
	if err := m.Store.DeleteSftpKey(u.ID, keyID); err != nil {
		return err
	}
	return m.recreate(ctx, serviceID)
}

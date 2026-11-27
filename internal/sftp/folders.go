package sftp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/store"
)

// SharedMount describe una carpeta compartida montada en el directorio de un usuario.
type SharedMount struct {
	User     string
	Folder   string
	ReadOnly bool
}

// FolderView es una carpeta compartida con sus permisos.
type FolderView struct {
	Folder store.SftpFolder
	Access []store.SftpFolderAccess
}

// SharedVolumeName es el volumen de una carpeta compartida (modo volume).
func SharedVolumeName(dataPath, folder string) string { return dataPath + "-shared-" + folder }

// SharedHostDir es la carpeta del host de una carpeta compartida (modo bind).
func SharedHostDir(dataPath, folder string) string { return filepath.Join(dataPath, "_shared", folder) }

// SharedTarget es el punto de montaje dentro del contenedor.
func SharedTarget(user, folder string) string { return "/home/" + user + "/" + folder }

// BuildSharedMounts es una función pura que calcula los montajes de las carpetas compartidas.
func BuildSharedMounts(dataMode, dataPath string, shared []SharedMount) ([]dockerx.Bind, []dockerx.VolumeMount) {
	var binds []dockerx.Bind
	var vols []dockerx.VolumeMount
	for _, s := range shared {
		t := SharedTarget(s.User, s.Folder)
		if dataMode == "bind" {
			binds = append(binds, dockerx.Bind{Source: SharedHostDir(dataPath, s.Folder), Target: t, ReadOnly: s.ReadOnly})
		} else {
			vols = append(vols, dockerx.VolumeMount{Name: SharedVolumeName(dataPath, s.Folder), Target: t, ReadOnly: s.ReadOnly})
		}
	}
	return binds, vols
}

// ValidFolderName valida el nombre de una carpeta compartida frente a las carpetas de carga de los usuarios,
// ".ssh", las carpetas existentes y los nombres de usuario que colisionarían con su volumen.
func ValidFolderName(name string, users []store.SftpUser, folders []store.SftpFolder) error {
	if err := ValidUploadDir(name); err != nil {
		return errors.New("nombre de carpeta inválido: un solo nombre sin barras (letras, números, _ . -)")
	}
	if strings.EqualFold(name, ".ssh") {
		return errors.New("el nombre .ssh está reservado")
	}
	for _, u := range users {
		if strings.EqualFold(u.UploadDir, name) {
			return fmt.Errorf("el nombre %q ya lo usa la carpeta de carga del usuario %q", name, u.Name)
		}
		if strings.EqualFold(u.Name, "shared-"+name) {
			return fmt.Errorf("el nombre %q colisionaría con el almacenamiento del usuario %q; elija otro", name, u.Name)
		}
	}
	for _, f := range folders {
		if strings.EqualFold(f.Name, name) {
			return fmt.Errorf("ya existe una carpeta compartida llamada %q", name)
		}
	}
	return nil
}

// checkUserVsFolders valida que un usuario nuevo no colisione con carpetas compartidas.
func checkUserVsFolders(name, uploadDir string, folders []store.SftpFolder) error {
	if name == "_shared" {
		return errors.New("el nombre de usuario _shared está reservado")
	}
	for _, f := range folders {
		if strings.EqualFold(f.Name, uploadDir) {
			return fmt.Errorf("la carpeta de carga %q coincide con la carpeta compartida %q; elija otro nombre", uploadDir, f.Name)
		}
		if strings.EqualFold(name, "shared-"+f.Name) {
			return fmt.Errorf("el nombre de usuario %q colisionaría con el almacenamiento de la carpeta compartida %q", name, f.Name)
		}
	}
	return nil
}

// sharedMounts calcula, a partir de la base de datos, los montajes compartidos del servicio.
func (m *Manager) sharedMounts(serviceID int64) ([]SharedMount, error) {
	fs, err := m.Store.ListSftpFolders(serviceID)
	if err != nil {
		return nil, err
	}
	acc, err := m.Store.ListSftpFolderAccess(serviceID)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, f := range fs {
		names[f.ID] = f.Name
	}
	var out []SharedMount
	for _, a := range acc {
		if fn, ok := names[a.FolderID]; ok {
			out = append(out, SharedMount{User: a.UserName, Folder: fn, ReadOnly: a.Level == "read"})
		}
	}
	return out, nil
}

// ListFolders lista las carpetas compartidas con su acceso. Vacío en servicios importados.
func (m *Manager) ListFolders(serviceID int64) ([]FolderView, error) {
	svc, err := m.Store.GetSftpService(serviceID)
	if err != nil {
		return nil, errors.New("el servicio SFTP no existe")
	}
	out := []FolderView{}
	if svc.Mode != "managed" {
		return out, nil
	}
	fs, err := m.Store.ListSftpFolders(serviceID)
	if err != nil {
		return nil, err
	}
	acc, err := m.Store.ListSftpFolderAccess(serviceID)
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		v := FolderView{Folder: f, Access: []store.SftpFolderAccess{}}
		for _, a := range acc {
			if a.FolderID == f.ID {
				v.Access = append(v.Access, a)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// CreateFolder crea una carpeta compartida (sin acceso para nadie; no recrea el contenedor).
func (m *Manager) CreateFolder(ctx context.Context, serviceID int64, name string) error {
	if _, err := m.managed(serviceID); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	us, err := m.Store.ListSftpUsers(serviceID)
	if err != nil {
		return err
	}
	fs, err := m.Store.ListSftpFolders(serviceID)
	if err != nil {
		return err
	}
	if err := ValidFolderName(name, us, fs); err != nil {
		return err
	}
	_, err = m.Store.CreateSftpFolder(serviceID, name)
	return err
}

// DeleteFolder elimina la carpeta; deleteFiles borra su volumen (en bind nunca se borran carpetas del host).
func (m *Manager) DeleteFolder(ctx context.Context, serviceID int64, name string, deleteFiles bool) error {
	svc, err := m.managed(serviceID)
	if err != nil {
		return err
	}
	f, err := m.Store.GetSftpFolder(serviceID, name)
	if err != nil {
		return fmt.Errorf("la carpeta compartida %q no existe", name)
	}
	acc, err := m.Store.ListSftpFolderAccess(serviceID)
	if err != nil {
		return err
	}
	had := false
	for _, a := range acc {
		if a.FolderID == f.ID {
			had = true
		}
	}
	if err := m.Store.DeleteSftpFolder(f.ID); err != nil {
		return err
	}
	if had {
		if err := m.recreate(ctx, serviceID); err != nil {
			return err
		}
	}
	if deleteFiles && svc.DataMode == "volume" {
		if cli, err := m.Docker(); err == nil {
			if err := cli.RemoveVolume(ctx, SharedVolumeName(svc.DataPath, name)); err != nil && !dockerx.IsNotFound(err) {
				return fmt.Errorf("la carpeta se eliminó pero no se pudo borrar su volumen: %w", err)
			}
		}
	}
	return nil
}

// SetFolderAccess fija el nivel (none|read|readwrite) de un usuario sobre una carpeta y recrea el contenedor.
func (m *Manager) SetFolderAccess(ctx context.Context, serviceID int64, folder, user, level string) error {
	if _, err := m.managed(serviceID); err != nil {
		return err
	}
	switch level {
	case "none", "read", "readwrite":
	default:
		return errors.New("nivel de acceso inválido: use none, read o readwrite")
	}
	f, err := m.Store.GetSftpFolder(serviceID, folder)
	if err != nil {
		return fmt.Errorf("la carpeta compartida %q no existe", folder)
	}
	u, err := m.Store.GetSftpUser(serviceID, user, 0)
	if err != nil {
		return fmt.Errorf("el usuario %q no existe", user)
	}
	if err := m.Store.SetSftpFolderAccess(f.ID, u.ID, level); err != nil {
		return err
	}
	return m.recreate(ctx, serviceID)
}

// ---------- abrir en el explorador ----------

var errVolumeOpen = errors.New("los archivos de este servicio están dentro de un volumen de Docker y no se pueden abrir desde el explorador del equipo. " +
	"Para acceder a ellos directamente, cree el servicio con una carpeta del equipo (modo bind)")

// volumeBases devuelve las carpetas candidatas donde Docker guarda los volúmenes, en orden de preferencia.
func volumeBases() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{
			`\\wsl.localhost\docker-desktop\mnt\docker-desktop-disk\data\docker\volumes`,
			`\\wsl$\docker-desktop\mnt\docker-desktop-disk\data\docker\volumes`,
			`\\wsl$\docker-desktop-data\data\docker\volumes`,
			`\\wsl.localhost\docker-desktop-data\data\docker\volumes`,
		}
	case "linux":
		return []string{"/var/lib/docker/volumes"}
	}
	return nil
}

// resolveVolumeDir busca la carpeta de un volumen (vol != "") o la carpeta padre de volúmenes (vol == "").
func resolveVolumeDir(vol string, bases []string) (string, bool) {
	for _, b := range bases {
		p := b
		if vol != "" {
			p = filepath.Join(b, vol, "_data")
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// OpenFolder abre en el explorador del sistema el almacenamiento de un servicio ("service"), de un usuario
// ("user") o de una carpeta compartida ("folder"). Devuelve la ruta abierta.
func (m *Manager) OpenFolder(serviceID int64, kind, name string) (string, error) {
	svc, err := m.Store.GetSftpService(serviceID)
	if err != nil {
		return "", errors.New("el servicio SFTP no existe")
	}
	path, err := m.storagePath(svc, kind, name, volumeBases())
	if err != nil {
		return "", err
	}
	if err := launchExplorer(path); err != nil {
		return "", fmt.Errorf("no se pudo abrir el explorador: %w", err)
	}
	return path, nil
}

func (m *Manager) storagePath(svc store.SftpService, kind, name string, bases []string) (string, error) {
	if svc.Mode != "managed" {
		if kind != "service" {
			return "", errors.New("este servicio fue importado: solo se puede abrir su carpeta principal")
		}
		if svc.DataMode != "bind" || svc.DataPath == "" {
			return "", errors.New("este servicio fue importado y no tiene una carpeta del equipo conocida; no se puede abrir desde la app")
		}
		return ensureDir(svc.DataPath)
	}
	var bindDir, vol string
	switch kind {
	case "service":
		bindDir = svc.DataPath
	case "user":
		u, err := m.Store.GetSftpUser(svc.ID, name, 0)
		if err != nil {
			return "", fmt.Errorf("el usuario %q no existe", name)
		}
		bindDir, vol = filepath.Join(svc.DataPath, u.Name), volumeName(svc, u.Name)
	case "folder":
		f, err := m.Store.GetSftpFolder(svc.ID, name)
		if err != nil {
			return "", fmt.Errorf("la carpeta compartida %q no existe", name)
		}
		bindDir, vol = SharedHostDir(svc.DataPath, f.Name), SharedVolumeName(svc.DataPath, f.Name)
	default:
		return "", errors.New("tipo de carpeta inválido: use service, user o folder")
	}
	if svc.DataMode == "bind" {
		return ensureDir(bindDir)
	}
	if p, ok := resolveVolumeDir(vol, bases); ok {
		return p, nil
	}
	return "", errVolumeOpen
}

func ensureDir(p string) (string, error) {
	if err := os.MkdirAll(p, 0o777); err != nil {
		return "", fmt.Errorf("no se pudo crear la carpeta %s: %w", p, err)
	}
	return p, nil
}

func launchExplorer(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// explorer.exe devuelve código 1 aunque abra bien: se ignora el resultado.
	go func() { _ = cmd.Wait() }()
	return nil
}

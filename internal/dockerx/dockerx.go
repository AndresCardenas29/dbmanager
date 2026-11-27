// Package dockerx es un wrapper agnóstico al motor sobre el SDK oficial de Docker.
package dockerx

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	LabelManaged = "dbmanager.managed"
	LabelEngine  = "dbmanager.engine"
)

// Client envuelve el cliente del SDK.
type Client struct {
	cli *client.Client
}

// New crea un cliente desde el entorno (DOCKER_HOST, etc.) con negociación de versión de API.
func New() (*Client, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if h := ResolveHost(); h != "" {
		opts = append(opts, client.WithHost(h))
	}
	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Client{cli: c}, nil
}

// Close libera el cliente.
func (c *Client) Close() error { return c.cli.Close() }

// Raw expone el cliente subyacente para usos avanzados.
func (c *Client) Raw() *client.Client { return c.cli }

// VersionInfo resume la versión del daemon.
type VersionInfo struct {
	Version    string
	APIVersion string
	OS         string
	Arch       string
}

// Ping comprueba que el daemon responde.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// Version devuelve la versión del daemon.
func (c *Client) Version(ctx context.Context) (VersionInfo, error) {
	v, err := c.cli.ServerVersion(ctx)
	if err != nil {
		return VersionInfo{}, err
	}
	return VersionInfo{Version: v.Version, APIVersion: v.APIVersion, OS: v.Os, Arch: v.Arch}, nil
}

// ---------- Imágenes ----------

// ImageExists indica si la imagen está disponible localmente.
func (c *Client) ImageExists(ctx context.Context, ref string) (bool, error) {
	_, err := c.cli.ImageInspect(ctx, ref)
	if err == nil {
		return true, nil
	}
	if client.IsErrNotFound(err) {
		return false, nil
	}
	return false, err
}

// PullProgress es un evento de progreso de descarga.
type PullProgress struct {
	Status  string
	ID      string
	Current int64
	Total   int64
	Percent int // 0-100 agregado sobre todas las capas conocidas
}

// PullImage descarga la imagen. platform puede ser "" o "linux/amd64".
func (c *Client) PullImage(ctx context.Context, ref, platform string, progress func(PullProgress)) error {
	rc, err := c.cli.ImagePull(ctx, ref, image.PullOptions{Platform: platform})
	if err != nil {
		return err
	}
	defer rc.Close()

	type layer struct{ cur, total int64 }
	layers := map[string]*layer{}
	dec := json.NewDecoder(rc)
	for {
		var m struct {
			Status         string `json:"status"`
			ID             string `json:"id"`
			Error          string `json:"error"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if m.Error != "" {
			return errors.New(m.Error)
		}
		if m.ID != "" && m.ProgressDetail.Total > 0 {
			l := layers[m.ID]
			if l == nil {
				l = &layer{}
				layers[m.ID] = l
			}
			l.cur, l.total = m.ProgressDetail.Current, m.ProgressDetail.Total
		}
		if m.Status == "Pull complete" || m.Status == "Download complete" {
			if l := layers[m.ID]; l != nil {
				l.cur = l.total
			}
		}
		if progress != nil {
			var cur, tot int64
			for _, l := range layers {
				cur += l.cur
				tot += l.total
			}
			pct := 0
			if tot > 0 {
				pct = int(cur * 100 / tot)
			}
			progress(PullProgress{Status: m.Status, ID: m.ID, Current: m.ProgressDetail.Current,
				Total: m.ProgressDetail.Total, Percent: pct})
		}
	}
}

// ---------- Contenedores ----------

// CreateOptions describe un contenedor a crear.
type CreateOptions struct {
	Name          string
	Image         string
	Engine        string   // se guarda en la etiqueta dbmanager.engine
	Env           []string // "K=V"
	HostPort      int
	ContainerPort int
	Platform      string // "linux/amd64" o ""
	// Almacenamiento: VolumeName (volumen con nombre) o BindPath (carpeta del host), montado en MountTarget.
	VolumeName      string
	BindPath        string
	MountTarget     string
	MemoryMB        int
	Labels          map[string]string
	Cmd             []string
	User            string // p. ej. "0:0"
	Binds           []Bind // montajes bind adicionales
	Volumes         []VolumeMount
	HostGateway     bool // añade host.docker.internal
	NoRestart       bool
	ExposeOnNetwork bool // false = solo 127.0.0.1
}

// VolumeMount es un volumen con nombre montado en Target.
type VolumeMount struct {
	Name     string
	Target   string
	ReadOnly bool
}

// Bind es un montaje de carpeta del host.
type Bind struct {
	Source   string
	Target   string
	ReadOnly bool
}

// CreateContainer crea (sin iniciar) un contenedor y devuelve su ID.
func (c *Client) CreateContainer(ctx context.Context, o CreateOptions) (string, error) {
	if o.Name == "" || o.Image == "" {
		return "", errors.New("nombre e imagen son obligatorios")
	}
	labels := map[string]string{LabelManaged: "true"}
	if o.Engine != "" {
		labels[LabelEngine] = o.Engine
	}
	for k, v := range o.Labels {
		labels[k] = v
	}
	cfg := &container.Config{Image: o.Image, Env: o.Env, Labels: labels}
	if len(o.Cmd) > 0 {
		cfg.Cmd = o.Cmd
	}
	hc := &container.HostConfig{RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped}}
	if o.ContainerPort > 0 {
		p := nat.Port(fmt.Sprintf("%d/tcp", o.ContainerPort))
		cfg.ExposedPorts = nat.PortSet{p: struct{}{}}
		hp := ""
		if o.HostPort > 0 {
			hp = strconv.Itoa(o.HostPort)
		}
		bindIP := "127.0.0.1"
		if o.ExposeOnNetwork {
			bindIP = "0.0.0.0"
		}
		hc.PortBindings = nat.PortMap{p: []nat.PortBinding{{HostIP: bindIP, HostPort: hp}}}
	}
	for _, v := range o.Volumes {
		hc.Mounts = append(hc.Mounts, mount.Mount{Type: mount.TypeVolume, Source: v.Name, Target: v.Target, ReadOnly: v.ReadOnly})
	}
	for _, b := range o.Binds {
		hc.Mounts = append(hc.Mounts, mount.Mount{Type: mount.TypeBind, Source: b.Source, Target: b.Target, ReadOnly: b.ReadOnly})
	}
	if o.NoRestart {
		hc.RestartPolicy = container.RestartPolicy{}
	}
	if o.User != "" {
		cfg.User = o.User
	}
	if o.HostGateway {
		hc.ExtraHosts = append(hc.ExtraHosts, "host.docker.internal:host-gateway")
	}
	if o.MemoryMB > 0 {
		hc.Resources.Memory = int64(o.MemoryMB) * 1024 * 1024
	}
	if o.MountTarget != "" {
		switch {
		case o.BindPath != "":
			if err := os.MkdirAll(o.BindPath, 0o777); err != nil {
				return "", fmt.Errorf("no se pudo crear la carpeta %s: %w", o.BindPath, err)
			}
			hc.Mounts = append(hc.Mounts, mount.Mount{Type: mount.TypeBind, Source: o.BindPath, Target: o.MountTarget})
		case o.VolumeName != "":
			hc.Mounts = append(hc.Mounts, mount.Mount{Type: mount.TypeVolume, Source: o.VolumeName, Target: o.MountTarget})
		}
	}
	var plat *ocispec.Platform
	if o.Platform != "" {
		parts := strings.SplitN(o.Platform, "/", 3)
		if len(parts) < 2 {
			return "", fmt.Errorf("plataforma inválida: %s", o.Platform)
		}
		plat = &ocispec.Platform{OS: parts[0], Architecture: parts[1]}
		if len(parts) == 3 {
			plat.Variant = parts[2]
		}
	}
	resp, err := c.cli.ContainerCreate(ctx, cfg, hc, &network.NetworkingConfig{}, plat, o.Name)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) Start(ctx context.Context, id string) error {
	return c.cli.ContainerStart(ctx, id, container.StartOptions{})
}

func (c *Client) Stop(ctx context.Context, id string) error {
	t := 30
	return c.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &t})
}

func (c *Client) Restart(ctx context.Context, id string) error {
	t := 30
	return c.cli.ContainerRestart(ctx, id, container.StopOptions{Timeout: &t})
}

// Remove elimina el contenedor (forzado) y opcionalmente sus volúmenes anónimos/asociados.
func (c *Client) Remove(ctx context.Context, id string, removeVolumes bool) error {
	return c.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true, RemoveVolumes: removeVolumes})
}

// RemoveVolume elimina un volumen con nombre.
func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	return c.cli.VolumeRemove(ctx, name, false)
}

// IsNotFound indica si el error es "no existe".
func IsNotFound(err error) bool { return err != nil && client.IsErrNotFound(err) }

// PortMapping es un puerto publicado.
type PortMapping struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Proto         string
}

// MountInfo describe un montaje.
type MountInfo struct {
	Type        string // volume | bind
	Name        string // nombre del volumen si aplica
	Source      string
	Destination string
}

// ContainerInfo es el resumen de un contenedor.
type ContainerInfo struct {
	ID         string
	Name       string
	Image      string
	State      string // running, exited, created, ...
	Status     string
	Running    bool
	Ports      []PortMapping
	Mounts     []MountInfo
	EnvNames   []string // solo nombres, nunca valores
	Labels     map[string]string
	Platform   string
	Created    time.Time
	Cmd        []string
	Entrypoint []string
	User       string
}

func toMounts(ms []container.MountPoint) []MountInfo {
	out := make([]MountInfo, 0, len(ms))
	for _, m := range ms {
		out = append(out, MountInfo{Type: string(m.Type), Name: m.Name, Source: m.Source, Destination: m.Destination})
	}
	return out
}

// Inspect devuelve el detalle de un contenedor por ID o nombre.
func (c *Client) Inspect(ctx context.Context, idOrName string) (ContainerInfo, error) {
	j, err := c.cli.ContainerInspect(ctx, idOrName)
	if err != nil {
		return ContainerInfo{}, err
	}
	info := ContainerInfo{ID: j.ID, Name: strings.TrimPrefix(j.Name, "/"), Platform: j.Platform, Mounts: toMounts(j.Mounts)}
	if j.Config != nil {
		info.Image = j.Config.Image
		info.Cmd = j.Config.Cmd
		info.Entrypoint = j.Config.Entrypoint
		info.User = j.Config.User
		info.Labels = j.Config.Labels
		for _, e := range j.Config.Env {
			name, _, _ := strings.Cut(e, "=")
			info.EnvNames = append(info.EnvNames, name)
		}
	}
	if j.State != nil {
		info.State = string(j.State.Status)
		info.Running = j.State.Running
		info.Status = info.State
	}
	if t, err := time.Parse(time.RFC3339Nano, j.Created); err == nil {
		info.Created = t
	}
	if j.NetworkSettings != nil {
		for p, binds := range j.NetworkSettings.Ports {
			for _, b := range binds {
				hp, _ := strconv.Atoi(b.HostPort)
				info.Ports = append(info.Ports, PortMapping{HostIP: b.HostIP, HostPort: hp, ContainerPort: p.Int(), Proto: p.Proto()})
			}
		}
	}
	if len(info.Ports) == 0 && j.HostConfig != nil {
		for p, binds := range j.HostConfig.PortBindings {
			for _, b := range binds {
				if hp, _ := strconv.Atoi(b.HostPort); hp > 0 {
					info.Ports = append(info.Ports, PortMapping{HostIP: b.HostIP, HostPort: hp, ContainerPort: p.Int(), Proto: p.Proto()})
				}
			}
		}
	}
	sort.Slice(info.Ports, func(i, k int) bool { return info.Ports[i].ContainerPort < info.Ports[k].ContainerPort })
	return info, nil
}

// ListContainers lista contenedores. labelFilter opcional "clave" o "clave=valor".
func (c *Client) ListContainers(ctx context.Context, all bool, labelFilter string) ([]ContainerInfo, error) {
	args := filters.NewArgs()
	if labelFilter != "" {
		args.Add("label", labelFilter)
	}
	list, err := c.cli.ContainerList(ctx, container.ListOptions{All: all, Filters: args})
	if err != nil {
		return nil, err
	}
	out := make([]ContainerInfo, 0, len(list))
	for _, s := range list {
		name := ""
		if len(s.Names) > 0 {
			name = strings.TrimPrefix(s.Names[0], "/")
		}
		ci := ContainerInfo{ID: s.ID, Name: name, Image: s.Image, State: string(s.State), Status: s.Status,
			Running: s.State == "running", Labels: s.Labels, Mounts: toMounts(s.Mounts), Created: time.Unix(s.Created, 0)}
		for _, p := range s.Ports {
			if p.PublicPort == 0 {
				continue
			}
			ci.Ports = append(ci.Ports, PortMapping{HostIP: p.IP, HostPort: int(p.PublicPort), ContainerPort: int(p.PrivatePort), Proto: p.Type})
		}
		out = append(out, ci)
	}
	return out, nil
}

// Logs devuelve las últimas tail líneas (stdout+stderr), demultiplexadas si no hay TTY.
func (c *Client) Logs(ctx context.Context, id string, tail int) (string, error) {
	j, err := c.cli.ContainerInspect(ctx, id)
	if err != nil {
		return "", err
	}
	t := "all"
	if tail > 0 {
		t = strconv.Itoa(tail)
	}
	rc, err := c.cli.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: t})
	if err != nil {
		return "", err
	}
	defer rc.Close()
	var buf bytes.Buffer
	if j.Config != nil && j.Config.Tty {
		_, err = io.Copy(&buf, rc)
	} else {
		_, err = stdcopy.StdCopy(&buf, &buf, rc)
	}
	return buf.String(), err
}

// ---------- Exec ----------

// ExecOptions configura un exec.
type ExecOptions struct {
	Cmd        []string
	Env        []string
	User       string
	WorkingDir string
	Stdin      io.Reader // opcional
}

// ExecResult es el resultado capturado.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Exec ejecuta un comando dentro del contenedor y captura salida y código de salida.
func (c *Client) Exec(ctx context.Context, id string, o ExecOptions) (ExecResult, error) {
	created, err := c.cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd: o.Cmd, Env: o.Env, User: o.User, WorkingDir: o.WorkingDir,
		AttachStdout: true, AttachStderr: true, AttachStdin: o.Stdin != nil,
	})
	if err != nil {
		return ExecResult{}, err
	}
	att, err := c.cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, err
	}
	defer att.Close()
	if o.Stdin != nil {
		go func() {
			io.Copy(att.Conn, o.Stdin)
			att.CloseWrite()
		}()
	}
	var so, se bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, e := stdcopy.StdCopy(&so, &se, att.Reader)
		done <- e
	}()
	select {
	case e := <-done:
		if e != nil && !errors.Is(e, io.EOF) {
			return ExecResult{}, e
		}
	case <-ctx.Done():
		return ExecResult{}, ctx.Err()
	}
	insp, err := c.cli.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return ExecResult{}, err
	}
	return ExecResult{Stdout: so.String(), Stderr: se.String(), ExitCode: insp.ExitCode}, nil
}

// ---------- Copia host <-> contenedor ----------

// CopyToContainer copia un archivo del host al directorio containerDir del contenedor.
func (c *Client) CopyToContainer(ctx context.Context, id, hostFile, containerDir string) error {
	return c.CopyToContainerAs(ctx, id, hostFile, containerDir, filepath.Base(hostFile))
}

// CopyToContainerAs copia un archivo del host al contenedor con otro nombre.
func (c *Client) CopyToContainerAs(ctx context.Context, id, hostFile, containerDir, name string) error {
	f, err := os.Open(hostFile)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.IsDir() {
		return errors.New("se esperaba un archivo, no una carpeta")
	}
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: st.Size(), ModTime: st.ModTime()}
		err := tw.WriteHeader(hdr)
		if err == nil {
			_, err = io.Copy(tw, f)
		}
		if err == nil {
			err = tw.Close()
		}
		pw.CloseWithError(err)
	}()
	err = c.cli.CopyToContainer(ctx, id, containerDir, pr, container.CopyToContainerOptions{})
	pr.CloseWithError(err)
	return err
}

// CopyFromContainer copia un archivo (o carpeta) del contenedor a hostDir.
// Devuelve la ruta del host del elemento copiado.
func (c *Client) CopyFromContainer(ctx context.Context, id, containerPath, hostDir string) (string, error) {
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		return "", err
	}
	rc, _, err := c.cli.CopyFromContainer(ctx, id, containerPath)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	root := path.Base(containerPath)
	base, err := filepath.Abs(hostDir)
	if err != nil {
		return "", err
	}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		// protección contra path traversal
		target := filepath.Join(base, filepath.FromSlash(h.Name))
		if target != base && !strings.HasPrefix(target, base+string(os.PathSeparator)) {
			return "", fmt.Errorf("entrada tar insegura: %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return "", err
			}
			if err := out.Close(); err != nil {
				return "", err
			}
		}
	}
	return filepath.Join(base, root), nil
}

// Wait espera a que el contenedor termine y devuelve su código de salida.
func (c *Client) Wait(ctx context.Context, id string) (int64, error) {
	okc, errc := c.cli.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	select {
	case r := <-okc:
		if r.Error != nil {
			return r.StatusCode, errors.New(r.Error.Message)
		}
		return r.StatusCode, nil
	case err := <-errc:
		return -1, err
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// RunOnce crea, ejecuta hasta terminar y elimina un contenedor temporal.
// Devuelve el código de salida y la salida combinada (stdout+stderr).
func (c *Client) RunOnce(ctx context.Context, o CreateOptions) (int64, string, error) {
	if o.Labels == nil {
		o.Labels = map[string]string{}
	}
	o.Labels["dbmanager.temp"] = "true"
	o.NoRestart = true
	id, err := c.CreateContainer(ctx, o)
	if err != nil {
		return -1, "", err
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c.Remove(rctx, id, true)
	}()
	if err := c.Start(ctx, id); err != nil {
		return -1, "", err
	}
	code, werr := c.Wait(ctx, id)
	lctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	logs, _ := c.Logs(lctx, id, 200)
	return code, logs, werr
}

// ImageEnv devuelve las variables de entorno (clave->valor) de la configuración de una imagen local.
func (c *Client) ImageEnv(ctx context.Context, ref string) (map[string]string, error) {
	ins, err := c.cli.ImageInspect(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if ins.Config != nil {
		for _, e := range ins.Config.Env {
			k, v, _ := strings.Cut(e, "=")
			out[k] = v
		}
	}
	return out, nil
}

// ListVolumes devuelve los nombres de volúmenes que empiezan por prefix.
func (c *Client) ListVolumes(ctx context.Context, prefix string) ([]string, error) {
	res, err := c.cli.VolumeList(ctx, volume.ListOptions{Filters: filters.NewArgs(filters.Arg("name", prefix))})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, v := range res.Volumes {
		if v != nil && strings.HasPrefix(v.Name, prefix) {
			out = append(out, v.Name)
		}
	}
	return out, nil
}

// Package testutil agrupa utilidades para las pruebas de integración: contenedores de prueba
// (siempre llamados dbmanager-test-*), puertos libres y limpieza.
//
// REGLA: este paquete solo toca contenedores cuyo nombre empieza por Prefix. Nunca los demás.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/engine"
	"dbmanager/internal/engine/mssql"
	"dbmanager/internal/engine/postgres"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/go-connections/nat"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Prefix es el prefijo obligatorio de los contenedores de prueba.
const Prefix = "dbmanager-test-"

// TestLabel marca los contenedores creados por estas utilidades.
const TestLabel = "dbmanager.test"

const (
	MSSQLImage    = "mcr.microsoft.com/mssql/server:2022-latest"
	PostgresImage = "postgres:17"
	MSSQLPassword = "T3st!Passw0rd_sa"
	PGPassword    = "T3st!Passw0rd_pg"
)

// Docker devuelve un cliente de Docker (respetando el contexto activo).
func Docker(t testing.TB) *dockerx.Client {
	t.Helper()
	cli, err := dockerx.New()
	if err != nil {
		t.Fatalf("docker: %v", err)
	}
	if err := cli.Ping(context.Background()); err != nil {
		t.Fatalf("docker no responde: %v", err)
	}
	return cli
}

// RandName devuelve dbmanager-test-<tag>-<hex>.
func RandName(tag string) string {
	b := make([]byte, 3)
	rand.Read(b)
	return Prefix + tag + "-" + hex.EncodeToString(b)
}

// FreePort devuelve un puerto TCP libre en 127.0.0.1.
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// Container describe un contenedor de prueba en ejecución.
type Container struct {
	Name     string
	ID       string
	Engine   string // mssql | postgres
	Port     int    // puerto publicado en el host
	User     string
	Password string
}

// Conn devuelve los parámetros de conexión desde el host.
func (c Container) Conn() engine.Conn {
	return engine.Conn{Host: "127.0.0.1", Port: c.Port, User: c.User, Password: c.Password,
		TLS: c.Engine == "mssql", TrustCert: true}
}

// RemoveByName elimina (forzado) un contenedor de prueba. Se niega a tocar nombres sin el prefijo.
func RemoveByName(name string) {
	if !strings.HasPrefix(name, Prefix) {
		panic("testutil: se negó a eliminar un contenedor que no es de prueba: " + name)
	}
	cli, err := dockerx.New()
	if err != nil {
		return
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cli.Remove(ctx, name, true)
}

// RemoveVolume elimina un volumen de prueba (solo si empieza por el prefijo).
func RemoveVolume(name string) {
	if !strings.HasPrefix(name, Prefix) {
		panic("testutil: se negó a eliminar un volumen que no es de prueba: " + name)
	}
	cli, err := dockerx.New()
	if err != nil {
		return
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i := 0; i < 5; i++ {
		if err := cli.RemoveVolume(ctx, name); err == nil || dockerx.IsNotFound(err) {
			return
		}
		time.Sleep(time.Second)
	}
}

// SweepOlderThan elimina los contenedores dbmanager-test-* (y temporales dbmanager-tmp-* de la app) creados hace más
// de d: restos de ejecuciones anteriores interrumpidas. No toca los de ejecuciones concurrentes recientes.
func SweepOlderThan(d time.Duration) {
	cli, err := dockerx.New()
	if err != nil {
		return
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	list, err := cli.ListContainers(ctx, true, "")
	if err != nil {
		return
	}
	for _, c := range list {
		if !(strings.HasPrefix(c.Name, Prefix) || (strings.HasPrefix(c.Name, "dbmanager-tmp-") && c.Labels["dbmanager.temp"] == "true")) {
			continue
		}
		if time.Since(c.Created) > d {
			cli.Remove(ctx, c.Name, true)
		}
	}
}

// ListNames devuelve los nombres de los contenedores cuyo nombre empieza por prefix.
func ListNames(prefix string) []string {
	cli, err := dockerx.New()
	if err != nil {
		return nil
	}
	defer cli.Close()
	list, err := cli.ListContainers(context.Background(), true, "")
	if err != nil {
		return nil
	}
	var out []string
	for _, c := range list {
		if strings.HasPrefix(c.Name, prefix) {
			out = append(out, c.Name)
		}
	}
	return out
}

// PlainOptions configura un contenedor "plano" (sin etiquetas dbmanager.*).
type PlainOptions struct {
	Name     string
	Engine   string // mssql | postgres
	Password string
	Image    string
	Bind     string // carpeta del host montada en el directorio de datos (opcional)
	Volume   string // volumen con nombre montado en el directorio de datos (opcional)
	Start    bool
}

// RunPlain es StartPlain atado a la prueba t: falla la prueba si hay error y elimina el contenedor al terminar.
func RunPlain(t testing.TB, o PlainOptions) Container {
	t.Helper()
	c, err := StartPlain(o)
	t.Cleanup(func() {
		RemoveByName(o.Name)
		if o.Volume != "" {
			RemoveVolume(o.Volume)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// StartPlain crea (con el SDK de Docker directamente) un contenedor SIN etiquetas dbmanager.*,
// lo inicia y espera a que el motor responda. El llamador debe eliminarlo (RemoveByName).
func StartPlain(o PlainOptions) (Container, error) {
	if !strings.HasPrefix(o.Name, Prefix) {
		return Container{}, fmt.Errorf("nombre de contenedor de prueba debe empezar por %s: %s", Prefix, o.Name)
	}
	cli, err := dockerx.New()
	if err != nil {
		return Container{}, err
	}
	defer cli.Close()
	raw := cli.Raw()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var cport, user, dataDir string
	var env []string
	var plat *ocispec.Platform
	switch o.Engine {
	case "mssql":
		cport, user, dataDir = "1433/tcp", "sa", "/var/opt/mssql"
		if o.Image == "" {
			o.Image = MSSQLImage
		}
		env = []string{"ACCEPT_EULA=Y", "MSSQL_SA_PASSWORD=" + o.Password, "MSSQL_PID=Developer"}
		plat = &ocispec.Platform{OS: "linux", Architecture: "amd64"}
	case "postgres":
		cport, user, dataDir = "5432/tcp", "postgres", "/var/lib/postgresql/data"
		if o.Image == "" {
			o.Image = PostgresImage
		}
		env = []string{"POSTGRES_PASSWORD=" + o.Password}
	default:
		return Container{}, fmt.Errorf("motor desconocido %q", o.Engine)
	}
	p := nat.Port(cport)
	cfg := &container.Config{Image: o.Image, Env: env, ExposedPorts: nat.PortSet{p: struct{}{}},
		Labels: map[string]string{TestLabel: "true"}}
	hc := &container.HostConfig{PortBindings: nat.PortMap{p: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}}}}
	switch {
	case o.Bind != "":
		target := dataDir
		if o.Engine == "mssql" {
			// En hosts Windows SQL Server solo arranca si el bind es /var/opt/mssql/data (no /var/opt/mssql completo).
			target = "/var/opt/mssql/data"
			cfg.User = "0:0"
		}
		hc.Mounts = append(hc.Mounts, mount.Mount{Type: mount.TypeBind, Source: o.Bind, Target: target})
	case o.Volume != "":
		hc.Mounts = append(hc.Mounts, mount.Mount{Type: mount.TypeVolume, Source: o.Volume, Target: dataDir})
	}
	resp, err := raw.ContainerCreate(ctx, cfg, hc, &network.NetworkingConfig{}, plat, o.Name)
	if err != nil {
		return Container{}, fmt.Errorf("crear contenedor %s: %w", o.Name, err)
	}
	if err := cli.Start(ctx, resp.ID); err != nil {
		return Container{}, fmt.Errorf("iniciar %s: %w", o.Name, err)
	}
	info, err := cli.Inspect(ctx, resp.ID)
	if err != nil {
		return Container{}, err
	}
	port := 0
	for _, pm := range info.Ports {
		if pm.HostPort > 0 {
			port = pm.HostPort
		}
	}
	if port == 0 {
		return Container{}, fmt.Errorf("el contenedor %s no publicó puerto: %+v", o.Name, info.Ports)
	}
	c := Container{Name: o.Name, ID: resp.ID, Engine: o.Engine, Port: port, User: user, Password: o.Password}
	timeout := 90 * time.Second
	if o.Engine == "mssql" {
		timeout = 180 * time.Second
	}
	if err := WaitReadyErr(c, timeout); err != nil {
		return c, err
	}
	return c, nil
}

// NewEngine abre el engine del contenedor.
func NewEngine(c Container) (engine.Engine, error) {
	switch c.Engine {
	case "mssql":
		return mssql.New(c.Conn())
	case "postgres":
		return postgres.New(c.Conn())
	}
	return nil, fmt.Errorf("motor %q", c.Engine)
}

// WaitReadyErr espera a que el motor responda a Ping.
func WaitReadyErr(c Container, timeout time.Duration) error {
	e, err := NewEngine(c)
	if err != nil {
		return err
	}
	defer e.Close()
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_, last = e.Ping(ctx)
		cancel()
		if last == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("el motor de %s no respondió en %s: %v", c.Name, timeout, last)
}

// Itoa es un atajo.
func Itoa(i int) string { return strconv.Itoa(i) }

// ContainerExists indica si existe un contenedor (cualquier estado) con ese nombre exacto.
func ContainerExists(name string) bool {
	for _, n := range ListNames(name) {
		if n == name {
			return true
		}
	}
	return false
}

// VolumeExists indica si existe el volumen.
func VolumeExists(name string) bool {
	cli, err := dockerx.New()
	if err != nil {
		return false
	}
	defer cli.Close()
	vs, err := cli.Raw().VolumeList(context.Background(), volume.ListOptions{Filters: filters.NewArgs(filters.Arg("name", name))})
	if err != nil {
		return false
	}
	for _, v := range vs.Volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

// RemoveVolumesPrefix elimina los volúmenes cuyo nombre empieza por prefix (que debe empezar por Prefix).
func RemoveVolumesPrefix(prefix string) {
	if !strings.HasPrefix(prefix, Prefix) {
		panic("testutil: se negó a eliminar volúmenes que no son de prueba: " + prefix)
	}
	cli, err := dockerx.New()
	if err != nil {
		return
	}
	defer cli.Close()
	vs, err := cli.Raw().VolumeList(context.Background(), volume.ListOptions{Filters: filters.NewArgs(filters.Arg("name", prefix))})
	if err != nil {
		return
	}
	for _, v := range vs.Volumes {
		if strings.HasPrefix(v.Name, prefix) {
			RemoveVolume(v.Name)
		}
	}
}

// VolumesWithPrefix lista los volúmenes cuyo nombre empieza por prefix.
func VolumesWithPrefix(prefix string) []string {
	cli, err := dockerx.New()
	if err != nil {
		return nil
	}
	defer cli.Close()
	vs, err := cli.Raw().VolumeList(context.Background(), volume.ListOptions{Filters: filters.NewArgs(filters.Arg("name", prefix))})
	if err != nil {
		return nil
	}
	var out []string
	for _, v := range vs.Volumes {
		if strings.HasPrefix(v.Name, prefix) {
			out = append(out, v.Name)
		}
	}
	return out
}

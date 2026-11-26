package services

import (
	"testing"
	"time"

	"dbmanager/internal/dockerx"
)

func TestPickPort(t *testing.T) {
	used := map[int]bool{14331: true, 14332: true}
	p, err := PickPort(14331, func(p int) bool { return used[p] })
	if err != nil || p != 14333 {
		t.Fatal(p, err)
	}
	if _, err := PickPort(1, func(int) bool { return true }); err == nil {
		t.Fatal("debe fallar si todo está ocupado")
	}
}

func TestHostPortFree(t *testing.T) {
	// un puerto libre dinámico debe reportarse libre; uno en escucha, ocupado
	p, _ := PickPort(40000, func(p int) bool { return !hostPortFree(p) })
	if !hostPortFree(p) {
		t.Fatal("debería estar libre")
	}
}

func TestPostgresMountTarget(t *testing.T) {
	cases := map[string]string{
		"postgres:17": "/var/lib/postgresql/data", "postgres:16.4-alpine": "/var/lib/postgresql/data",
		"postgres:18": "/var/lib/postgresql", "postgres:latest": "/var/lib/postgresql", "postgres": "/var/lib/postgresql",
		"registry:5000/postgres:15": "/var/lib/postgresql/data",
	}
	for img, want := range cases {
		if got := PostgresMountTarget(img); got != want {
			t.Errorf("%s -> %s, want %s", img, got, want)
		}
	}
}

func TestDetectEngine(t *testing.T) {
	if DetectEngine("mcr.microsoft.com/mssql/server:2022-latest", nil, nil, nil, nil) != "mssql" {
		t.Fatal("mssql por imagen")
	}
	if DetectEngine("custom/img", nil, []string{"/opt/mssql/bin/sqlservr"}, nil, nil) != "mssql" {
		t.Fatal("mssql por entrypoint")
	}
	if DetectEngine("x", nil, nil, []string{"ACCEPT_EULA"}, nil) != "mssql" {
		t.Fatal("mssql por env")
	}
	if DetectEngine("postgres:17", nil, nil, nil, nil) != "postgres" || DetectEngine("postgis/postgis:16-3.4", nil, nil, nil, nil) != "postgres" ||
		DetectEngine("bitnami/postgresql:16", nil, nil, nil, nil) != "postgres" {
		t.Fatal("postgres por imagen")
	}
	if DetectEngine("x", nil, nil, []string{"POSTGRES_PASSWORD"}, nil) != "postgres" {
		t.Fatal("postgres por env")
	}
	if DetectEngine("x", nil, nil, nil, []dockerx.MountInfo{{Destination: "/var/lib/postgresql/data"}}) != "postgres" {
		t.Fatal("postgres por montaje")
	}
	if DetectEngine("nginx:latest", nil, nil, []string{"PATH"}, nil) != "" {
		t.Fatal("no debe detectar nginx")
	}
}

func TestDescribePorts(t *testing.T) {
	im, ok := describe(dockerx.ContainerInfo{Name: "sql", Image: "mcr.microsoft.com/mssql/server:2022-latest", State: "running",
		Ports:  []dockerx.PortMapping{{HostPort: 1433, ContainerPort: 1433}},
		Mounts: []dockerx.MountInfo{{Type: "bind", Source: "C:/data", Destination: "/var/opt/mssql/data"}}})
	if !ok || im.HostPort != 1433 || im.DataMode != "bind" || im.DataPath != "C:/data" {
		t.Fatalf("%+v", im)
	}
}

func TestCreateRequestValidate(t *testing.T) {
	r := CreateRequest{Name: "x", Engine: "mssql", AdminPassword: "weak"}
	if r.Validate() == nil {
		t.Fatal("contraseña débil debe fallar")
	}
	r = CreateRequest{Name: "x", Engine: "mssql", AdminPassword: "Str0ng!Pass"}
	if err := r.Validate(); err != nil || r.Image != DefaultMSSQLImage || r.Edition != "Developer" || r.DataMode != "volume" {
		t.Fatalf("%v %+v", err, r)
	}
	r = CreateRequest{Name: "x", Engine: "postgres", AdminPassword: "p", DataMode: "bind", DataPath: "relativa"}
	if r.Validate() == nil {
		t.Fatal("bind relativo debe fallar")
	}
	r = CreateRequest{Name: "x", Engine: "postgres", AdminPassword: "p"}
	if err := r.Validate(); err != nil || r.Image != DefaultPostgresImage {
		t.Fatal(err)
	}
	if (&CreateRequest{Name: "", Engine: "postgres", AdminPassword: "p"}).Validate() == nil {
		t.Fatal("nombre vacío")
	}
	if (&CreateRequest{Name: "x", Engine: "oracle", AdminPassword: "p"}).Validate() == nil {
		t.Fatal("motor inválido")
	}
}

func TestSlugAndNames(t *testing.T) {
	if Slug("Mi Servidor #1") != "mi-servidor-1" || Slug("!!!") != "servicio" {
		t.Fatal(Slug("Mi Servidor #1"))
	}
	if got := BackupFileName("my db;x", "bak", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); got != "my_db_x_20260102_030405.bak" {
		t.Fatal(got)
	}
	if dockerHostFor("localhost") != "host.docker.internal" || dockerHostFor("db.example.com") != "db.example.com" {
		t.Fatal("host")
	}
	if postgresImageFor("PostgreSQL 16.4") != "postgres:16" || postgresImageFor("?") != DefaultPostgresImage {
		t.Fatal("img")
	}
}

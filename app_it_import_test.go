//go:build integration

package main

import (
	"context"
	"strings"
	"testing"

	"dbmanager/internal/testutil"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// ListImportableContainers / ImportContainer con contenedores planos (sin etiquetas dbmanager.*).
func TestImportPlainContainers(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)

			// 1) detectado
			list, err := a.ListImportableContainers()
			if err != nil {
				t.Fatal(err)
			}
			var found *ContainerInfo
			for i, ci := range list {
				if ci.Name == c.Name {
					found = &list[i]
				}
				// contenedores que no son motores de BD nunca deben aparecer
				if ci.Name == "sftp-iflow" || ci.Name == "sftp_server" {
					t.Errorf("%s no es una base de datos y fue listado", ci.Name)
				}
			}
			if found == nil {
				t.Fatalf("el contenedor plano %s no fue detectado. Lista: %+v", c.Name, list)
			}
			if found.Engine != eng || found.HostPort != c.Port || found.State != "running" {
				t.Errorf("datos incorrectos: %+v (esperaba engine=%s puerto=%d)", *found, eng, c.Port)
			}
			wantCP := 5432
			if eng == "mssql" {
				wantCP = 1433
			}
			if found.ContainerPort != wantCP {
				t.Errorf("puerto de contenedor %d, esperaba %d", found.ContainerPort, wantCP)
			}

			// 2) contraseña incorrecta: falla, en español, sin guardar y sin filtrar la contraseña
			const wrong = "Wr0ng!Passw0rd-zzz"
			_, err = a.ImportContainer(ImportContainerRequest{ContainerName: c.Name, AdminUser: c.User, AdminPassword: wrong})
			if err == nil {
				t.Fatal("importar con contraseña incorrecta debía fallar")
			}
			if !strings.Contains(err.Error(), "no se pudo conectar") {
				t.Errorf("mensaje inesperado: %v", err)
			}
			noSecret(t, "error de importación", err.Error(), wrong, c.Password)
			if l, _ := a.ListServers(); len(l) != 0 {
				t.Fatalf("no debía guardarse nada tras un import fallido: %+v", l)
			}
			// ni un usuario inexistente
			if _, err := a.ImportContainer(ImportContainerRequest{ContainerName: c.Name, AdminUser: "no_existe_xx", AdminPassword: c.Password}); err == nil {
				t.Error("usuario inexistente debía fallar")
			}
			// ni contraseña vacía
			if _, err := a.ImportContainer(ImportContainerRequest{ContainerName: c.Name, AdminUser: c.User, AdminPassword: ""}); err == nil {
				t.Error("contraseña vacía debía fallar")
			}
			if l, _ := a.ListServers(); len(l) != 0 {
				t.Fatalf("no debía guardarse nada: %+v", l)
			}

			// 3) contraseña correcta; usuario por defecto (vacío) = sa / postgres
			sv, err := a.ImportContainer(ImportContainerRequest{ContainerName: c.Name, AdminPassword: c.Password})
			if err != nil {
				t.Fatalf("import correcto: %v", err)
			}
			if sv.Engine != eng || sv.Mode != "local" || sv.Port != c.Port || sv.AdminUser != c.User || !sv.HasPassword || sv.ContainerName != c.Name {
				t.Errorf("servidor importado: %+v", sv)
			}
			noSecret(t, "Server JSON", mustJSON(t, sv), c.Password)
			st, _ := a.TestConnection(sv.ID)
			if !st.Reachable || st.State != "running" || st.Version == "" {
				t.Errorf("TestConnection: %+v", st)
			}
			ss, _ := a.ServerStatus(sv.ID)
			if ss.State != "running" {
				t.Errorf("ServerStatus: %+v", ss)
			}

			// 4) ya no aparece como importable y no se puede importar dos veces
			list, _ = a.ListImportableContainers()
			for _, ci := range list {
				if ci.Name == c.Name {
					t.Error("el contenedor registrado no debe seguir listado")
				}
			}
			if _, err := a.ImportContainer(ImportContainerRequest{ContainerName: c.Name, AdminUser: c.User, AdminPassword: c.Password}); err == nil ||
				!strings.Contains(err.Error(), "ya está registrado") {
				t.Errorf("doble importación: %v", err)
			}
			// 5) casos de error
			if _, err := a.ImportContainer(ImportContainerRequest{ContainerName: "dbmanager-test-no-existe-xyz", AdminPassword: "x"}); err == nil ||
				!strings.Contains(err.Error(), "no existe") {
				t.Errorf("contenedor inexistente: %v", err)
			}
			if _, err := a.ImportContainer(ImportContainerRequest{ContainerName: "  ", AdminPassword: "x"}); err == nil {
				t.Error("nombre vacío debía fallar")
			}
		})
	}
}

// Un contenedor que no es SQL Server/PostgreSQL no puede importarse ni se lista.
func TestImportRejectsNonDatabaseContainer(t *testing.T) {
	name := testutil.RandName("nodb")
	cli := testutil.Docker(t)
	defer cli.Close()
	ctx := context.Background()
	resp, err := cli.Raw().ContainerCreate(ctx, &container.Config{Image: "node:22-bookworm-slim", Cmd: []string{"sleep", "300"},
		Labels: map[string]string{testutil.TestLabel: "true"}}, &container.HostConfig{}, &network.NetworkingConfig{}, nil, name)
	if err != nil {
		t.Skipf("imagen node no disponible: %v", err)
	}
	t.Cleanup(func() { testutil.RemoveByName(name) })
	if err := cli.Start(ctx, resp.ID); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp(t)
	list, err := a.ListImportableContainers()
	if err != nil {
		t.Fatal(err)
	}
	for _, ci := range list {
		if ci.Name == name {
			t.Errorf("un contenedor node fue listado como importable: %+v", ci)
		}
	}
	_, err = a.ImportContainer(ImportContainerRequest{ContainerName: name, AdminPassword: "x"})
	if err == nil || !strings.Contains(err.Error(), "no parece ser") {
		t.Fatalf("mensaje inesperado: %v", err)
	}
	if l, _ := a.ListServers(); len(l) != 0 {
		t.Fatalf("no se debe guardar nada: %+v", l)
	}
}

// BUG: importar un contenedor detenido dice "no publica ningún puerto" (Inspect de un contenedor parado no trae HostPort).
func TestImportStoppedContainerMisleadingMessage(t *testing.T) {
	c := testutil.RandName("stopped")
	pc, err := testutil.StartPlain(testutil.PlainOptions{Name: c, Engine: "postgres", Password: testutil.PGPassword})
	t.Cleanup(func() { testutil.RemoveByName(c) })
	if err != nil {
		t.Fatal(err)
	}
	cli := testutil.Docker(t)
	defer cli.Close()
	if err := cli.Stop(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp(t)
	_, err = a.ImportContainer(ImportContainerRequest{ContainerName: c, AdminPassword: pc.Password})
	if err == nil || !strings.Contains(err.Error(), "detenido") {
		t.Fatalf("esperaba aviso de contenedor detenido, fue: %v", err)
	}
}

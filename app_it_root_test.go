//go:build integration

package main

import (
	"strings"
	"testing"
)

// Usuario root: un servidor importado con un login sin permisos se arregla creando el root con un
// administrador existente; luego se cambian contraseña y nombre, y se crean usuarios de una sola base.
func TestRootUserLifecycle(t *testing.T) {
	for _, eng := range engines {
		eng := eng
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			su := rawDB{c: c, user: c.User, pw: c.Password}
			lim, limPw := uniq("lim"), "Lim1t3d!Pass"
			create := "CREATE LOGIN " + lim + " WITH PASSWORD = '" + limPw + "'"
			if eng == "postgres" {
				create = "CREATE ROLE " + lim + " LOGIN PASSWORD '" + limPw + "'"
			}
			if err := su.exec(create); err != nil {
				t.Fatal(err)
			}

			a, _ := newTestApp(t)
			sv, err := a.ImportContainer(ImportContainerRequest{ContainerName: c.Name, AdminUser: lim, AdminPassword: limPw})
			if err != nil {
				t.Fatal(err)
			}
			st, err := a.GetRootStatus(sv.ID)
			if err != nil || !st.Reachable || st.IsAdmin {
				t.Fatalf("login limitado debería verse sin permisos: %+v %v", st, err)
			}

			root := uniq("root")
			// Sin credenciales de un administrador no se puede crear el root.
			if _, err := a.ConfigureRoot(sv.ID, ConfigureRootRequest{User: root, Password: "root"}); err == nil {
				t.Fatal("ConfigureRoot con un login sin permisos debería fallar")
			}
			// Con el superusuario como arranque; contraseña débil "root" permitida (CHECK_POLICY=OFF en mssql).
			res, err := a.ConfigureRoot(sv.ID, ConfigureRootRequest{User: root, Password: "root", BootstrapUser: c.User, BootstrapPassword: c.Password})
			if err != nil {
				t.Fatal(err)
			}
			if res.Server.AdminUser != root {
				t.Fatalf("admin guardado = %q, se esperaba %q", res.Server.AdminUser, root)
			}
			if st, _ := a.GetRootStatus(sv.ID); !st.IsAdmin {
				t.Fatalf("el root debería ser administrador: %+v", st)
			}

			// Usuario de una sola base de datos.
			db := uniq("db")
			if err := a.CreateDatabase(sv.ID, db, ""); err != nil {
				t.Fatal(err)
			}
			u := uniq("u")
			if err := a.CreateDatabaseUser(sv.ID, db, CreateLoginRequest{Name: u, Password: "Us3r!Pass99"}, "readonly"); err != nil {
				t.Fatal(err)
			}
			m, err := a.GetPermissionMatrix(sv.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, cell := range m.Cells {
				if cell.Login != u {
					continue
				}
				want := "none"
				if cell.Database == db {
					want = "readonly"
				}
				if cell.Level != want {
					t.Errorf("%s en %s = %s, se esperaba %s", u, cell.Database, cell.Level, want)
				}
			}
			if err := a.SetDatabaseAccess(sv.ID, u, db, "readwrite"); err != nil {
				t.Fatal(err)
			}
			// Nivel inválido: no crea nada.
			if err := a.CreateDatabaseUser(sv.ID, db, CreateLoginRequest{Name: uniq("u"), Password: "Us3r!Pass99"}, "none"); err == nil {
				t.Fatal("nivel none debería rechazarse")
			}

			// Solo contraseña.
			if _, err := a.ConfigureRoot(sv.ID, ConfigureRootRequest{User: root, Password: "otra"}); err != nil {
				t.Fatal(err)
			}
			if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: uniq("x"), Password: "Us3r!Pass99"}); err != nil {
				t.Fatalf("con el root se deben poder crear usuarios: %v", err)
			}

			// Renombrar eliminando el root anterior.
			root2 := uniq("root")
			if res, err := a.ConfigureRoot(sv.ID, ConfigureRootRequest{User: root2, Password: "root", DropPrevious: true}); err != nil {
				t.Fatal(err)
			} else if res.Warning != "" {
				t.Fatalf("aviso inesperado: %s", res.Warning)
			}
			logins, err := a.ListLogins(sv.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, l := range logins {
				if strings.EqualFold(l.Name, root) {
					t.Fatalf("el root anterior %q debería haberse eliminado", root)
				}
			}
			a.DropDatabase(sv.ID, db)

			// Nombres reservados.
			sys := "sa"
			if eng == "postgres" {
				sys = "postgres"
			}
			if _, err := a.ConfigureRoot(sv.ID, ConfigureRootRequest{User: sys, Password: "x"}); err == nil {
				t.Fatalf("%s no debería aceptarse como root", sys)
			}
		})
	}
}

func TestRootDefaultsHidden(t *testing.T) {
	a, _ := newTestApp(t)
	d, err := a.GetRootDefaults()
	if err != nil || d.User != "root" || !d.PasswordIsDefault {
		t.Fatalf("%+v %v", d, err)
	}
	if err := a.SetRootDefaults("admin_db", "S3creta"); err != nil {
		t.Fatal(err)
	}
	d, _ = a.GetRootDefaults()
	if d.User != "admin_db" || d.PasswordIsDefault {
		t.Fatalf("%+v", d)
	}
	all, _ := a.GetSettings()
	for k, v := range all {
		if strings.Contains(v, "S3creta") || strings.HasSuffix(k, "Enc") {
			t.Fatalf("GetSettings expone un secreto: %s", k)
		}
	}
	if err := a.SetSetting("rootPasswordEnc", "x"); err == nil {
		t.Fatal("SetSetting no debería permitir escribir secretos")
	}
	if err := a.SetRootDefaults("sa", ""); err == nil {
		t.Fatal("sa no debería aceptarse como root predeterminado")
	}
}

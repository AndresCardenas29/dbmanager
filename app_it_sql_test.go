//go:build integration

package main

import (
	"strings"
	"testing"

	"dbmanager/internal/testutil"
)

var nastyPasswords = []string{
	"Qq1'; DROP LOGIN sa; --",
	`Qq1"]]'\`,
	"Qq1ñandú€日本語",
	"Qq1 con espacios",
	"Qq1%s%d$1{}[]()<>|&^~",
	`Qq1\\xyz\`,
	"Qq1:@/?#=&+",
}

func badNames(eng string) []string {
	long := strings.Repeat("a", 129)
	if eng == "postgres" {
		long = strings.Repeat("a", 64)
	}
	return []string{"a]b", "x'; DROP DATABASE master; --", "ñandú", "", " ", "a b", "db\nname", "1abc", "a.b", `a"b`, "a;b", "a[b", "--x", "x/*", long, "a\x00b"}
}

// Inyección en identificadores y literales de contraseña, y límites de longitud.
func TestSQLInjectionAndBoundaries(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			before, err := a.ListDatabases(sv.ID)
			if err != nil {
				t.Fatal(err)
			}
			loginsBefore, _ := a.ListLogins(sv.ID)

			for _, n := range badNames(eng) {
				if err := a.CreateDatabase(sv.ID, n, ""); err == nil {
					t.Errorf("CreateDatabase(%q) debía fallar", n)
					a.DropDatabase(sv.ID, n)
				} else if !testutil.HasSpanish(err.Error()) {
					t.Errorf("CreateDatabase(%q): mensaje no en español: %v", n, err)
				}
				if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: n, Password: strongPW()}); err == nil {
					t.Errorf("CreateLogin(%q) debía fallar", n)
					a.DropLogin(sv.ID, n)
				}
			}
			// propietario inyectado: no debe crearse la base
			dbn := uniq("own")
			for _, ow := range []string{"x'; DROP DATABASE " + dbn + "; --", "a]b", "no_existe_" + uniq("o")} {
				if err := a.CreateDatabase(sv.ID, dbn, ow); err == nil {
					t.Errorf("CreateDatabase con owner %q debía fallar", ow)
				}
				if dbExistsRaw(t, c, dbn) {
					t.Errorf("la base %s no debió crearse con owner inválido %q", dbn, ow)
					a.DropDatabase(sv.ID, dbn)
				}
			}
			// rol de servidor inyectado
			lg := uniq("lg")
			if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: lg, Password: strongPW(), ServerRoles: []string{"sysadmin; DROP LOGIN sa"}}); err == nil {
				t.Error("rol inyectado debía fallar")
				a.DropLogin(sv.ID, lg)
			}
			if loginExistsRaw(t, c, lg) {
				t.Error("no debió crearse el login con un rol inválido")
				dropLoginRaw(c, lg)
			}

			// nada cambió
			after, _ := a.ListDatabases(sv.ID)
			if len(after) != len(before) {
				t.Errorf("la lista de bases cambió: %d -> %d", len(before), len(after))
			}
			loginsAfter, _ := a.ListLogins(sv.ID)
			if len(loginsAfter) != len(loginsBefore) {
				t.Errorf("la lista de logins cambió: %d -> %d", len(loginsBefore), len(loginsAfter))
			}
			sys := "master"
			if eng == "postgres" {
				sys = "template1"
			}
			if !hasDB(after, sys) {
				t.Fatal("falta la base del sistema " + sys)
			}

			// límites de longitud correctos
			max := 100 // límite real de SQL Server con archivos .mdf/.ldf: ver TestBUG_MSSQLMaxDatabaseNameLength
			if eng == "postgres" {
				max = 63
			}
			for _, n := range []int{1, max} {
				name := "z" + uniq("b")
				for len(name) < n {
					name += "z"
				}
				name = name[:n]
				if n == 1 {
					name = "q"
					a.DropDatabase(sv.ID, name)
				}
				if err := a.CreateDatabase(sv.ID, name, ""); err != nil {
					t.Errorf("CreateDatabase con %d caracteres: %v", n, err)
					continue
				}
				if !dbExistsRaw(t, c, name) {
					t.Errorf("la base de %d caracteres no existe", n)
				}
				if err := a.DropDatabase(sv.ID, name); err != nil {
					t.Errorf("DropDatabase %d: %v", n, err)
				}
				lname := name
				if n == 1 {
					lname = "w"
				}
				if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: lname, Password: strongPW()}); err != nil {
					t.Errorf("CreateLogin con %d caracteres: %v", n, err)
				} else if err := a.DropLogin(sv.ID, lname); err != nil {
					t.Errorf("DropLogin %d: %v", n, err)
				}
			}
			// nombres con guion y palabras reservadas
			for _, n := range []string{"mi-base-1", "select", "user", "table", "public", "null"} {
				nn := n
				if n != "mi-base-1" {
					nn = n + "_" + uniq("r")[:0] // se mantiene la palabra reservada pura salvo que ya exista
					nn = n
				}
				if err := a.CreateDatabase(sv.ID, nn, ""); err != nil {
					// 'public'/'user'/... no son bases del sistema; deben poder crearse
					t.Errorf("CreateDatabase(%q): %v", nn, err)
					continue
				}
				if err := a.DropDatabase(sv.ID, nn); err != nil {
					t.Errorf("DropDatabase(%q): %v", nn, err)
				}
			}

			// contraseñas hostiles: se crean, autentican exactamente, se cambian
			for i, pw := range nastyPasswords {
				name := uniq("pwu")
				if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: name, Password: pw}); err != nil {
					t.Errorf("CreateLogin con contraseña %q: %v", pw, err)
					continue
				}
				t.Cleanup(func() { dropLoginRaw(c, name) })
				if err := asUser(c, name, pw, "").exec("SELECT 1"); err != nil {
					t.Errorf("el login no autentica con la contraseña exacta %q: %v", pw, err)
				}
				pw2 := nastyPasswords[(i+1)%len(nastyPasswords)]
				if err := a.ChangePassword(sv.ID, name, pw2); err != nil {
					t.Errorf("ChangePassword %q: %v", pw2, err)
					continue
				}
				if err := asUser(c, name, pw2, "").exec("SELECT 1"); err != nil {
					t.Errorf("no autentica con la nueva contraseña %q: %v", pw2, err)
				}
				if pw2 != pw {
					if err := asUser(c, name, pw, "").exec("SELECT 1"); err == nil {
						t.Errorf("la contraseña anterior %q sigue funcionando", pw)
					}
				}
			}
			// contraseñas inválidas
			if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: uniq("e"), Password: ""}); err == nil {
				t.Error("contraseña vacía debía fallar")
			}
			if eng == "mssql" {
				if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: uniq("e"), Password: "corta1A"}); err == nil {
					t.Error("contraseña débil debía fallar")
				}
			}
			// la base de sistema y el admin siguen intactos
			st, _ := a.TestConnection(sv.ID)
			if !st.Reachable {
				t.Errorf("el servidor dejó de responder: %+v", st)
			}
		})
	}
}

// Objetos que YA existen con nombres hostiles (creados fuera de la app) deben poder gestionarse sin inyección.
func TestWeirdExistingNamesAreManageable(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			id := uniq("w")
			dbName := `w]e'` + `ird` + id
			lgName := `l]o'g` + id
			if eng == "postgres" {
				dbName = `w"e'ird` + id
				lgName = `l"o'g` + id
			}
			mkDB(t, c, dbName, 2)
			pw := nastyPasswords[0]
			mkLoginRaw(t, c, lgName, pw)
			ctlDB := uniq("ctl") // base normal: no debe ser alterada
			mkDB(t, c, ctlDB, 1)

			dbs, err := a.ListDatabases(sv.ID)
			if err != nil || !hasDB(dbs, dbName) {
				t.Fatalf("ListDatabases no muestra %q: %v", dbName, err)
			}
			lgs, _ := a.ListLogins(sv.ID)
			if !hasLogin(lgs, lgName) {
				t.Fatalf("ListLogins no muestra %q", lgName)
			}
			m, err := a.GetPermissionMatrix(sv.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !contains(m.Databases, dbName) || !contains(m.Logins, lgName) {
				t.Fatalf("la matriz no incluye los objetos hostiles")
			}
			for _, lv := range []string{"readwrite", "readonly", "owner", "none", "readonly"} {
				if err := a.SetDatabaseAccess(sv.ID, lgName, dbName, lv); err != nil {
					t.Fatalf("SetDatabaseAccess %s: %v", lv, err)
				}
				m, _ := a.GetPermissionMatrix(sv.ID)
				if got := levelOf(m, lgName, dbName); got != lv {
					t.Errorf("nivel %s: la matriz dice %s", lv, got)
				}
			}
			if err := a.ChangePassword(sv.ID, lgName, nastyPasswords[1]); err != nil {
				t.Errorf("ChangePassword: %v", err)
			}
			if err := a.SetServerRoles(sv.ID, lgName, nil); err != nil {
				t.Errorf("SetServerRoles: %v", err)
			}
			// backup de una base con nombre hostil
			dest := t.TempDir()
			rec, err := a.CreateBackup(BackupRequest{ServerID: sv.ID, Database: dbName, DestDir: dest, RequestID: "weird"})
			if err != nil {
				t.Errorf("CreateBackup de base hostil: %v", err)
			} else if strings.ContainsAny(rec.FilePath[len(dest):], "]'\"") {
				t.Errorf("nombre de archivo de backup inseguro: %s", rec.FilePath)
			}
			if err := a.SetDatabaseAccess(sv.ID, lgName, dbName, "readwrite"); err != nil {
				t.Fatal(err)
			}
			if err := a.DropDatabase(sv.ID, dbName); err != nil {
				t.Errorf("DropDatabase hostil: %v", err)
			}
			if dbExistsRaw(t, c, dbName) {
				t.Error("la base hostil no se eliminó")
			}
			if !dbExistsRaw(t, c, ctlDB) {
				t.Error("la base de control desapareció")
			}
			if err := a.DropLogin(sv.ID, lgName); err != nil {
				t.Errorf("DropLogin hostil: %v", err)
			}
			if loginExistsRaw(t, c, lgName) {
				t.Error("el login hostil no se eliminó")
			}
		})
	}
}

// Los objetos de sistema no se pueden eliminar ni modificar (ni con otra capitalización).
func TestSystemObjectProtection(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			userDB := uniq("udb")
			mkDB(t, c, userDB, 1)

			sysDBs := []string{"master", "tempdb", "model", "msdb", "MASTER", "TempDB"}
			sysLogins := []string{"sa", "SA", "Sa"}
			if eng == "postgres" {
				sysDBs = []string{"postgres", "template0", "template1", "POSTGRES", "Template1"}
				sysLogins = []string{"postgres", "POSTGRES", "pg_monitor", "pg_read_all_data", "pg_write_all_data"}
			}
			for _, d := range sysDBs {
				if err := a.DropDatabase(sv.ID, d); err == nil {
					t.Errorf("DropDatabase(%s) debía fallar", d)
				}
				if err := a.CreateDatabase(sv.ID, d, ""); err == nil {
					t.Errorf("CreateDatabase(%s) debía fallar", d)
				}
				if err := a.SetDatabaseAccess(sv.ID, c.User, d, "none"); err == nil {
					t.Errorf("SetDatabaseAccess en %s debía fallar", d)
				}
			}
			for _, l := range sysLogins {
				if err := a.DropLogin(sv.ID, l); err == nil {
					t.Errorf("DropLogin(%s) debía fallar", l)
				}
				if err := a.ChangePassword(sv.ID, l, "Hack!Passw0rd1"); err == nil {
					t.Errorf("ChangePassword(%s) debía fallar", l)
				}
				if err := a.SetServerRoles(sv.ID, l, nil); err == nil {
					t.Errorf("SetServerRoles(%s) debía fallar", l)
				}
				if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: l, Password: "Hack!Passw0rd1"}); err == nil {
					t.Errorf("CreateLogin(%s) debía fallar", l)
				}
				if err := a.SetDatabaseAccess(sv.ID, l, userDB, "none"); err == nil {
					t.Errorf("SetDatabaseAccess del login %s debía fallar", l)
				}
			}
			// todos los logins marcados IsSystem son intocables
			lgs, _ := a.ListLogins(sv.ID)
			sysSeen := 0
			for _, l := range lgs {
				if !l.IsSystem {
					continue
				}
				sysSeen++
				if err := a.DropLogin(sv.ID, l.Name); err == nil {
					t.Errorf("DropLogin del login de sistema %q debía fallar", l.Name)
				}
				if err := a.SetServerRoles(sv.ID, l.Name, nil); err == nil {
					t.Errorf("SetServerRoles de %q debía fallar", l.Name)
				}
			}
			if sysSeen == 0 {
				t.Error("no hay logins marcados como sistema")
			}
			dbs, _ := a.ListDatabases(sv.ID)
			sysFlag := 0
			for _, d := range dbs {
				if d.System {
					sysFlag++
				}
				if d.Name == userDB && d.System {
					t.Error("una base de usuario aparece como de sistema")
				}
			}
			if sysFlag < 3 {
				t.Errorf("faltan bases de sistema marcadas: %d", sysFlag)
			}
			// el admin sigue autenticando con su contraseña original
			if err := admin(c, "").exec("SELECT 1"); err != nil {
				t.Fatalf("el admin dejó de funcionar: %v", err)
			}
			st, _ := a.TestConnection(sv.ID)
			if !st.Reachable {
				t.Fatalf("%+v", st)
			}
			// la matriz no incluye logins/bases de sistema
			m, _ := a.GetPermissionMatrix(sv.ID)
			for _, l := range m.Logins {
				if strings.EqualFold(l, c.User) {
					t.Errorf("la matriz incluye el admin %s", l)
				}
			}
			for _, d := range m.Databases {
				for _, s := range sysDBs {
					if d == s {
						t.Errorf("la matriz incluye la base de sistema %s", d)
					}
				}
			}
		})
	}
}

// BUG: el admin registrado (si no se llama sa/postgres) puede quitarse permisos o cambiarse la contraseña
// a sí mismo desde la app, dejando el servidor inutilizable o con credenciales guardadas obsoletas.
func TestAdminCanLockItselfOut(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			adm := uniq("adm")
			pw := strongPW()
			// admin alternativo creado con el admin crudo
			if eng == "postgres" {
				if err := admin(c, "").exec("CREATE ROLE " + adm + " LOGIN SUPERUSER PASSWORD " + qstr(eng, pw)); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := admin(c, "").exec("CREATE LOGIN " + adm + " WITH PASSWORD = " + qstr(eng, pw) + "; ALTER SERVER ROLE sysadmin ADD MEMBER " + adm); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { dropLoginRaw(c, adm) })
			sv, err := a.AddRemoteServer(RemoteServerRequest{Name: "alt-admin", Engine: eng, Host: "127.0.0.1", Port: c.Port, AdminUser: adm,
				AdminPassword: pw, TLS: eng == "mssql", TrustServerCert: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.ListDatabases(sv.ID); err != nil {
				t.Fatalf("el admin alternativo no conecta: %v", err)
			}
			// quitarse a sí mismo los permisos de administración
			errRoles := a.SetServerRoles(sv.ID, adm, nil)
			if errRoles == nil {
				t.Errorf("SetServerRoles sobre el propio admin conectado (%s) debería rechazarse (bloqueo del servidor)", adm)
			}
			// cambiarse la contraseña desincroniza la contraseña guardada
			errPw := a.ChangePassword(sv.ID, adm, strongPW())
			if errPw == nil {
				if _, err := a.ListDatabases(sv.ID); err != nil {
					t.Errorf("tras ChangePassword del propio admin, la app ya no puede conectarse (contraseña guardada obsoleta): %v", err)
				}
			}
		})
	}
}

// BUG (media): validate.Identifier permite 128 caracteres para SQL Server, pero CREATE DATABASE falla con
// un error interno críptico y en inglés ("Internal error. The string routine in file ... filemgr.cpp ... 0x8007007a")
// porque el nombre del archivo físico (<nombre>_log.ldf, ruta completa) excede el límite. La validación debería
// aceptar solo longitudes que realmente funcionan (o la app debería usar nombres de archivo cortos).
func TestMSSQLMaxDatabaseNameLength(t *testing.T) {
	c := sharedContainer(t, "mssql")
	a, _ := newTestApp(t)
	sv := importShared(t, a, c)
	maxOK := 0
	for n := 128; n >= 1; n-- {
		name := strings.Repeat("m", n-8) + uniq("x")[2:]
		name = name[:n]
		err := a.CreateDatabase(sv.ID, name, "")
		if err == nil {
			maxOK = n
			a.DropDatabase(sv.ID, name)
			break
		}
		a.DropDatabase(sv.ID, name)
	}
	t.Logf("longitud máxima de nombre que SQL Server acepta realmente: %d", maxOK)
	if maxOK < 128 {
		t.Errorf("validate.Identifier acepta hasta 128 caracteres pero SQL Server solo permite %d; el error es críptico", maxOK)
	}
}

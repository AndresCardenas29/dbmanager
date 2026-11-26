//go:build integration

package main

import (
	"strings"
	"testing"

	"dbmanager/internal/testutil"
)

var levels = []string{"none", "readonly", "readwrite", "owner"}

// Matriz: un login con acceso a varias bases, y cambios de nivel en todas las direcciones.
func TestPermissionMatrixMultiDB(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			d1, d2, d3 := uniq("pm1"), uniq("pm2"), uniq("pm3")
			for _, d := range []string{d1, d2, d3} {
				mkDB(t, c, d, 1)
			}
			l1, l2, l3 := uniq("pl1"), uniq("pl2"), uniq("pl3")
			pw1, pw2 := strongPW(), strongPW()
			for _, l := range []struct{ n, pw string }{{l1, pw1}, {l2, pw2}, {l3, strongPW()}} {
				if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: l.n, Password: l.pw}); err != nil {
					t.Fatal(err)
				}
				name := l.n
				t.Cleanup(func() { dropLoginRaw(c, name) })
			}
			set := func(l, d, lv string) {
				t.Helper()
				if err := a.SetDatabaseAccess(sv.ID, l, d, lv); err != nil {
					t.Fatalf("SetDatabaseAccess(%s,%s,%s): %v", l, d, lv, err)
				}
			}
			expect := func(want map[[2]string]string) {
				t.Helper()
				m, err := a.GetPermissionMatrix(sv.ID)
				if err != nil {
					t.Fatal(err)
				}
				for k, v := range want {
					if got := levelOf(m, k[0], k[1]); got != v {
						t.Errorf("matriz[%s,%s]=%s, esperaba %s", strings.TrimPrefix(k[0], "p"), k[1], got, v)
					}
				}
			}
			// l1 tiene acceso a varias bases con niveles distintos; l2 también; l3 a ninguna
			set(l1, d1, "owner")
			set(l1, d2, "readwrite")
			set(l1, d3, "readonly")
			set(l2, d1, "readonly")
			set(l2, d3, "readwrite")
			expect(map[[2]string]string{
				{l1, d1}: "owner", {l1, d2}: "readwrite", {l1, d3}: "readonly",
				{l2, d1}: "readonly", {l2, d2}: "none", {l2, d3}: "readwrite",
				{l3, d1}: "none", {l3, d2}: "none", {l3, d3}: "none",
			})
			// la matriz solo lista logins no-sistema
			m, _ := a.GetPermissionMatrix(sv.ID)
			for _, l := range []string{l1, l2, l3} {
				if !contains(m.Logins, l) {
					t.Errorf("la matriz no lista a %s", l)
				}
			}
			for _, d := range []string{d1, d2, d3} {
				if !contains(m.Databases, d) {
					t.Errorf("la matriz no lista a %s", d)
				}
			}
			// cambio entre todos los niveles en todas las direcciones sobre una base con un solo usuario
			d4 := uniq("pm4")
			mkDB(t, c, d4, 1)
			for _, from := range levels {
				for _, to := range levels {
					set(l3, d4, from)
					set(l3, d4, to)
					mm, _ := a.GetPermissionMatrix(sv.ID)
					if got := levelOf(mm, l3, d4); got != to {
						t.Errorf("%s -> %s: la matriz dice %s", from, to, got)
					}
				}
			}
			// idempotencia: repetir el mismo nivel no falla ni cambia nada
			set(l3, d4, "readwrite")
			set(l3, d4, "readwrite")
			mm, _ := a.GetPermissionMatrix(sv.ID)
			if got := levelOf(mm, l3, d4); got != "readwrite" {
				t.Errorf("idempotencia: %s", got)
			}
			// errores de entrada (en español)
			for name, err := range map[string]error{
				"nivel inválido":    a.SetDatabaseAccess(sv.ID, l3, d4, "admin"),
				"nivel vacío":       a.SetDatabaseAccess(sv.ID, l3, d4, ""),
				"nivel inyectado":   a.SetDatabaseAccess(sv.ID, l3, d4, "owner'; DROP"),
				"login inexistente": a.SetDatabaseAccess(sv.ID, "no_existe_"+uniq("x"), d4, "readonly"),
				"base inexistente":  a.SetDatabaseAccess(sv.ID, l3, "no_existe_"+uniq("x"), "readonly"),
			} {
				if err == nil {
					t.Errorf("%s: debía fallar", name)
				} else if !testutil.HasSpanish(err.Error()) {
					t.Errorf("%s: mensaje no está en español: %v", name, err)
				}
			}
		})
	}
}

// Verifica el efecto REAL de cada nivel conectándose como el usuario.
func TestPermissionLevelsEnforced(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			db := uniq("enf")
			mkDB(t, c, db, 2)
			lg, pw := uniq("enfu"), strongPW()
			if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: lg, Password: pw}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { dropLoginRaw(c, lg) })
			u := asUser(c, lg, pw, db)
			set := func(lv string) {
				t.Helper()
				if err := a.SetDatabaseAccess(sv.ID, lg, db, lv); err != nil {
					t.Fatal(err)
				}
			}
			// readonly
			set("readonly")
			if _, err := u.queryInt("SELECT COUNT(*) FROM t"); err != nil {
				t.Errorf("readonly: SELECT debía funcionar: %v", err)
			}
			if err := u.exec("INSERT INTO t VALUES (99, 'x')"); err == nil {
				t.Error("readonly: INSERT debía fallar")
			}
			// una tabla nueva creada por el admin después de otorgar el acceso también debe ser legible
			if err := admin(c, db).exec("CREATE TABLE t_nueva (id int)"); err != nil {
				t.Fatal(err)
			}
			if _, err := u.queryInt("SELECT COUNT(*) FROM t_nueva"); err != nil {
				t.Errorf("readonly: las tablas nuevas deberían ser legibles: %v", err)
			}
			// readwrite
			set("readwrite")
			if err := u.exec("INSERT INTO t VALUES (100, 'rw')"); err != nil {
				t.Errorf("readwrite: INSERT: %v", err)
			}
			if err := u.exec("UPDATE t SET v='z' WHERE id=100"); err != nil {
				t.Errorf("readwrite: UPDATE: %v", err)
			}
			if err := u.exec("DELETE FROM t WHERE id=100"); err != nil {
				t.Errorf("readwrite: DELETE: %v", err)
			}
			if err := u.exec("CREATE TABLE t_rw (id int)"); err == nil {
				t.Error("readwrite: CREATE TABLE no debería estar permitido")
				u.exec("DROP TABLE t_rw")
			}
			// owner
			set("owner")
			if err := u.exec("CREATE TABLE t_own (id int)"); err != nil {
				t.Errorf("owner: CREATE TABLE debía funcionar: %v", err)
			}
			// owner -> readonly: ya no puede crear ni escribir
			set("readonly")
			if err := u.exec("CREATE TABLE t_own2 (id int)"); err == nil {
				t.Error("tras bajar a readonly, CREATE TABLE no debía funcionar")
			}
			if err := u.exec("INSERT INTO t VALUES (101, 'x')"); err == nil {
				t.Error("tras bajar a readonly, INSERT no debía funcionar")
			}
			// none: no puede ni conectarse a la base
			set("none")
			if _, err := u.queryInt("SELECT COUNT(*) FROM t"); err == nil {
				t.Errorf("none: el usuario no debería poder leer datos")
			}
		})
	}
}

// BUG (media, PostgreSQL): el nivel "none" revoca los privilegios explícitos pero no impide CONNECT,
// porque PUBLIC tiene CONNECT por defecto en cada base nueva. El usuario "sin acceso" aún puede conectarse a la base
// (y ver el catálogo, el esquema public, etc.). La matriz sí dice "none".
func TestPG_NoneLevelStillAllowsConnect(t *testing.T) {
	c := sharedContainer(t, "postgres")
	a, _ := newTestApp(t)
	sv := importShared(t, a, c)
	db := uniq("nc")
	mkDB(t, c, db, 1)
	lg, pw := uniq("ncu"), strongPW()
	if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: lg, Password: pw}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dropLoginRaw(c, lg) })
	for _, lv := range []string{"readwrite", "none"} {
		if err := a.SetDatabaseAccess(sv.ID, lg, db, lv); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := a.GetPermissionMatrix(sv.ID)
	if levelOf(m, lg, db) != "none" {
		t.Fatalf("la matriz debería decir none: %s", levelOf(m, lg, db))
	}
	if err := asUser(c, lg, pw, db).exec("SELECT 1"); err == nil {
		t.Error("el usuario con nivel 'none' puede conectarse a la base (PUBLIC conserva CONNECT); debería requerirse REVOKE CONNECT ... FROM PUBLIC o equivalente")
	}
}

// BUG (media, PostgreSQL): ALTER DEFAULT PRIVILEGES se ejecuta como el admin, por lo que SOLO cubre tablas creadas por el admin.
// Las tablas creadas después por el propietario de la base (el caso normal de una aplicación) no son legibles
// para los usuarios readonly/readwrite. En SQL Server (roles db_datareader) sí funciona.
func TestPG_DefaultPrivilegesIgnoreTablesCreatedByOwner(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			owner, opw := uniq("dbo"), strongPW()
			reader, rpw := uniq("rd"), strongPW()
			for _, l := range []struct{ n, pw string }{{owner, opw}, {reader, rpw}} {
				if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: l.n, Password: l.pw}); err != nil {
					t.Fatal(err)
				}
				name := l.n
				t.Cleanup(func() { dropLoginRaw(c, name) })
			}
			db := uniq("own")
			if err := a.CreateDatabase(sv.ID, db, owner); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { dropDBRaw(c, db) })
			if err := a.SetDatabaseAccess(sv.ID, reader, db, "readonly"); err != nil {
				t.Fatal(err)
			}
			if err := asUser(c, owner, opw, db).exec("CREATE TABLE tabla_app (id int)"); err != nil {
				t.Fatalf("el propietario no puede crear tablas: %v", err)
			}
			if _, err := asUser(c, reader, rpw, db).queryInt("SELECT COUNT(*) FROM tabla_app"); err != nil {
				t.Errorf("el usuario readonly no puede leer una tabla creada después por el propietario: %v", err)
			}
		})
	}
}

// Quién es dueño: en PostgreSQL solo hay un propietario; promover a otro usuario "owner" degrada al anterior.
func TestPG_OwnerIsExclusive(t *testing.T) {
	c := sharedContainer(t, "postgres")
	a, _ := newTestApp(t)
	sv := importShared(t, a, c)
	db := uniq("ox")
	mkDB(t, c, db, 1)
	l1, l2 := uniq("o1"), uniq("o2")
	for _, l := range []string{l1, l2} {
		if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: l, Password: strongPW()}); err != nil {
			t.Fatal(err)
		}
		name := l
		t.Cleanup(func() { dropLoginRaw(c, name) })
	}
	a.SetDatabaseAccess(sv.ID, l1, db, "owner")
	a.SetDatabaseAccess(sv.ID, l2, db, "owner")
	m, _ := a.GetPermissionMatrix(sv.ID)
	owners := 0
	for _, l := range []string{l1, l2} {
		if levelOf(m, l, db) == "owner" {
			owners++
		}
	}
	t.Logf("l1=%s l2=%s (PostgreSQL solo admite un propietario)", levelOf(m, l1, db), levelOf(m, l2, db))
	if owners != 1 {
		t.Errorf("debe haber exactamente 1 propietario, hay %d", owners)
	}
	// bajar al propietario devuelve la propiedad al admin
	a.SetDatabaseAccess(sv.ID, l2, db, "readonly")
	dbs, _ := a.ListDatabases(sv.ID)
	for _, d := range dbs {
		if d.Name == db && d.Owner != c.User {
			t.Errorf("la propiedad debía volver al admin, propietario=%s", d.Owner)
		}
	}
}

// Eliminar una base a la que varios usuarios tienen acceso (con sesiones abiertas) y volver a crearla.
func TestDropDatabaseWithUsersAndOpenSessions(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			d1, d2 := uniq("dd1"), uniq("dd2")
			mkDB(t, c, d1, 1)
			mkDB(t, c, d2, 1)
			l1, pw1 := uniq("du1"), strongPW()
			l2, pw2 := uniq("du2"), strongPW()
			for _, l := range []struct{ n, pw string }{{l1, pw1}, {l2, pw2}} {
				if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: l.n, Password: l.pw}); err != nil {
					t.Fatal(err)
				}
				name := l.n
				t.Cleanup(func() { dropLoginRaw(c, name) })
			}
			for _, l := range []string{l1, l2} {
				a.SetDatabaseAccess(sv.ID, l, d1, "readwrite")
				a.SetDatabaseAccess(sv.ID, l, d2, "owner")
			}
			// sesión abierta de l1 en d2 durante el DROP
			sess := openSession(t, c, l1, pw1, d2)
			defer sess()

			if err := a.DropDatabase(sv.ID, d2); err != nil {
				t.Fatalf("DropDatabase con usuarios y sesión abierta: %v", err)
			}
			if dbExistsRaw(t, c, d2) {
				t.Fatal("la base sigue existiendo")
			}
			m, _ := a.GetPermissionMatrix(sv.ID)
			if contains(m.Databases, d2) {
				t.Error("la matriz aún lista la base eliminada")
			}
			lgs, _ := a.ListLogins(sv.ID)
			if !hasLogin(lgs, l1) || !hasLogin(lgs, l2) {
				t.Error("los logins debían sobrevivir a la eliminación de la base")
			}
			if got := levelOf(m, l1, d1); got != "readwrite" {
				t.Errorf("el acceso a la otra base cambió: %s", got)
			}
			// recrear con el mismo nombre: el acceso anterior no debe resucitar
			if err := a.CreateDatabase(sv.ID, d2, ""); err != nil {
				t.Fatalf("recrear: %v", err)
			}
			t.Cleanup(func() { dropDBRaw(c, d2) })
			m, _ = a.GetPermissionMatrix(sv.ID)
			if got := levelOf(m, l1, d2); got != "none" {
				t.Errorf("tras recrear la base, el acceso previo reapareció: %s", got)
			}
		})
	}
}

// DropLogin con accesos en varias bases y sesión abierta; el propietario de una base no puede eliminarse sin aviso claro.
func TestDropLoginCases(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			d1, d2 := uniq("dl1"), uniq("dl2")
			mkDB(t, c, d1, 1)
			mkDB(t, c, d2, 1)
			lg, pw := uniq("dlu"), strongPW()
			if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: lg, Password: pw}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { dropLoginRaw(c, lg) })
			a.SetDatabaseAccess(sv.ID, lg, d1, "readwrite")
			a.SetDatabaseAccess(sv.ID, lg, d2, "readonly")
			sess := openSession(t, c, lg, pw, d1)
			defer sess()
			if err := a.DropLogin(sv.ID, lg); err != nil {
				t.Fatalf("DropLogin con accesos y sesión abierta: %v", err)
			}
			if loginExistsRaw(t, c, lg) {
				t.Fatal("el login sigue existiendo")
			}
			// no deben quedar usuarios huérfanos en SQL Server
			if eng == "mssql" {
				for _, d := range []string{d1, d2} {
					n, err := admin(c, d).queryInt("SELECT COUNT(*) FROM sys.database_principals WHERE name = " + qstr(eng, lg))
					if err != nil || n != 0 {
						t.Errorf("usuario huérfano en %s: n=%d err=%v", d, n, err)
					}
				}
			}
			// quitar al inexistente
			if err := a.DropLogin(sv.ID, lg); err == nil {
				t.Error("eliminar un login inexistente debía fallar")
			}
			// propietario de una base
			own := uniq("dlo")
			if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: own, Password: strongPW()}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { dropLoginRaw(c, own) })
			ownedDB := uniq("dlod")
			if err := a.CreateDatabase(sv.ID, ownedDB, own); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { dropDBRaw(c, ownedDB) })
			err := a.DropLogin(sv.ID, own)
			if err == nil {
				t.Fatal("eliminar al propietario de una base debía fallar")
			}
			if !loginExistsRaw(t, c, own) {
				t.Error("el login propietario se eliminó")
			}
			if testutil.LooksEnglish(err.Error()) && !strings.Contains(err.Error(), "propietario") {
				t.Errorf("BUG (baja): mensaje en inglés / sin guía al eliminar al propietario de una base: %v", err)
			}
			if err := a.DropDatabase(sv.ID, ownedDB); err != nil {
				t.Fatal(err)
			}
			if err := a.DropLogin(sv.ID, own); err != nil {
				t.Errorf("tras eliminar la base, DropLogin: %v", err)
			}
		})
	}
}

// Roles de servidor: ida y vuelta.
func TestServerRolesRoundTrip(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			r1, r2 := "CREATEDB", "CREATEROLE"
			if eng == "mssql" {
				r1, r2 = "dbcreator", "securityadmin"
			}
			lg := uniq("sr")
			if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: lg, Password: strongPW(), ServerRoles: []string{r1}}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { dropLoginRaw(c, lg) })
			rolesOf := func() []string {
				lgs, _ := a.ListLogins(sv.ID)
				for _, l := range lgs {
					if l.Name == lg {
						return l.ServerRoles
					}
				}
				return nil
			}
			if r := rolesOf(); len(r) != 1 || !strings.EqualFold(r[0], r1) {
				t.Errorf("roles tras crear: %v", r)
			}
			if err := a.SetServerRoles(sv.ID, lg, []string{r1, r2}); err != nil {
				t.Fatal(err)
			}
			if r := rolesOf(); len(r) != 2 {
				t.Errorf("roles tras ampliar: %v", r)
			}
			if err := a.SetServerRoles(sv.ID, lg, []string{r2}); err != nil {
				t.Fatal(err)
			}
			if r := rolesOf(); len(r) != 1 || !strings.EqualFold(r[0], r2) {
				t.Errorf("roles tras reducir: %v", r)
			}
			if err := a.SetServerRoles(sv.ID, lg, nil); err != nil {
				t.Fatal(err)
			}
			if r := rolesOf(); len(r) != 0 {
				t.Errorf("roles tras vaciar: %v", r)
			}
			if err := a.SetServerRoles(sv.ID, lg, []string{"nope; DROP"}); err == nil {
				t.Error("rol inválido debía fallar")
			}
			if err := a.SetServerRoles(sv.ID, "no_existe_"+uniq("x"), nil); err == nil {
				t.Error("login inexistente debía fallar")
			}
			// un login sin roles aparece con slice vacío (no null) en JSON
			lgs, _ := a.ListLogins(sv.ID)
			if strings.Contains(mustJSON(t, lgs), `"serverRoles":null`) {
				t.Error("serverRoles no debe ser null")
			}
		})
	}
}

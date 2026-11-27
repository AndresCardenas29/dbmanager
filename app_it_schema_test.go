//go:build integration

package main

import (
	"sort"
	"strings"
	"testing"
)

const strongPw = "Str0ng!Pass#2026"

func mustOK(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func canDo(t *testing.T, r rawDB, ok bool, stmt string) {
	t.Helper()
	err := r.exec(stmt)
	if ok && err != nil {
		t.Errorf("%s debería poder: %s -> %v", r.user, stmt, err)
	}
	if !ok && err == nil {
		t.Errorf("%s NO debería poder: %s", r.user, stmt)
	}
}

func TestSchemaPermissions(t *testing.T) {
	for _, eng := range engines {
		eng := eng
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			db := uniq("sch")
			mustOK(t, "CreateDatabase", a.CreateDatabase(sv.ID, db, ""))
			t.Cleanup(func() { dropDBRaw(c, db) })
			su := admin(c, db)
			for _, s := range []string{"marketdata", "clients", "audit", "payments"} {
				mustOK(t, "CreateSchema "+s, a.CreateSchema(sv.ID, db, s))
			}
			if err := a.CreateSchema(sv.ID, db, "audit"); err == nil {
				t.Error("crear esquema duplicado debería fallar")
			}
			for _, s := range []string{
				"CREATE TABLE marketdata.rates(id int, rate decimal(10,2))",
				"CREATE TABLE clients.clients(id int, name varchar(50))",
				"CREATE TABLE clients.secrets(id int, x varchar(50))",
				"CREATE TABLE audit.log(id int, msg varchar(100))",
				"CREATE TABLE payments.payments(id int, client_id int, amount decimal(10,2))",
			} {
				mustOK(t, s, su.exec(s))
			}
			schemas, err := a.ListSchemas(sv.ID, db)
			mustOK(t, "ListSchemas", err)
			found := map[string][]string{}
			for _, s := range schemas {
				found[s.Name] = s.Tables
			}
			if len(found["clients"]) != 2 || len(found["audit"]) != 1 {
				t.Errorf("ListSchemas: %+v", found)
			}

			names := map[string]string{}
			for _, n := range []string{"clients_svc", "marketdata_svc", "audit_svc", "payments_svc"} {
				names[n] = uniq(n)
				nn := names[n]
				mustOK(t, "CreateDatabaseUser "+n, a.CreateDatabaseUser(sv.ID, db, CreateLoginRequest{Name: nn, Password: strongPw}, "custom"))
				t.Cleanup(func() { dropLoginRaw(c, nn) })
			}
			g := func(user, schema, table string, p ...string) {
				t.Helper()
				mustOK(t, "SetObjectGrant", a.SetObjectGrant(sv.ID, db, ObjectGrantRequest{Login: names[user], Schema: schema, Table: table, Privileges: p}))
			}
			dml := []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
			g("clients_svc", "clients", "", dml...)
			g("clients_svc", "audit", "", "INSERT")
			g("marketdata_svc", "marketdata", "", dml...)
			g("marketdata_svc", "audit", "", "INSERT")
			g("audit_svc", "audit", "", "SELECT")
			g("payments_svc", "payments", "", dml...)
			g("payments_svc", "marketdata", "", "SELECT")
			g("payments_svc", "clients", "clients", "SELECT")
			g("payments_svc", "audit", "", "INSERT")

			as := func(n string) rawDB { return asUser(c, names[n], strongPw, db) }
			sel := func(tb string) string { return "SELECT * FROM " + tb }
			// clients_svc
			cs := as("clients_svc")
			canDo(t, cs, true, sel("clients.clients"))
			canDo(t, cs, true, "INSERT INTO clients.clients(id,name) VALUES (1,'a')")
			canDo(t, cs, true, "INSERT INTO audit.log(id,msg) VALUES (1,'x')")
			canDo(t, cs, false, sel("audit.log"))
			canDo(t, cs, false, sel("marketdata.rates"))
			canDo(t, cs, false, "INSERT INTO marketdata.rates(id,rate) VALUES (1,1)")
			// audit_svc
			au := as("audit_svc")
			canDo(t, au, true, sel("audit.log"))
			canDo(t, au, false, "INSERT INTO audit.log(id,msg) VALUES (2,'y')")
			canDo(t, au, false, sel("marketdata.rates"))
			canDo(t, au, false, sel("clients.clients"))
			// payments_svc
			ps := as("payments_svc")
			canDo(t, ps, true, sel("marketdata.rates"))
			canDo(t, ps, false, "INSERT INTO marketdata.rates(id,rate) VALUES (1,1)")
			canDo(t, ps, true, sel("clients.clients"))
			canDo(t, ps, false, sel("clients.secrets"))
			canDo(t, ps, false, "UPDATE clients.clients SET name='z'")
			canDo(t, ps, true, "INSERT INTO audit.log(id,msg) VALUES (3,'p')")
			canDo(t, ps, true, "INSERT INTO payments.payments(id,client_id,amount) VALUES (1,1,5)")
			canDo(t, ps, true, "UPDATE payments.payments SET amount=6")
			canDo(t, ps, true, sel("payments.payments"))
			canDo(t, ps, true, "DELETE FROM payments.payments")
			// marketdata_svc
			ms := as("marketdata_svc")
			canDo(t, ms, true, "INSERT INTO marketdata.rates(id,rate) VALUES (1,1)")
			canDo(t, ms, true, sel("marketdata.rates"))
			canDo(t, ms, false, sel("clients.clients"))
			canDo(t, ms, false, "INSERT INTO clients.clients(id,name) VALUES (9,'q')")
			canDo(t, ms, false, sel("payments.payments"))

			// ListObjectGrants
			grants, err := a.ListObjectGrants(sv.ID, db)
			mustOK(t, "ListObjectGrants", err)
			key := func(l, s, tb string) string { return l + "|" + s + "|" + tb }
			gm := map[string]string{}
			for _, x := range grants {
				p := append([]string{}, x.Privileges...)
				sort.Strings(p)
				gm[key(x.Login, x.Schema, x.Table)] = strings.Join(p, ",")
			}
			wantG := map[string]string{
				key(names["clients_svc"], "clients", ""):         "DELETE,INSERT,SELECT,UPDATE",
				key(names["clients_svc"], "audit", ""):           "INSERT",
				key(names["audit_svc"], "audit", ""):             "SELECT",
				key(names["payments_svc"], "clients", "clients"): "SELECT",
				key(names["payments_svc"], "marketdata", ""):     "SELECT",
				key(names["payments_svc"], "payments", ""):       "DELETE,INSERT,SELECT,UPDATE",
			}
			for k, v := range wantG {
				if gm[k] != v {
					t.Errorf("grant %s = %q, se esperaba %q (todos: %v)", k, gm[k], v, gm)
				}
			}
			if _, bad := gm[key(names["payments_svc"], "clients", "")]; bad {
				t.Error("payments_svc no debería tener grant a nivel de esquema clients")
			}

			// Matriz
			m, err := a.GetPermissionMatrix(sv.ID)
			mustOK(t, "matrix", err)
			for _, cell := range m.Cells {
				for _, n := range names {
					if cell.Login != n {
						continue
					}
					want := "none"
					if cell.Database == db {
						want = "custom"
					}
					if cell.Level != want {
						t.Errorf("matriz %s en %s = %s, se esperaba %s", n, cell.Database, cell.Level, want)
					}
				}
			}

			// ListConnectionUsers
			cu, err := a.ListConnectionUsers(sv.ID, db)
			mustOK(t, "ListConnectionUsers", err)
			if len(cu) == 0 || !cu[0].IsRoot {
				t.Fatalf("root debería ir primero: %+v", cu)
			}
			have := map[string]ConnectionUser{}
			for _, u := range cu {
				have[u.Login] = u
			}
			for _, n := range names {
				if u, ok := have[n]; !ok || !u.HasPassword || u.IsRoot {
					t.Errorf("ConnectionUser %s: %+v", n, u)
				}
			}
			if pw, err := a.GetLoginPassword(sv.ID, names["audit_svc"]); err != nil || pw != strongPw {
				t.Errorf("GetLoginPassword = %q, %v", pw, err)
			}
			if pw, err := a.GetLoginPassword(sv.ID, c.User); err != nil || pw != c.Password {
				t.Errorf("GetLoginPassword root = %q, %v", pw, err)
			}

			// Revocar
			g("clients_svc", "audit", "")
			canDo(t, cs, false, "INSERT INTO audit.log(id,msg) VALUES (4,'x')")
			canDo(t, cs, true, sel("clients.clients"))

			// ALTER a nivel de esquema
			g("marketdata_svc", "marketdata", "", append(append([]string{}, dml...), "ALTER")...)
			canDo(t, ms, true, "CREATE TABLE marketdata.newt(id int)")
			canDo(t, ms, false, "CREATE TABLE clients.newt(id int)")

			// RememberLoginPassword
			raw, rawPw := uniq("rawu"), strongPw
			mkLoginRaw(t, c, raw, rawPw)
			if err := a.RememberLoginPassword(sv.ID, raw, "Wrong!Pass#999"); err == nil {
				t.Error("contraseña incorrecta debería fallar")
			}
			if pw, _ := a.GetLoginPassword(sv.ID, raw); pw != "" {
				t.Error("no debería haberse guardado")
			}
			mustOK(t, "Remember", a.RememberLoginPassword(sv.ID, raw, rawPw))
			if pw, _ := a.GetLoginPassword(sv.ID, raw); pw != rawPw {
				t.Errorf("recordada = %q", pw)
			}

			// DropLogin con grants de esquema
			mustOK(t, "DropLogin", a.DropLogin(sv.ID, names["payments_svc"]))
			if pw, _ := a.GetLoginPassword(sv.ID, names["payments_svc"]); pw != "" {
				t.Error("el secreto debería eliminarse al borrar el login")
			}
			canDo(t, ps, false, sel("marketdata.rates"))
			grants, _ = a.ListObjectGrants(sv.ID, db)
			for _, x := range grants {
				if x.Login == names["payments_svc"] {
					t.Errorf("quedó grant de login eliminado: %+v", x)
				}
			}

			// Acceso none
			mustOK(t, "none", a.SetDatabaseAccess(sv.ID, names["audit_svc"], db, "none"))
			canDo(t, au, false, sel("audit.log"))
			mustOK(t, "none", a.SetDatabaseAccess(sv.ID, names["marketdata_svc"], db, "none"))
			canDo(t, ms, false, sel("marketdata.rates"))
			canDo(t, ms, false, "CREATE TABLE marketdata.newt2(id int)")
			grants, _ = a.ListObjectGrants(sv.ID, db)
			for _, x := range grants {
				if x.Login == names["audit_svc"] || x.Login == names["marketdata_svc"] {
					t.Errorf("none debería quitar grants: %+v", x)
				}
			}
			m, _ = a.GetPermissionMatrix(sv.ID)
			for _, cell := range m.Cells {
				if cell.Database == db && (cell.Login == names["audit_svc"] || cell.Login == names["marketdata_svc"]) && cell.Level != "none" {
					t.Errorf("tras none: %s = %s", cell.Login, cell.Level)
				}
			}
		})
	}
}

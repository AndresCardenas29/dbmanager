package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"dbmanager/internal/testutil"
)

// Tipos que viajan al frontend como RESPUESTA: ninguno puede exponer secretos.
func TestResponseTypesNeverExposeSecrets(t *testing.T) {
	responses := []any{EnvStatus{}, Server{}, ContainerInfo{}, ServerStatus{}, Database{}, Login{}, PermissionMatrix{},
		PermissionCell{}, BackupRecord{}, BackupInfo{}, BackupFile{}, RestoreRecord{}, Operation{}, ProgressEvent{}}
	var walk func(rt reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		for rt.Kind() == reflect.Slice || rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Map {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			low := strings.ToLower(f.Name + " " + f.Tag.Get("json"))
			for _, bad := range []string{"password", "passwd", "secret", "pwd", "token", "apikey", "key"} {
				// "hasPassword" es el booleano permitido
				if strings.Contains(low, bad) && f.Name != "HasPassword" {
					t.Errorf("%s.%s expone un campo sensible (%s)", path, f.Name, f.Tag.Get("json"))
				}
			}
			if f.Name == "HasPassword" && f.Type.Kind() != reflect.Bool {
				t.Errorf("HasPassword debe ser bool, es %s", f.Type)
			}
			walk(f.Type, path+"."+f.Name)
		}
	}
	for _, r := range responses {
		walk(reflect.TypeOf(r), reflect.TypeOf(r).Name())
	}
}

// Todo campo JSON debe estar en camelCase (contrato con el frontend).
func TestJSONTagsCamelCase(t *testing.T) {
	all := []any{EnvStatus{}, Server{}, CreateServiceRequest{}, ImportContainerRequest{}, RemoteServerRequest{}, UpdateServerRequest{},
		ContainerInfo{}, ServerStatus{}, Database{}, Login{}, CreateLoginRequest{}, PermissionCell{}, PermissionMatrix{}, FileFilter{},
		BackupRequest{}, BackupRecord{}, BackupFile{}, BackupInfo{}, RestoreRequest{}, RestoreRecord{}, Operation{}, ProgressEvent{}}
	for _, v := range all {
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "" || tag == "-" {
				t.Errorf("%s.%s sin tag json", rt.Name(), f.Name)
				continue
			}
			if tag[0] < 'a' || tag[0] > 'z' || strings.ContainsAny(tag, "_- ") {
				t.Errorf("%s.%s: tag %q no es camelCase", rt.Name(), f.Name, tag)
			}
		}
	}
}

func TestServersNeverLeakPasswordInJSON(t *testing.T) {
	a, _ := newTestApp(t)
	const pw = "Zz9!SuperSecretPw#1"
	sv, err := a.AddRemoteServer(RemoteServerRequest{Name: "r1", Engine: "postgres", Host: "127.0.0.1", Port: 1, AdminUser: "u", AdminPassword: pw})
	if err != nil {
		t.Fatal(err)
	}
	if !sv.HasPassword {
		t.Error("HasPassword debe ser true")
	}
	list, _ := a.ListServers()
	got, _ := a.GetServer(sv.ID)
	up, _ := a.UpdateServer(sv.ID, UpdateServerRequest{Name: "r1b", Host: "127.0.0.1", Port: 2, AdminUser: "u", AdminPassword: pw + "x"})
	ops, _ := a.ListOperations(50)
	set, _ := a.GetSettings()
	for what, v := range map[string]any{"AddRemoteServer": sv, "ListServers": list, "GetServer": got, "UpdateServer": up, "ListOperations": ops, "GetSettings": set} {
		noSecret(t, what, mustJSON(t, v), pw, pw+"x")
	}
	// y la contraseña quedó guardada cifrada y recuperable por el store
	st, _ := a.db()
	s2, _ := st.GetServer(sv.ID)
	if dec, _ := st.ServerPassword(s2); dec != pw+"x" {
		t.Errorf("contraseña no actualizada: %q", dec)
	}
	if s2.AdminPasswordEnc == pw+"x" || strings.Contains(s2.AdminPasswordEnc, pw) {
		t.Error("contraseña sin cifrar en el store")
	}
}

func TestAddRemoteServerValidationSpanish(t *testing.T) {
	a, _ := newTestApp(t)
	ok := RemoteServerRequest{Name: "n", Engine: "mssql", Host: "h", Port: 1433, AdminUser: "sa", AdminPassword: "x"}
	mut := func(f func(r *RemoteServerRequest)) RemoteServerRequest { r := ok; f(&r); return r }
	bad := map[string]RemoteServerRequest{
		"sin nombre":    mut(func(r *RemoteServerRequest) { r.Name = "  " }),
		"motor":         mut(func(r *RemoteServerRequest) { r.Engine = "oracle" }),
		"sin host":      mut(func(r *RemoteServerRequest) { r.Host = " " }),
		"puerto cero":   mut(func(r *RemoteServerRequest) { r.Port = 0 }),
		"puerto grande": mut(func(r *RemoteServerRequest) { r.Port = 65536 }),
		"sin usuario":   mut(func(r *RemoteServerRequest) { r.AdminUser = "" }),
	}
	for name, r := range bad {
		_, err := a.AddRemoteServer(r)
		if err == nil {
			t.Errorf("%s: debía fallar", name)
			continue
		}
		if testutil.LooksEnglish(err.Error()) || !testutil.HasSpanish(err.Error()) {
			t.Errorf("%s: mensaje no está en español: %v", name, err)
		}
	}
	if l, _ := a.ListServers(); len(l) != 0 {
		t.Fatalf("no debe guardarse nada: %+v", l)
	}
	if _, err := a.AddRemoteServer(ok); err != nil {
		t.Fatal(err)
	}
	_, err := a.AddRemoteServer(ok)
	if err == nil || !strings.Contains(err.Error(), "ya existe") {
		t.Fatalf("duplicado: %v", err)
	}
	// Se recorta el espacio en nombre/host
	s, err := a.AddRemoteServer(mut(func(r *RemoteServerRequest) { r.Name = "  trim  "; r.Host = " h2 " }))
	if err != nil || s.Name != "trim" || s.Host != "h2" {
		t.Fatalf("%v %+v", err, s)
	}
}

// Documenta la inconsistencia: AddRemoteServer permite nombres que difieren solo en mayúsculas,
// mientras CreateLocal (EqualFold) los rechaza.
func TestServerNameCaseSensitivityInconsistent(t *testing.T) {
	a, _ := newTestApp(t)
	r := RemoteServerRequest{Name: "Produccion", Engine: "postgres", Host: "h", Port: 5432, AdminUser: "u"}
	if _, err := a.AddRemoteServer(r); err != nil {
		t.Fatal(err)
	}
	r.Name = "produccion"
	if _, err := a.AddRemoteServer(r); err == nil {
		t.Error("AddRemoteServer acepta 'produccion' aunque existe 'Produccion' (CreateLocal/Import lo rechazan con EqualFold)")
	}
}

func TestUpdateServer(t *testing.T) {
	a, _ := newTestApp(t)
	sv, _ := a.AddRemoteServer(RemoteServerRequest{Name: "a", Engine: "postgres", Host: "h", Port: 5432, AdminUser: "u", AdminPassword: "orig"})
	other, _ := a.AddRemoteServer(RemoteServerRequest{Name: "b", Engine: "postgres", Host: "h", Port: 5432, AdminUser: "u"})
	st, _ := a.db()

	// contraseña vacía = conservar
	up, err := a.UpdateServer(sv.ID, UpdateServerRequest{Name: "a2", Host: "h2", Port: 6000, AdminUser: "u2", TLS: true, TrustServerCert: true})
	if err != nil || up.Name != "a2" || up.Host != "h2" || up.Port != 6000 || up.AdminUser != "u2" || !up.TLS || !up.TrustServerCert || !up.HasPassword {
		t.Fatalf("%v %+v", err, up)
	}
	raw, _ := st.GetServer(sv.ID)
	if pw, _ := st.ServerPassword(raw); pw != "orig" {
		t.Fatalf("la contraseña debía conservarse, es %q", pw)
	}
	// errores
	if _, err := a.UpdateServer(sv.ID, UpdateServerRequest{Name: "", Host: "h", Port: 1}); err == nil {
		t.Error("nombre vacío debe fallar")
	}
	if _, err := a.UpdateServer(sv.ID, UpdateServerRequest{Name: "x", Host: "", Port: 1}); err == nil {
		t.Error("host vacío debe fallar")
	}
	if _, err := a.UpdateServer(sv.ID, UpdateServerRequest{Name: "x", Host: "h", Port: 0}); err == nil {
		t.Error("puerto 0 debe fallar")
	}
	if _, err := a.UpdateServer(sv.ID, UpdateServerRequest{Name: other.Name, Host: "h", Port: 1}); err == nil || !strings.Contains(err.Error(), "ya existe") {
		t.Errorf("nombre duplicado: %v", err)
	}
	if _, err := a.UpdateServer(9999, UpdateServerRequest{Name: "x"}); err == nil || err.Error() != "el servidor no existe" {
		t.Errorf("inexistente: %v", err)
	}
	// el nombre no puede cambiar el motor ni el modo
	got, _ := a.GetServer(sv.ID)
	if got.Engine != "postgres" || got.Mode != "remote" {
		t.Errorf("%+v", got)
	}
}

func TestServerNotFoundMessagesSpanish(t *testing.T) {
	a, _ := newTestApp(t)
	for name, err := range map[string]error{
		"GetServer":    func() error { _, e := a.GetServer(42); return e }(),
		"DeleteServer": a.DeleteServer(42, true, true),
		"StartServer":  a.StartServer(42),
		"StopServer":   a.StopServer(42),
		"Restart":      a.RestartServer(42),
		"Status":       func() error { _, e := a.ServerStatus(42); return e }(),
		"Logs":         func() error { _, e := a.ServerLogs(42, 10); return e }(),
		"TestConn":     func() error { _, e := a.TestConnection(42); return e }(),
		"ListDBs":      func() error { _, e := a.ListDatabases(42); return e }(),
		"CreateDB":     a.CreateDatabase(42, "x", ""),
		"DropDB":       a.DropDatabase(42, "x"),
		"ListLogins":   func() error { _, e := a.ListLogins(42); return e }(),
		"Matrix":       func() error { _, e := a.GetPermissionMatrix(42); return e }(),
		"Backup": func() error {
			_, e := a.CreateBackup(BackupRequest{ServerID: 42, Database: "x", DestDir: "C:\\x"})
			return e
		}(),
		"Restore": func() error { _, e := a.RestoreBackup(RestoreRequest{ServerID: 42}); return e }(),
		"Inspect": func() error { _, e := a.InspectBackup(42, "x"); return e }(),
	} {
		if err == nil {
			t.Errorf("%s: debía fallar", name)
			continue
		}
		if err.Error() != "el servidor no existe" {
			t.Errorf("%s: mensaje %q", name, err)
		}
	}
}

func TestRemoteServerContainerOpsRejected(t *testing.T) {
	a, _ := newTestApp(t)
	sv, _ := a.AddRemoteServer(RemoteServerRequest{Name: "r", Engine: "postgres", Host: "h", Port: 5432, AdminUser: "u"})
	for name, err := range map[string]error{"start": a.StartServer(sv.ID), "stop": a.StopServer(sv.ID), "restart": a.RestartServer(sv.ID),
		"logs": func() error { _, e := a.ServerLogs(sv.ID, 1); return e }()} {
		if err == nil || !strings.Contains(err.Error(), "solo aplica a servicios locales") {
			t.Errorf("%s: %v", name, err)
		}
	}
	st, err := a.ServerStatus(sv.ID)
	if err != nil || st.State != "remote" {
		t.Fatalf("%+v %v", st, err)
	}
	// DeleteServer remoto con removeContainer=true no toca Docker y borra el registro
	if err := a.DeleteServer(sv.ID, true, true); err != nil {
		t.Fatal(err)
	}
	if l, _ := a.ListServers(); len(l) != 0 {
		t.Fatal("debía borrarse")
	}
}

// Conexiones fallidas: el mensaje debe ser útil, en lo posible en español, y NUNCA contener la contraseña.
func TestConnectionErrorsDoNotLeakPassword(t *testing.T) {
	const pw = "Lk!9secret'; DROP--zz@host/x"
	for _, eng := range []string{"postgres", "mssql"} {
		for _, host := range []string{"127.0.0.1", "bad host name", "ex ample.invalid", "::1"} {
			t.Run(eng+"/"+host, func(t *testing.T) {
				a, _ := newTestApp(t)
				req := RemoteServerRequest{Name: "r", Engine: eng, Host: host, Port: 1, AdminUser: "adm", AdminPassword: pw}
				st, err := a.TestRemoteConnection(req)
				if err != nil {
					noSecret(t, "TestRemoteConnection err", err.Error(), pw)
				}
				noSecret(t, "TestRemoteConnection msg", mustJSON(t, st), pw, "DROP--zz")
				if st.Reachable {
					t.Error("no debe estar accesible")
				}
				sv, err := a.AddRemoteServer(req)
				if err != nil {
					t.Fatal(err)
				}
				_, err = a.ListDatabases(sv.ID)
				if err == nil {
					t.Fatal("debía fallar")
				}
				noSecret(t, "ListDatabases err", err.Error(), pw, "DROP--zz")
				err = a.CreateDatabase(sv.ID, "x", "")
				if err == nil {
					t.Fatal("debía fallar")
				}
				noSecret(t, "CreateDatabase err", err.Error(), pw, "DROP--zz")
				ts, _ := a.TestConnection(sv.ID)
				noSecret(t, "TestConnection", mustJSON(t, ts), pw, "DROP--zz")
				ops, _ := a.ListOperations(20)
				noSecret(t, "operaciones", mustJSON(t, ops), pw, "DROP--zz")
			})
		}
	}
}

func TestTestRemoteConnectionValidation(t *testing.T) {
	a, _ := newTestApp(t)
	for name, r := range map[string]RemoteServerRequest{
		"motor": {Engine: "x", Host: "h", Port: 1}, "host": {Engine: "mssql", Host: " ", Port: 1}, "puerto": {Engine: "mssql", Host: "h", Port: 99999},
	} {
		if _, err := a.TestRemoteConnection(r); err == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
	// no guarda nada ni registra operaciones
	a.TestRemoteConnection(RemoteServerRequest{Engine: "postgres", Host: "127.0.0.1", Port: 1, AdminUser: "u"})
	if l, _ := a.ListServers(); len(l) != 0 {
		t.Error("TestRemoteConnection no debe guardar")
	}
	if ops, _ := a.ListOperations(10); len(ops) != 0 {
		t.Errorf("TestRemoteConnection no debe registrar operaciones: %+v", ops)
	}
}

func TestSettings(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.SetSetting("  ", "x"); err == nil {
		t.Error("clave vacía debe fallar")
	}
	if err := a.SetSetting("theme", "dark"); err != nil {
		t.Fatal(err)
	}
	m, _ := a.GetSettings()
	if m["theme"] != "dark" {
		t.Fatal(m)
	}
}

func TestAppWithoutStartupFailsGracefully(t *testing.T) {
	a := NewApp() // sin startup: sin store
	if _, err := a.ListServers(); err == nil || !strings.Contains(err.Error(), "almacenamiento interno") {
		t.Fatalf("%v", err)
	}
}

// Escanea el código de producción: los mensajes de error construidos por la app deben estar en español.
func TestProductionErrorStringsAreSpanish(t *testing.T) {
	fset := token.NewFileSet()
	var offenders []string
	check := func(path string, f *ast.File) {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || !((pkg.Name == "errors" && sel.Sel.Name == "New") || (pkg.Name == "fmt" && sel.Sel.Name == "Errorf")) {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, _ := strconv.Unquote(lit.Value)
			if testutil.LooksEnglish(s) {
				offenders = append(offenders, fset.Position(lit.Pos()).String()+": "+s)
			}
			return true
		})
	}
	root := "."
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "frontend", "node_modules", "build", "testutil", ".git", "third_party":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err == nil {
			check(p, f)
		}
		return nil
	})
	for _, o := range offenders {
		t.Errorf("mensaje de error en inglés: %s", o)
	}
}

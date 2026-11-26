//go:build integration

package main

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbmanager/internal/services"
	"dbmanager/internal/testutil"
)

func ext(eng string) string {
	if eng == "mssql" {
		return ".bak"
	}
	return ".dump"
}

func checkEvents(t *testing.T, evs []services.Progress, rid string, id int64) {
	t.Helper()
	if len(evs) < 3 {
		t.Errorf("se esperaban >=3 eventos de progreso, hubo %d", len(evs))
		return
	}
	last := -1
	for i, e := range evs {
		if e.RequestID != rid {
			t.Errorf("evento %d sin requestId %q: %+v", i, rid, e)
		}
		if id != 0 && e.ID != id {
			t.Errorf("evento %d con id %d, esperaba %d", i, e.ID, id)
		}
		if e.Percent < last {
			t.Errorf("el porcentaje retrocede en el evento %d: %d -> %d", i, last, e.Percent)
		}
		last = e.Percent
	}
	f := evs[len(evs)-1]
	if !f.Done || f.Error || f.Percent != 100 {
		t.Errorf("último evento inesperado: %+v", f)
	}
}

func randomFile(t *testing.T, name string, n int) string {
	p := filepath.Join(t.TempDir(), name)
	b := make([]byte, n)
	rand.Read(b)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func lastRestore(t *testing.T, a *App, serverID int64) RestoreRecord {
	t.Helper()
	l, err := a.ListRestores(serverID)
	if err != nil || len(l) == 0 {
		t.Fatalf("ListRestores: %v %v", l, err)
	}
	return l[0]
}

func mustBackup(t *testing.T, a *App, sv Server, db, rid string) BackupRecord {
	t.Helper()
	rec, err := a.CreateBackup(BackupRequest{ServerID: sv.ID, Database: db, DestDir: t.TempDir(), RequestID: rid})
	if err != nil {
		t.Fatalf("CreateBackup(%s): %v", db, err)
	}
	if rec.FilePath == "" {
		t.Fatalf("sin ruta: %+v", rec)
	}
	return rec
}

// Ciclo completo backup -> inspect -> restore (nuevo nombre) -> restore sin replace falla -> restore con replace.
func TestBackupRestoreRoundTrip(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, evlog := newTestApp(t)
			sv := importShared(t, a, c)
			src := uniq("bk")
			mkDB(t, c, src, 3)

			dest := filepath.Join(t.TempDir(), "carpeta", "nueva")
			rid := "bk-" + eng
			rec, err := a.CreateBackup(BackupRequest{ServerID: sv.ID, Database: src, DestDir: dest, RequestID: rid})
			if err != nil {
				t.Fatalf("CreateBackup: %v", err)
			}
			if rec.Status != "ok" || rec.SizeBytes <= 0 || rec.Database != src || rec.ServerName != sv.Name || rec.CreatedAt == "" {
				t.Errorf("registro: %+v", rec)
			}
			if filepath.Dir(rec.FilePath) != dest || !strings.HasSuffix(rec.FilePath, ext(eng)) {
				t.Errorf("ruta inesperada: %s (destino %s)", rec.FilePath, dest)
			}
			if st, err := os.Stat(rec.FilePath); err != nil || st.Size() != rec.SizeBytes {
				t.Errorf("archivo: %v", err)
			}
			checkEvents(t, evlog.of("backup:progress"), rid, rec.ID)
			for _, sid := range []int64{sv.ID, 0} {
				bl, _ := a.ListBackups(sid)
				found := false
				for _, b := range bl {
					if b.ID == rec.ID && b.Status == "ok" {
						found = true
					}
				}
				if !found {
					t.Errorf("ListBackups(%d) no contiene el backup", sid)
				}
			}

			// Inspect
			info, err := a.InspectBackup(sv.ID, rec.FilePath)
			if err != nil {
				t.Fatalf("InspectBackup: %v", err)
			}
			if eng == "mssql" {
				if info.DatabaseName != src || len(info.Files) < 2 {
					t.Errorf("inspect mssql: %+v", info)
				}
				kinds := map[string]bool{}
				for _, f := range info.Files {
					kinds[f.Type] = true
				}
				if !kinds["data"] || !kinds["log"] {
					t.Errorf("faltan archivos data/log: %+v", info.Files)
				}
			} else if info.Format != "custom" {
				t.Errorf("formato pg: %+v", info)
			}

			// Restore a nombre nuevo
			tgt := uniq("rs")
			t.Cleanup(func() { dropDBRaw(c, tgt) })
			evlog.reset()
			rr, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: rec.FilePath, TargetDatabase: tgt, RequestID: "rs-" + eng})
			if err != nil {
				t.Fatalf("RestoreBackup: %v", err)
			}
			if rr.Status != "ok" || rr.FinishedAt == "" || rr.TargetDatabase != tgt {
				t.Errorf("registro de restauración: %+v", rr)
			}
			checkEvents(t, evlog.of("restore:progress"), "rs-"+eng, rr.ID)
			if n := rowCount(t, c, tgt); n != 3 {
				t.Fatalf("filas restauradas: %d, esperaba 3", n)
			}

			// sin replace sobre existente: falla y NO daña la base
			if err := admin(c, tgt).exec("INSERT INTO t VALUES (500, 'marcador')"); err != nil {
				t.Fatal(err)
			}
			_, err = a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: rec.FilePath, TargetDatabase: tgt})
			if err == nil {
				t.Fatal("restaurar sobre una base existente sin replace debía fallar")
			}
			if !strings.Contains(err.Error(), "ya existe") {
				t.Errorf("mensaje: %v", err)
			}
			if n := rowCount(t, c, tgt); n != 4 {
				t.Errorf("la base existente fue dañada: %d filas (esperaba 4 con el marcador)", n)
			}
			if lr := lastRestore(t, a, sv.ID); lr.Status != "error" || lr.Message == "" || lr.FinishedAt == "" {
				t.Errorf("registro del fallo: %+v", lr)
			}

			// con replace: vuelve al contenido del backup
			rr, err = a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: rec.FilePath, TargetDatabase: tgt, Replace: true})
			if err != nil {
				t.Fatalf("restore con replace: %v", err)
			}
			if n := rowCount(t, c, tgt); n != 3 {
				t.Errorf("replace: %d filas, esperaba 3 (el marcador debía desaparecer)", n)
			}
			if eng == "mssql" {
				// la base debe quedar en MULTI_USER y ONLINE
				n, err := admin(c, "").queryInt("SELECT COUNT(*) FROM sys.databases WHERE name = " + qstr(eng, tgt) + " AND user_access = 0 AND state = 0")
				if err != nil || n != 1 {
					t.Errorf("tras replace la base no quedó MULTI_USER/ONLINE (n=%d, err=%v)", n, err)
				}
			}
			// restaurar con nombre con el que ya hay otra restauración y otro nombre distinto en paralelo
			tgt2 := uniq("rs")
			t.Cleanup(func() { dropDBRaw(c, tgt2) })
			if _, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: rec.FilePath, TargetDatabase: tgt2}); err != nil {
				t.Errorf("segunda restauración a otro nombre: %v", err)
			}

			// no quedan archivos temporales dentro del contenedor
			if eng == "mssql" {
				out, _ := dockerExec(t, c.Name, "sh", "-c", "ls -A /var/opt/mssql/backup 2>/dev/null | grep -c '"+src+"' || true")
				if strings.TrimSpace(out) != "0" {
					t.Errorf("quedaron archivos de backup temporales en el contenedor: %s", out)
				}
			} else {
				out, _ := dockerExec(t, c.Name, "sh", "-c", "ls /tmp | grep -c '"+src+"' || true")
				if strings.TrimSpace(out) != "0" {
					t.Errorf("quedaron archivos temporales en /tmp del contenedor: %s", out)
				}
			}
		})
	}
}

func TestBackupAndRestoreInputErrors(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, evlog := newTestApp(t)
			sv := importShared(t, a, c)
			src := uniq("ie")
			mkDB(t, c, src, 1)
			good := mustBackup(t, a, sv, src, "ie")
			evlog.reset()

			// Backup: errores
			bk := map[string]BackupRequest{
				"sin base":           {ServerID: sv.ID, Database: "", DestDir: t.TempDir()},
				"sin destino":        {ServerID: sv.ID, Database: src, DestDir: ""},
				"destino relativo":   {ServerID: sv.ID, Database: src, DestDir: "relativo/carpeta"},
				"base inexistente":   {ServerID: sv.ID, Database: "no_existe_" + uniq("x"), DestDir: t.TempDir()},
				"base con inyección": {ServerID: sv.ID, Database: "x'; DROP DATABASE " + src + "; --", DestDir: t.TempDir()},
			}
			for name, req := range bk {
				req.RequestID = "bad"
				if _, err := a.CreateBackup(req); err == nil {
					t.Errorf("backup %s: debía fallar", name)
				} else if testutil.LooksEnglish(err.Error()) && !testutil.HasSpanish(err.Error()) {
					t.Errorf("backup %s: mensaje en inglés: %v", name, err)
				}
			}
			if !dbExistsRaw(t, c, src) {
				t.Fatal("la base origen desapareció tras un backup con nombre inyectado")
			}
			// un backup fallido por la base queda registrado con estado error
			bl, _ := a.ListBackups(sv.ID)
			errs := 0
			for _, b := range bl {
				if b.Status == "error" {
					errs++
					if b.Message == "" {
						t.Errorf("backup en error sin mensaje: %+v", b)
					}
				}
				if b.Status == "running" {
					t.Errorf("backup atascado en running: %+v", b)
				}
			}
			if errs < 2 {
				t.Errorf("se esperaban >=2 backups en error registrados, hay %d", errs)
			}
			for _, e := range evlog.of("backup:progress") {
				if e.RequestID != "bad" {
					t.Errorf("evento sin requestId: %+v", e)
				}
			}

			// Restore: errores (los de validación no crean registro)
			before, _ := a.ListRestores(sv.ID)
			rs := map[string]RestoreRequest{
				"destino con ]":     {FilePath: good.FilePath, TargetDatabase: "a]b"},
				"destino inyección": {FilePath: good.FilePath, TargetDatabase: "x'; DROP DATABASE " + src + "; --"},
				"destino vacío":     {FilePath: good.FilePath, TargetDatabase: ""},
				"destino unicode":   {FilePath: good.FilePath, TargetDatabase: "ñandú"},
				"sin archivo":       {FilePath: "", TargetDatabase: uniq("x")},
				"archivo inexist.":  {FilePath: filepath.Join(t.TempDir(), "no.bak"), TargetDatabase: uniq("x")},
				"carpeta":           {FilePath: t.TempDir(), TargetDatabase: uniq("x")},
			}
			for name, req := range rs {
				req.ServerID = sv.ID
				if _, err := a.RestoreBackup(req); err == nil {
					t.Errorf("restore %s: debía fallar", name)
				} else if !testutil.HasSpanish(err.Error()) {
					t.Errorf("restore %s: mensaje no está en español: %v", name, err)
				}
			}
			after, _ := a.ListRestores(sv.ID)
			if len(after) != len(before) {
				t.Errorf("las validaciones previas no deben crear registros: %d -> %d", len(before), len(after))
			}
			for _, r := range after {
				if r.Status == "running" {
					t.Errorf("restauración atascada en running: %+v", r)
				}
			}
			if !dbExistsRaw(t, c, src) {
				t.Fatal("la base origen desapareció")
			}
			// restaurar sobre bases de sistema: prohibido incluso con replace
			sysT := []string{"master", "tempdb", "model", "msdb"}
			if eng == "postgres" {
				sysT = []string{"postgres", "template0", "template1"}
			}
			for _, s := range sysT {
				for _, rep := range []bool{false, true} {
					if _, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: good.FilePath, TargetDatabase: s, Replace: rep}); err == nil {
						t.Errorf("restore sobre la base de sistema %s (replace=%v) debía fallar", s, rep)
					}
				}
			}
			if st, _ := a.TestConnection(sv.ID); !st.Reachable {
				t.Fatalf("servidor inaccesible tras intentos sobre bases de sistema: %+v", st)
			}
			// InspectBackup con archivo inexistente
			if _, err := a.InspectBackup(sv.ID, filepath.Join(t.TempDir(), "nada"+ext(eng))); err == nil {
				t.Error("inspect de archivo inexistente debía fallar")
			}
		})
	}
}

// Archivos corruptos / truncados: deben fallar limpiamente y dejar el registro en error.
func TestRestoreCorruptAndTruncatedFiles(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, evlog := newTestApp(t)
			sv := importShared(t, a, c)
			src := uniq("cr")
			mkDB(t, c, src, 3)
			good := mustBackup(t, a, sv, src, "cr")
			data, _ := os.ReadFile(good.FilePath)
			trunc := filepath.Join(t.TempDir(), "trunc"+ext(eng))
			os.WriteFile(trunc, data[:len(data)*6/10], 0o600)

			files := map[string]string{
				"basura": randomFile(t, "basura"+ext(eng), 8192), "truncado": trunc,
			}
			if eng == "mssql" {
				files["vacio"] = filepath.Join(t.TempDir(), "vacio.bak")
				os.WriteFile(files["vacio"], nil, 0o600)
			}
			for name, f := range files {
				tgt := uniq("cx")
				t.Cleanup(func() { dropDBRaw(c, tgt) })
				evlog.reset()
				_, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: f, TargetDatabase: tgt, RequestID: "cx-" + name})
				if err == nil {
					t.Errorf("%s: restaurar un archivo %s debía fallar", name, name)
					continue
				}
				lr := lastRestore(t, a, sv.ID)
				if lr.Status != "error" || lr.Message == "" || lr.TargetDatabase != tgt || lr.FinishedAt == "" {
					t.Errorf("%s: registro de restauración: %+v", name, lr)
				}
				evs := evlog.of("restore:progress")
				if len(evs) == 0 {
					t.Errorf("%s: sin eventos", name)
				} else if l := evs[len(evs)-1]; !l.Done || !l.Error || l.RequestID != "cx-"+name {
					t.Errorf("%s: último evento: %+v", name, l)
				}
				t.Logf("%s -> %v", name, services.CleanErr(err))
			}
		})
	}
}

// BUG (alta, PostgreSQL): una restauración con replace=true elimina la base existente ANTES de validar el archivo;
// si el archivo está corrupto/truncado la base original se pierde por completo.
func TestReplaceWithBadFileMustNotDestroyExistingDB(t *testing.T) {
	for _, eng := range engines {
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			src := uniq("rp")
			mkDB(t, c, src, 3)
			good := mustBackup(t, a, sv, src, "rp")
			data, _ := os.ReadFile(good.FilePath)
			trunc := filepath.Join(t.TempDir(), "trunc"+ext(eng))
			os.WriteFile(trunc, data[:len(data)*6/10], 0o600)
			cases := map[string]string{"basura": randomFile(t, "basura"+ext(eng), 8192), "truncado": trunc}
			for name, f := range cases {
				t.Run(name, func(t *testing.T) {
					tgt := uniq("rx")
					mkDB(t, c, tgt, 5)
					_, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: f, TargetDatabase: tgt, Replace: true})
					if err == nil {
						t.Fatal("debía fallar")
					}
					if !dbExistsRaw(t, c, tgt) {
						t.Fatalf("la base existente %s FUE ELIMINADA por una restauración fallida (replace=true)", tgt)
					}
					n, err := admin(c, tgt).queryInt("SELECT COUNT(*) FROM t")
					if err != nil || n != 5 {
						t.Errorf("los datos originales se perdieron/ dañaron: filas=%d err=%v", n, err)
					}
				})
			}
		})
	}
}

// BUG (media, PostgreSQL): una restauración fallida (sin replace) deja creada la base destino vacía.
func TestPG_FailedRestoreLeavesEmptyTargetDB(t *testing.T) {
	c := sharedContainer(t, "postgres")
	a, _ := newTestApp(t)
	sv := importShared(t, a, c)
	tgt := uniq("lf")
	t.Cleanup(func() { dropDBRaw(c, tgt) })
	_, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: randomFile(t, "x.dump", 4096), TargetDatabase: tgt})
	if err == nil {
		t.Fatal("debía fallar")
	}
	if dbExistsRaw(t, c, tgt) {
		t.Errorf("la base destino %s quedó creada (vacía/parcial) tras la restauración fallida", tgt)
	}
}

// BUG (media, PostgreSQL): un archivo vacío (0 bytes) se detecta como SQL plano y psql "restaura" con éxito: la app
// informa Restauración completada con una base vacía.
func TestPG_EmptyFileReportsSuccess(t *testing.T) {
	c := sharedContainer(t, "postgres")
	a, _ := newTestApp(t)
	sv := importShared(t, a, c)
	tgt := uniq("em")
	t.Cleanup(func() { dropDBRaw(c, tgt) })
	f := filepath.Join(t.TempDir(), "vacio.dump")
	os.WriteFile(f, nil, 0o600)
	rec, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: f, TargetDatabase: tgt})
	if err == nil {
		t.Errorf("restaurar un archivo vacío debía fallar; en cambio: %+v", rec)
	}
}

// Restauración de un .sql plano en PostgreSQL.
func TestPG_PlainSQLRestore(t *testing.T) {
	c := sharedContainer(t, "postgres")
	a, _ := newTestApp(t)
	sv := importShared(t, a, c)
	dir := t.TempDir()
	okSQL := filepath.Join(dir, "plano.sql")
	os.WriteFile(okSQL, []byte("CREATE TABLE t (id int, v varchar(50));\r\nINSERT INTO t VALUES (1,'a'),(2,'ñandú'),(3,'it''s');\r\n"), 0o600)
	info, err := a.InspectBackup(sv.ID, okSQL)
	if err != nil || info.Format != "plain" {
		t.Fatalf("inspect: %+v %v", info, err)
	}
	tgt := uniq("ps")
	t.Cleanup(func() { dropDBRaw(c, tgt) })
	rec, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: okSQL, TargetDatabase: tgt})
	if err != nil {
		t.Fatalf("restore .sql: %v", err)
	}
	if rec.Status != "ok" {
		t.Errorf("%+v", rec)
	}
	if n := rowCount(t, c, tgt); n != 3 {
		t.Errorf("filas: %d", n)
	}
	n, _ := admin(c, tgt).queryInt("SELECT COUNT(*) FROM t WHERE v = 'ñandú'")
	if n != 1 {
		t.Error("el contenido unicode no se restauró bien")
	}
	// .sql con un error a mitad: debe fallar (ON_ERROR_STOP) y quedar registrado
	badSQL := filepath.Join(dir, "malo.sql")
	os.WriteFile(badSQL, []byte("CREATE TABLE t (id int);\nINSERT INTO no_existe VALUES (1);\n"), 0o600)
	tgt2 := uniq("pb")
	t.Cleanup(func() { dropDBRaw(c, tgt2) })
	if _, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: badSQL, TargetDatabase: tgt2}); err == nil {
		t.Error(".sql con error debía fallar")
	}
	if lr := lastRestore(t, a, sv.ID); lr.Status != "error" {
		t.Errorf("%+v", lr)
	}
	// archivo con nombre raro (espacios, comilla) en el host
	odd := filepath.Join(dir, "my file ñ's.sql")
	os.WriteFile(odd, []byte("CREATE TABLE t (id int); INSERT INTO t VALUES (1);"), 0o600)
	tgt3 := uniq("po")
	t.Cleanup(func() { dropDBRaw(c, tgt3) })
	if _, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: odd, TargetDatabase: tgt3}); err != nil {
		t.Errorf("restore con nombre de archivo con espacios/comilla: %v", err)
	}
}

// BUG (media, SQL Server): el .bak del host se copia a /var/opt/mssql/backup/<nombre> dentro del contenedor y luego se
// BORRA con rm -f. Si ya existía allí un archivo con el mismo nombre (p. ej. un backup propio del usuario en un
// contenedor importado, que suele ser un montaje del host), se sobrescribe y se elimina.
func TestMSSQL_RestoreDeletesPreexistingContainerFile(t *testing.T) {
	c := sharedContainer(t, "mssql")
	a, _ := newTestApp(t)
	sv := importShared(t, a, c)
	name := uniq("precioso") + ".bak"
	if out, code := dockerExec(t, c.Name, "sh", "-c", "mkdir -p /var/opt/mssql/backup && echo contenido-del-usuario > /var/opt/mssql/backup/"+name); code != 0 {
		t.Fatal(out)
	}
	t.Cleanup(func() { dockerExec(t, c.Name, "rm", "-f", "/var/opt/mssql/backup/"+name) })
	f := randomFile(t, name, 4096) // mismo nombre, contenido basura
	tgt := uniq("pc")
	t.Cleanup(func() { dropDBRaw(c, tgt) })
	a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: f, TargetDatabase: tgt}) // falla (basura)
	out, _ := dockerExec(t, c.Name, "cat", "/var/opt/mssql/backup/"+name)
	if !strings.Contains(out, "contenido-del-usuario") {
		t.Errorf("el archivo preexistente del contenedor fue sobrescrito/eliminado: %q", out)
	}
}

// Backup/restore de un PostgreSQL "remoto" (el contenedor de prueba visto como servidor remoto en localhost):
// ejercita el contenedor temporal postgres:<versión> + host.docker.internal.
func TestRemotePostgresBackupRestore(t *testing.T) {
	c := sharedContainer(t, "postgres")
	a, evlog := newTestApp(t)
	sv, err := a.AddRemoteServer(RemoteServerRequest{Name: "remoto-pg", Engine: "postgres", Host: "localhost", Port: c.Port, AdminUser: c.User, AdminPassword: c.Password})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := a.TestConnection(sv.ID)
	if !st.Reachable || st.State != "remote" || !strings.HasPrefix(st.Version, "PostgreSQL 17") {
		t.Fatalf("TestConnection remoto: %+v", st)
	}
	src := uniq("rb")
	mkDB(t, c, src, 4)

	dest := filepath.Join(t.TempDir(), "remoto")
	rec, err := a.CreateBackup(BackupRequest{ServerID: sv.ID, Database: src, DestDir: dest, RequestID: "rb"})
	if err != nil {
		t.Fatalf("backup remoto: %v", err)
	}
	if rec.Status != "ok" || rec.SizeBytes <= 0 || !strings.HasSuffix(rec.FilePath, ".dump") {
		t.Errorf("registro: %+v", rec)
	}
	if _, err := os.Stat(rec.FilePath); err != nil {
		t.Fatal(err)
	}
	checkEvents(t, evlog.of("backup:progress"), "rb", rec.ID)
	if left := testutil.ListNames("dbmanager-tmp-"); len(left) != 0 {
		t.Errorf("contenedores temporales sin eliminar: %v", left)
	}

	// restore remoto (custom) a nombre nuevo, luego con replace
	tgt := uniq("rr")
	t.Cleanup(func() { dropDBRaw(c, tgt) })
	evlog.reset()
	rr, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: rec.FilePath, TargetDatabase: tgt, RequestID: "rr"})
	if err != nil {
		t.Fatalf("restore remoto: %v", err)
	}
	checkEvents(t, evlog.of("restore:progress"), "rr", rr.ID)
	if n := rowCount(t, c, tgt); n != 4 {
		t.Errorf("filas: %d", n)
	}
	admin(c, tgt).exec("INSERT INTO t VALUES (9,'m')")
	if _, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: rec.FilePath, TargetDatabase: tgt}); err == nil {
		t.Error("sin replace debía fallar")
	}
	if n := rowCount(t, c, tgt); n != 5 {
		t.Errorf("base dañada: %d", n)
	}
	if _, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: rec.FilePath, TargetDatabase: tgt, Replace: true}); err != nil {
		t.Errorf("replace remoto: %v", err)
	}
	if n := rowCount(t, c, tgt); n != 4 {
		t.Errorf("replace remoto: %d", n)
	}
	// .sql plano remoto
	plain := filepath.Join(t.TempDir(), "r.sql")
	os.WriteFile(plain, []byte("CREATE TABLE t (id int, v varchar(5)); INSERT INTO t VALUES (1,'x');"), 0o600)
	tgt2 := uniq("rp")
	t.Cleanup(func() { dropDBRaw(c, tgt2) })
	if _, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: plain, TargetDatabase: tgt2}); err != nil {
		t.Errorf(".sql remoto: %v", err)
	}
	// archivo corrupto remoto: falla
	if _, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: randomFile(t, "c.dump", 2048), TargetDatabase: uniq("rc")}); err == nil {
		t.Error("corrupto remoto debía fallar")
	}
	// contraseña incorrecta en el registro remoto: error claro, registro en error y sin contenedores huérfanos
	bad, _ := a.AddRemoteServer(RemoteServerRequest{Name: "remoto-mal", Engine: "postgres", Host: "localhost", Port: c.Port, AdminUser: c.User, AdminPassword: "mala-" + c.Password})
	if _, err := a.CreateBackup(BackupRequest{ServerID: bad.ID, Database: src, DestDir: t.TempDir()}); err == nil {
		t.Error("backup con contraseña incorrecta debía fallar")
	} else {
		noSecret(t, "error", err.Error(), c.Password, "mala-"+c.Password)
	}
	bl, _ := a.ListBackups(bad.ID)
	if len(bl) != 1 || bl[0].Status != "error" {
		t.Errorf("registro de backup fallido: %+v", bl)
	}
	if left := testutil.ListNames("dbmanager-tmp-"); len(left) != 0 {
		t.Errorf("contenedores temporales sin eliminar: %v", left)
	}
}

// SQL Server "remoto": la ruta de destino es del servidor.
func TestRemoteMSSQLBackupRestore(t *testing.T) {
	c := sharedContainer(t, "mssql")
	a, _ := newTestApp(t)
	sv, err := a.AddRemoteServer(RemoteServerRequest{Name: "remoto-ms", Engine: "mssql", Host: "localhost", Port: c.Port, AdminUser: "sa", AdminPassword: c.Password,
		TLS: true, TrustServerCert: true})
	if err != nil {
		t.Fatal(err)
	}
	src := uniq("rm")
	mkDB(t, c, src, 2)
	rec, err := a.CreateBackup(BackupRequest{ServerID: sv.ID, Database: src, DestDir: "/var/opt/mssql/backup", RequestID: "rm"})
	if err != nil {
		t.Fatalf("backup remoto mssql: %v", err)
	}
	if rec.Status != "ok" || !strings.HasPrefix(rec.FilePath, "/var/opt/mssql/backup/") || rec.Message == "" {
		t.Errorf("registro: %+v", rec)
	}
	t.Cleanup(func() { dockerExec(t, c.Name, "rm", "-f", rec.FilePath) })
	if out, code := dockerExec(t, c.Name, "ls", rec.FilePath); code != 0 {
		t.Fatalf("el archivo no está en el servidor: %s", out)
	}
	info, err := a.InspectBackup(sv.ID, rec.FilePath)
	if err != nil || info.DatabaseName != src {
		t.Fatalf("inspect remoto: %+v %v", info, err)
	}
	tgt := uniq("rt")
	t.Cleanup(func() { dropDBRaw(c, tgt) })
	if _, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: rec.FilePath, TargetDatabase: tgt}); err != nil {
		t.Fatalf("restore remoto mssql: %v", err)
	}
	if n := rowCount(t, c, tgt); n != 2 {
		t.Errorf("filas: %d", n)
	}
	// ruta inexistente en el servidor
	if _, err := a.CreateBackup(BackupRequest{ServerID: sv.ID, Database: src, DestDir: "/ruta/que/no/existe"}); err == nil {
		t.Error("ruta inexistente debía fallar")
	}
	if _, err := a.RestoreBackup(RestoreRequest{ServerID: sv.ID, FilePath: "/var/opt/mssql/backup/no_hay.bak", TargetDatabase: uniq("rz")}); err == nil {
		t.Error("archivo inexistente en el servidor debía fallar")
	}
	bl, _ := a.ListBackups(sv.ID)
	for _, b := range bl {
		if b.Status == "running" {
			t.Errorf("backup atascado: %+v", b)
		}
	}
}

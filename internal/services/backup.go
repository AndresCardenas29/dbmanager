package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/engine/mssql"
	"dbmanager/internal/engine/postgres"
	"dbmanager/internal/store"
	"dbmanager/internal/validate"
)

var fileSafeRe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// BackupFileName arma el nombre de archivo <db>_<timestamp>.<ext>.
func BackupFileName(database, ext string, t time.Time) string {
	return fileSafeRe.ReplaceAllString(database, "_") + "_" + t.Format("20060102_150405") + "." + ext
}

// dockerHostFor traduce localhost para contenedores temporales.
func dockerHostFor(host string) string {
	switch strings.ToLower(host) {
	case "", "localhost", "127.0.0.1", "::1":
		return "host.docker.internal"
	}
	return host
}

// postgresImageFor elige la imagen de herramientas según la versión del servidor.
func postgresImageFor(version string) string {
	if m := postgres.MajorFromVersion(version); m > 0 {
		return fmt.Sprintf("postgres:%d", m)
	}
	return DefaultPostgresImage
}

// execChecked ejecuta un comando en el contenedor y falla si el código de salida != 0.
func execChecked(ctx context.Context, cli *dockerx.Client, container string, o dockerx.ExecOptions) error {
	res, err := cli.Exec(ctx, container, o)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return fmt.Errorf("el comando %s terminó con código %d: %s", o.Cmd[0], res.ExitCode, tail(msg, 1500))
	}
	return nil
}

func tail(s string, n int) string {
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}

// ensurePostgresImage descarga la imagen si no existe localmente.
func (s *Services) ensureImage(ctx context.Context, cli *dockerx.Client, image string, report func(string)) error {
	ok, err := cli.ImageExists(ctx, image)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	report("Descargando imagen " + image)
	return cli.PullImage(ctx, image, "", nil)
}

// Backup crea un backup de una base de datos y devuelve el registro.
func (s *Services) Backup(ctx context.Context, sv store.Server, database, destDir, rid string) (store.Backup, error) {
	database = strings.TrimSpace(database)
	destDir = strings.TrimSpace(destDir)
	if database == "" {
		return store.Backup{}, errors.New("seleccione la base de datos")
	}
	if destDir == "" {
		return store.Backup{}, errors.New("seleccione la carpeta de destino")
	}
	if sv.Mode == "local" || sv.Engine == validate.EnginePostgres {
		if !filepath.IsAbs(destDir) {
			return store.Backup{}, errors.New("la carpeta de destino debe ser una ruta absoluta")
		}
	}
	if sv.Mode == "local" {
		if err := s.requireRunning(ctx, sv); err != nil {
			return store.Backup{}, err
		}
	}
	rec, err := s.Store.CreateBackup(store.Backup{ServerID: sv.ID, ServerName: sv.Name, Database: database, Status: "running"})
	if err != nil {
		return store.Backup{}, err
	}
	emit := func(pct int, msg string, done, isErr bool) {
		s.emit("backup:progress", Progress{ID: rec.ID, RequestID: rid, Percent: pct, Message: msg, Done: done, Error: isErr})
	}
	emit(0, "Iniciando backup", false, false)

	file, size, err := s.doBackup(ctx, sv, database, destDir, func(p int, m string) { emit(p, m, false, false) })
	if err != nil {
		rec.Status, rec.Message = "error", CleanErr(err)
		s.Store.UpdateBackup(rec)
		s.logOp(sv.ID, "backup", sv.Name+"/"+database, err)
		emit(0, rec.Message, true, true)
		return rec, err
	}
	rec.FilePath, rec.SizeBytes, rec.Status = file, size, "ok"
	if sv.Mode == "remote" && sv.Engine == validate.EngineMSSQL {
		rec.Message = "El archivo quedó en el servidor remoto (ruta del servidor)."
	}
	s.Store.UpdateBackup(rec)
	s.Store.SetSetting("lastBackupDir", destDir)
	s.logOp(sv.ID, "backup", sv.Name+"/"+database, nil)
	emit(100, "Backup completado", true, false)
	out, _ := s.Store.GetBackup(rec.ID)
	return out, nil
}

func (s *Services) doBackup(ctx context.Context, sv store.Server, database, destDir string, prog func(int, string)) (string, int64, error) {
	conn, err := s.ConnFor(sv)
	if err != nil {
		return "", 0, err
	}
	now := time.Now()
	switch sv.Engine {
	case validate.EngineMSSQL:
		e, err := mssql.New(conn)
		if err != nil {
			return "", 0, err
		}
		defer e.Close()
		name := BackupFileName(database, "bak", now)
		onP := func(p int, m string) { prog(5+p*80/100, m) }
		if sv.Mode == "remote" {
			p := destDir
			sep := "/"
			if strings.Contains(destDir, `\`) || (len(destDir) > 1 && destDir[1] == ':') {
				sep = `\`
			}
			p = strings.TrimRight(destDir, `/\`) + sep + name
			prog(5, "Ejecutando BACKUP en el servidor remoto (la ruta es del servidor)")
			if err := e.BackupDatabase(ctx, database, p, onP); err != nil {
				return "", 0, fmt.Errorf("%w (en servidores remotos la carpeta de destino es una ruta del propio servidor SQL Server y debe tener permisos de escritura)", err)
			}
			return p, 0, nil
		}
		cli, err := s.Docker()
		if err != nil {
			return "", 0, err
		}
		if err := execChecked(ctx, cli, sv.ContainerName, dockerx.ExecOptions{Cmd: []string{"mkdir", "-p", mssqlBackupDir}}); err != nil {
			return "", 0, err
		}
		tmp := mssqlBackupDir + "/" + name
		defer cli.Exec(context.Background(), sv.ContainerName, dockerx.ExecOptions{Cmd: []string{"rm", "-f", tmp}})
		prog(5, "Ejecutando BACKUP DATABASE")
		if err := e.BackupDatabase(ctx, database, tmp, onP); err != nil {
			return "", 0, err
		}
		prog(88, "Copiando el archivo al equipo")
		return copyOut(ctx, cli, sv.ContainerName, tmp, destDir)

	case validate.EnginePostgres:
		e, err := postgres.New(conn)
		if err != nil {
			return "", 0, err
		}
		if ok, err := e.DatabaseExists(ctx, database); err != nil {
			return "", 0, err
		} else if !ok {
			return "", 0, fmt.Errorf("la base de datos %q no existe", database)
		}
		cli, err := s.Docker()
		if err != nil {
			return "", 0, err
		}
		name := BackupFileName(database, "dump", now)
		if sv.Mode == "local" {
			tmp := "/tmp/" + name
			defer cli.Exec(context.Background(), sv.ContainerName, dockerx.ExecOptions{Cmd: []string{"rm", "-f", tmp}})
			prog(10, "Ejecutando pg_dump")
			err := execChecked(ctx, cli, sv.ContainerName, dockerx.ExecOptions{
				Cmd: postgres.DumpCmd("", 0, sv.AdminUser, database, tmp), Env: []string{"PGPASSWORD=" + conn.Password}})
			if err != nil {
				return "", 0, err
			}
			prog(80, "Copiando el archivo al equipo")
			return copyOut(ctx, cli, sv.ContainerName, tmp, destDir)
		}
		// remoto: contenedor temporal con las herramientas de la misma versión mayor
		ver, err := e.Ping(ctx)
		if err != nil {
			return "", 0, err
		}
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return "", 0, err
		}
		img := postgresImageFor(ver)
		prog(5, "Preparando contenedor temporal "+img)
		if err := s.ensureImage(ctx, cli, img, func(m string) { prog(8, m) }); err != nil {
			return "", 0, err
		}
		prog(20, "Ejecutando pg_dump contra el servidor remoto")
		code, logs, err := cli.RunOnce(ctx, dockerx.CreateOptions{
			Name: "dbmanager-tmp-dump-" + fmt.Sprint(now.UnixNano()), Image: img, Engine: "postgres",
			Env:         []string{"PGPASSWORD=" + conn.Password, "PGSSLMODE=prefer"},
			Cmd:         postgres.DumpCmd(dockerHostFor(conn.Host), conn.Port, conn.User, database, "/out/"+name),
			Binds:       []dockerx.Bind{{Source: destDir, Target: "/out"}},
			HostGateway: true,
		})
		if err != nil {
			return "", 0, err
		}
		if code != 0 {
			return "", 0, fmt.Errorf("pg_dump terminó con código %d: %s", code, tail(strings.TrimSpace(logs), 1500))
		}
		full := filepath.Join(destDir, name)
		st, err := os.Stat(full)
		if err != nil {
			return "", 0, fmt.Errorf("no se encontró el archivo generado: %w", err)
		}
		return full, st.Size(), nil
	}
	return "", 0, fmt.Errorf("motor no soportado: %s", sv.Engine)
}

func copyOut(ctx context.Context, cli *dockerx.Client, container, containerPath, destDir string) (string, int64, error) {
	host, err := cli.CopyFromContainer(ctx, container, containerPath, destDir)
	if err != nil {
		return "", 0, fmt.Errorf("no se pudo copiar el backup a %s: %w", destDir, err)
	}
	st, err := os.Stat(host)
	if err != nil {
		return "", 0, err
	}
	return host, st.Size(), nil
}

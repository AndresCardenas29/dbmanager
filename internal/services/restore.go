package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/engine"
	"dbmanager/internal/engine/mssql"
	"dbmanager/internal/engine/postgres"
	"dbmanager/internal/store"
	"dbmanager/internal/validate"
)

// RestoreRequest son los parámetros de una restauración.
type RestoreRequest struct {
	RequestID      string
	FilePath       string
	TargetDatabase string
	DataDir        string
	LogDir         string
	Replace        bool
}

// copyIn copia un archivo del host a /var/opt/mssql/backup del contenedor y devuelve su ruta.
func (s *Services) copyInMSSQL(ctx context.Context, cli *dockerx.Client, container, hostFile string) (string, error) {
	if err := execChecked(ctx, cli, container, dockerx.ExecOptions{Cmd: []string{"mkdir", "-p", mssqlBackupDir}}); err != nil {
		return "", err
	}
	// Nombre único para no pisar (ni borrar después) un archivo preexistente del contenedor.
	name := ".dbm-" + randHex() + "-" + filepath.Base(hostFile)
	if err := cli.CopyToContainerAs(ctx, container, hostFile, mssqlBackupDir, name); err != nil {
		return "", fmt.Errorf("no se pudo copiar el archivo al contenedor: %w", err)
	}
	return mssqlBackupDir + "/" + name, nil
}

func randHex() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// checkNonEmpty rechaza archivos de 0 bytes.
func checkNonEmpty(p string) error {
	if st, err := os.Stat(p); err == nil && st.Size() == 0 {
		return errors.New("el archivo de backup está vacío (0 bytes)")
	}
	return nil
}

func checkHostFile(p string) error {
	if p == "" {
		return errors.New("seleccione el archivo de backup")
	}
	st, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("no se puede leer el archivo %q: %w", p, err)
	}
	if st.IsDir() {
		return errors.New("la ruta indicada es una carpeta, no un archivo")
	}
	return nil
}

// Inspect describe un backup. Para mssql local el archivo se copia temporalmente al contenedor.
// En servidores remotos mssql, filePath es una ruta del servidor.
func (s *Services) Inspect(ctx context.Context, sv store.Server, filePath string) (engine.BackupInfo, error) {
	filePath = strings.TrimSpace(filePath)
	switch sv.Engine {
	case validate.EnginePostgres:
		if err := checkHostFile(filePath); err != nil {
			return engine.BackupInfo{}, err
		}
		if err := checkNonEmpty(filePath); err != nil {
			return engine.BackupInfo{}, err
		}
		f, err := postgres.DetectFormatFile(filePath)
		if err != nil {
			return engine.BackupInfo{}, err
		}
		return engine.BackupInfo{Format: f, Files: []engine.BackupFile{}}, nil
	case validate.EngineMSSQL:
		conn, err := s.ConnFor(sv)
		if err != nil {
			return engine.BackupInfo{}, err
		}
		e, err := mssql.New(conn)
		if err != nil {
			return engine.BackupInfo{}, err
		}
		defer e.Close()
		serverPath := filePath
		if sv.Mode == "local" {
			if err := checkHostFile(filePath); err != nil {
				return engine.BackupInfo{}, err
			}
			if err := s.requireRunning(ctx, sv); err != nil {
				return engine.BackupInfo{}, err
			}
			cli, err := s.Docker()
			if err != nil {
				return engine.BackupInfo{}, err
			}
			if serverPath, err = s.copyInMSSQL(ctx, cli, sv.ContainerName, filePath); err != nil {
				return engine.BackupInfo{}, err
			}
			defer cli.Exec(context.Background(), sv.ContainerName, dockerx.ExecOptions{Cmd: []string{"rm", "-f", serverPath}})
		} else if serverPath == "" {
			return engine.BackupInfo{}, errors.New("indique la ruta del archivo .bak en el servidor")
		}
		return e.InspectFile(ctx, serverPath)
	}
	return engine.BackupInfo{}, fmt.Errorf("motor no soportado: %s", sv.Engine)
}

// Restore restaura un backup. Crea y actualiza el registro en el store.
func (s *Services) Restore(ctx context.Context, sv store.Server, r RestoreRequest) (store.Restore, error) {
	r.FilePath = strings.TrimSpace(r.FilePath)
	r.TargetDatabase = strings.TrimSpace(r.TargetDatabase)
	if err := validate.Identifier(sv.Engine, r.TargetDatabase); err != nil {
		return store.Restore{}, fmt.Errorf("base de datos destino: %w", err)
	}
	if r.FilePath == "" {
		return store.Restore{}, errors.New("seleccione el archivo de backup")
	}
	if sv.Mode == "local" {
		if err := s.requireRunning(ctx, sv); err != nil {
			return store.Restore{}, err
		}
	}
	if sv.Mode == "local" || sv.Engine == validate.EnginePostgres {
		if err := checkHostFile(r.FilePath); err != nil {
			return store.Restore{}, err
		}
	}
	rec, err := s.Store.CreateRestore(store.Restore{ServerID: sv.ID, ServerName: sv.Name, SourceFile: r.FilePath,
		TargetDatabase: r.TargetDatabase, DataDir: r.DataDir, LogDir: r.LogDir, Status: "running"})
	if err != nil {
		return store.Restore{}, err
	}
	emit := func(pct int, msg string, done, isErr bool) {
		s.emit("restore:progress", Progress{ID: rec.ID, RequestID: r.RequestID, Percent: pct, Message: msg, Done: done, Error: isErr})
	}
	emit(0, "Iniciando restauración", false, false)
	prog := func(p int, m string) { emit(p, m, false, false) }

	if sv.Mode == "local" || sv.Engine == validate.EnginePostgres {
		if e := checkNonEmpty(r.FilePath); e != nil {
			s.Store.FinishRestore(rec.ID, "error", e.Error())
			s.logOp(sv.ID, "restore", sv.Name+"/"+r.TargetDatabase, e)
			emit(0, e.Error(), true, true)
			return rec, e
		}
	}
	var msg string
	switch sv.Engine {
	case validate.EngineMSSQL:
		err = s.restoreMSSQL(ctx, sv, r, prog)
	case validate.EnginePostgres:
		msg, err = s.restorePostgres(ctx, sv, r, prog)
	default:
		err = fmt.Errorf("motor no soportado: %s", sv.Engine)
	}
	if err != nil {
		s.Store.FinishRestore(rec.ID, "error", CleanErr(err))
		s.logOp(sv.ID, "restore", sv.Name+"/"+r.TargetDatabase, err)
		emit(0, CleanErr(err), true, true)
		return rec, err
	}
	if msg == "" {
		msg = "Restauración completada"
	}
	s.Store.FinishRestore(rec.ID, "ok", msg)
	s.logOp(sv.ID, "restore", sv.Name+"/"+r.TargetDatabase, nil)
	emit(100, msg, true, false)
	return s.Store.GetRestore(rec.ID)
}

func (s *Services) restoreMSSQL(ctx context.Context, sv store.Server, r RestoreRequest, prog func(int, string)) error {
	conn, err := s.ConnFor(sv)
	if err != nil {
		return err
	}
	e, err := mssql.New(conn)
	if err != nil {
		return err
	}
	defer e.Close()
	serverPath := r.FilePath
	if sv.Mode == "local" {
		cli, err := s.Docker()
		if err != nil {
			return err
		}
		prog(2, "Copiando el backup al contenedor")
		if serverPath, err = s.copyInMSSQL(ctx, cli, sv.ContainerName, r.FilePath); err != nil {
			return err
		}
		defer cli.Exec(context.Background(), sv.ContainerName, dockerx.ExecOptions{Cmd: []string{"rm", "-f", serverPath}})
	}
	prog(10, "Ejecutando RESTORE DATABASE")
	return e.RestoreDatabase(ctx, mssql.RestoreOptions{ServerPath: serverPath, Target: r.TargetDatabase,
		DataDir: r.DataDir, LogDir: r.LogDir, Replace: r.Replace},
		func(p int, m string) { prog(10+p*88/100, m) })
}

func (s *Services) restorePostgres(ctx context.Context, sv store.Server, r RestoreRequest, prog func(int, string)) (string, error) {
	conn, err := s.ConnFor(sv)
	if err != nil {
		return "", err
	}
	e, err := postgres.New(conn)
	if err != nil {
		return "", err
	}
	defer e.Close()
	format, err := postgres.DetectFormatFile(r.FilePath)
	if err != nil {
		return "", err
	}
	exists, err := e.DatabaseExists(ctx, r.TargetDatabase)
	if err != nil {
		return "", err
	}
	if exists && !r.Replace {
		return "", fmt.Errorf("la base de datos %q ya existe; elija otro nombre o active \"reemplazar\"", r.TargetDatabase)
	}
	// Se restaura en una base temporal; solo si todo sale bien reemplaza al destino.
	suffix := fmt.Sprintf("__dbm_restore_%d", time.Now().Unix())
	base := r.TargetDatabase
	if len(base) > 63-len(suffix) {
		base = base[:63-len(suffix)]
	}
	tmpDB := base + suffix
	prog(10, "Creando base temporal de restauración")
	if err := e.CreateDatabase(ctx, tmpDB, ""); err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			dctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			e.DropDatabase(dctx, tmpDB)
		}
	}()
	cli, err := s.Docker()
	if err != nil {
		return "", err
	}
	var code int64
	var out string
	if sv.Mode == "local" {
		prog(20, "Copiando el backup al contenedor")
		tmpName := ".dbm-" + randHex() + "-" + filepath.Base(r.FilePath)
		if err := cli.CopyToContainerAs(ctx, sv.ContainerName, r.FilePath, "/tmp", tmpName); err != nil {
			return "", fmt.Errorf("no se pudo copiar el archivo al contenedor: %w", err)
		}
		tmp := "/tmp/" + tmpName
		defer cli.Exec(context.Background(), sv.ContainerName, dockerx.ExecOptions{Cmd: []string{"rm", "-f", tmp}})
		prog(40, "Restaurando ("+format+")")
		res, err := cli.Exec(ctx, sv.ContainerName, dockerx.ExecOptions{
			Cmd: postgres.RestoreCmd(format, "", 0, sv.AdminUser, tmpDB, tmp), Env: []string{"PGPASSWORD=" + conn.Password}})
		if err != nil {
			return "", err
		}
		code, out = int64(res.ExitCode), res.Stderr+res.Stdout
	} else {
		ver, err := e.Ping(ctx)
		if err != nil {
			return "", err
		}
		img := postgresImageFor(ver)
		prog(15, "Preparando contenedor temporal "+img)
		if err := s.ensureImage(ctx, cli, img, func(m string) { prog(15, m) }); err != nil {
			return "", err
		}
		prog(40, "Restaurando ("+format+")")
		code, out, err = cli.RunOnce(ctx, dockerx.CreateOptions{
			Name: "dbmanager-tmp-restore-" + fmt.Sprint(time.Now().UnixNano()), Image: img, Engine: "postgres",
			Env:         []string{"PGPASSWORD=" + conn.Password, "PGSSLMODE=prefer"},
			Cmd:         postgres.RestoreCmd(format, dockerHostFor(conn.Host), conn.Port, conn.User, tmpDB, "/in/"+filepath.Base(r.FilePath)),
			Binds:       []dockerx.Bind{{Source: filepath.Dir(r.FilePath), Target: "/in", ReadOnly: true}},
			HostGateway: true,
		})
		if err != nil {
			return "", err
		}
	}
	commit := func() error {
		prog(95, "Aplicando la restauración")
		if exists {
			if err := e.DropDatabase(ctx, r.TargetDatabase); err != nil {
				return err
			}
		}
		if err := e.RenameDatabase(ctx, tmpDB, r.TargetDatabase); err != nil {
			return err
		}
		committed = true
		return nil
	}
	if code != 0 {
		if format == postgres.FormatCustom && strings.Contains(out, "errors ignored on restore") {
			if err := commit(); err != nil {
				return "", err
			}
			return "Restauración completada con advertencias: " + tail(strings.TrimSpace(out), 600), nil
		}
		return "", WrapErr(fmt.Errorf("la restauración terminó con código %d: %s", code, tail(strings.TrimSpace(out), 1500)))
	}
	return "", commit()
}

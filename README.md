# DB Manager

App de escritorio para **Windows, macOS y Ubuntu** que gestiona motores **SQL Server** y **PostgreSQL**
en Docker local, y también servidores remotos ya existentes.

- Crear servicios locales (contenedores) con asistente: versión, puerto, contraseña, edición y dónde guardar los datos.
- Iniciar, detener, reiniciar, ver logs, importar contenedores existentes y conectar servidores remotos.
- Bases de datos: crear y eliminar.
- Usuarios: crear, eliminar, cambiar contraseña y roles de servidor.
- Permisos por base de datos: Propietario, Lectura y escritura, Solo lectura o Sin acceso.
- Backups y restauraciones eligiendo el archivo y la carpeta, con barra de progreso.
- **SFTP:**
  - Servicios `atmoz/sftp` con usuarios por contraseña y/o llave.
  - Genera llaves ed25519/RSA 4096 (OpenSSH + `.ppk` para WinSCP/PuTTY).
  - Huella del servidor estable y líneas `known_hosts` listas para copiar.
  - Botón "Probar acceso" con login real.
  - Importa contenedores SFTP existentes solo para iniciar, detener y ver logs.
- **Certificados:**
  - Genera un `.p12` a partir de la llave privada, el certificado de dominio, el intermedio y la raíz (opcional), con alias y contraseña.
  - Valida la cadena antes de generar.
  - Después verifica lo mismo que `keytool -list -v`: Entry type, Alias name y Certificate chain length. Si hay Java instalado, ejecuta además `keytool`.
  - Nota: keytool muestra los alias de PKCS#12 en minúsculas.
- Historial y configuración guardados en SQLite.

## Para quien usa la app

Requisito único: **Docker**.

| Sistema | Docker | Notas |
|---|---|---|
| Windows 10/11 | Docker Desktop | — |
| macOS | Docker Desktop | En Mac con chip Apple (M1/M2/M3), SQL Server necesita activar *Settings → General → "Use Rosetta for x86_64/amd64 emulation"*. La primera vez que abras la app: clic derecho → **Abrir** (la app no está firmada por Apple). |
| Ubuntu | Docker Engine | El usuario debe estar en el grupo `docker`: `sudo usermod -aG docker $USER` y volver a iniciar sesión. |

Los servicios creados escuchan solo en tu equipo (`127.0.0.1`), salvo que actives
"Permitir conexiones desde otros equipos de la red" en el asistente.

Los datos de la app (servidores, historial, ajustes y contraseñas cifradas con AES-256) se guardan en:
- Windows: `%AppData%\dbmanager\`
- macOS: `~/Library/Application Support/dbmanager/`
- Linux: `~/.config/dbmanager/`

El archivo `secret.key` de esa carpeta cifra las contraseñas guardadas. No lo compartas. Si lo borras, tendrás que volver a introducir las contraseñas.

### Backups y restauraciones
- **Servicio local:** eliges cualquier archivo o carpeta de tu equipo. La app copia los archivos al contenedor y desde él.
- **SQL Server remoto:** las rutas son **del servidor remoto**, porque SQL Server lee y escribe los backups en su propio disco.
- **PostgreSQL remoto:** la app usa un contenedor temporal `postgres` con `pg_dump`/`pg_restore`, así que no necesitas instalar herramientas.
- **Restauración con "Reemplazar si existe" en PostgreSQL:** primero se restaura en una base temporal y solo si todo sale bien sustituye a la original.

## Para desarrollar

Requisitos: Go 1.27+, Node 22+, Wails CLI v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1`).
En Ubuntu, además: `sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev`.

```bash
wails dev                 # desarrollo con recarga en caliente
wails build               # ejecutable en build/bin/
# Ubuntu 24.04: wails build -tags webkit2_41
# macOS universal: wails build -platform darwin/universal
```

Pruebas:
```bash
go test ./...                                              # unitarias (sin Docker)
go test -tags integration -run TestSmoke -v ./internal/services   # humo con contenedores reales
go test -tags integration -run 'TestPerm|TestSQL' -v .     # integración por subconjuntos
```
Las pruebas de integración crean contenedores `dbmanager-test-*` en puertos libres y los eliminan al terminar.
Ejecútalas por subconjuntos (`-run`), porque varias instancias de SQL Server a la vez consumen mucha memoria.

### Compilar para los 3 sistemas
Wails no compila de forma cruzada para macOS ni para Linux desde Windows. El workflow
`.github/workflows/build.yml` compila en GitHub Actions para Windows, macOS (universal) y Ubuntu
al publicar una etiqueta `v*`, o manualmente desde la pestaña *Actions*.

## Estructura

```
main.go, app.go, app_ops.go, types.go   # arranque y métodos expuestos a la interfaz
internal/store      # SQLite (modernc, sin CGO), migraciones, cifrado AES-GCM
internal/dockerx    # SDK de Docker: contexto activo, pull, contenedores, exec, copias
internal/engine     # interfaz Engine + mssql/ (go-mssqldb) y postgres/ (pgx)
internal/services   # crear/importar servicios, backups, restauraciones
internal/validate   # validación y escapado seguro de identificadores
frontend/           # React + TypeScript + Tailwind
docs/ENVIRONMENT.md # notas del entorno de desarrollo
PLAN.md             # plan y contrato backend ↔ frontend
```

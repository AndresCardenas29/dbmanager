# DB Manager — Plan del proyecto

App de escritorio multiplataforma (Windows, macOS, Ubuntu) para gestionar motores de bases de datos
**SQL Server** y **PostgreSQL** que corren en **Docker local**, con soporte para registrar
**servidores remotos ya existentes** (solo conexión). El usuario final solo necesita Docker instalado.

Idioma de la interfaz: **español**.

## Stack

| Capa | Tecnología |
|---|---|
| App de escritorio | Go 1.27 + **Wails v2** (v2.17.x) |
| Frontend | React 18 + TypeScript + Vite + Tailwind CSS (plantilla `react-ts` de Wails) |
| Docker | SDK oficial `github.com/docker/docker/client` |
| SQL Server | `github.com/microsoft/go-mssqldb` |
| PostgreSQL | `github.com/jackc/pgx/v5` |
| Almacenamiento interno | SQLite con `modernc.org/sqlite` (Go puro, **sin CGO** — importante para compilar en los 3 SO) |

Datos de la app en `os.UserConfigDir()/dbmanager/` → `app.db` (SQLite) y `secret.key` (clave AES-256, permisos 0600).

## Funcionalidades

1. **Servicios (motores)**
   - Crear servicio local en Docker con asistente: motor (mssql/postgres), nombre, versión/imagen, puerto,
     contraseña de administrador, almacenamiento de datos (volumen Docker con nombre **o** carpeta del host elegida por el usuario).
   - Iniciar / detener / reiniciar / eliminar (con opción de conservar datos), ver estado y logs.
   - **Importar contenedor existente** (p. ej. `sqlserver-apimanager` ya creado) como servicio gestionado.
   - Registrar **servidor remoto** (host, puerto, usuario admin, contraseña, TLS) — sin control de Docker.
   - Comprobaciones: Docker instalado/en ejecución; en macOS ARM avisar de Rosetta para SQL Server (imagen `linux/amd64`);
     en Linux, avisar si el usuario no tiene permiso sobre el socket de Docker.
2. **Bases de datos**: listar (tamaño, estado, dueño), crear (con dueño opcional), eliminar (confirmación escribiendo el nombre).
3. **Usuarios / logins**: listar, crear, eliminar, cambiar contraseña, roles de servidor
   (mssql: `sysadmin`, `dbcreator`, `securityadmin`; postgres: `SUPERUSER`, `CREATEDB`, `CREATEROLE`).
4. **Permisos por base de datos**: matriz usuario × base de datos con niveles predefinidos:
   - `owner` → mssql `db_owner` / postgres dueño de la BD
   - `readwrite` → mssql `db_datareader`+`db_datawriter` / postgres CONNECT + USAGE + SELECT/INSERT/UPDATE/DELETE en el esquema public + default privileges
   - `readonly` → mssql `db_datareader` / postgres CONNECT + USAGE + SELECT + default privileges
   - `none` → quitar acceso
5. **Backups**: crear backup de una BD y guardarlo en una **carpeta del host elegida por el usuario**.
   - mssql local: `BACKUP DATABASE ... TO DISK` dentro del contenedor → copiar al host (CopyFromContainer).
   - postgres local: `pg_dump -Fc` vía `docker exec` → copiar al host.
   - remoto: mssql → ruta en el servidor remoto indicada por el usuario; postgres → ejecutar `pg_dump` en un contenedor
     temporal `postgres:<versión>` apuntando al host remoto (así no hace falta tener herramientas instaladas).
6. **Restauraciones**: el usuario elige el **archivo de backup en cualquier ubicación del host** y el **destino**:
   - mssql: se copia el `.bak` al contenedor, `RESTORE FILELISTONLY` para leer los archivos lógicos, el usuario elige
     nombre de la BD destino y carpeta de datos/log (por defecto `/var/opt/mssql/data`), `RESTORE ... WITH MOVE, REPLACE(opcional), STATS=5`.
   - postgres: `.dump`/`.backup` → `pg_restore`; `.sql` → `psql`. Crea la BD destino si no existe.
   - Progreso por eventos de Wails; historial guardado en SQLite.
7. **Historial y configuración (SQLite)**: servidores, rutas de backups, restauraciones, últimas carpetas usadas, ajustes, registro de operaciones.

## Estructura del repositorio

```
dbmanager/
  main.go, app.go            # arranque Wails; app.go = métodos expuestos al frontend (contrato abajo)
  wails.json
  internal/
    store/       # SQLite: migraciones, repositorios, cifrado de secretos (AES-GCM)
    dockerx/     # wrapper del SDK de Docker: ping, pull con progreso, create/start/stop/rm, logs, exec, copy in/out
    engine/      # interfaz Engine + implementaciones mssql/ y postgres/
    services/    # orquestación: servicios, backups, restauraciones (usa store + dockerx + engine)
    validate/    # validación de nombres, quoting seguro de identificadores
  frontend/      # React + TS + Tailwind
  .github/workflows/build.yml  # build matriz windows/macos/ubuntu
  PLAN.md, README.md
```

## Reglas de seguridad (obligatorias)

- **Nunca** concatenar nombres sin escapar. mssql: `[nombre]` duplicando `]`; postgres: `pgx.Identifier{}.Sanitize()`.
- Validar nombres de BD/usuarios: `^[A-Za-z_][A-Za-z0-9_\-]{0,127}$` (postgres ≤ 63).
- Contraseñas en literales SQL (CREATE LOGIN / ALTER ROLE no aceptan parámetros): escapar `'` → `''`. Nunca loguearlas.
- Contraseñas guardadas en SQLite **cifradas** con AES-GCM; nunca se devuelven al frontend (solo `hasPassword: bool`).
- Las pruebas de integración usan contenedores **temporales propios** con nombres `dbmanager-test-*` y puertos libres;
  **jamás** tocar el contenedor existente `sqlserver-apimanager` ni sus bases (APIManager, iflowdb, iflow_test).

## Contrato backend ↔ frontend (métodos de `App` en app.go)

Todos devuelven `(T, error)`; los errores llegan al frontend como excepción con mensaje en español.
Los tipos Go llevan tags JSON en camelCase idénticos a los nombres de abajo.

```go
// Entorno
CheckEnvironment() (EnvStatus, error)          // {dockerAvailable, dockerVersion, os, arch, rosettaWarning, messages[]}

// Servidores / servicios
ListServers() ([]Server, error)
GetServer(id int64) (Server, error)
CreateLocalService(req CreateServiceRequest) (Server, error)   // crea contenedor + registro; emite "service:progress"
ImportContainer(req ImportContainerRequest) (Server, error)
ListImportableContainers() ([]ContainerInfo, error)            // contenedores mssql/postgres no registrados
AddRemoteServer(req RemoteServerRequest) (Server, error)
UpdateServer(id int64, req UpdateServerRequest) (Server, error)
DeleteServer(id int64, removeContainer bool, removeData bool) error
StartServer(id int64) error
StopServer(id int64) error
RestartServer(id int64) error
ServerStatus(id int64) (ServerStatus, error)                   // {state: running|stopped|missing|remote, reachable, version, message}
ServerLogs(id int64, tail int) (string, error)
TestConnection(id int64) (ServerStatus, error)

// Bases de datos
ListDatabases(serverId int64) ([]Database, error)              // {name, sizeMB, owner, state, system}
CreateDatabase(serverId int64, name string, owner string) error
DropDatabase(serverId int64, name string) error

// Usuarios y permisos
ListLogins(serverId int64) ([]Login, error)                    // {name, serverRoles[], disabled, isSystem}
CreateLogin(serverId int64, req CreateLoginRequest) error      // {name, password, serverRoles[]}
DropLogin(serverId int64, name string) error
ChangePassword(serverId int64, name string, password string) error
SetServerRoles(serverId int64, name string, roles []string) error
GetPermissionMatrix(serverId int64) (PermissionMatrix, error)  // {logins[], databases[], cells:[{login, database, level}]}
SetDatabaseAccess(serverId int64, login string, database string, level string) error  // owner|readwrite|readonly|none

// Backups / restauraciones
PickFile(title string, filters []FileFilter) (string, error)   // diálogo nativo; recuerda última carpeta
PickDirectory(title string) (string, error)
CreateBackup(req BackupRequest) (BackupRecord, error)          // {serverId, database, destDir}; emite "backup:progress"
ListBackups(serverId int64) ([]BackupRecord, error)            // serverId 0 = todos
InspectBackup(serverId int64, filePath string) (BackupInfo, error)  // mssql: archivos lógicos; postgres: formato
RestoreBackup(req RestoreRequest) (RestoreRecord, error)       // {serverId, filePath, targetDatabase, dataDir, logDir, replace}; emite "restore:progress" {id, percent, message}
ListRestores(serverId int64) ([]RestoreRecord, error)

// Ajustes e historial
GetSettings() (map[string]string, error)
SetSetting(key string, value string) error
ListOperations(limit int) ([]Operation, error)                 // registro de operaciones
```

Tipos principales:
```
Server { id, name, engine: "mssql"|"postgres", mode: "local"|"remote", host, port, adminUser, hasPassword,
         containerName, image, dataMode: "volume"|"bind"|"", dataPath, createdAt }
CreateServiceRequest { name, engine, image, port, adminPassword, dataMode, dataPath, memoryMB }
ImportContainerRequest { containerName, adminUser, adminPassword }
RemoteServerRequest { name, engine, host, port, adminUser, adminPassword, tls, trustServerCert }
BackupRecord { id, serverId, serverName, database, filePath, sizeBytes, createdAt, status, message }
RestoreRecord { id, serverId, serverName, sourceFile, targetDatabase, dataDir, logDir, status, message, startedAt, finishedAt }
BackupInfo { format, databaseName, files: [{logicalName, physicalName, type: "data"|"log"}] }
Operation { id, serverId, kind, target, status, message, createdAt }
```

## Fases y responsables

| Fase | Agente | Entregable |
|---|---|---|
| 0 | Orquestador | Go + Wails instalados, este plan |
| 1 | Explorador (Haiku) | Inventario del entorno Docker actual (contenedores, puertos, volúmenes) para la función de importar |
| 1 | Desarrollador (Sonnet) | Scaffold Wails, `app.go` con el contrato completo (stubs que compilan), `store`, `validate`, `dockerx` |
| 2 | Desarrollador (Sonnet) | `engine/mssql`, `engine/postgres`, `services` (backups, restauraciones), implementación real de `app.go` |
| 2 | Frontend (Sonnet) | UI completa en español contra el contrato (empieza cuando la fase 1 compila) |
| 3 | Test (Sonnet) | Unit tests + integración con contenedores temporales + `wails build`; informe de fallos |
| 4 | Desarrollador / Frontend | Corrección de fallos del informe de test |
| 5 | Orquestador | Verificación final, README, workflow de build multiplataforma |

## Notas de entorno (Windows, máquina de desarrollo)

- Go y Wails pueden no estar en el PATH de una shell nueva. En PowerShell, al inicio de cada comando:
  `$env:Path = [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User') + ";$env:USERPROFILE\go\bin"`
- Node 26 / npm 11 disponibles. Docker Desktop 29 en ejecución.

---

# Ampliación v2: SFTP y certificados (2026-11-26)

La app pasa a ser una caja de herramientas con 3 secciones en la barra lateral: **Bases de datos** (lo actual),
**SFTP** y **Certificados**. Todo se guarda en el mismo SQLite. Mismas reglas de seguridad que v1.

## A. Servicios SFTP (Docker)

- Imagen por defecto `atmoz/sftp:alpine` (la que ya usa el usuario). Contenedores con labels `dbmanager.managed=true`, `dbmanager.engine=sftp`.
- **Configuración gestionada por la app** en `<appdata>/sftp/<serviceId>/`:
  - `users.conf` (formato atmoz: `user:pass:uid:gid:dir`; contraseña vacía si solo usa llave; uid fijos desde 1001 y estables por usuario),
  - `keys/<user>/*.pub` montado en `/home/<user>/.ssh/keys/` (solo lectura),
  - `hostkeys/ssh_host_ed25519_key` + `ssh_host_rsa_key` generadas por la app **una vez** y montadas en `/etc/ssh/`, para que la huella del servidor no cambie al recrear el contenedor.
- Datos: carpeta del host elegida por el usuario (bind) o volumen Docker; cada usuario tiene `/home/<user>/<uploadDir>` (por defecto `upload`) escribible y chroot en `/home/<user>`. Verificar permisos con bind mounts en Docker Desktop (Windows/macOS).
- Cualquier cambio de usuarios o llaves regenera la configuración y **recrea** el contenedor (atmoz lee users.conf al arrancar); los datos persisten.
- Puerto por defecto: el siguiente libre desde **2224** (22 y 2223 ocupados en la máquina de desarrollo). Escucha en 127.0.0.1 salvo `exposeOnNetwork`.
- **Llaves de acceso**: generar en Go (`golang.org/x/crypto/ssh`): ed25519 (por defecto) o RSA 4096, frase de paso opcional. La llave privada se **guarda en la carpeta elegida por el usuario**: `<user>_id_ed25519` (formato OpenSSH), `.pub` y, opcionalmente, `.ppk` (PuTTY/WinSCP). **La app no guarda la llave privada**; solo la pública, su huella SHA256 y un comentario. También se puede pegar una llave pública existente.
- **Importar** contenedores sftp existentes (p. ej. `sftp-iflow`, `sftp_server`) en modo `external`: solo iniciar/detener/logs/estado; usuarios no editables (su configuración no la gestiona la app). Nunca recrearlos.
- **Probar acceso**: login real por SSH/SFTP (`github.com/pkg/sftp`) con contraseña o llave, listar el directorio home.

### Contrato SFTP
```go
ListSftpServices() ([]SftpService, error)
CreateSftpService(req CreateSftpRequest) (SftpService, error)   // {name, image, port, dataMode, dataPath, exposeOnNetwork, requestId}; emite "sftp:progress"
ListImportableSftpContainers() ([]ContainerInfo, error)
ImportSftpContainer(containerName string) (SftpService, error)
DeleteSftpService(id int64, removeContainer bool, removeData bool) error
StartSftpService(id int64) error
StopSftpService(id int64) error
RestartSftpService(id int64) error
SftpServiceStatus(id int64) (ServerStatus, error)
SftpServiceLogs(id int64, tail int) (string, error)
GetSftpHostFingerprints(id int64) ([]string, error)            // "ED25519 SHA256:..." para known_hosts
ListSftpUsers(serviceId int64) ([]SftpUser, error)
CreateSftpUser(serviceId int64, req CreateSftpUserRequest) (SftpUserResult, error)
DeleteSftpUser(serviceId int64, name string, deleteFiles bool) error
SetSftpUserPassword(serviceId int64, name string, password string) error   // "" = quitar contraseña (solo llave)
AddSftpUserKey(serviceId int64, name string, req SftpKeyRequest) (SftpUserResult, error)
RemoveSftpUserKey(serviceId int64, name string, keyId int64) error
TestSftpLogin(req SftpLoginTest) (SftpLoginResult, error)
```
Tipos:
```
SftpService { id, name, mode: "managed"|"external", containerName, image, host, port, dataMode, dataPath, exposeOnNetwork, createdAt }
SftpUser { name, uid, hasPassword, uploadDir, keys: [SftpKey], createdAt }
SftpKey { id, comment, type, fingerprint, publicKey, createdAt }
CreateSftpUserRequest { name, password, uploadDir, key: SftpKeyRequest|null }
SftpKeyRequest { mode: "generate"|"paste", keyType: "ed25519"|"rsa4096", passphrase, comment, outputDir, exportPpk, publicKey }
SftpUserResult { user: SftpUser, privateKeyPath, publicKeyPath, ppkPath, connectionHints: [string] }
SftpLoginTest { serviceId, user, password, privateKeyPath, passphrase }
SftpLoginResult { ok, message, hostFingerprint, entries: [string] }
```

## B. Certificados: generar PKCS#12 (.p12)

Equivalente a:
`openssl pkcs12 -export -inkey privkey.pem -in cert.pem -certfile intermediate.pem [+ root] -name <alias> -out nuevo.p12`
y su verificación `keytool -list -v -keystore nuevo.p12 -storetype PKCS12 -storepass ...`.

- Go puro con `software.sslmate.com/src/go-pkcs12` (sin depender de openssl). Formato `modern` (AES-256, por defecto; Java 8u301+/11+) o `legacy` (3DES/RC2, para Java/Tomcat antiguos).
- Entradas: llave privada (PEM PKCS#1/PKCS#8/EC, también cifrada con frase de paso; o DER), certificado de dominio, intermedio (puede contener varios certificados), raíz **opcional**. Alias (por defecto `tomcat`), contraseña (≥ 6 caracteres, requisito de keytool), ruta de salida.
- Validaciones **antes** de generar: la llave corresponde al certificado; el certificado está firmado por el intermedio; el intermedio por la raíz (si se da); la raíz es autofirmada; fechas (vencido / vence en < 30 días = aviso); orden correcto de la cadena; mostrar CN/SAN.
- **Verificación después de generar** (lo que el usuario revisa con keytool): decodificar el .p12 con la contraseña y comprobar `Entry type: PrivateKeyEntry`, `Alias name == alias pedido`, `Certificate chain length` = 2 o 3 (según se incluya la raíz), llave ↔ certificado. Si `keytool` existe (PATH o `JAVA_HOME/bin`), ejecutarlo con `-storepass:env` (nunca la contraseña en la línea de comandos) y parsear su salida como verificación adicional, mostrando la salida.
- Historial en SQLite: alias, CN, vencimiento, ruta de salida, fecha, resultado de verificación. **Nunca** guardar contraseñas.

### Contrato certificados
```go
InspectCertFile(path string, passphrase string) (CertFileInfo, error)
BuildPkcs12(req Pkcs12Request) (Pkcs12Result, error)
VerifyPkcs12(path string, password string, expectedAlias string) (Pkcs12Verification, error)
ListCertificateHistory(limit int) ([]CertHistory, error)
SaveFile(title string, defaultName string, filters []FileFilter) (string, error)   // diálogo nativo "guardar como"
```
Tipos:
```
CertFileInfo { kind: "privateKey"|"certificate"|"bundle", keyType, encrypted, certs: [CertSummary] }
CertSummary { subject, issuer, commonName, sans: [string], notBefore, notAfter, serial, isCA, selfSigned, sha256 }
Pkcs12Request { privateKeyPath, keyPassphrase, certPath, intermediatePath, rootPath, alias, password, outputPath, format: "modern"|"legacy" }
Pkcs12Result { outputPath, verification: Pkcs12Verification, warnings: [string] }
Pkcs12Verification { ok, entryType, alias, chainLength, chain: [CertSummary], keyMatchesCert, checks: [{name, ok, detail}], keytoolAvailable, keytoolOk, keytoolOutput }
CertHistory { id, alias, commonName, notAfter, outputPath, chainLength, verified, createdAt }
```

## Fases v2
| Fase | Agente | Entregable |
|---|---|---|
| v2-1 | Explorador (Haiku) | Configuración de los contenedores sftp existentes (atmoz), disponibilidad de keytool/openssl/java |
| v2-1 | Desarrollador | Tipos + stubs de todo el contrato v2 + bindings; luego implementación real |
| v2-2 | Frontend | Navegación en 3 secciones, pantallas SFTP y Certificados |
| v2-3 | Test | Pruebas de SFTP (login real con llave/contraseña) y PKCS#12 (cadenas válidas/inválidas generadas en el test) |

# Entorno de desarrollo (inventario del explorador, 2026-11-26)

Sin secretos. Fuente: agente explorador (solo lectura).

## Docker
- Docker Desktop 4.54.0, Engine 29.1.2, backend WSL2, linux/amd64.
- Contexto activo: `desktop-linux` → `npipe:////./pipe/dockerDesktopLinuxEngine`. El contexto `default` usa `npipe:////./pipe/docker_engine`.
  **La app debe respetar el contexto activo** (leer `~/.docker/config.json` → `currentContext` y `~/.docker/contexts/meta/*/meta.json`), con `DOCKER_HOST` como prioridad y el pipe por defecto como último recurso.
- Imágenes: `mcr.microsoft.com/mssql/server:2022-latest` ya descargada; **postgres no** (hay que hacer pull con progreso).
- No hay volúmenes con nombre.

## Contenedor existente `sqlserver-apimanager` (NO TOCAR en pruebas)
- Imagen mssql/server:2022-latest, SQL Server 2022 CU22 Express. Proyecto compose `docker-axa` (archivo `docker-compose-alt.yml`).
- Variable de contraseña: **`SA_PASSWORD`** (no `MSSQL_SA_PASSWORD`). Usuario del contenedor `0:0` (root).
- Puerto 1433→1433. Montajes **bind**: `sqlserver-data` → `/var/opt/mssql/data`, `APIManager.bak` → `/var/opt/mssql/backup/APIManager.bak`, `init-db.sql` → `/init-db.sql`.
- sqlcmd: **`/opt/mssql-tools18/bin/sqlcmd -C`** (la ruta `/opt/mssql-tools/bin/sqlcmd` no existe).
- Bases: master, tempdb, model, msdb, APIManager, iflowdb, iflow_test. Logins SQL: sa, APIUser, iflowusr.

## Puertos
- Ocupados: 1433 (Docker), **1434 (sqlservr.exe nativo de Windows)**, 22, 2223, 8080 (Keycloak si arranca).
- Libres: 5432, 5433, 14330-14340, 54320-54330.
- Defaults recomendados: servicios mssql nuevos desde **14331**, postgres desde **54321** (siguiente libre). Pruebas: puerto libre dinámico (listener en 127.0.0.1:0).
- Para detectar colisiones revisar puertos publicados por Docker **y** listeners del host.

## Heurísticas de importación
- mssql: imagen contiene `mssql/server` o `azure-sql-edge`, o cmd/entrypoint contiene `sqlservr`, o env `ACCEPT_EULA`; puerto 1433/tcp.
- postgres: imagen `postgres`, `postgis/postgis`, `bitnami/postgresql`, o env `POSTGRES_PASSWORD`/`POSTGRES_USER`, o montaje en `/var/lib/postgresql/data`; puerto 5432/tcp.
- Contraseña: no se lee del inspect; el formulario de importación la pide.
- Puerto real: `NetworkSettings.Ports[...][0].HostPort`. Datos: montaje bind → `dataMode: bind`, volumen → `volume`.
- Al importar, no recrear el contenedor (respetar user 0:0 y montajes).

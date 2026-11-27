// Tipos planos del contrato (espejo de types.go). Se usan en lugar de las clases generadas.
export type Engine = 'mssql' | 'postgres'
export type ServerState = 'running' | 'stopped' | 'missing' | 'remote'
export type AccessLevel = 'owner' | 'readwrite' | 'readonly' | 'custom' | 'none'

export interface EnvStatus {
  dockerAvailable: boolean; dockerVersion: string; os: string; arch: string
  rosettaWarning: boolean; messages: string[] | null
}
export interface Server {
  id: number; name: string; engine: Engine; mode: 'local' | 'remote'; host: string; port: number
  adminUser: string; hasPassword: boolean; containerName: string; image: string
  dataMode: 'volume' | 'bind' | ''; dataPath: string; tls: boolean; trustServerCert: boolean; createdAt: string
}
export interface CreateServiceRequest {
  name: string; engine: Engine; image: string; port: number; adminPassword: string
  dataMode: 'volume' | 'bind'; dataPath: string; memoryMB: number; edition?: string; exposeOnNetwork?: boolean; requestId: string
  rootUser?: string; rootPassword?: string
}
export interface RootDefaults { user: string; passwordIsDefault: boolean }
export interface RootStatus { user: string; reachable: boolean; isAdmin: boolean; message: string }
export interface ConfigureRootRequest {
  user: string; password: string; bootstrapUser: string; bootstrapPassword: string; dropPrevious: boolean
}
export interface ConfigureRootResult { server: Server; warning: string }
export interface ImportContainerRequest { containerName: string; adminUser: string; adminPassword: string }
export interface RemoteServerRequest {
  name: string; engine: Engine; host: string; port: number; adminUser: string; adminPassword: string
  tls: boolean; trustServerCert: boolean
}
export interface UpdateServerRequest {
  name: string; host: string; port: number; adminUser: string; adminPassword: string
  tls: boolean; trustServerCert: boolean
}
export interface ContainerInfo {
  id: string; name: string; image: string; engine: Engine; state: string; status: string
  hostPort: number; containerPort: number; dataMode: string; dataPath: string
}
export interface ServerStatus { state: ServerState; reachable: boolean; version: string; message: string }
export interface Database { name: string; sizeMB: number; owner: string; state: string; system: boolean }
export interface Login { name: string; serverRoles: string[] | null; disabled: boolean; isSystem: boolean }
export interface CreateLoginRequest { name: string; password: string; serverRoles: string[] }
export interface PermissionCell { login: string; database: string; level: AccessLevel }
export interface PermissionMatrix { logins: string[] | null; databases: string[] | null; cells: PermissionCell[] | null }
export interface FileFilter { displayName: string; pattern: string }
export interface BackupRequest { serverId: number; database: string; destDir: string; requestId: string }
export interface BackupRecord {
  id: number; serverId: number; serverName: string; database: string; filePath: string
  sizeBytes: number; createdAt: string; status: string; message: string
}
export interface BackupFile { logicalName: string; physicalName: string; type: 'data' | 'log' }
export interface BackupInfo { format: string; databaseName: string; files: BackupFile[] | null }
export interface RestoreRequest {
  serverId: number; filePath: string; targetDatabase: string; dataDir: string; logDir: string; replace: boolean; requestId: string
}
export interface RestoreRecord {
  id: number; serverId: number; serverName: string; sourceFile: string; targetDatabase: string
  dataDir: string; logDir: string; status: string; message: string; startedAt: string; finishedAt: string
}
export interface Operation {
  id: number; serverId: number; kind: string; target: string; status: string; message: string; createdAt: string
}
export interface ProgressEvent { requestId: string; id: number; percent: number; message: string; done: boolean; error: boolean }

export type Privilege = 'SELECT' | 'INSERT' | 'UPDATE' | 'DELETE' | 'EXECUTE' | 'ALTER'
export interface SchemaInfo { name: string; owner: string; tables: string[] | null; system: boolean }
export interface ObjectGrant { login: string; schema: string; table: string; privileges: Privilege[] | null }
export type ObjectGrantRequest = ObjectGrant
export interface ConnectionUser { login: string; level: string; isRoot: boolean; hasPassword: boolean }

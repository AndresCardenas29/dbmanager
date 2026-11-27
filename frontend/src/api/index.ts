// Envoltorios tipados sobre los bindings de Wails. Los errores llegan como promesas rechazadas.
import * as G from '../../wailsjs/go/main/App'
import type * as T from './types'

export * from './types'
export {errMsg} from './errors'

// Los bindings esperan clases generadas; en runtime son objetos JSON planos.
const x = <R>(p: Promise<unknown>) => p as Promise<R>
const a = (v: unknown) => v as never

export const api = {
  checkEnvironment: () => x<T.EnvStatus>(G.CheckEnvironment()),
  listServers: async () => (await x<T.Server[] | null>(G.ListServers())) ?? [],
  getServer: (id: number) => x<T.Server>(G.GetServer(id)),
  createLocalService: (r: T.CreateServiceRequest) => x<T.Server>(G.CreateLocalService(a(r))),
  importContainer: (r: T.ImportContainerRequest) => x<T.Server>(G.ImportContainer(a(r))),
  listImportable: async () => (await x<T.ContainerInfo[] | null>(G.ListImportableContainers())) ?? [],
  addRemoteServer: (r: T.RemoteServerRequest) => x<T.Server>(G.AddRemoteServer(a(r))),
  updateServer: (id: number, r: T.UpdateServerRequest) => x<T.Server>(G.UpdateServer(id, a(r))),
  deleteServer: (id: number, removeContainer: boolean, removeData: boolean) => G.DeleteServer(id, removeContainer, removeData),
  startServer: (id: number) => G.StartServer(id),
  stopServer: (id: number) => G.StopServer(id),
  restartServer: (id: number) => G.RestartServer(id),
  serverStatus: (id: number) => x<T.ServerStatus>(G.ServerStatus(id)),
  serverLogs: (id: number, tail: number) => G.ServerLogs(id, tail),
  testRemoteConnection: (r: T.RemoteServerRequest) => x<T.ServerStatus>(G.TestRemoteConnection(a(r))),
  testConnection: (id: number) => x<T.ServerStatus>(G.TestConnection(id)),

  listDatabases: async (id: number) => (await x<T.Database[] | null>(G.ListDatabases(id))) ?? [],
  createDatabase: (id: number, name: string, owner: string) => G.CreateDatabase(id, name, owner),
  dropDatabase: (id: number, name: string) => G.DropDatabase(id, name),

  listLogins: async (id: number) => (await x<T.Login[] | null>(G.ListLogins(id))) ?? [],
  createLogin: (id: number, r: T.CreateLoginRequest) => G.CreateLogin(id, a(r)),
  dropLogin: (id: number, name: string) => G.DropLogin(id, name),
  changePassword: (id: number, name: string, pw: string) => G.ChangePassword(id, name, pw),
  setServerRoles: (id: number, name: string, roles: string[]) => G.SetServerRoles(id, name, roles),
  getPermissionMatrix: (id: number) => x<T.PermissionMatrix>(G.GetPermissionMatrix(id)),
  setDatabaseAccess: (id: number, login: string, db: string, level: T.AccessLevel) =>
    G.SetDatabaseAccess(id, login, db, level),
  createDatabaseUser: (id: number, db: string, r: T.CreateLoginRequest, level: T.AccessLevel) =>
    G.CreateDatabaseUser(id, db, a(r), level),

  listSchemas: async (id: number, db: string) => (await x<T.SchemaInfo[] | null>(G.ListSchemas(id, db))) ?? [],
  createSchema: (id: number, db: string, name: string) => G.CreateSchema(id, db, name),
  listObjectGrants: async (id: number, db: string) => (await x<T.ObjectGrant[] | null>(G.ListObjectGrants(id, db))) ?? [],
  setObjectGrant: (id: number, db: string, r: T.ObjectGrantRequest) => G.SetObjectGrant(id, db, a(r)),
  listConnectionUsers: async (id: number, db: string) => (await x<T.ConnectionUser[] | null>(G.ListConnectionUsers(id, db))) ?? [],
  getLoginPassword: (id: number, login: string) => G.GetLoginPassword(id, login),
  rememberLoginPassword: (id: number, login: string, pw: string) => G.RememberLoginPassword(id, login, pw),

  getRootDefaults: () => x<T.RootDefaults>(G.GetRootDefaults()),
  setRootDefaults: (user: string, password: string) => G.SetRootDefaults(user, password),
  getRootStatus: (id: number) => x<T.RootStatus>(G.GetRootStatus(id)),
  configureRoot: (id: number, r: T.ConfigureRootRequest) => x<T.ConfigureRootResult>(G.ConfigureRoot(id, a(r))),

  pickFile: (title: string, filters: T.FileFilter[]) => G.PickFile(title, a(filters)),
  pickDirectory: (title: string) => G.PickDirectory(title),
  createBackup: (r: T.BackupRequest) => x<T.BackupRecord>(G.CreateBackup(a(r))),
  listBackups: async (id: number) => (await x<T.BackupRecord[] | null>(G.ListBackups(id))) ?? [],
  inspectBackup: (id: number, path: string) => x<T.BackupInfo>(G.InspectBackup(id, path)),
  restoreBackup: (r: T.RestoreRequest) => x<T.RestoreRecord>(G.RestoreBackup(a(r))),
  listRestores: async (id: number) => (await x<T.RestoreRecord[] | null>(G.ListRestores(id))) ?? [],

  getSettings: async () => (await G.GetSettings()) ?? {},
  setSetting: (k: string, v: string) => G.SetSetting(k, v),
  listOperations: async (limit: number) => (await x<T.Operation[] | null>(G.ListOperations(limit))) ?? [],
}

import * as G from '../../wailsjs/go/main/App'
import type * as T from './types'
import type * as V from './types2'

export * from './types2'

const x = <R>(p: Promise<unknown>) => p as Promise<R>
const a = (v: unknown) => v as never

/** API v2: SFTP y certificados. */
export const api2 = {
  listSftpServices: async () => (await x<V.SftpService[] | null>(G.ListSftpServices())) ?? [],
  createSftpService: (r: V.CreateSftpRequest) => x<V.SftpService>(G.CreateSftpService(a(r))),
  listImportableSftp: async () => (await x<T.ContainerInfo[] | null>(G.ListImportableSftpContainers())) ?? [],
  importSftp: (name: string) => x<V.SftpService>(G.ImportSftpContainer(name)),
  deleteSftp: (id: number, rc: boolean, rd: boolean) => G.DeleteSftpService(id, rc, rd),
  startSftp: (id: number) => G.StartSftpService(id),
  stopSftp: (id: number) => G.StopSftpService(id),
  restartSftp: (id: number) => G.RestartSftpService(id),
  sftpStatus: (id: number) => x<T.ServerStatus>(G.SftpServiceStatus(id)),
  sftpLogs: (id: number, tail: number) => G.SftpServiceLogs(id, tail),
  sftpHostKeys: async (id: number) => (await G.GetSftpHostKeys(id)) ?? [],
  sftpFingerprints: async (id: number) => (await G.GetSftpHostFingerprints(id)) ?? [],
  listSftpUsers: async (id: number) => (await x<V.SftpUser[] | null>(G.ListSftpUsers(id))) ?? [],
  createSftpUser: (id: number, r: V.CreateSftpUserRequest) => x<V.SftpUserResult>(G.CreateSftpUser(id, a(r))),
  deleteSftpUser: (id: number, name: string, files: boolean) => G.DeleteSftpUser(id, name, files),
  setSftpPassword: (id: number, name: string, pw: string) => G.SetSftpUserPassword(id, name, pw),
  addSftpKey: (id: number, name: string, r: V.SftpKeyRequest) => x<V.SftpUserResult>(G.AddSftpUserKey(id, name, a(r))),
  removeSftpKey: (id: number, name: string, keyId: number) => G.RemoveSftpUserKey(id, name, keyId),
  listSftpFolders: async (id: number) => (await x<V.SftpFolder[] | null>(G.ListSftpFolders(id))) ?? [],
  createSftpFolder: (id: number, name: string) => G.CreateSftpFolder(id, name),
  deleteSftpFolder: (id: number, name: string, files: boolean) => G.DeleteSftpFolder(id, name, files),
  setSftpFolderAccess: (id: number, folder: string, user: string, level: V.SftpFolderLevel) => G.SetSftpFolderAccess(id, folder, user, level),
  openSftpFolder: (id: number, kind: 'service' | 'user' | 'folder', name: string) => G.OpenSftpFolder(id, kind, name),
  testSftpLogin: (r: V.SftpLoginTest) => x<V.SftpLoginResult>(G.TestSftpLogin(a(r))),

  inspectCert: (path: string, pass: string) => x<V.CertFileInfo>(G.InspectCertFile(path, pass)),
  buildPkcs12: (r: V.Pkcs12Request) => x<V.Pkcs12Result>(G.BuildPkcs12(a(r))),
  verifyPkcs12: (path: string, pw: string, alias: string) => x<V.Pkcs12Verification>(G.VerifyPkcs12(path, pw, alias)),
  listCertHistory: async (limit: number) => (await x<V.CertHistory[] | null>(G.ListCertificateHistory(limit))) ?? [],
  saveFile: (title: string, name: string, filters: T.FileFilter[]) => G.SaveFile(title, name, a(filters)),
}

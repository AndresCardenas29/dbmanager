// Tipos v2 (SFTP y certificados), espejo de types_v2.go.

export interface SftpService {
  id: number; name: string; mode: 'managed' | 'external'; containerName: string; image: string; host: string
  port: number; dataMode: string; dataPath: string; exposeOnNetwork: boolean; createdAt: string
}
export interface CreateSftpRequest {
  requestId: string; name: string; image: string; port: number; dataMode: 'volume' | 'bind'; dataPath: string; exposeOnNetwork: boolean
}
export interface SftpKey { id: number; comment: string; type: string; fingerprint: string; publicKey: string; createdAt: string }
export interface SftpUser { name: string; uid: number; hasPassword: boolean; uploadDir: string; keys: SftpKey[] | null; createdAt: string }
export interface SftpKeyRequest {
  mode: 'generate' | 'paste'; keyType: 'ed25519' | 'rsa4096'; passphrase: string; comment: string
  outputDir: string; exportPpk: boolean; publicKey: string
}
export interface CreateSftpUserRequest { name: string; password: string; uploadDir: string; key: SftpKeyRequest | null }
export interface SftpUserResult {
  user: SftpUser; privateKeyPath: string; publicKeyPath: string; ppkPath: string; connectionHints: string[] | null
}
export interface SftpLoginTest { serviceId: number; user: string; password: string; privateKeyPath: string; passphrase: string }
export interface SftpLoginResult { ok: boolean; message: string; hostFingerprint: string; entries: string[] | null }

export interface CertSummary {
  subject: string; issuer: string; commonName: string; sans: string[] | null; notBefore: string; notAfter: string
  serial: string; isCA: boolean; selfSigned: boolean; sha256: string
}
export interface CertFileInfo { kind: 'privateKey' | 'certificate' | 'bundle'; keyType: string; encrypted: boolean; certs: CertSummary[] | null }
export interface Pkcs12Request {
  privateKeyPath: string; keyPassphrase: string; certPath: string; intermediatePath: string; rootPath: string
  alias: string; password: string; outputPath: string; format: 'modern' | 'legacy'; overwrite?: boolean
}
export interface Pkcs12Check { name: string; ok: boolean; severity: 'ok' | 'warning' | 'error'; detail: string }
export interface Pkcs12Verification {
  ok: boolean; entryType: string; alias: string; chainLength: number; chain: CertSummary[] | null; keyMatchesCert: boolean
  checks: Pkcs12Check[] | null; keytoolAvailable: boolean; keytoolOk: boolean; keytoolOutput: string
}
export interface Pkcs12Result { outputPath: string; verification: Pkcs12Verification; warnings: string[] | null }
export interface CertHistory {
  id: number; alias: string; commonName: string; notAfter: string; outputPath: string; chainLength: number
  verified: boolean; createdAt: string
}

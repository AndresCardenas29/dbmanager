export namespace main {
	
	export class BackupFile {
	    logicalName: string;
	    physicalName: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new BackupFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.logicalName = source["logicalName"];
	        this.physicalName = source["physicalName"];
	        this.type = source["type"];
	    }
	}
	export class BackupInfo {
	    format: string;
	    databaseName: string;
	    files: BackupFile[];
	
	    static createFrom(source: any = {}) {
	        return new BackupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.databaseName = source["databaseName"];
	        this.files = this.convertValues(source["files"], BackupFile);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BackupRecord {
	    id: number;
	    serverId: number;
	    serverName: string;
	    database: string;
	    filePath: string;
	    sizeBytes: number;
	    createdAt: string;
	    status: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new BackupRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.serverId = source["serverId"];
	        this.serverName = source["serverName"];
	        this.database = source["database"];
	        this.filePath = source["filePath"];
	        this.sizeBytes = source["sizeBytes"];
	        this.createdAt = source["createdAt"];
	        this.status = source["status"];
	        this.message = source["message"];
	    }
	}
	export class BackupRequest {
	    requestId: string;
	    serverId: number;
	    database: string;
	    destDir: string;
	
	    static createFrom(source: any = {}) {
	        return new BackupRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.serverId = source["serverId"];
	        this.database = source["database"];
	        this.destDir = source["destDir"];
	    }
	}
	export class CertSummary {
	    subject: string;
	    issuer: string;
	    commonName: string;
	    sans: string[];
	    notBefore: string;
	    notAfter: string;
	    serial: string;
	    isCA: boolean;
	    selfSigned: boolean;
	    sha256: string;
	
	    static createFrom(source: any = {}) {
	        return new CertSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.subject = source["subject"];
	        this.issuer = source["issuer"];
	        this.commonName = source["commonName"];
	        this.sans = source["sans"];
	        this.notBefore = source["notBefore"];
	        this.notAfter = source["notAfter"];
	        this.serial = source["serial"];
	        this.isCA = source["isCA"];
	        this.selfSigned = source["selfSigned"];
	        this.sha256 = source["sha256"];
	    }
	}
	export class CertFileInfo {
	    kind: string;
	    keyType: string;
	    encrypted: boolean;
	    certs: CertSummary[];
	
	    static createFrom(source: any = {}) {
	        return new CertFileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.keyType = source["keyType"];
	        this.encrypted = source["encrypted"];
	        this.certs = this.convertValues(source["certs"], CertSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CertHistory {
	    id: number;
	    alias: string;
	    commonName: string;
	    notAfter: string;
	    outputPath: string;
	    chainLength: number;
	    verified: boolean;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new CertHistory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.alias = source["alias"];
	        this.commonName = source["commonName"];
	        this.notAfter = source["notAfter"];
	        this.outputPath = source["outputPath"];
	        this.chainLength = source["chainLength"];
	        this.verified = source["verified"];
	        this.createdAt = source["createdAt"];
	    }
	}
	
	export class ConfigureRootRequest {
	    user: string;
	    password: string;
	    bootstrapUser: string;
	    bootstrapPassword: string;
	    dropPrevious: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ConfigureRootRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user = source["user"];
	        this.password = source["password"];
	        this.bootstrapUser = source["bootstrapUser"];
	        this.bootstrapPassword = source["bootstrapPassword"];
	        this.dropPrevious = source["dropPrevious"];
	    }
	}
	export class Server {
	    id: number;
	    name: string;
	    engine: string;
	    mode: string;
	    host: string;
	    port: number;
	    adminUser: string;
	    hasPassword: boolean;
	    containerName: string;
	    image: string;
	    dataMode: string;
	    dataPath: string;
	    tls: boolean;
	    trustServerCert: boolean;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Server(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.engine = source["engine"];
	        this.mode = source["mode"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.adminUser = source["adminUser"];
	        this.hasPassword = source["hasPassword"];
	        this.containerName = source["containerName"];
	        this.image = source["image"];
	        this.dataMode = source["dataMode"];
	        this.dataPath = source["dataPath"];
	        this.tls = source["tls"];
	        this.trustServerCert = source["trustServerCert"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class ConfigureRootResult {
	    server: Server;
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigureRootResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = this.convertValues(source["server"], Server);
	        this.warning = source["warning"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConnectionUser {
	    login: string;
	    level: string;
	    isRoot: boolean;
	    hasPassword: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionUser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.login = source["login"];
	        this.level = source["level"];
	        this.isRoot = source["isRoot"];
	        this.hasPassword = source["hasPassword"];
	    }
	}
	export class ContainerInfo {
	    id: string;
	    name: string;
	    image: string;
	    engine: string;
	    state: string;
	    status: string;
	    hostPort: number;
	    containerPort: number;
	    dataMode: string;
	    dataPath: string;
	
	    static createFrom(source: any = {}) {
	        return new ContainerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.image = source["image"];
	        this.engine = source["engine"];
	        this.state = source["state"];
	        this.status = source["status"];
	        this.hostPort = source["hostPort"];
	        this.containerPort = source["containerPort"];
	        this.dataMode = source["dataMode"];
	        this.dataPath = source["dataPath"];
	    }
	}
	export class CreateLoginRequest {
	    name: string;
	    password: string;
	    serverRoles: string[];
	
	    static createFrom(source: any = {}) {
	        return new CreateLoginRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.password = source["password"];
	        this.serverRoles = source["serverRoles"];
	    }
	}
	export class CreateServiceRequest {
	    requestId: string;
	    name: string;
	    engine: string;
	    image: string;
	    port: number;
	    adminPassword: string;
	    dataMode: string;
	    dataPath: string;
	    memoryMB: number;
	    edition: string;
	    exposeOnNetwork: boolean;
	    rootUser: string;
	    rootPassword: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateServiceRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.name = source["name"];
	        this.engine = source["engine"];
	        this.image = source["image"];
	        this.port = source["port"];
	        this.adminPassword = source["adminPassword"];
	        this.dataMode = source["dataMode"];
	        this.dataPath = source["dataPath"];
	        this.memoryMB = source["memoryMB"];
	        this.edition = source["edition"];
	        this.exposeOnNetwork = source["exposeOnNetwork"];
	        this.rootUser = source["rootUser"];
	        this.rootPassword = source["rootPassword"];
	    }
	}
	export class CreateSftpRequest {
	    requestId: string;
	    name: string;
	    image: string;
	    port: number;
	    dataMode: string;
	    dataPath: string;
	    exposeOnNetwork: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CreateSftpRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.name = source["name"];
	        this.image = source["image"];
	        this.port = source["port"];
	        this.dataMode = source["dataMode"];
	        this.dataPath = source["dataPath"];
	        this.exposeOnNetwork = source["exposeOnNetwork"];
	    }
	}
	export class SftpKeyRequest {
	    mode: string;
	    keyType: string;
	    passphrase: string;
	    comment: string;
	    outputDir: string;
	    exportPpk: boolean;
	    publicKey: string;
	
	    static createFrom(source: any = {}) {
	        return new SftpKeyRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.keyType = source["keyType"];
	        this.passphrase = source["passphrase"];
	        this.comment = source["comment"];
	        this.outputDir = source["outputDir"];
	        this.exportPpk = source["exportPpk"];
	        this.publicKey = source["publicKey"];
	    }
	}
	export class CreateSftpUserRequest {
	    name: string;
	    password: string;
	    uploadDir: string;
	    key?: SftpKeyRequest;
	
	    static createFrom(source: any = {}) {
	        return new CreateSftpUserRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.password = source["password"];
	        this.uploadDir = source["uploadDir"];
	        this.key = this.convertValues(source["key"], SftpKeyRequest);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Database {
	    name: string;
	    sizeMB: number;
	    owner: string;
	    state: string;
	    system: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Database(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.sizeMB = source["sizeMB"];
	        this.owner = source["owner"];
	        this.state = source["state"];
	        this.system = source["system"];
	    }
	}
	export class EnvStatus {
	    dockerAvailable: boolean;
	    dockerVersion: string;
	    os: string;
	    arch: string;
	    rosettaWarning: boolean;
	    messages: string[];
	
	    static createFrom(source: any = {}) {
	        return new EnvStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dockerAvailable = source["dockerAvailable"];
	        this.dockerVersion = source["dockerVersion"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.rosettaWarning = source["rosettaWarning"];
	        this.messages = source["messages"];
	    }
	}
	export class FileFilter {
	    displayName: string;
	    pattern: string;
	
	    static createFrom(source: any = {}) {
	        return new FileFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.displayName = source["displayName"];
	        this.pattern = source["pattern"];
	    }
	}
	export class ImportContainerRequest {
	    containerName: string;
	    adminUser: string;
	    adminPassword: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportContainerRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.containerName = source["containerName"];
	        this.adminUser = source["adminUser"];
	        this.adminPassword = source["adminPassword"];
	    }
	}
	export class Login {
	    name: string;
	    serverRoles: string[];
	    disabled: boolean;
	    isSystem: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Login(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.serverRoles = source["serverRoles"];
	        this.disabled = source["disabled"];
	        this.isSystem = source["isSystem"];
	    }
	}
	export class McpToolInfo {
	    name: string;
	    description: string;
	    mutating: boolean;
	
	    static createFrom(source: any = {}) {
	        return new McpToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.mutating = source["mutating"];
	    }
	}
	export class McpSettings {
	    enabled: boolean;
	    mode: string;
	    allowSql: boolean;
	    exposePasswords: boolean;
	
	    static createFrom(source: any = {}) {
	        return new McpSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.mode = source["mode"];
	        this.allowSql = source["allowSql"];
	        this.exposePasswords = source["exposePasswords"];
	    }
	}
	export class McpInfo {
	    settings: McpSettings;
	    exePath: string;
	    claudeCodeCommand: string;
	    claudeDesktopJson: string;
	    genericJson: string;
	    tools: McpToolInfo[];
	    devBuild: boolean;
	
	    static createFrom(source: any = {}) {
	        return new McpInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = this.convertValues(source["settings"], McpSettings);
	        this.exePath = source["exePath"];
	        this.claudeCodeCommand = source["claudeCodeCommand"];
	        this.claudeDesktopJson = source["claudeDesktopJson"];
	        this.genericJson = source["genericJson"];
	        this.tools = this.convertValues(source["tools"], McpToolInfo);
	        this.devBuild = source["devBuild"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class McpTestResult {
	    ok: boolean;
	    message: string;
	    toolCount: number;
	    serverVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new McpTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
	        this.toolCount = source["toolCount"];
	        this.serverVersion = source["serverVersion"];
	    }
	}
	
	export class ObjectGrant {
	    login: string;
	    schema: string;
	    table: string;
	    privileges: string[];
	
	    static createFrom(source: any = {}) {
	        return new ObjectGrant(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.login = source["login"];
	        this.schema = source["schema"];
	        this.table = source["table"];
	        this.privileges = source["privileges"];
	    }
	}
	export class ObjectGrantRequest {
	    login: string;
	    schema: string;
	    table: string;
	    privileges: string[];
	
	    static createFrom(source: any = {}) {
	        return new ObjectGrantRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.login = source["login"];
	        this.schema = source["schema"];
	        this.table = source["table"];
	        this.privileges = source["privileges"];
	    }
	}
	export class Operation {
	    id: number;
	    serverId: number;
	    kind: string;
	    target: string;
	    status: string;
	    message: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Operation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.serverId = source["serverId"];
	        this.kind = source["kind"];
	        this.target = source["target"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class PermissionCell {
	    login: string;
	    database: string;
	    level: string;
	
	    static createFrom(source: any = {}) {
	        return new PermissionCell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.login = source["login"];
	        this.database = source["database"];
	        this.level = source["level"];
	    }
	}
	export class PermissionMatrix {
	    logins: string[];
	    databases: string[];
	    cells: PermissionCell[];
	
	    static createFrom(source: any = {}) {
	        return new PermissionMatrix(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.logins = source["logins"];
	        this.databases = source["databases"];
	        this.cells = this.convertValues(source["cells"], PermissionCell);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Pkcs12Check {
	    name: string;
	    ok: boolean;
	    detail: string;
	    severity: string;
	
	    static createFrom(source: any = {}) {
	        return new Pkcs12Check(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ok = source["ok"];
	        this.detail = source["detail"];
	        this.severity = source["severity"];
	    }
	}
	export class Pkcs12Request {
	    privateKeyPath: string;
	    keyPassphrase: string;
	    certPath: string;
	    intermediatePath: string;
	    rootPath: string;
	    alias: string;
	    password: string;
	    outputPath: string;
	    format: string;
	    overwrite: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Pkcs12Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.privateKeyPath = source["privateKeyPath"];
	        this.keyPassphrase = source["keyPassphrase"];
	        this.certPath = source["certPath"];
	        this.intermediatePath = source["intermediatePath"];
	        this.rootPath = source["rootPath"];
	        this.alias = source["alias"];
	        this.password = source["password"];
	        this.outputPath = source["outputPath"];
	        this.format = source["format"];
	        this.overwrite = source["overwrite"];
	    }
	}
	export class Pkcs12Verification {
	    ok: boolean;
	    entryType: string;
	    alias: string;
	    chainLength: number;
	    chain: CertSummary[];
	    keyMatchesCert: boolean;
	    checks: Pkcs12Check[];
	    keytoolAvailable: boolean;
	    keytoolOk: boolean;
	    keytoolOutput: string;
	
	    static createFrom(source: any = {}) {
	        return new Pkcs12Verification(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.entryType = source["entryType"];
	        this.alias = source["alias"];
	        this.chainLength = source["chainLength"];
	        this.chain = this.convertValues(source["chain"], CertSummary);
	        this.keyMatchesCert = source["keyMatchesCert"];
	        this.checks = this.convertValues(source["checks"], Pkcs12Check);
	        this.keytoolAvailable = source["keytoolAvailable"];
	        this.keytoolOk = source["keytoolOk"];
	        this.keytoolOutput = source["keytoolOutput"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Pkcs12Result {
	    outputPath: string;
	    verification: Pkcs12Verification;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new Pkcs12Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outputPath = source["outputPath"];
	        this.verification = this.convertValues(source["verification"], Pkcs12Verification);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ProgressEvent {
	    requestId: string;
	    id: number;
	    percent: number;
	    message: string;
	    done: boolean;
	    error: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProgressEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.id = source["id"];
	        this.percent = source["percent"];
	        this.message = source["message"];
	        this.done = source["done"];
	        this.error = source["error"];
	    }
	}
	export class RemoteServerRequest {
	    name: string;
	    engine: string;
	    host: string;
	    port: number;
	    adminUser: string;
	    adminPassword: string;
	    tls: boolean;
	    trustServerCert: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteServerRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.engine = source["engine"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.adminUser = source["adminUser"];
	        this.adminPassword = source["adminPassword"];
	        this.tls = source["tls"];
	        this.trustServerCert = source["trustServerCert"];
	    }
	}
	export class RestoreRecord {
	    id: number;
	    serverId: number;
	    serverName: string;
	    sourceFile: string;
	    targetDatabase: string;
	    dataDir: string;
	    logDir: string;
	    status: string;
	    message: string;
	    startedAt: string;
	    finishedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new RestoreRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.serverId = source["serverId"];
	        this.serverName = source["serverName"];
	        this.sourceFile = source["sourceFile"];
	        this.targetDatabase = source["targetDatabase"];
	        this.dataDir = source["dataDir"];
	        this.logDir = source["logDir"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	    }
	}
	export class RestoreRequest {
	    requestId: string;
	    serverId: number;
	    filePath: string;
	    targetDatabase: string;
	    dataDir: string;
	    logDir: string;
	    replace: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RestoreRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.serverId = source["serverId"];
	        this.filePath = source["filePath"];
	        this.targetDatabase = source["targetDatabase"];
	        this.dataDir = source["dataDir"];
	        this.logDir = source["logDir"];
	        this.replace = source["replace"];
	    }
	}
	export class RootDefaults {
	    user: string;
	    passwordIsDefault: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RootDefaults(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user = source["user"];
	        this.passwordIsDefault = source["passwordIsDefault"];
	    }
	}
	export class RootStatus {
	    user: string;
	    reachable: boolean;
	    isAdmin: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new RootStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user = source["user"];
	        this.reachable = source["reachable"];
	        this.isAdmin = source["isAdmin"];
	        this.message = source["message"];
	    }
	}
	export class SchemaInfo {
	    name: string;
	    owner: string;
	    tables: string[];
	    system: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SchemaInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.owner = source["owner"];
	        this.tables = source["tables"];
	        this.system = source["system"];
	    }
	}
	
	export class ServerStatus {
	    state: string;
	    reachable: boolean;
	    version: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.reachable = source["reachable"];
	        this.version = source["version"];
	        this.message = source["message"];
	    }
	}
	export class SftpFolderAccess {
	    user: string;
	    level: string;
	
	    static createFrom(source: any = {}) {
	        return new SftpFolderAccess(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user = source["user"];
	        this.level = source["level"];
	    }
	}
	export class SftpFolder {
	    id: number;
	    name: string;
	    access: SftpFolderAccess[];
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SftpFolder(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.access = this.convertValues(source["access"], SftpFolderAccess);
	        this.createdAt = source["createdAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SftpKey {
	    id: number;
	    comment: string;
	    type: string;
	    fingerprint: string;
	    publicKey: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SftpKey(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.comment = source["comment"];
	        this.type = source["type"];
	        this.fingerprint = source["fingerprint"];
	        this.publicKey = source["publicKey"];
	        this.createdAt = source["createdAt"];
	    }
	}
	
	export class SftpLoginResult {
	    ok: boolean;
	    message: string;
	    hostFingerprint: string;
	    entries: string[];
	
	    static createFrom(source: any = {}) {
	        return new SftpLoginResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
	        this.hostFingerprint = source["hostFingerprint"];
	        this.entries = source["entries"];
	    }
	}
	export class SftpLoginTest {
	    serviceId: number;
	    user: string;
	    password: string;
	    privateKeyPath: string;
	    passphrase: string;
	
	    static createFrom(source: any = {}) {
	        return new SftpLoginTest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serviceId = source["serviceId"];
	        this.user = source["user"];
	        this.password = source["password"];
	        this.privateKeyPath = source["privateKeyPath"];
	        this.passphrase = source["passphrase"];
	    }
	}
	export class SftpService {
	    id: number;
	    name: string;
	    mode: string;
	    containerName: string;
	    image: string;
	    host: string;
	    port: number;
	    dataMode: string;
	    dataPath: string;
	    exposeOnNetwork: boolean;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SftpService(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.mode = source["mode"];
	        this.containerName = source["containerName"];
	        this.image = source["image"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.dataMode = source["dataMode"];
	        this.dataPath = source["dataPath"];
	        this.exposeOnNetwork = source["exposeOnNetwork"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class SftpUser {
	    name: string;
	    uid: number;
	    hasPassword: boolean;
	    uploadDir: string;
	    keys: SftpKey[];
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SftpUser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.uid = source["uid"];
	        this.hasPassword = source["hasPassword"];
	        this.uploadDir = source["uploadDir"];
	        this.keys = this.convertValues(source["keys"], SftpKey);
	        this.createdAt = source["createdAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SftpUserResult {
	    user: SftpUser;
	    privateKeyPath: string;
	    publicKeyPath: string;
	    ppkPath: string;
	    connectionHints: string[];
	
	    static createFrom(source: any = {}) {
	        return new SftpUserResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user = this.convertValues(source["user"], SftpUser);
	        this.privateKeyPath = source["privateKeyPath"];
	        this.publicKeyPath = source["publicKeyPath"];
	        this.ppkPath = source["ppkPath"];
	        this.connectionHints = source["connectionHints"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UpdateServerRequest {
	    name: string;
	    host: string;
	    port: number;
	    adminUser: string;
	    adminPassword: string;
	    tls: boolean;
	    trustServerCert: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdateServerRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.adminUser = source["adminUser"];
	        this.adminPassword = source["adminPassword"];
	        this.tls = source["tls"];
	        this.trustServerCert = source["trustServerCert"];
	    }
	}

}


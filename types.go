package main

// Tipos del contrato backend <-> frontend. Los tags JSON están en camelCase.
// Las fechas se exponen como cadenas RFC3339 (UTC); cadena vacía = sin valor.

// EnvStatus describe el estado del entorno (Docker, SO).
type EnvStatus struct {
	DockerAvailable bool     `json:"dockerAvailable"`
	DockerVersion   string   `json:"dockerVersion"`
	OS              string   `json:"os"`
	Arch            string   `json:"arch"`
	RosettaWarning  bool     `json:"rosettaWarning"`
	Messages        []string `json:"messages"`
}

// Server es un servidor registrado (local en Docker o remoto). Nunca incluye la contraseña.
type Server struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Engine          string `json:"engine"` // mssql | postgres
	Mode            string `json:"mode"`   // local | remote
	Host            string `json:"host"`
	Port            int    `json:"port"`
	AdminUser       string `json:"adminUser"`
	HasPassword     bool   `json:"hasPassword"`
	ContainerName   string `json:"containerName"`
	Image           string `json:"image"`
	DataMode        string `json:"dataMode"` // volume | bind | ""
	DataPath        string `json:"dataPath"`
	TLS             bool   `json:"tls"`
	TrustServerCert bool   `json:"trustServerCert"`
	CreatedAt       string `json:"createdAt"`
}

type CreateServiceRequest struct {
	RequestID       string `json:"requestId"`
	Name            string `json:"name"`
	Engine          string `json:"engine"`
	Image           string `json:"image"`
	Port            int    `json:"port"`
	AdminPassword   string `json:"adminPassword"`
	DataMode        string `json:"dataMode"` // volume | bind
	DataPath        string `json:"dataPath"` // nombre del volumen (opcional) o carpeta del host
	MemoryMB        int    `json:"memoryMB"`
	Edition         string `json:"edition"`         // mssql: Developer (por defecto) | Express | ...
	ExposeOnNetwork bool   `json:"exposeOnNetwork"` // true = publicar en 0.0.0.0 (por defecto solo 127.0.0.1)
}

type ImportContainerRequest struct {
	ContainerName string `json:"containerName"`
	AdminUser     string `json:"adminUser"`
	AdminPassword string `json:"adminPassword"`
}

type RemoteServerRequest struct {
	Name            string `json:"name"`
	Engine          string `json:"engine"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	AdminUser       string `json:"adminUser"`
	AdminPassword   string `json:"adminPassword"`
	TLS             bool   `json:"tls"`
	TrustServerCert bool   `json:"trustServerCert"`
}

// UpdateServerRequest: AdminPassword vacío = conservar la contraseña actual.
type UpdateServerRequest struct {
	Name            string `json:"name"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	AdminUser       string `json:"adminUser"`
	AdminPassword   string `json:"adminPassword"`
	TLS             bool   `json:"tls"`
	TrustServerCert bool   `json:"trustServerCert"`
}

// ContainerInfo es un contenedor Docker existente que se puede importar.
type ContainerInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	Engine        string `json:"engine"` // mssql | postgres
	State         string `json:"state"`
	Status        string `json:"status"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	DataMode      string `json:"dataMode"`
	DataPath      string `json:"dataPath"`
}

// ServerStatus: state = running | stopped | missing | remote.
type ServerStatus struct {
	State     string `json:"state"`
	Reachable bool   `json:"reachable"`
	Version   string `json:"version"`
	Message   string `json:"message"`
}

type Database struct {
	Name   string  `json:"name"`
	SizeMB float64 `json:"sizeMB"`
	Owner  string  `json:"owner"`
	State  string  `json:"state"`
	System bool    `json:"system"`
}

type Login struct {
	Name        string   `json:"name"`
	ServerRoles []string `json:"serverRoles"`
	Disabled    bool     `json:"disabled"`
	IsSystem    bool     `json:"isSystem"`
}

type CreateLoginRequest struct {
	Name        string   `json:"name"`
	Password    string   `json:"password"`
	ServerRoles []string `json:"serverRoles"`
}

type PermissionCell struct {
	Login    string `json:"login"`
	Database string `json:"database"`
	Level    string `json:"level"` // owner | readwrite | readonly | none
}

type PermissionMatrix struct {
	Logins    []string         `json:"logins"`
	Databases []string         `json:"databases"`
	Cells     []PermissionCell `json:"cells"`
}

type FileFilter struct {
	DisplayName string `json:"displayName"` // p. ej. "Backups (*.bak;*.dump)"
	Pattern     string `json:"pattern"`     // p. ej. "*.bak;*.dump"
}

type BackupRequest struct {
	RequestID string `json:"requestId"`
	ServerID  int64  `json:"serverId"`
	Database  string `json:"database"`
	DestDir   string `json:"destDir"`
}

type BackupRecord struct {
	ID         int64  `json:"id"`
	ServerID   int64  `json:"serverId"`
	ServerName string `json:"serverName"`
	Database   string `json:"database"`
	FilePath   string `json:"filePath"`
	SizeBytes  int64  `json:"sizeBytes"`
	CreatedAt  string `json:"createdAt"`
	Status     string `json:"status"` // running | ok | error
	Message    string `json:"message"`
}

type BackupFile struct {
	LogicalName  string `json:"logicalName"`
	PhysicalName string `json:"physicalName"`
	Type         string `json:"type"` // data | log
}

type BackupInfo struct {
	Format       string       `json:"format"`
	DatabaseName string       `json:"databaseName"`
	Files        []BackupFile `json:"files"`
}

type RestoreRequest struct {
	RequestID      string `json:"requestId"`
	ServerID       int64  `json:"serverId"`
	FilePath       string `json:"filePath"`
	TargetDatabase string `json:"targetDatabase"`
	DataDir        string `json:"dataDir"`
	LogDir         string `json:"logDir"`
	Replace        bool   `json:"replace"`
}

type RestoreRecord struct {
	ID             int64  `json:"id"`
	ServerID       int64  `json:"serverId"`
	ServerName     string `json:"serverName"`
	SourceFile     string `json:"sourceFile"`
	TargetDatabase string `json:"targetDatabase"`
	DataDir        string `json:"dataDir"`
	LogDir         string `json:"logDir"`
	Status         string `json:"status"` // running | ok | error
	Message        string `json:"message"`
	StartedAt      string `json:"startedAt"`
	FinishedAt     string `json:"finishedAt"`
}

type Operation struct {
	ID        int64  `json:"id"`
	ServerID  int64  `json:"serverId"`
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	Status    string `json:"status"` // ok | error
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

// ProgressEvent es el payload de los eventos "service:progress", "backup:progress" y "restore:progress".
type ProgressEvent struct {
	RequestID string `json:"requestId"`
	ID        int64  `json:"id"`
	Percent   int    `json:"percent"`
	Message   string `json:"message"`
	Done      bool   `json:"done"`
	Error     bool   `json:"error"`
}

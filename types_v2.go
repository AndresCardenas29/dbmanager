package main

// Tipos de la ampliación v2 (SFTP y certificados). JSON en camelCase.

type SftpService struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Mode            string `json:"mode"` // managed | external
	ContainerName   string `json:"containerName"`
	Image           string `json:"image"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	DataMode        string `json:"dataMode"`
	DataPath        string `json:"dataPath"`
	ExposeOnNetwork bool   `json:"exposeOnNetwork"`
	CreatedAt       string `json:"createdAt"`
}

type CreateSftpRequest struct {
	RequestID       string `json:"requestId"`
	Name            string `json:"name"`
	Image           string `json:"image"`
	Port            int    `json:"port"`
	DataMode        string `json:"dataMode"` // volume | bind
	DataPath        string `json:"dataPath"`
	ExposeOnNetwork bool   `json:"exposeOnNetwork"`
}

type SftpKey struct {
	ID          int64  `json:"id"`
	Comment     string `json:"comment"`
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publicKey"`
	CreatedAt   string `json:"createdAt"`
}

type SftpUser struct {
	Name        string    `json:"name"`
	UID         int       `json:"uid"`
	HasPassword bool      `json:"hasPassword"`
	UploadDir   string    `json:"uploadDir"`
	Keys        []SftpKey `json:"keys"`
	CreatedAt   string    `json:"createdAt"`
}

type SftpKeyRequest struct {
	Mode       string `json:"mode"`    // generate | paste
	KeyType    string `json:"keyType"` // ed25519 | rsa4096
	Passphrase string `json:"passphrase"`
	Comment    string `json:"comment"`
	OutputDir  string `json:"outputDir"`
	ExportPpk  bool   `json:"exportPpk"`
	PublicKey  string `json:"publicKey"`
}

type CreateSftpUserRequest struct {
	Name      string          `json:"name"`
	Password  string          `json:"password"`
	UploadDir string          `json:"uploadDir"`
	Key       *SftpKeyRequest `json:"key"`
}

type SftpUserResult struct {
	User            SftpUser `json:"user"`
	PrivateKeyPath  string   `json:"privateKeyPath"`
	PublicKeyPath   string   `json:"publicKeyPath"`
	PpkPath         string   `json:"ppkPath"`
	ConnectionHints []string `json:"connectionHints"`
}

type SftpLoginTest struct {
	ServiceID      int64  `json:"serviceId"`
	User           string `json:"user"`
	Password       string `json:"password"`
	PrivateKeyPath string `json:"privateKeyPath"`
	Passphrase     string `json:"passphrase"`
}

type SftpLoginResult struct {
	OK              bool     `json:"ok"`
	Message         string   `json:"message"`
	HostFingerprint string   `json:"hostFingerprint"`
	Entries         []string `json:"entries"`
}

// ---------- Certificados ----------

type CertSummary struct {
	Subject    string   `json:"subject"`
	Issuer     string   `json:"issuer"`
	CommonName string   `json:"commonName"`
	SANs       []string `json:"sans"`
	NotBefore  string   `json:"notBefore"`
	NotAfter   string   `json:"notAfter"`
	Serial     string   `json:"serial"`
	IsCA       bool     `json:"isCA"`
	SelfSigned bool     `json:"selfSigned"`
	SHA256     string   `json:"sha256"`
}

type CertFileInfo struct {
	Kind      string        `json:"kind"` // privateKey | certificate | bundle
	KeyType   string        `json:"keyType"`
	Encrypted bool          `json:"encrypted"`
	Certs     []CertSummary `json:"certs"`
}

type Pkcs12Request struct {
	PrivateKeyPath   string `json:"privateKeyPath"`
	KeyPassphrase    string `json:"keyPassphrase"`
	CertPath         string `json:"certPath"`
	IntermediatePath string `json:"intermediatePath"`
	RootPath         string `json:"rootPath"`
	Alias            string `json:"alias"`
	Password         string `json:"password"`
	OutputPath       string `json:"outputPath"`
	Format           string `json:"format"`    // modern | legacy
	Overwrite        bool   `json:"overwrite"` // permitir reemplazar outputPath si ya existe
}

type Pkcs12Check struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
	Severity string `json:"severity"` // ok | warning | error
}

type Pkcs12Verification struct {
	OK               bool          `json:"ok"`
	EntryType        string        `json:"entryType"`
	Alias            string        `json:"alias"`
	ChainLength      int           `json:"chainLength"`
	Chain            []CertSummary `json:"chain"`
	KeyMatchesCert   bool          `json:"keyMatchesCert"`
	Checks           []Pkcs12Check `json:"checks"`
	KeytoolAvailable bool          `json:"keytoolAvailable"`
	KeytoolOK        bool          `json:"keytoolOk"`
	KeytoolOutput    string        `json:"keytoolOutput"`
}

type Pkcs12Result struct {
	OutputPath   string             `json:"outputPath"`
	Verification Pkcs12Verification `json:"verification"`
	Warnings     []string           `json:"warnings"`
}

type CertHistory struct {
	ID          int64  `json:"id"`
	Alias       string `json:"alias"`
	CommonName  string `json:"commonName"`
	NotAfter    string `json:"notAfter"`
	OutputPath  string `json:"outputPath"`
	ChainLength int    `json:"chainLength"`
	Verified    bool   `json:"verified"`
	CreatedAt   string `json:"createdAt"`
}

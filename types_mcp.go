package main

// McpSettings son los ajustes del servidor MCP integrado (se guardan en la tabla de ajustes, claves mcp*).
type McpSettings struct {
	Enabled         bool   `json:"enabled"`
	Mode            string `json:"mode"` // readonly | full
	AllowSql        bool   `json:"allowSql"`
	ExposePasswords bool   `json:"exposePasswords"`
}

// McpToolInfo describe una herramienta MCP expuesta.
type McpToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mutating    bool   `json:"mutating"`
}

// McpInfo reúne lo necesario para que la interfaz muestre cómo conectar un agente.
type McpInfo struct {
	Settings          McpSettings   `json:"settings"`
	ExePath           string        `json:"exePath"`
	ClaudeCodeCommand string        `json:"claudeCodeCommand"`
	ClaudeDesktopJson string        `json:"claudeDesktopJson"`
	GenericJson       string        `json:"genericJson"`
	Tools             []McpToolInfo `json:"tools"`
	DevBuild          bool          `json:"devBuild"`
}

// McpTestResult es el resultado de probar el servidor MCP levantando el propio ejecutable.
type McpTestResult struct {
	Ok            bool   `json:"ok"`
	Message       string `json:"message"`
	ToolCount     int    `json:"toolCount"`
	ServerVersion string `json:"serverVersion"`
}

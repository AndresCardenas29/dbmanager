import * as G from '../../wailsjs/go/main/App'

export interface McpSettings { enabled: boolean; mode: string; allowSql: boolean; exposePasswords: boolean }
export interface McpToolInfo { name: string; description: string; mutating: boolean }
export interface McpInfo {
  settings: McpSettings; exePath: string; claudeCodeCommand: string; claudeDesktopJson: string
  genericJson: string; tools: McpToolInfo[] | null; devBuild: boolean
}
export interface McpTestResult { ok: boolean; message: string; toolCount: number; serverVersion: string }

const x = <R>(p: Promise<unknown>) => p as Promise<R>

/** API MCP: servidor para agentes de IA. */
export const apiMcp = {
  getInfo: () => x<McpInfo>(G.GetMcpInfo()),
  setSettings: (s: McpSettings) => G.SetMcpSettings(s as never),
  test: () => x<McpTestResult>(G.TestMcpServer()),
}

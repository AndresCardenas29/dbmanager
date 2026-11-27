import {useEffect, useMemo, useState} from 'react'
import {Bot, Play, RefreshCw, Save} from 'lucide-react'
import {api} from '../api'
import {apiMcp} from '../api/mcp'
import type {McpSettings, McpTestResult} from '../api/mcp'
import {useAsync} from '../hooks/useAsync'
import {useToast} from '../components/Toast'
import {Badge, Button, Card, Checkbox, CopyButton, EmptyState, ErrorBanner, fmtDate, Input, Notice, PageHeader, Select, Spinner, statusLabel, statusTone, Table, Tabs, Td, Th} from '../components/ui'

type ClientTab = 'code' | 'desktop' | 'other'

function CodeBlock({text}: {text: string}) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-line bg-surface2 p-2">
      <pre className="min-w-0 flex-1 overflow-x-auto whitespace-pre font-mono text-xs">{text}</pre>
      <CopyButton text={text}/>
    </div>
  )
}

export default function McpPage() {
  const toast = useToast()
  const info = useAsync(() => apiMcp.getInfo(), [])
  const ops = useAsync(async () => (await api.listOperations(200)).filter(o => o.kind.startsWith('mcp:')), [])
  const [form, setForm] = useState<McpSettings | null>(null)
  const [saving, setSaving] = useState(false)
  const [tab, setTab] = useState<ClientTab>('code')
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<McpTestResult | null>(null)
  const [filter, setFilter] = useState('')

  const data = info.data
  useEffect(() => { if (data) setForm({...data.settings}) }, [data])

  const dirty = !!(data && form && JSON.stringify(data.settings) !== JSON.stringify(form))
  const tools = useMemo(() => {
    const q = filter.trim().toLowerCase()
    return (data?.tools ?? []).filter(t => !q || t.name.toLowerCase().includes(q) || t.description.toLowerCase().includes(q))
  }, [data, filter])

  const save = async () => {
    if (!form) return
    setSaving(true)
    try {
      await apiMcp.setSettings(form)
      toast.success('Ajustes MCP guardados')
      await info.reload()
    } catch (e) { toast.error(e) } finally { setSaving(false) }
  }

  const test = async () => {
    setTesting(true)
    setResult(null)
    try { setResult(await apiMcp.test()) } catch (e) { toast.error(e) } finally { setTesting(false) }
  }

  if (info.error && !data) return <div className="p-6"><ErrorBanner message={info.error} onRetry={info.reload}/></div>
  if (!data || !form) return <div className="p-6"><Spinner/></div>

  const set = (p: Partial<McpSettings>) => setForm({...form, ...p})

  return (
    <div className="space-y-4 p-6">
      <PageHeader title="Servidor MCP"
        subtitle="Permite que agentes de IA (Claude Code, Claude Desktop, Cursor…) administren y prueben servidores, usuarios, permisos y SFTP de esta aplicación."/>

      <Card title="Estado y seguridad" actions={
        <Button variant="primary" icon={<Save size={15}/>} loading={saving} disabled={!dirty} onClick={save}>Guardar</Button>
      }>
        <div className="space-y-4">
          <div className="flex items-center gap-3">
            <Checkbox checked={form.enabled} onChange={e => set({enabled: e.target.checked})}
              label={<span className="text-base font-semibold">Activado</span>}/>
            <Badge tone={data.settings.enabled ? 'success' : 'neutral'} className="px-2.5 py-1 text-xs">
              {data.settings.enabled ? 'Activo' : 'Desactivado'}
            </Badge>
          </div>
          <div className="max-w-sm">
            <label className="mb-1 block text-sm font-medium" htmlFor="mcp-mode">Modo</label>
            <Select id="mcp-mode" value={form.mode} onChange={e => set({mode: e.target.value})}>
              <option value="readonly">Solo lectura</option>
              <option value="full">Completo</option>
            </Select>
            <p className="mt-1 text-xs text-muted">
              Solo lectura: los agentes únicamente consultan. Completo: además pueden crear, borrar y modificar servidores, usuarios, permisos y SFTP.
            </p>
          </div>
          <Checkbox checked={form.allowSql} onChange={e => set({allowSql: e.target.checked})}
            label="Permitir run_sql (ejecutar SQL como un usuario para probar permisos)"/>
          <Checkbox checked={form.exposePasswords} onChange={e => set({exposePasswords: e.target.checked})}
            label="Exponer contraseñas a los agentes"/>
          {form.exposePasswords && (
            <Notice tone="danger">Las contraseñas se enviarán al agente y quedarán en su conversación. Actívalo solo si confías en el agente y en el servicio que lo ejecuta.</Notice>
          )}
          {form.mode === 'full' && (
            <Notice tone="warn">Modo completo: los agentes podrán modificar y eliminar recursos.</Notice>
          )}
          <Notice tone="accent">Las herramientas destructivas exigen el parámetro <span className="font-mono">confirm_name</span> con el nombre exacto del recurso, y toda llamada de un agente queda registrada en el historial de operaciones.</Notice>
        </div>
      </Card>

      <Card title="Conectar un agente">
        <div className="space-y-4">
          <div>
            <p className="mb-1 text-sm font-medium">Ejecutable del servidor MCP</p>
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 break-all rounded-md border border-line bg-surface2 px-2 py-1.5 font-mono text-xs">{data.exePath}</code>
              <CopyButton text={data.exePath}/>
            </div>
          </div>
          {data.devBuild && (
            <Notice tone="warn">La ruta apunta a una compilación de desarrollo. Compila con <span className="font-mono">wails build</span> y usa <span className="font-mono">build\bin\andres-tools.exe</span>.</Notice>
          )}
          <Tabs<ClientTab> value={tab} onChange={setTab} tabs={[
            {key: 'code', label: 'Claude Code'}, {key: 'desktop', label: 'Claude Desktop'}, {key: 'other', label: 'Otros clientes'},
          ]}/>
          {tab === 'code' && (
            <div className="space-y-2">
              <p className="text-sm text-muted">Ejecuta este comando en una terminal:</p>
              <CodeBlock text={data.claudeCodeCommand}/>
            </div>
          )}
          {tab === 'desktop' && (
            <div className="space-y-2">
              <p className="text-sm text-muted">Pega este bloque en <span className="font-mono">claude_desktop_config.json</span> dentro de <span className="font-mono">mcpServers</span> (Windows: <span className="font-mono">%APPDATA%\Claude\claude_desktop_config.json</span>) y reinicia Claude Desktop.</p>
              <CodeBlock text={data.claudeDesktopJson}/>
            </div>
          )}
          {tab === 'other' && (
            <div className="space-y-2">
              <p className="text-sm text-muted">Configuración genérica para clientes MCP por stdio (Cursor, etc.):</p>
              <CodeBlock text={data.genericJson}/>
            </div>
          )}
          <div className="flex flex-wrap items-center gap-3 border-t border-line pt-3">
            <Button icon={<Play size={15}/>} loading={testing} onClick={test}>Probar servidor</Button>
            {result && (
              <div className="flex flex-wrap items-center gap-2 text-sm">
                <Badge tone={result.ok ? 'success' : 'danger'}>{result.ok ? 'Correcto' : 'Error'}</Badge>
                {result.ok && <span>{result.toolCount} herramientas</span>}
                {result.serverVersion && <span className="text-muted">v{result.serverVersion}</span>}
                {result.message && <span className="text-muted">{result.message}</span>}
              </div>
            )}
          </div>
        </div>
      </Card>

      <Card title={`Herramientas (${data.tools?.length ?? 0})`} actions={
        <Input value={filter} onChange={e => setFilter(e.target.value)} placeholder="Filtrar herramientas…" className="w-56"/>
      }>
        <p className="mb-3 text-xs text-muted">Las herramientas que modifican solo funcionan en Modo completo; <span className="font-mono">run_sql</span> además requiere su casilla.</p>
        {tools.length === 0 ? <EmptyState icon={<Bot size={20}/>} title="Sin herramientas"/> : (
          <Table>
            <thead><tr><Th>Nombre</Th><Th>Descripción</Th><Th>Tipo</Th></tr></thead>
            <tbody>
              {tools.map(t => (
                <tr key={t.name}>
                  <Td className="font-mono text-xs">{t.name}</Td>
                  <Td>{t.description}</Td>
                  <Td><Badge tone={t.mutating ? 'warn' : 'neutral'}>{t.mutating ? 'Modifica' : 'Lectura'}</Badge></Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      <Card title="Actividad reciente de agentes" actions={
        <Button size="sm" icon={<RefreshCw size={14}/>} loading={ops.loading} onClick={ops.reload}>Actualizar</Button>
      }>
        {ops.error ? <ErrorBanner message={ops.error} onRetry={ops.reload}/> :
          !ops.data ? <Spinner/> :
          ops.data.length === 0 ? <EmptyState icon={<Bot size={20}/>} title="Sin actividad de agentes todavía"/> : (
            <Table>
              <thead><tr><Th>Fecha</Th><Th>Herramienta</Th><Th>Destino</Th><Th>Estado</Th><Th>Mensaje</Th></tr></thead>
              <tbody>
                {ops.data.map(o => (
                  <tr key={o.id}>
                    <Td className="whitespace-nowrap">{fmtDate(o.createdAt)}</Td>
                    <Td className="font-mono text-xs">{o.kind.slice(4)}</Td>
                    <Td>{o.target}</Td>
                    <Td><Badge tone={statusTone(o.status)}>{statusLabel(o.status)}</Badge></Td>
                    <Td title={o.message}>{o.message}</Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
      </Card>
    </div>
  )
}

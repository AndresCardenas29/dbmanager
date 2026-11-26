import {useEffect, useState} from 'react'
import {FolderOpen, Save} from 'lucide-react'
import {api} from '../api'
import {useApp} from '../store'
import {useAsync} from '../hooks/useAsync'
import {useToast} from '../components/Toast'
import {Button, Card, ErrorBanner, Field, Input, PageHeader, Select, Spinner, Table, Td, Th} from '../components/ui'
import type {ThemePref} from '../hooks/useTheme'

export default function SettingsPage() {
  const toast = useToast()
  const {theme, setTheme} = useApp()
  const settings = useAsync(() => api.getSettings(), [])
  const [dir, setDir] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => { if (settings.data) setDir(settings.data.defaultBackupDir ?? '') }, [settings.data])

  const pick = async () => {
    try { const d = await api.pickDirectory('Carpeta predeterminada para backups'); if (d) setDir(d) } catch (e) { toast.error(e) }
  }
  const save = async () => {
    setSaving(true)
    try { await api.setSetting('defaultBackupDir', dir.trim()); toast.success('Ajuste guardado.'); await settings.reload() }
    catch (e) { toast.error(e) } finally { setSaving(false) }
  }
  const entries = Object.entries(settings.data ?? {}).sort(([a], [b]) => a.localeCompare(b))

  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader title="Ajustes"/>
      <div className="space-y-4">
        <Card title="Apariencia">
          <Field label="Tema">
            {id => <Select id={id} value={theme} onChange={e => setTheme(e.target.value as ThemePref)} className="max-w-xs">
              <option value="system">Automático (según el sistema)</option><option value="light">Claro</option><option value="dark">Oscuro</option></Select>}
          </Field>
        </Card>
        <Card title="Backups">
          <Field label="Carpeta predeterminada para backups" hint="Se propone al crear un backup. Vacío = la última carpeta usada.">
            {id => <div className="flex gap-2">
              <Input id={id} value={dir} onChange={e => setDir(e.target.value)} placeholder="Sin definir"/>
              <Button icon={<FolderOpen size={15}/>} onClick={pick}>Elegir</Button>
              <Button variant="primary" icon={<Save size={15}/>} loading={saving} onClick={save}>Guardar</Button>
            </div>}
          </Field>
        </Card>
        <Card title="Todos los ajustes guardados">
          <ErrorBanner message={settings.error} onRetry={() => void settings.reload()}/>
          {settings.loading && !settings.data && <Spinner/>}
          {settings.data && entries.length === 0 && <p className="text-sm text-muted">No hay ajustes guardados todavía.</p>}
          {entries.length > 0 && (
            <Table>
              <thead><tr><Th>Clave</Th><Th>Valor</Th></tr></thead>
              <tbody>{entries.map(([k, v]) => <tr key={k}><Td className="font-mono text-xs">{k}</Td><Td className="break-all font-mono text-xs">{v}</Td></tr>)}</tbody>
            </Table>
          )}
        </Card>
      </div>
    </div>
  )
}

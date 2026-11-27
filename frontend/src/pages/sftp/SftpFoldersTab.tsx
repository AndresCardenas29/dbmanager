import {useMemo, useState} from 'react'
import {FolderOpen, FolderPlus, Grid3x3, Loader2, Plus, RefreshCw, Trash2} from 'lucide-react'
import {errMsg} from '../../api'
import {api2, type SftpFolderLevel, type SftpService} from '../../api/v2'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {
  Badge, Button, Card, Checkbox, ConfirmDialog, EmptyState, ErrorBanner, Field, IconButton, Input, Modal, Notice, Select,
  Spinner, Table, Td, Th,
} from '../../components/ui'

const NAME_RE = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$/
const LEVELS: Array<{value: SftpFolderLevel; label: string}> = [
  {value: 'none', label: 'Sin acceso'}, {value: 'read', label: 'Solo lectura'}, {value: 'readwrite', label: 'Lectura y escritura'},
]
export const levelLabel = (l: string) => (l === 'readwrite' ? 'Lectura y escritura' : l === 'read' ? 'Solo lectura' : 'Sin acceso')

export default function SftpFoldersTab({svc}: {svc: SftpService}) {
  const toast = useToast()
  const folders = useAsync(() => api2.listSftpFolders(svc.id), [svc.id])
  const users = useAsync(() => api2.listSftpUsers(svc.id), [svc.id])
  const [creating, setCreating] = useState(false)
  const [dropping, setDropping] = useState<string | null>(null)
  const [delFiles, setDelFiles] = useState(false)
  const [pending, setPending] = useState<Record<string, boolean>>({})
  const key = (f: string, u: string) => `${f}\u0000${u}`

  const rows = folders.data ?? []
  const names = (users.data ?? []).map(u => u.name)
  const levels = useMemo(() => {
    const m = new Map<string, SftpFolderLevel>()
    for (const f of folders.data ?? []) for (const a of f.access ?? []) m.set(`${f.name}\u0000${a.user}`, a.level)
    return m
  }, [folders.data])

  const open = async (name: string) => {
    try { const p = await api2.openSftpFolder(svc.id, 'folder', name); toast.success(`Carpeta abierta: ${p}`) } catch (e) { toast.error(e) }
  }

  const change = async (folder: string, user: string, level: SftpFolderLevel) => {
    const k = key(folder, user)
    const prev = levels.get(k) ?? 'none'
    if (prev === level) return
    const apply = (lv: SftpFolderLevel) => folders.setData(d => (d ?? []).map(f => {
      if (f.name !== folder) return f
      const access = (f.access ?? []).filter(a => a.user !== user)
      if (lv !== 'none') access.push({user, level: lv})
      return {...f, access}
    }))
    apply(level)
    setPending(p => ({...p, [k]: true}))
    try { await api2.setSftpFolderAccess(svc.id, folder, user, level) }
    catch (e) { apply(prev); toast.error(e) }
    finally { setPending(p => { const n = {...p}; delete n[k]; return n }) }
  }

  const reload = () => { void folders.reload(); void users.reload() }

  return (
    <div className="space-y-4">
      <Notice tone="accent">
        Cada carpeta compartida aparece dentro del home de cada usuario con acceso como <code>/&lt;carpeta&gt;</code>. El modo solo lectura lo hace cumplir el servidor.
        Cada cambio de acceso recrea el contenedor (unos segundos sin servicio).
      </Notice>
      <Card title="Carpetas compartidas" actions={<>
        <IconButton label="Actualizar" onClick={reload}><RefreshCw size={15} className={folders.loading ? 'spin' : ''}/></IconButton>
        <Button size="sm" variant="primary" icon={<Plus size={14}/>} onClick={() => setCreating(true)}>Crear carpeta</Button>
      </>}>
        <ErrorBanner message={folders.error} onRetry={() => void folders.reload()}/>
        {folders.loading && !folders.data && <Spinner/>}
        {folders.data && rows.length === 0 && <EmptyState icon={<FolderPlus size={28}/>} title="No hay carpetas compartidas">Crea una y asigna qué usuarios pueden verla.</EmptyState>}
        {rows.length > 0 && (
          <Table>
            <thead><tr><Th>Carpeta</Th><Th>Acceso</Th><Th><span className="sr-only">Acciones</span></Th></tr></thead>
            <tbody>{rows.map(f => (
              <tr key={f.id} className="align-top hover:bg-surface2/60">
                <Td className="font-mono text-xs font-medium">/{f.name}</Td>
                <Td>{(f.access ?? []).length === 0 ? <span className="text-muted">Nadie</span> : (
                  <div className="flex flex-wrap gap-1">{(f.access ?? []).map(a => (
                    <Badge key={a.user} tone={a.level === 'readwrite' ? 'success' : 'warn'}>{a.user}: {levelLabel(a.level)}</Badge>))}</div>)}</Td>
                <Td><div className="flex items-center justify-end gap-1">
                  <Button size="sm" icon={<FolderOpen size={14}/>} onClick={() => open(f.name)}>Abrir</Button>
                  <IconButton label={`Eliminar ${f.name}`} onClick={() => { setDelFiles(false); setDropping(f.name) }}><Trash2 size={15} className="text-danger"/></IconButton>
                </div></Td>
              </tr>))}
            </tbody>
          </Table>
        )}
      </Card>

      <Card title="Permisos por usuario y carpeta">
        <ErrorBanner message={users.error} onRetry={() => void users.reload()}/>
        {(names.length === 0 || rows.length === 0)
          ? <EmptyState icon={<Grid3x3 size={28}/>} title="No hay nada que mostrar">Se necesita al menos un usuario y una carpeta compartida.</EmptyState>
          : (
            <Table>
              <thead><tr><Th>Usuario</Th>{rows.map(f => <Th key={f.id}>/{f.name}</Th>)}</tr></thead>
              <tbody>{names.map(u => (
                <tr key={u} className="hover:bg-surface2/60">
                  <Td className="font-medium">{u}</Td>
                  {rows.map(f => {
                    const k = key(f.name, u)
                    return (
                      <Td key={f.id}>
                        <div className="flex items-center gap-2">
                          <Select aria-label={`Acceso de ${u} a ${f.name}`} className="!h-8 text-xs" value={levels.get(k) ?? 'none'} disabled={!!pending[k]}
                            onChange={e => void change(f.name, u, e.target.value as SftpFolderLevel)}>
                            {LEVELS.map(l => <option key={l.value} value={l.value}>{l.label}</option>)}
                          </Select>
                          {pending[k] && <Loader2 size={14} className="spin text-muted"/>}
                        </div>
                      </Td>
                    )
                  })}
                </tr>))}
              </tbody>
            </Table>
          )}
      </Card>

      {creating && <CreateFolderModal svc={svc} onClose={() => setCreating(false)} onDone={reload}/>}
      {dropping && (
        <ConfirmDialog title="Eliminar carpeta compartida" danger confirmLabel="Eliminar" typeToConfirm={dropping} onClose={() => setDropping(null)}
          onConfirm={async () => { try { await api2.deleteSftpFolder(svc.id, dropping, delFiles); toast.success(`«${dropping}» eliminada.`); reload() } catch (e) { toast.error(e); throw e } }}>
          <p>Se eliminará la carpeta compartida <b>{dropping}</b> y se quitará el acceso a todos los usuarios.</p>
          <Checkbox label="Eliminar también los archivos" checked={delFiles} onChange={e => setDelFiles(e.target.checked)}
            hint="Borra su contenido de forma irreversible. En modo «carpeta del equipo» las carpetas del equipo nunca se borran."/>
        </ConfirmDialog>
      )}
    </div>
  )
}

function CreateFolderModal({svc, onClose, onDone}: {svc: SftpService; onClose: () => void; onDone: () => void}) {
  const toast = useToast()
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const create = async () => {
    setBusy(true); setError('')
    try { await api2.createSftpFolder(svc.id, name); toast.success(`Carpeta «${name}» creada.`); onDone(); onClose() }
    catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title="Crear carpeta compartida" width="max-w-md" onClose={() => !busy && onClose()} footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" disabled={!NAME_RE.test(name)} loading={busy} onClick={create}>Crear carpeta</Button></>}>
      <div className="space-y-3">
        <Field label="Nombre" hint="Letras, números, _ . y -. Hasta 64 caracteres." error={name && !NAME_RE.test(name) ? 'Nombre no válido.' : ''}>
          {id => <Input id={id} autoFocus value={name} onChange={e => setName(e.target.value)} autoComplete="off"/>}</Field>
        <ErrorBanner message={error}/>
      </div>
    </Modal>
  )
}

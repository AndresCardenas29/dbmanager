import {useState} from 'react'
import {Database as DbIcon, Plus, RefreshCw, Trash2} from 'lucide-react'
import {api, errMsg, type Server} from '../../api'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {
  Badge, Button, Card, Checkbox, ConfirmDialog, EmptyState, ErrorBanner, Field, fmtMB, IconButton, Input, Modal,
  Notice, Select, Spinner, Table, Td, Th,
} from '../../components/ui'

const STATES: Record<string, {label: string; tone: 'success' | 'warn' | 'danger' | 'neutral'}> = {
  ONLINE: {label: 'En línea', tone: 'success'}, OFFLINE: {label: 'Fuera de línea', tone: 'neutral'},
  RESTORING: {label: 'Restaurando', tone: 'warn'}, RECOVERING: {label: 'Recuperando', tone: 'warn'},
  'RECOVERY_PENDING': {label: 'Recuperación pendiente', tone: 'warn'}, SUSPECT: {label: 'Sospechosa', tone: 'danger'},
  EMERGENCY: {label: 'Emergencia', tone: 'danger'},
}
const dbState = (s: string) => STATES[(s || '').toUpperCase()] ?? {label: s || '—', tone: 'neutral' as const}
const NAME_RE = /^[A-Za-z_][A-Za-z0-9_-]{0,127}$/

export default function DatabasesTab({server}: {server: Server}) {
  const toast = useToast()
  const dbs = useAsync(() => api.listDatabases(server.id), [server.id])
  const [showSystem, setShowSystem] = useState(false)
  const [creating, setCreating] = useState(false)
  const [dropping, setDropping] = useState<string | null>(null)
  const rows = (dbs.data ?? []).filter(d => showSystem || !d.system)
  const maxLen = server.engine === 'postgres' ? 63 : 128

  return (
    <Card title="Bases de datos" actions={<>
      <Checkbox label={<span className="text-xs">Mostrar de sistema</span>} checked={showSystem} onChange={e => setShowSystem(e.target.checked)}/>
      <IconButton label="Actualizar" onClick={() => void dbs.reload()}><RefreshCw size={15} className={dbs.loading ? 'spin' : ''}/></IconButton>
      <Button size="sm" variant="primary" icon={<Plus size={14}/>} onClick={() => setCreating(true)}>Crear base de datos</Button>
    </>}>
      <ErrorBanner message={dbs.error} onRetry={() => void dbs.reload()}/>
      {dbs.loading && !dbs.data && <Spinner/>}
      {dbs.data && rows.length === 0 && <EmptyState icon={<DbIcon size={28}/>} title="No hay bases de datos">
        {showSystem ? 'Este servidor no tiene bases de datos.' : 'Crea la primera o activa «Mostrar de sistema».'}</EmptyState>}
      {rows.length > 0 && (
        <Table>
          <thead><tr><Th>Nombre</Th><Th>Tamaño</Th><Th>Dueño</Th><Th>Estado</Th><Th className="w-12"><span className="sr-only">Acciones</span></Th></tr></thead>
          <tbody>
            {rows.map(d => (
              <tr key={d.name} className="hover:bg-surface2/60">
                <Td className="font-medium">{d.name} {d.system && <Badge className="ml-1">sistema</Badge>}</Td>
                <Td>{fmtMB(d.sizeMB)}</Td>
                <Td>{d.owner || '—'}</Td>
                <Td><Badge tone={dbState(d.state).tone}>{dbState(d.state).label}</Badge></Td>
                <Td>{!d.system && <IconButton label={`Eliminar ${d.name}`} onClick={() => setDropping(d.name)}><Trash2 size={15} className="text-danger"/></IconButton>}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}

      {creating && <CreateModal server={server} maxLen={maxLen} onClose={() => setCreating(false)} onDone={() => { toast.success('Base de datos creada.'); void dbs.reload() }}/>}
      {dropping && (
        <ConfirmDialog title="Eliminar base de datos" danger typeToConfirm={dropping} confirmLabel="Eliminar" onClose={() => setDropping(null)}
          onConfirm={async () => {
            try { await api.dropDatabase(server.id, dropping); toast.success(`«${dropping}» eliminada.`); void dbs.reload() }
            catch (e) { toast.error(e); throw e }
          }}>
          <Notice tone="danger">Se eliminará <b>{dropping}</b> con todos sus datos. Esta acción no se puede deshacer. Considera crear un backup antes.</Notice>
        </ConfirmDialog>
      )}
    </Card>
  )
}

function CreateModal({server, maxLen, onClose, onDone}: {server: Server; maxLen: number; onClose: () => void; onDone: () => void}) {
  const logins = useAsync(() => api.listLogins(server.id), [server.id])
  const [name, setName] = useState('')
  const [owner, setOwner] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const ok = NAME_RE.test(name) && name.length <= maxLen

  const create = async () => {
    setBusy(true); setError('')
    try { await api.createDatabase(server.id, name, owner); onDone(); onClose() }
    catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title="Crear base de datos" onClose={onClose} footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" disabled={!ok} loading={busy} onClick={create}>Crear</Button>
    </>}>
      <form className="space-y-4" onSubmit={e => { e.preventDefault(); if (ok && !busy) void create() }}>
        <Field label="Nombre" hint={`Empieza con letra o _; letras, números, _ y -. Máx. ${maxLen} caracteres.`} error={name && !ok ? 'Nombre no válido.' : ''}>
          {id => <Input id={id} value={name} onChange={e => setName(e.target.value)} autoComplete="off"/>}
        </Field>
        <Field label="Dueño (opcional)" hint={logins.error ? `No se pudieron cargar los usuarios: ${logins.error}` : 'Vacío = el administrador del servidor.'}>
          {id => <Select id={id} value={owner} onChange={e => setOwner(e.target.value)}>
            <option value="">(administrador)</option>
            {(logins.data ?? []).filter(l => !l.isSystem).map(l => <option key={l.name} value={l.name}>{l.name}</option>)}
          </Select>}
        </Field>
        <ErrorBanner message={error}/>
      </form>
    </Modal>
  )
}

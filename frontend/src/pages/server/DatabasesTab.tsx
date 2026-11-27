import {useState} from 'react'
import {Database as DbIcon, Plus, RefreshCw, Trash2, UserPlus, Users as UsersIcon} from 'lucide-react'
import {api, errMsg, type AccessLevel, type Server} from '../../api'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {
  Badge, Button, Card, Checkbox, ConfirmDialog, cx, EmptyState, ErrorBanner, Field, fmtMB, IconButton, Input, Modal,
  Notice, PasswordInput, Select, Spinner, Table, Td, Th,
} from '../../components/ui'
import {LEVELS} from './PermissionsTab'
import {passwordStrength, StrengthMeter} from '../NewServicePage'

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
  const [usersOf, setUsersOf] = useState<string | null>(null)
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
          <thead><tr><Th>Nombre</Th><Th>Tamaño</Th><Th>Dueño</Th><Th>Estado</Th><Th className="w-40"><span className="sr-only">Acciones</span></Th></tr></thead>
          <tbody>
            {rows.map(d => (
              <tr key={d.name} className="hover:bg-surface2/60">
                <Td className="font-medium">{d.name} {d.system && <Badge className="ml-1">sistema</Badge>}</Td>
                <Td>{fmtMB(d.sizeMB)}</Td>
                <Td>{d.owner || '—'}</Td>
                <Td><Badge tone={dbState(d.state).tone}>{dbState(d.state).label}</Badge></Td>
                <Td>{!d.system && <div className="flex justify-end gap-1">
                  <Button size="sm" icon={<UsersIcon size={14}/>} onClick={() => setUsersOf(d.name)}>Usuarios</Button>
                  <IconButton label={`Eliminar ${d.name}`} onClick={() => setDropping(d.name)}><Trash2 size={15} className="text-danger"/></IconButton>
                </div>}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}

      {usersOf && <DbUsersModal server={server} database={usersOf} onClose={() => setUsersOf(null)}/>}
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

const GRANTABLE = LEVELS.filter(l => l.value !== 'none')

// Usuarios de una sola base de datos: crear, cambiar su nivel de acceso, quitarle el acceso o eliminarlo.
// El usuario root (administrador) tiene acceso a todas las bases y no se lista aquí.
function DbUsersModal({server, database, onClose}: {server: Server; database: string; onClose: () => void}) {
  const toast = useToast()
  const m = useAsync(() => api.getPermissionMatrix(server.id), [server.id])
  const [pending, setPending] = useState('')
  const [deleting, setDeleting] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [pw, setPw] = useState('')
  const [level, setLevel] = useState<AccessLevel>('readwrite')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const isRoot = (l: string) => l.toLowerCase() === server.adminUser.toLowerCase()
  const users = (m.data?.cells ?? []).filter(c => c.database === database && c.level !== 'none' && !isRoot(c.login))
    .sort((a, b) => a.login.localeCompare(b.login))
  const strength = passwordStrength(server.engine, pw)
  const ok = NAME_RE.test(name) && strength.ok

  const setAccess = async (login: string, lv: AccessLevel) => {
    setPending(login)
    try { await api.setDatabaseAccess(server.id, login, database, lv); toast.success(lv === 'none' ? `Se quitó el acceso de «${login}».` : 'Permiso actualizado.'); await m.reload() }
    catch (e) { toast.error(e) } finally { setPending('') }
  }
  const create = async () => {
    setBusy(true); setError('')
    try {
      await api.createDatabaseUser(server.id, database, {name, password: pw, serverRoles: []}, level)
      toast.success(`Usuario «${name}» creado con acceso a ${database}.`)
      setName(''); setPw(''); await m.reload()
    } catch (e) { setError(errMsg(e)) } finally { setBusy(false) }
  }

  return (
    <Modal title={`Usuarios de ${database}`} width="max-w-2xl" onClose={onClose} footer={<Button onClick={onClose}>Cerrar</Button>}>
      <div className="space-y-4">
        <p className="text-xs text-muted">El usuario root «{server.adminUser}» tiene acceso a todas las bases. Los usuarios de aquí solo acceden a {database}.</p>
        <ErrorBanner message={m.error} onRetry={() => void m.reload()}/>
        {m.loading && !m.data && <Spinner/>}
        {m.data && users.length === 0 && <p className="text-sm text-muted">Esta base de datos todavía no tiene usuarios propios.</p>}
        {users.length > 0 && (
          <Table>
            <thead><tr><Th>Usuario</Th><Th>Permiso</Th><Th className="w-24"><span className="sr-only">Acciones</span></Th></tr></thead>
            <tbody>{users.map(u => (
              <tr key={u.login}>
                <Td className="font-medium">{u.login}</Td>
                <Td>
                  <select aria-label={`Permiso de ${u.login}`} value={u.level} disabled={pending === u.login}
                    onChange={e => void setAccess(u.login, e.target.value as AccessLevel)}
                    className={cx('h-8 w-full rounded border border-line bg-surface px-2 text-xs disabled:opacity-60', LEVELS.find(x => x.value === u.level)?.tone)}>
                    {LEVELS.map(o => <option key={o.value} value={o.value}>{o.value === 'none' ? 'Quitar acceso' : o.label}</option>)}
                  </select>
                </Td>
                <Td><IconButton label={`Eliminar usuario ${u.login}`} onClick={() => setDeleting(u.login)}><Trash2 size={15} className="text-danger"/></IconButton></Td>
              </tr>
            ))}</tbody>
          </Table>
        )}

        <form className="space-y-3 rounded-md border border-line bg-surface2 p-3" onSubmit={e => { e.preventDefault(); if (ok && !busy) void create() }}>
          <p className="flex items-center gap-2 text-sm font-medium"><UserPlus size={15}/> Agregar usuario a {database}</p>
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="Usuario" error={name && !NAME_RE.test(name) ? 'Nombre no válido.' : ''}>
              {id => <Input id={id} value={name} autoComplete="off" onChange={e => setName(e.target.value)}/>}
            </Field>
            <Field label="Contraseña">{id => <><PasswordInput id={id} value={pw} onChange={setPw}/><StrengthMeter engine={server.engine} pw={pw}/></>}</Field>
            <Field label="Permiso">
              {id => <Select id={id} value={level} onChange={e => setLevel(e.target.value as AccessLevel)}>
                {GRANTABLE.map(l => <option key={l.value} value={l.value}>{l.label}</option>)}
              </Select>}
            </Field>
          </div>
          <p className="text-xs text-muted">{server.engine === 'mssql' ? LEVELS.find(l => l.value === level)?.mssql : LEVELS.find(l => l.value === level)?.pg}</p>
          <ErrorBanner message={error}/>
          <div className="flex justify-end"><Button type="submit" variant="primary" icon={<UserPlus size={15}/>} disabled={!ok} loading={busy}>Agregar usuario</Button></div>
        </form>
      </div>
      {deleting && (
        <ConfirmDialog title="Eliminar usuario" danger confirmLabel="Eliminar" onClose={() => setDeleting(null)}
          onConfirm={async () => {
            try { await api.dropLogin(server.id, deleting); toast.success(`Usuario «${deleting}» eliminado.`); await m.reload() }
            catch (e) { toast.error(e); throw e }
          }}>
          <p>Se eliminará el usuario <b>{deleting}</b> del servidor, incluido su acceso a cualquier otra base de datos.</p>
        </ConfirmDialog>
      )}
    </Modal>
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

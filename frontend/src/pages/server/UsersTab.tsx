import {useState} from 'react'
import {KeyRound, Plus, RefreshCw, ShieldCheck, Trash2, UserRound, Users} from 'lucide-react'
import {api, errMsg, type Login, type Server} from '../../api'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {StrengthMeter, passwordStrength} from '../NewServicePage'
import {
  Badge, Button, Card, Checkbox, ConfirmDialog, EmptyState, ErrorBanner, Field, IconButton, Input, Modal,
  Notice, PasswordInput, Spinner, Table, Td, Th,
} from '../../components/ui'

const ROLES: Record<string, Array<{value: string; hint: string}>> = {
  mssql: [
    {value: 'sysadmin', hint: 'Control total del servidor'},
    {value: 'dbcreator', hint: 'Puede crear y restaurar bases de datos'},
    {value: 'securityadmin', hint: 'Gestiona logins y permisos'},
  ],
  postgres: [
    {value: 'SUPERUSER', hint: 'Control total del servidor'},
    {value: 'CREATEDB', hint: 'Puede crear bases de datos'},
    {value: 'CREATEROLE', hint: 'Puede crear y modificar roles'},
  ],
}
const NAME_RE = /^[A-Za-z_][A-Za-z0-9_-]{0,127}$/

function RoleChecks({engine, value, onChange}: {engine: string; value: string[]; onChange: (v: string[]) => void}) {
  return (
    <fieldset className="space-y-2">
      <legend className="mb-1 text-xs font-medium text-muted">Roles de servidor</legend>
      {ROLES[engine].map(r => (
        <Checkbox key={r.value} label={<span className="font-mono text-xs">{r.value}</span>} hint={r.hint}
          checked={value.some(v => v.toLowerCase() === r.value.toLowerCase())}
          onChange={e => onChange(e.target.checked ? [...value, r.value] : value.filter(v => v.toLowerCase() !== r.value.toLowerCase()))}/>
      ))}
    </fieldset>
  )
}

export default function UsersTab({server}: {server: Server}) {
  const toast = useToast()
  const logins = useAsync(() => api.listLogins(server.id), [server.id])
  const [creating, setCreating] = useState(false)
  const [pwFor, setPwFor] = useState<Login | null>(null)
  const [rolesFor, setRolesFor] = useState<Login | null>(null)
  const [dropping, setDropping] = useState<Login | null>(null)
  const rows = logins.data ?? []

  return (
    <Card title="Usuarios (logins)" actions={<>
      <IconButton label="Actualizar" onClick={() => void logins.reload()}><RefreshCw size={15} className={logins.loading ? 'spin' : ''}/></IconButton>
      <Button size="sm" variant="primary" icon={<Plus size={14}/>} onClick={() => setCreating(true)}>Crear usuario</Button>
    </>}>
      <ErrorBanner message={logins.error} onRetry={() => void logins.reload()}/>
      {logins.loading && !logins.data && <Spinner/>}
      {logins.data && rows.length === 0 && <EmptyState icon={<Users size={28}/>} title="No hay usuarios"/>}
      {rows.length > 0 && (
        <Table>
          <thead><tr><Th>Usuario</Th><Th>Roles de servidor</Th><Th>Estado</Th><Th className="w-32"><span className="sr-only">Acciones</span></Th></tr></thead>
          <tbody>
            {rows.map(l => (
              <tr key={l.name} className="hover:bg-surface2/60">
                <Td className="font-medium"><span className="inline-flex items-center gap-2"><UserRound size={14} className="text-muted"/>{l.name}{l.isSystem && <Badge>sistema</Badge>}</span></Td>
                <Td>{(l.serverRoles ?? []).length ? <span className="flex flex-wrap gap-1">{(l.serverRoles ?? []).map(r => <Badge key={r} tone="accent">{r}</Badge>)}</span> : <span className="text-muted">—</span>}</Td>
                <Td>{l.disabled ? <Badge tone="danger">Deshabilitado</Badge> : <Badge tone="success">Activo</Badge>}</Td>
                <Td><div className="flex justify-end">
                  {!l.isSystem && <IconButton label={`Cambiar contraseña de ${l.name}`} onClick={() => setPwFor(l)}><KeyRound size={15}/></IconButton>}
                  {!l.isSystem && <IconButton label={`Editar roles de ${l.name}`} onClick={() => setRolesFor(l)}><ShieldCheck size={15}/></IconButton>}
                  {!l.isSystem && <IconButton label={`Eliminar ${l.name}`} onClick={() => setDropping(l)}><Trash2 size={15} className="text-danger"/></IconButton>}
                </div></Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}

      {creating && <CreateModal server={server} onClose={() => setCreating(false)} onDone={() => { toast.success('Usuario creado.'); void logins.reload() }}/>}
      {pwFor && <PasswordModal server={server} login={pwFor} onClose={() => setPwFor(null)}/>}
      {rolesFor && <RolesModal server={server} login={rolesFor} onClose={() => setRolesFor(null)} onDone={() => { toast.success('Roles actualizados.'); void logins.reload() }}/>}
      {dropping && (
        <ConfirmDialog title="Eliminar usuario" danger confirmLabel="Eliminar" typeToConfirm={dropping.name} onClose={() => setDropping(null)}
          onConfirm={async () => {
            try { await api.dropLogin(server.id, dropping.name); toast.success(`«${dropping.name}» eliminado.`); void logins.reload() }
            catch (e) { toast.error(e); throw e }
          }}>
          <Notice tone="danger">Se eliminará el usuario <b>{dropping.name}</b> y perderá todo acceso.</Notice>
        </ConfirmDialog>
      )}
    </Card>
  )
}

function CreateModal({server, onClose, onDone}: {server: Server; onClose: () => void; onDone: () => void}) {
  const [name, setName] = useState('')
  const [pw, setPw] = useState('')
  const [roles, setRoles] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const ok = NAME_RE.test(name) && passwordStrength(server.engine, pw).ok
  const create = async () => {
    setBusy(true); setError('')
    try { await api.createLogin(server.id, {name, password: pw, serverRoles: roles}); onDone(); onClose() }
    catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title="Crear usuario" onClose={onClose} footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" disabled={!ok} loading={busy} onClick={create}>Crear</Button>
    </>}>
      <form className="space-y-4" onSubmit={e => { e.preventDefault(); if (ok && !busy) void create() }}>
        <Field label="Nombre" error={name && !NAME_RE.test(name) ? 'Empieza con letra o _; letras, números, _ y -.' : ''}>
          {id => <Input id={id} value={name} onChange={e => setName(e.target.value)} autoComplete="off"/>}
        </Field>
        <Field label="Contraseña">{id => <><PasswordInput id={id} value={pw} onChange={setPw}/><StrengthMeter engine={server.engine} pw={pw}/></>}</Field>
        <RoleChecks engine={server.engine} value={roles} onChange={setRoles}/>
        <ErrorBanner message={error}/>
      </form>
    </Modal>
  )
}

function PasswordModal({server, login, onClose}: {server: Server; login: Login; onClose: () => void}) {
  const toast = useToast()
  const [pw, setPw] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const ok = passwordStrength(server.engine, pw).ok
  const save = async () => {
    setBusy(true); setError('')
    try { await api.changePassword(server.id, login.name, pw); toast.success('Contraseña actualizada.'); onClose() }
    catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title={`Cambiar contraseña de ${login.name}`} onClose={onClose} width="max-w-md" footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" disabled={!ok} loading={busy} onClick={save}>Cambiar</Button>
    </>}>
      <form className="space-y-4" onSubmit={e => { e.preventDefault(); if (ok && !busy) void save() }}>
        <Field label="Nueva contraseña">{id => <><PasswordInput id={id} autoFocus value={pw} onChange={setPw}/><StrengthMeter engine={server.engine} pw={pw}/></>}</Field>
        <ErrorBanner message={error}/>
      </form>
    </Modal>
  )
}

function RolesModal({server, login, onClose, onDone}: {server: Server; login: Login; onClose: () => void; onDone: () => void}) {
  const [roles, setRoles] = useState<string[]>(login.serverRoles ?? [])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const save = async () => {
    setBusy(true); setError('')
    try { await api.setServerRoles(server.id, login.name, roles); onDone(); onClose() }
    catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title={`Roles de ${login.name}`} onClose={onClose} width="max-w-md" footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" loading={busy} onClick={save}>Guardar</Button>
    </>}>
      <div className="space-y-4">
        <RoleChecks engine={server.engine} value={roles} onChange={setRoles}/>
        <ErrorBanner message={error}/>
      </div>
    </Modal>
  )
}

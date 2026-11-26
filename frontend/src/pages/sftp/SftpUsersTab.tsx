import {useState} from 'react'
import {KeyRound, Key, Plus, RefreshCw, Trash2, UserRound, Users} from 'lucide-react'
import {errMsg} from '../../api'
import {api2, type SftpKey, type SftpService, type SftpUser, type SftpUserResult} from '../../api/v2'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import KeyForm, {emptyKeyForm, keyFormValid, toKeyRequest, type KeyFormState} from '../../components/KeyForm'
import {
  Badge, Button, Card, Checkbox, ConfirmDialog, CopyButton, EmptyState, ErrorBanner, Field, IconButton, Input, Modal,
  Notice, PasswordInput, Spinner, Table, Td, Th,
} from '../../components/ui'

const NAME_RE = /^[a-z_][a-z0-9_-]{0,31}$/

export default function SftpUsersTab({svc}: {svc: SftpService}) {
  const toast = useToast()
  const users = useAsync(() => api2.listSftpUsers(svc.id), [svc.id])
  const [creating, setCreating] = useState(false)
  const [result, setResult] = useState<SftpUserResult | null>(null)
  const [pwFor, setPwFor] = useState<SftpUser | null>(null)
  const [keyFor, setKeyFor] = useState<SftpUser | null>(null)
  const [dropping, setDropping] = useState<SftpUser | null>(null)
  const [rmKey, setRmKey] = useState<{user: SftpUser; key: SftpKey} | null>(null)
  const [delFiles, setDelFiles] = useState(false)
  const rows = users.data ?? []

  return (
    <div className="space-y-4">
      <Notice tone="accent">Cada cambio de usuarios o llaves recrea el contenedor (unos segundos sin servicio). Los archivos se conservan.</Notice>
      <Card title="Usuarios SFTP" actions={<>
        <IconButton label="Actualizar" onClick={() => void users.reload()}><RefreshCw size={15} className={users.loading ? 'spin' : ''}/></IconButton>
        <Button size="sm" variant="primary" icon={<Plus size={14}/>} onClick={() => setCreating(true)}>Crear usuario</Button>
      </>}>
        <ErrorBanner message={users.error} onRetry={() => void users.reload()}/>
        {users.loading && !users.data && <Spinner/>}
        {users.data && rows.length === 0 && <EmptyState icon={<Users size={28}/>} title="No hay usuarios">Crea el primero para poder conectarte.</EmptyState>}
        {rows.length > 0 && (
          <Table>
            <thead><tr><Th>Usuario</Th><Th>UID</Th><Th>Contraseña</Th><Th>Carpeta de subida</Th><Th>Llaves</Th><Th><span className="sr-only">Acciones</span></Th></tr></thead>
            <tbody>{rows.map(u => (
              <tr key={u.name} className="align-top hover:bg-surface2/60">
                <Td className="font-medium"><span className="inline-flex items-center gap-2"><UserRound size={14} className="text-muted"/>{u.name}</span></Td>
                <Td>{u.uid}</Td>
                <Td>{u.hasPassword ? <Badge tone="success">Sí</Badge> : <Badge>No</Badge>}</Td>
                <Td className="font-mono text-xs">{u.uploadDir || '—'}</Td>
                <Td>{(u.keys ?? []).length === 0 ? <span className="text-muted">0</span> : (
                  <ul className="space-y-1">{(u.keys ?? []).map(k => (
                    <li key={k.id} className="flex items-center gap-1 text-xs">
                      <Badge tone="accent">{k.type}</Badge><span className="max-w-[12rem] truncate font-mono" title={k.fingerprint}>{k.fingerprint}</span>
                      <IconButton label={`Quitar llave ${k.comment || k.fingerprint}`} className="!h-6 !w-6" onClick={() => setRmKey({user: u, key: k})}><Trash2 size={12} className="text-danger"/></IconButton>
                    </li>))}</ul>)}</Td>
                <Td><div className="flex justify-end">
                  <IconButton label={`Contraseña de ${u.name}`} onClick={() => setPwFor(u)}><KeyRound size={15}/></IconButton>
                  <IconButton label={`Añadir llave a ${u.name}`} onClick={() => setKeyFor(u)}><Key size={15}/></IconButton>
                  <IconButton label={`Eliminar ${u.name}`} onClick={() => { setDelFiles(false); setDropping(u) }}><Trash2 size={15} className="text-danger"/></IconButton>
                </div></Td>
              </tr>))}
            </tbody>
          </Table>
        )}
      </Card>

      {creating && <CreateModal svc={svc} onClose={() => setCreating(false)} onDone={r => { setResult(r); void users.reload() }}/>}
      {keyFor && <AddKeyModal svc={svc} user={keyFor} onClose={() => setKeyFor(null)} onDone={r => { setResult(r); void users.reload() }}/>}
      {pwFor && <PasswordModal svc={svc} user={pwFor} onClose={() => setPwFor(null)} onDone={() => void users.reload()}/>}
      {result && <ResultDialog r={result} svc={svc} onClose={() => setResult(null)}/>}
      {rmKey && (
        <ConfirmDialog title="Quitar llave" danger confirmLabel="Quitar" onClose={() => setRmKey(null)}
          onConfirm={async () => { try { await api2.removeSftpKey(svc.id, rmKey.user.name, rmKey.key.id); toast.success('Llave eliminada.'); void users.reload() } catch (e) { toast.error(e); throw e } }}>
          <p>Se quitará la llave de <b>{rmKey.user.name}</b> y dejará de poder entrar con ella.</p>
          <code className="block break-all rounded bg-surface2 p-2 font-mono text-xs">{rmKey.key.fingerprint}</code>
        </ConfirmDialog>
      )}
      {dropping && (
        <ConfirmDialog title="Eliminar usuario" danger confirmLabel="Eliminar" typeToConfirm={dropping.name} onClose={() => setDropping(null)}
          onConfirm={async () => { try { await api2.deleteSftpUser(svc.id, dropping.name, delFiles); toast.success(`«${dropping.name}» eliminado.`); void users.reload() } catch (e) { toast.error(e); throw e } }}>
          <p>Se eliminará el usuario <b>{dropping.name}</b>.</p>
          <Checkbox label="Eliminar también sus archivos" checked={delFiles} onChange={e => setDelFiles(e.target.checked)} hint="Borra su carpeta de forma irreversible."/>
        </ConfirmDialog>
      )}
    </div>
  )
}

function CreateModal({svc, onClose, onDone}: {svc: SftpService; onClose: () => void; onDone: (r: SftpUserResult) => void}) {
  const [name, setName] = useState('')
  const [pw, setPw] = useState('')
  const [dir, setDir] = useState('upload')
  const [key, setKey] = useState<KeyFormState>(emptyKeyForm())
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const hasAccess = pw !== '' || key.mode !== 'none'
  const ok = NAME_RE.test(name) && hasAccess && keyFormValid(key) && dir.trim() !== ''
  const create = async () => {
    setBusy(true); setError('')
    try { const r = await api2.createSftpUser(svc.id, {name, password: pw, uploadDir: dir.trim(), key: toKeyRequest(key)}); onDone(r); onClose() }
    catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title="Crear usuario SFTP" width="max-w-xl" onClose={() => !busy && onClose()} footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" disabled={!ok} loading={busy} onClick={create}>Crear usuario</Button></>}>
      <div className="space-y-4">
        <Field label="Nombre" hint="Minúsculas, números, _ y -." error={name && !NAME_RE.test(name) ? 'Nombre no válido.' : ''}>
          {id => <Input id={id} value={name} onChange={e => setName(e.target.value)} autoComplete="off"/>}</Field>
        <Field label="Contraseña (opcional si usas llave)">{id => <PasswordInput id={id} value={pw} onChange={setPw}/>}</Field>
        <Field label="Carpeta de subida" hint="Dentro de /home/usuario; el usuario queda enjaulado en su home.">
          {id => <Input id={id} value={dir} onChange={e => setDir(e.target.value)}/>}</Field>
        <KeyForm value={key} onChange={setKey}/>
        {!hasAccess && <p className="text-xs text-warn">Indica una contraseña, una llave, o ambas.</p>}
        <ErrorBanner message={error}/>
        {busy && <p className="text-xs text-muted">Recreando el contenedor, esto tarda unos segundos...</p>}
      </div>
    </Modal>
  )
}

function AddKeyModal({svc, user, onClose, onDone}: {svc: SftpService; user: SftpUser; onClose: () => void; onDone: (r: SftpUserResult) => void}) {
  const [key, setKey] = useState<KeyFormState>(emptyKeyForm(false))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const add = async () => {
    const req = toKeyRequest(key); if (!req) return
    setBusy(true); setError('')
    try { const r = await api2.addSftpKey(svc.id, user.name, req); onDone(r); onClose() } catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title={`Añadir llave a ${user.name}`} width="max-w-xl" onClose={() => !busy && onClose()} footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" disabled={!keyFormValid(key)} loading={busy} onClick={add}>Añadir llave</Button></>}>
      <div className="space-y-4"><KeyForm value={key} onChange={setKey} allowNone={false}/><ErrorBanner message={error}/></div>
    </Modal>
  )
}

function PasswordModal({svc, user, onClose, onDone}: {svc: SftpService; user: SftpUser; onClose: () => void; onDone: () => void}) {
  const toast = useToast()
  const [pw, setPw] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const save = async (value: string) => {
    setBusy(true); setError('')
    try { await api2.setSftpPassword(svc.id, user.name, value); toast.success(value ? 'Contraseña actualizada.' : 'Contraseña eliminada.'); onDone(); onClose() }
    catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  const canRemove = user.hasPassword && (user.keys ?? []).length > 0
  return (
    <Modal title={`Contraseña de ${user.name}`} width="max-w-md" onClose={() => !busy && onClose()} footer={<>
      {canRemove && <Button variant="danger" disabled={busy} onClick={() => save('')}>Quitar contraseña</Button>}
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" disabled={!pw} loading={busy} onClick={() => save(pw)}>Guardar</Button></>}>
      <div className="space-y-3">
        <Field label="Nueva contraseña">{id => <PasswordInput id={id} autoFocus value={pw} onChange={setPw}/>}</Field>
        {!canRemove && <p className="text-xs text-muted">Solo puedes quitar la contraseña si el usuario tiene al menos una llave.</p>}
        <ErrorBanner message={error}/>
      </div>
    </Modal>
  )
}

function ResultDialog({r, svc, onClose}: {r: SftpUserResult; svc: SftpService; onClose: () => void}) {
  const rows: Array<[string, string]> = [['Llave privada', r.privateKeyPath], ['Llave pública (.pub)', r.publicKeyPath], ['Archivo .ppk (WinSCP/PuTTY)', r.ppkPath]]
  const hints = r.connectionHints ?? []
  return (
    <Modal title={`Usuario «${r.user.name}» listo`} width="max-w-xl" onClose={onClose} footer={<Button variant="primary" onClick={onClose}>Entendido</Button>}>
      <div className="space-y-4 text-sm">
        {r.privateKeyPath && <Notice tone="danger"><b>DB Manager no guarda la llave privada.</b> Cópiala ahora a un lugar seguro: si la pierdes tendrás que generar otra.</Notice>}
        {rows.filter(([, v]) => v).map(([k, v]) => (
          <div key={k}><p className="text-xs text-muted">{k}</p>
            <div className="flex items-center gap-2 rounded-md border border-line bg-surface2 px-3 py-1.5"><code className="min-w-0 flex-1 break-all font-mono text-xs">{v}</code><CopyButton text={v}/></div></div>
        ))}
        <div><p className="text-xs text-muted">Conexión</p>
          <p>Host <b>{svc.host || 'localhost'}</b> · Puerto <b>{svc.port}</b> · Usuario <b>{r.user.name}</b></p></div>
        {hints.length > 0 && <ul className="list-disc space-y-1 pl-5 text-muted">{hints.map(h => <li key={h} className="break-words">{h}</li>)}</ul>}
      </div>
    </Modal>
  )
}

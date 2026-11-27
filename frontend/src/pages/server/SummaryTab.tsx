import {useEffect, useState} from 'react'
import {Check, Copy, Eye, EyeOff, Pencil, Play, RefreshCw, RotateCw, Square, Trash2, Plug} from 'lucide-react'
import {api, errMsg, type Server, type ServerStatus} from '../../api'
import {useApp} from '../../store'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {
  Badge, Button, Card, Checkbox, ConfirmDialog, engineLabel, ErrorBanner, Field, fmtDate, IconButton, Input, Modal,
  Notice, PasswordInput, Select,
} from '../../components/ui'

const stateLabel: Record<string, string> = {running: 'En ejecución', stopped: 'Detenido', missing: 'Contenedor no encontrado', remote: 'Remoto'}

function connStrings(s: Server, u: string, db: string, pw: string): Record<string, string> {
  const host = s.host || 'localhost'
  if (s.engine === 'mssql') return {
    '.NET': `Server=${host},${s.port};${db ? `Database=${db};` : ''}User Id=${u};Password=${pw};Encrypt=${s.tls};TrustServerCertificate=${s.mode === 'local' ? 'True' : s.trustServerCert ? 'True' : 'False'};`,
    JDBC: `jdbc:sqlserver://${host}:${s.port};${db ? `databaseName=${db};` : ''}user=${u};password=${pw};encrypt=${s.tls};trustServerCertificate=true`,
    'sqlcmd': `sqlcmd -S ${host},${s.port}${db ? ` -d ${db}` : ''} -U ${u} -P "${pw}" -C`,
  }
  const ssl = s.tls ? 'require' : 'disable'
  const d = db || 'postgres'
  return {
    '.NET': `Host=${host};Port=${s.port};Username=${u};Password=${pw};Database=${d};SSL Mode=${s.tls ? 'Require' : 'Disable'}`,
    JDBC: `jdbc:postgresql://${host}:${s.port}/${d}?user=${u}&password=${pw}&sslmode=${ssl}`,
    psql: `psql "postgresql://${u}:${pw}@${host}:${s.port}/${d}?sslmode=${ssl}"`,
  }
}

const MASK = '••••••••'
const PLACEHOLDER = 'TU_CONTRASEÑA'

function AsyncCopy({getText, label}: {getText: () => Promise<string>; label: string}) {
  const [ok, setOk] = useState(false)
  return (
    <IconButton label={ok ? 'Copiado' : label} onClick={async () => {
      try { await navigator.clipboard.writeText(await getText()); setOk(true); setTimeout(() => setOk(false), 1500) } catch { /* ignorar */ }
    }}>{ok ? <Check size={15} className="text-success"/> : <Copy size={15}/>}</IconButton>
  )
}

export default function SummaryTab({server}: {server: Server}) {
  const {refresh, go} = useApp()
  const toast = useToast()
  const local = server.mode === 'local'
  const status = useAsync<ServerStatus>(async () => {
    if (!local) return api.testConnection(server.id)
    const st = await api.serverStatus(server.id)
    if (st.state !== 'running') return st
    try { const t = await api.testConnection(server.id); return {...st, reachable: t.reachable, version: t.version, message: t.message || st.message} }
    catch (e) { return {...st, message: errMsg(e)} }
  }, [server.id])
  const [busy, setBusy] = useState('')
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [db, setDb] = useState('')
  const [user, setUser] = useState('')
  const [pws, setPws] = useState<Record<string, string>>({})
  const [shown, setShown] = useState(false)
  const [newPw, setNewPw] = useState('')
  const [savingPw, setSavingPw] = useState(false)
  const running = !local || status.data?.state === 'running'
  const dbList = useAsync(async () => (running ? (await api.listDatabases(server.id)).filter(d => !d.system) : []), [server.id, running])
  const cusers = useAsync(async () => (running ? api.listConnectionUsers(server.id, db) : []), [server.id, db, running])
  const cuList = cusers.data ?? []
  const cu = cuList.find(u => u.login === user) ?? cuList[0]
  const login = cu?.login || server.adminUser
  const known = pws[login]
  const hasPw = cu ? cu.hasPassword : true
  const fetchPw = async (): Promise<string> => {
    if (pws[login] !== undefined) return pws[login]
    let v = ''
    try { v = await api.getLoginPassword(server.id, login) } catch { v = '' }
    setPws(p => ({...p, [login]: v}))
    return v
  }
  useEffect(() => { setShown(false); setNewPw('') }, [login, db])
  const unknownPw = !hasPw || (known !== undefined && known === '')
  const strings = connStrings(server, login, db, shown && known ? known : MASK)
  const keys = Object.keys(strings)
  const [fmt, setFmt] = useState<string>(keys[0])
  const reveal = async () => {
    if (shown) { setShown(false); return }
    await fetchPw(); setShown(true)
  }
  const savePw = async () => {
    setSavingPw(true)
    try {
      await api.rememberLoginPassword(server.id, login, newPw)
      toast.success('Contraseña guardada.')
      setPws(p => ({...p, [login]: newPw})); setNewPw(''); await cusers.reload()
    } catch (e) { toast.error(e) } finally { setSavingPw(false) }
  }
  const copyConn = async () => {
    const pw = hasPw ? await fetchPw() : ''
    return connStrings(server, login, db, pw || PLACEHOLDER)[fmt]
  }
  const state = local ? status.data?.state : 'remote'

  const act = async (kind: 'start' | 'stop' | 'restart') => {
    setBusy(kind)
    try {
      if (kind === 'start') await api.startServer(server.id)
      else if (kind === 'stop') await api.stopServer(server.id)
      else await api.restartServer(server.id)
      toast.success(kind === 'start' ? 'Servidor iniciado.' : kind === 'stop' ? 'Servidor detenido.' : 'Servidor reiniciado.')
      await status.reload(); await refresh()
    } catch (e) { toast.error(e) } finally { setBusy('') }
  }

  return (
    <div className="space-y-4">
      <Card title="Estado" actions={<>
        <Button size="sm" icon={local ? <RefreshCw size={14}/> : <Plug size={14}/>} loading={status.loading && !!status.data} onClick={() => void status.reload()}>
          {local ? 'Actualizar' : 'Probar conexión'}
        </Button>
      </>}>
        {status.error && <ErrorBanner message={status.error} onRetry={() => void status.reload()}/>}
        {status.data && (
          <dl className="grid grid-cols-[11rem_1fr] gap-y-2 text-sm">
            <dt className="text-muted">Estado</dt>
            <dd><Badge tone={state === 'running' ? 'success' : state === 'missing' ? 'danger' : state === 'remote' ? 'accent' : 'neutral'}>{stateLabel[state ?? ''] ?? state}</Badge></dd>
            <dt className="text-muted">Alcanzable</dt><dd>{status.data.reachable ? 'Sí' : 'No'}</dd>
            <dt className="text-muted">Versión</dt><dd className="break-words">{status.data.version || '—'}</dd>
            {status.data.message && <><dt className="text-muted">Mensaje</dt><dd className="break-words">{status.data.message}</dd></>}
          </dl>
        )}
        {!status.data && !status.error && <p className="text-sm text-muted">Consultando estado...</p>}
        {local && (
          <div className="mt-4 flex flex-wrap gap-2 border-t border-line pt-4">
            <Button icon={<Play size={15}/>} loading={busy === 'start'} disabled={!!busy || state === 'running' || state === 'missing'} onClick={() => act('start')}>Iniciar</Button>
            <Button icon={<Square size={15}/>} loading={busy === 'stop'} disabled={!!busy || state !== 'running'} onClick={() => act('stop')}>Detener</Button>
            <Button icon={<RotateCw size={15}/>} loading={busy === 'restart'} disabled={!!busy || state === 'missing'} onClick={() => act('restart')}>Reiniciar</Button>
          </div>
        )}
        {local && state === 'missing' && <div className="mt-3"><Notice tone="danger">El contenedor «{server.containerName}» ya no existe en Docker. Puedes eliminar este registro.</Notice></div>}
      </Card>

      <Card title="Conexión" actions={
        <Select aria-label="Formato" className="!h-8 !w-auto text-xs" value={fmt} onChange={e => setFmt(e.target.value)}>
          {keys.map(k => <option key={k}>{k}</option>)}
        </Select>}>
        <dl className="mb-4 grid grid-cols-[11rem_1fr] gap-y-2 text-sm">
          <dt className="text-muted">Motor</dt><dd>{engineLabel(server.engine)}</dd>
          <dt className="text-muted">Host</dt><dd className="font-mono text-xs leading-5">{server.host || 'localhost'}</dd>
          <dt className="text-muted">Puerto</dt><dd className="font-mono text-xs leading-5">{server.port}</dd>
          <dt className="text-muted">Usuario administrador</dt><dd className="font-mono text-xs leading-5">{server.adminUser}</dd>
          <dt className="text-muted"><label htmlFor="conn-db">Base de datos</label></dt>
          <dd><Select id="conn-db" className="!h-8 max-w-xs text-xs" value={db} onChange={e => setDb(e.target.value)}>
            <option value="">(ninguna)</option>
            {(dbList.data ?? []).map(d => <option key={d.name} value={d.name}>{d.name}</option>)}
          </Select></dd>
          <dt className="text-muted"><label htmlFor="conn-user">Usuario</label></dt>
          <dd><Select id="conn-user" className="!h-8 max-w-xs text-xs" value={login} onChange={e => setUser(e.target.value)}>
            {cuList.length === 0 && <option value={server.adminUser}>{server.adminUser}</option>}
            {cuList.map(u => <option key={u.login} value={u.login}>{u.isRoot ? 'root (administrador)' : u.level ? `${u.login} (${u.level})` : u.login}</option>)}
          </Select></dd>
          <dt className="text-muted">Contraseña</dt>
          <dd>
            {unknownPw ? (
              <div className="space-y-2">
                <span className="text-muted">Desconocida</span>
                <form className="flex max-w-sm items-center gap-2" onSubmit={e => { e.preventDefault(); if (newPw && !savingPw) void savePw() }}>
                  <div className="min-w-0 flex-1"><PasswordInput value={newPw} onChange={setNewPw} placeholder="Contraseña de este usuario"/></div>
                  <Button type="submit" size="sm" variant="primary" disabled={!newPw} loading={savingPw}>Guardar contraseña</Button>
                </form>
              </div>
            ) : (
              <span className="flex items-center gap-1">
                <span className="font-mono text-xs leading-5">{shown && known ? known : MASK}</span>
                <IconButton label={shown ? 'Ocultar contraseña' : 'Mostrar contraseña'} onClick={() => void reveal()}>{shown ? <EyeOff size={15}/> : <Eye size={15}/>}</IconButton>
                <AsyncCopy label="Copiar contraseña" getText={fetchPw}/>
              </span>
            )}
          </dd>
          {local && <><dt className="text-muted">Contenedor</dt><dd className="font-mono text-xs leading-5">{server.containerName}</dd>
            <dt className="text-muted">Imagen</dt><dd className="break-all font-mono text-xs leading-5">{server.image}</dd>
            <dt className="text-muted">Datos</dt><dd className="break-all">{server.dataMode === 'bind' ? `Carpeta: ${server.dataPath}` : server.dataMode === 'volume' ? `Volumen: ${server.dataPath || '(automático)'}` : '—'}</dd></>}
          {!local && <><dt className="text-muted">TLS</dt><dd>{server.tls ? 'Sí' : 'No'}{server.trustServerCert ? ' · confía en el certificado' : ''}</dd></>}
          <dt className="text-muted">Registrado</dt><dd>{fmtDate(server.createdAt)}</dd>
        </dl>
        <div className="flex items-start gap-2 rounded-md border border-line bg-surface2 p-3">
          <code className="min-w-0 flex-1 break-all font-mono text-xs leading-5">{strings[fmt]}</code>
          <AsyncCopy label="Copiar cadena de conexión" getText={copyConn}/>
        </div>
        <p className="mt-2 text-xs text-muted">La contraseña se muestra oculta; el botón de copiar incluye la contraseña real{unknownPw ? ` (desconocida: se copiará ${PLACEHOLDER}; guárdala arriba)` : ""}.</p>
      </Card>

      <Card title="Administrar">
        <div className="flex flex-wrap gap-2">
          <Button icon={<Pencil size={15}/>} onClick={() => setEditing(true)}>Editar</Button>
          <Button variant="danger" icon={<Trash2 size={15}/>} onClick={() => setDeleting(true)}>{local ? 'Eliminar servicio' : 'Quitar servidor'}</Button>
        </div>
      </Card>

      {editing && <EditModal server={server} onClose={() => setEditing(false)} onSaved={async () => { await refresh(); await status.reload() }}/>}
      {deleting && <DeleteDialog server={server} onClose={() => setDeleting(false)} onDone={async () => { await refresh(); go({page: 'home'}) }}/>}
    </div>
  )
}

function EditModal({server, onClose, onSaved}: {server: Server; onClose: () => void; onSaved: () => Promise<void>}) {
  const toast = useToast()
  const local = server.mode === 'local'
  const [name, setName] = useState(server.name)
  const [host, setHost] = useState(server.host)
  const [port, setPort] = useState(String(server.port))
  const [user, setUser] = useState(server.adminUser)
  const [pw, setPw] = useState('')
  const [tls, setTls] = useState(server.tls)
  const [trust, setTrust] = useState(server.trustServerCert)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const portNum = Number(port)
  const ok = name.trim() && user.trim() && portNum > 0 && portNum < 65536

  const save = async () => {
    setBusy(true); setError('')
    try {
      await api.updateServer(server.id, {name: name.trim(), host: host.trim(), port: portNum, adminUser: user.trim(), adminPassword: pw, tls, trustServerCert: trust})
      toast.success('Servidor actualizado.'); await onSaved(); onClose()
    } catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title="Editar servidor" onClose={onClose} footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" disabled={!ok} loading={busy} onClick={save}>Guardar</Button>
    </>}>
      <div className="space-y-4">
        <Field label="Nombre">{id => <Input id={id} value={name} onChange={e => setName(e.target.value)}/>}</Field>
        <div className="grid grid-cols-3 gap-4">
          <Field label="Host" className="col-span-2">{id => <Input id={id} value={host} disabled={local} onChange={e => setHost(e.target.value)}/>}</Field>
          <Field label="Puerto">{id => <Input id={id} value={port} disabled={local} inputMode="numeric" onChange={e => setPort(e.target.value.replace(/\D/g, ''))}/>}</Field>
        </div>
        {local && <p className="-mt-2 text-xs text-muted">Host y puerto de un servicio local los define su contenedor.</p>}
        <Field label="Usuario administrador">{id => <Input id={id} value={user} onChange={e => setUser(e.target.value)}/>}</Field>
        <Field label="Nueva contraseña" hint="Déjala vacía para conservar la actual. Esto actualiza la contraseña guardada en Andres Tools.">
          {id => <PasswordInput id={id} autoComplete="off" value={pw} onChange={setPw}/>}
        </Field>
        {!local && <div className="space-y-2">
          <Checkbox label="Usar TLS / cifrado" checked={tls} onChange={e => setTls(e.target.checked)}/>
          <Checkbox label="Confiar en el certificado del servidor" checked={trust} onChange={e => setTrust(e.target.checked)}/>
        </div>}
        <ErrorBanner message={error}/>
      </div>
    </Modal>
  )
}

function DeleteDialog({server, onClose, onDone}: {server: Server; onClose: () => void; onDone: () => Promise<void>}) {
  const toast = useToast()
  const local = server.mode === 'local'
  const [rmContainer, setRmContainer] = useState(true)
  const [rmData, setRmData] = useState(false)
  return (
    <ConfirmDialog title={local ? 'Eliminar servicio' : 'Quitar servidor'} danger typeToConfirm={server.name}
      confirmLabel={local ? 'Eliminar definitivamente' : 'Quitar'} onClose={onClose}
      onConfirm={async () => {
        try {
          await api.deleteServer(server.id, local && rmContainer, local && rmContainer && rmData)
          toast.success(`«${server.name}» eliminado.`); await onDone()
        } catch (e) { toast.error(e); throw e }
      }}>
      {local ? <>
        <p>Vas a eliminar <b>{server.name}</b> de Andres Tools.</p>
        <Checkbox label="Eliminar también el contenedor de Docker" checked={rmContainer} onChange={e => { setRmContainer(e.target.checked); if (!e.target.checked) setRmData(false) }}/>
        <Checkbox label="Eliminar también los datos (volumen o carpeta)" disabled={!rmContainer} checked={rmData} onChange={e => setRmData(e.target.checked)}
          hint="Esta acción borra las bases de datos de forma irreversible."/>
        {rmData && <Notice tone="danger">Se perderán todas las bases de datos de este servicio. No se puede deshacer.</Notice>}
      </> : <p>Se quitará <b>{server.name}</b> de Andres Tools. El servidor remoto no se modifica.</p>}
    </ConfirmDialog>
  )
}

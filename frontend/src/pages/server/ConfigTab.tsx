import {useState} from 'react'
import {KeyRound, RefreshCw, ShieldAlert, ShieldCheck} from 'lucide-react'
import {api, errMsg, type Server} from '../../api'
import {useApp} from '../../store'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {Badge, Button, Card, Checkbox, ErrorBanner, Field, IconButton, Input, Notice, PasswordInput, Spinner} from '../../components/ui'

const superUser = (s: Server) => (s.engine === 'mssql' ? 'sa' : 'postgres')
const adminRole = (s: Server) => (s.engine === 'mssql' ? 'sysadmin' : 'SUPERUSER')

// Configuración del usuario root: el administrador total con el que Andres Tools gestiona el servidor.
export default function ConfigTab({server}: {server: Server}) {
  const {refresh} = useApp()
  const toast = useToast()
  const status = useAsync(() => api.getRootStatus(server.id), [server.id, server.adminUser])
  const [user, setUser] = useState(server.adminUser)
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  const [bootUser, setBootUser] = useState(superUser(server))
  const [bootPw, setBootPw] = useState('')
  const [dropPrev, setDropPrev] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const notAdmin = status.data ? !status.data.isAdmin : false
  const changingUser = user.trim() !== '' && user.trim().toLowerCase() !== server.adminUser.toLowerCase()
  const needsPw = changingUser || notAdmin
  const pwMismatch = pw !== pw2
  const ok = user.trim() !== '' && !pwMismatch && (!needsPw || pw !== '') && (!notAdmin || (bootUser.trim() !== '' && bootPw !== '')) &&
    (changingUser || pw !== '' || notAdmin)

  const save = async () => {
    setBusy(true); setError('')
    try {
      const r = await api.configureRoot(server.id, {
        user: user.trim(), password: pw, dropPrevious: changingUser && dropPrev,
        bootstrapUser: notAdmin ? bootUser.trim() : '', bootstrapPassword: notAdmin ? bootPw : '',
      })
      toast.success(`Usuario root «${r.server.adminUser}» configurado.`)
      if (r.warning) toast.error(r.warning)
      setPw(''); setPw2(''); setBootPw(''); setDropPrev(false)
      await refresh(); await status.reload()
    } catch (e) { setError(errMsg(e)) } finally { setBusy(false) }
  }

  return (
    <div className="space-y-4">
      <Card title="Usuario root" actions={
        <IconButton label="Actualizar" onClick={() => void status.reload()}><RefreshCw size={15} className={status.loading ? 'spin' : ''}/></IconButton>}>
        <p className="mb-3 text-sm text-muted">
          Es el administrador total ({adminRole(server)}) con el que Andres Tools se conecta a este servidor. Tiene acceso a todas las
          bases de datos; los demás usuarios solo acceden a las bases que les asignes.
        </p>
        <ErrorBanner message={status.error} onRetry={() => void status.reload()}/>
        {status.loading && !status.data && <Spinner/>}
        {status.data && (
          <dl className="grid grid-cols-[11rem_1fr] gap-y-2 text-sm">
            <dt className="text-muted">Usuario actual</dt><dd className="font-mono text-xs leading-5">{status.data.user}</dd>
            <dt className="text-muted">Permisos</dt>
            <dd>{!status.data.reachable ? <Badge tone="neutral">Sin conexión</Badge>
              : status.data.isAdmin ? <Badge tone="success"><ShieldCheck size={12} className="mr-1 inline"/>Administrador ({adminRole(server)})</Badge>
                : <Badge tone="danger"><ShieldAlert size={12} className="mr-1 inline"/>Sin permisos de administrador</Badge>}</dd>
            <dt className="text-muted">Detalle</dt><dd className="break-words">{status.data.message}</dd>
          </dl>
        )}
      </Card>

      <Card title={notAdmin ? 'Crear el usuario root' : 'Cambiar usuario o contraseña root'}>
        <form className="space-y-4" onSubmit={e => { e.preventDefault(); if (ok && !busy) void save() }}>
          {notAdmin && <Notice tone="warn">
            El usuario «{server.adminUser}» no puede crear usuarios ni cambiar permisos. Indica una vez las credenciales de un
            administrador (por ejemplo {superUser(server)}) para crear el usuario root; Andres Tools usará el root desde entonces.
          </Notice>}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Usuario root" hint="Letras, números, _ y -.">
              {id => <Input id={id} value={user} autoComplete="off" onChange={e => setUser(e.target.value)}/>}
            </Field>
            <div/>
            <Field label={needsPw ? 'Contraseña' : 'Nueva contraseña'} hint={needsPw ? '' : 'Vacía = conservar la actual.'}>
              {id => <PasswordInput id={id} value={pw} onChange={setPw}/>}
            </Field>
            <Field label="Confirmar contraseña" error={pw2 && pwMismatch ? 'Las contraseñas no coinciden.' : ''}>
              {id => <PasswordInput id={id} value={pw2} onChange={setPw2}/>}
            </Field>
          </div>
          {notAdmin && <div className="grid gap-4 rounded-md border border-line bg-surface2 p-3 sm:grid-cols-2">
            <Field label="Administrador existente">{id => <Input id={id} value={bootUser} autoComplete="off" onChange={e => setBootUser(e.target.value)}/>}</Field>
            <Field label="Su contraseña" hint="Solo se usa ahora; no se guarda.">{id => <PasswordInput id={id} autoComplete="off" value={bootPw} onChange={setBootPw}/>}</Field>
          </div>}
          {changingUser && !notAdmin && <Checkbox label={`Eliminar el usuario anterior «${server.adminUser}» del servidor`} checked={dropPrev}
            onChange={e => setDropPrev(e.target.checked)} hint="No lo marques si alguna aplicación usa ese usuario."/>}
          <ErrorBanner message={error}/>
          <div className="flex justify-end">
            <Button type="submit" variant="primary" icon={<KeyRound size={15}/>} disabled={!ok} loading={busy}>
              {notAdmin ? 'Crear usuario root' : 'Guardar'}
            </Button>
          </div>
        </form>
      </Card>
    </div>
  )
}

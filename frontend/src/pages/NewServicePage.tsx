import {useState} from 'react'
import {ArrowLeft, ArrowRight, Check, Database, FolderOpen, Rocket} from 'lucide-react'
import {api, errMsg, type Engine} from '../api'
import {useApp} from '../store'
import {useProgress} from '../hooks/useProgress'
import {useAsync} from '../hooks/useAsync'
import {useToast} from '../components/Toast'
import {Button, Card, Checkbox, cx, engineLabel, ErrorBanner, Field, Input, Notice, PageHeader, PasswordInput, ProgressBar, Select} from '../components/ui'

const PRESETS: Record<Engine, Array<{image: string; label: string}>> = {
  mssql: [
    {image: 'mcr.microsoft.com/mssql/server:2022-latest', label: 'SQL Server 2022 (2022-latest)'},
    {image: 'mcr.microsoft.com/mssql/server:2019-latest', label: 'SQL Server 2019 (2019-latest)'},
  ],
  postgres: [
    {image: 'postgres:17', label: 'PostgreSQL 17'},
    {image: 'postgres:16', label: 'PostgreSQL 16'},
    {image: 'postgres:18', label: 'PostgreSQL 18'},
  ],
}
const DEFAULT_PORT: Record<Engine, number> = {mssql: 1433, postgres: 5432}

export function passwordStrength(engine: Engine, pw: string) {
  const cats = [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/].filter(r => r.test(pw)).length
  if (engine === 'mssql') {
    const ok = pw.length >= 8 && cats >= 3
    return {ok, hint: ok ? 'Contraseña válida para SQL Server.' : 'SQL Server exige al menos 8 caracteres y 3 de 4 categorías: mayúsculas, minúsculas, números y símbolos.', score: Math.min(cats, 4)}
  }
  const ok = pw.length >= 1
  return {ok, hint: pw.length < 8 ? 'Se recomienda usar al menos 8 caracteres.' : 'Contraseña aceptable.', score: Math.min(cats, 4)}
}

export function StrengthMeter({engine, pw}: {engine: Engine; pw: string}) {
  const s = passwordStrength(engine, pw)
  if (!pw) return null
  return (
    <div className="mt-1.5">
      <div className="flex gap-1" aria-hidden>
        {[1, 2, 3, 4].map(i => <span key={i} className={cx('h-1 flex-1 rounded', i <= s.score ? (s.ok ? 'bg-success' : 'bg-warn') : 'bg-surface2')}/>)}
      </div>
      <p className={cx('mt-1 text-xs', s.ok ? 'text-success' : 'text-warn')}>{s.hint}</p>
    </div>
  )
}

const STEPS = ['Motor', 'Servicio', 'Seguridad y datos', 'Resumen']

export default function NewServicePage() {
  const {go, refresh, env} = useApp()
  const toast = useToast()
  const {event, start} = useProgress('service:progress')
  const [step, setStep] = useState(0)
  const [engine, setEngine] = useState<Engine>('mssql')
  const [name, setName] = useState('')
  const [image, setImage] = useState(PRESETS.mssql[0].image)
  const [custom, setCustom] = useState(false)
  const [port, setPort] = useState('')
  const [memory, setMemory] = useState('')
  const [password, setPassword] = useState('')
  const [dataMode, setDataMode] = useState<'volume' | 'bind'>('volume')
  const [dataPath, setDataPath] = useState('')
  const [expose, setExpose] = useState(false)
  const [edition, setEdition] = useState('Developer')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const rootDefaults = useAsync(() => api.getRootDefaults(), [])
  const [rootUser, setRootUser] = useState('')
  const [rootPassword, setRootPassword] = useState('')
  const effectiveRoot = rootUser.trim() || rootDefaults.data?.user || 'root'
  const reserved = engine === 'mssql' ? /^(sa|##.*|nt .*)$/i : /^(postgres|pg_.*)$/i
  const rootOk = /^[A-Za-z_][A-Za-z0-9_-]{0,62}$/.test(effectiveRoot) && !reserved.test(effectiveRoot)

  const portNum = port.trim() === '' ? 0 : Number(port)
  const portOk = port.trim() === '' || (Number.isInteger(portNum) && portNum >= 1 && portNum <= 65535)
  const memNum = memory.trim() === '' ? 0 : Number(memory)
  const memOk = memory.trim() === '' || (Number.isInteger(memNum) && memNum >= 512)
  const nameOk = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$/.test(name)
  const pw = passwordStrength(engine, password)

  const valid = [true, nameOk && image.trim() !== '' && portOk && memOk, pw.ok && rootOk && (dataMode === 'volume' || dataPath !== ''), true]

  const chooseEngine = (e: Engine) => { setEngine(e); setImage(PRESETS[e][0].image); setCustom(false) }
  const pick = async () => {
    try { const d = await api.pickDirectory('Carpeta para los datos del servidor'); if (d) setDataPath(d) } catch (e) { toast.error(e) }
  }

  const create = async () => {
    setBusy(true); setError(''); const requestId = start()
    try {
      const s = await api.createLocalService({
        name: name.trim(), engine, image: image.trim(), port: portNum, adminPassword: password,
        dataMode, dataPath: dataMode === 'bind' ? dataPath : dataPath.trim(), memoryMB: memNum, requestId, exposeOnNetwork: expose, edition: engine === 'mssql' ? edition : '',
        rootUser: rootUser.trim(), rootPassword,
      })
      toast.success(`Servicio «${s.name}» creado.`)
      await refresh()
      go({page: 'server', id: s.id})
    } catch (e) {
      setError(errMsg(e)); toast.error(e); setBusy(false)
    }
  }

  return (
    <div className="mx-auto max-w-2xl">
      <PageHeader title="Nuevo servicio" subtitle="Crea un servidor de base de datos en un contenedor de Docker."/>
      {env && !env.dockerAvailable && <div className="mb-4"><Notice tone="danger">Docker no está disponible; la creación fallará hasta que lo inicies (ver Inicio).</Notice></div>}
      <ol className="mb-4 flex gap-2" aria-label="Pasos">
        {STEPS.map((s, i) => (
          <li key={s} aria-current={i === step ? 'step' : undefined}
            className={cx('flex flex-1 items-center gap-2 border-b-2 pb-2 text-xs font-medium', i === step ? 'border-accent text-accent' : i < step ? 'border-success text-fg' : 'border-line text-muted')}>
            <span className={cx('flex h-5 w-5 items-center justify-center rounded-full text-[11px]', i < step ? 'bg-success text-white' : 'bg-surface2')}>{i < step ? <Check size={12}/> : i + 1}</span>{s}
          </li>
        ))}
      </ol>

      <Card>
        <form onSubmit={e => { e.preventDefault(); if (step < 3 && valid[step]) setStep(step + 1) }} className="space-y-4">
          {step === 0 && (
            <div className="grid gap-3 sm:grid-cols-2" role="radiogroup" aria-label="Motor">
              {(['mssql', 'postgres'] as Engine[]).map(e => (
                <button type="button" key={e} role="radio" aria-checked={engine === e} onClick={() => chooseEngine(e)}
                  className={cx('rounded-lg border p-4 text-left', engine === e ? 'border-accent bg-accent/5 ring-1 ring-accent' : 'border-line hover:bg-surface2')}>
                  <Database className="mb-2 text-accent"/>
                  <p className="font-semibold">{engineLabel(e)}</p>
                  <p className="text-xs text-muted">{e === 'mssql' ? 'Microsoft SQL Server en Linux. Puerto 1433.' : 'PostgreSQL oficial. Puerto 5432.'}</p>
                </button>
              ))}
            </div>
          )}

          {step === 1 && <>
            <Field label="Nombre del servicio" hint="Letras, números, punto, guion y guion bajo. Se usará como nombre del contenedor."
              error={name && !nameOk ? 'Nombre no válido.' : ''}>
              {id => <Input id={id} autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="mi-servidor"/>}
            </Field>
            <Field label="Imagen / versión">
              {id => <>
                <Select id={id} value={custom ? '__custom' : image} onChange={e => {
                  if (e.target.value === '__custom') { setCustom(true) } else { setCustom(false); setImage(e.target.value) }
                }}>
                  {PRESETS[engine].map(p => <option key={p.image} value={p.image}>{p.label}</option>)}
                  <option value="__custom">Otra imagen...</option>
                </Select>
                {custom && <Input className="mt-2" aria-label="Imagen personalizada" value={image} onChange={e => setImage(e.target.value)} placeholder="repositorio/imagen:etiqueta"/>}
              </>}
            </Field>
            {engine === 'mssql' && <Field label="Edición" hint="Developer es gratuita para desarrollo; Express tiene límites de tamaño. Standard y Enterprise requieren licencia.">
              {id => <Select id={id} value={edition} onChange={e => setEdition(e.target.value)}><option value="Developer">Developer</option><option value="Express">Express</option></Select>}
            </Field>}
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Puerto en tu equipo (opcional)" hint={`Vacío = automático (el predeterminado es ${DEFAULT_PORT[engine]} si está libre).`} error={!portOk ? 'Puerto entre 1 y 65535.' : ''}>
                {id => <Input id={id} inputMode="numeric" value={port} onChange={e => setPort(e.target.value.replace(/\D/g, ''))} placeholder="Automático"/>}
              </Field>
              <Field label="Memoria máxima en MB (opcional)" hint="Mínimo 512. Vacío = sin límite." error={!memOk ? 'Mínimo 512 MB.' : ''}>
                {id => <Input id={id} inputMode="numeric" value={memory} onChange={e => setMemory(e.target.value.replace(/\D/g, ''))} placeholder="Sin límite"/>}
              </Field>
            </div>
            <Checkbox label="Permitir conexiones desde otros equipos de la red" checked={expose} onChange={e => setExpose(e.target.checked)}
              hint="Por defecto el servicio solo escucha en 127.0.0.1 (este equipo)."/>
            {expose && <Notice tone="warn">Cualquier equipo de la red local podrá alcanzar la base de datos. Usa una contraseña robusta.</Notice>}
          </>}

          {step === 2 && <>
            <Field label={`Contraseña del superusuario del motor (${engine === 'mssql' ? 'sa' : 'postgres'})`}
              hint="La pide el motor al crear el contenedor. Andres Tools no la usa después: se conecta con el usuario root.">
              {id => <><PasswordInput id={id} autoFocus value={password} onChange={setPassword}/><StrengthMeter engine={engine} pw={password}/></>}
            </Field>
            <div className="grid gap-4 rounded-md border border-line p-3 sm:grid-cols-2">
              <p className="text-xs text-muted sm:col-span-2">
                <b className="text-fg">Usuario root:</b> administrador total que Andres Tools crea y usa para gestionar el servicio
                (acceso a todas las bases). Puedes cambiarlo después en la pestaña Configuración del servidor.
              </p>
              <Field label="Usuario root" error={!rootOk ? 'Nombre no válido o reservado.' : ''}>
                {id => <Input id={id} value={rootUser} autoComplete="off" onChange={e => setRootUser(e.target.value)} placeholder={rootDefaults.data?.user ?? 'root'}/>}
              </Field>
              <Field label="Contraseña root" hint={rootDefaults.data?.passwordIsDefault ? 'Vacía = «root» (predeterminada en Ajustes).' : 'Vacía = la predeterminada de Ajustes.'}>
                {id => <PasswordInput id={id} value={rootPassword} onChange={setRootPassword}/>}
              </Field>
            </div>
            <fieldset>
              <legend className="mb-1 text-xs font-medium text-muted">Almacenamiento de datos</legend>
              <div className="grid gap-2 sm:grid-cols-2">
                {([['volume', 'Volumen de Docker', 'Gestionado por Docker. Recomendado.'], ['bind', 'Carpeta de mi equipo', 'Los datos quedan en una carpeta que eliges.']] as const).map(([k, t, d]) => (
                  <label key={k} className={cx('flex cursor-pointer items-start gap-2 rounded-md border p-3', dataMode === k ? 'border-accent bg-accent/5' : 'border-line')}>
                    <input type="radio" name="dataMode" checked={dataMode === k} onChange={() => { setDataMode(k); setDataPath('') }} className="mt-1 accent-[rgb(var(--accent))]"/>
                    <span><span className="block font-medium">{t}</span><span className="text-xs text-muted">{d}</span></span>
                  </label>
                ))}
              </div>
              {dataMode === 'bind' ? (
                <div className="mt-3 flex gap-2">
                  <Input aria-label="Carpeta de datos" readOnly value={dataPath} placeholder="Ninguna carpeta seleccionada"/>
                  <Button icon={<FolderOpen size={15}/>} onClick={pick}>Elegir</Button>
                </div>
              ) : (
                <div className="mt-3"><Field label="Nombre del volumen (opcional)" hint="Vacío = se genera automáticamente.">
                  {id => <Input id={id} value={dataPath} onChange={e => setDataPath(e.target.value)} placeholder="dbmanager-datos"/>}
                </Field></div>
              )}
              {dataMode === 'bind' && engine === 'mssql' && <p className="mt-2 text-xs text-muted">En Linux, la carpeta debe permitir escritura al usuario del contenedor (uid 10001).</p>}
            </fieldset>
          </>}

          {step === 3 && <>
            <dl className="grid grid-cols-[10rem_1fr] gap-y-2 text-sm">
              <dt className="text-muted">Motor</dt><dd>{engineLabel(engine)}</dd>
              <dt className="text-muted">Nombre</dt><dd>{name}</dd>
              <dt className="text-muted">Imagen</dt><dd className="break-all font-mono text-xs leading-5">{image}</dd>
              {engine === 'mssql' && <><dt className="text-muted">Edición</dt><dd>{edition}</dd></>}
              <dt className="text-muted">Puerto</dt><dd>{portNum || 'Automático'}</dd>
              <dt className="text-muted">Acceso</dt><dd>{expose ? 'Toda la red local' : 'Solo este equipo (127.0.0.1)'}</dd>
              <dt className="text-muted">Memoria</dt><dd>{memNum ? `${memNum} MB` : 'Sin límite'}</dd>
              <dt className="text-muted">Contraseña {engine === 'mssql' ? 'sa' : 'postgres'}</dt><dd>{'•'.repeat(Math.min(password.length, 16))}</dd>
              <dt className="text-muted">Usuario root</dt><dd className="font-mono text-xs leading-5">{effectiveRoot}</dd>
              <dt className="text-muted">Datos</dt><dd>{dataMode === 'volume' ? `Volumen de Docker${dataPath ? ` (${dataPath})` : ''}` : `Carpeta: ${dataPath}`}</dd>
            </dl>
            <p className="text-xs text-muted">La primera vez se descargará la imagen, lo que puede tardar varios minutos.</p>
            {busy && <ProgressBar percent={event?.percent ?? 0} message={event?.message ?? 'Iniciando...'} error={event?.error}/>}
            <ErrorBanner message={error}/>
          </>}

          <div className="flex justify-between border-t border-line pt-4">
            <Button icon={<ArrowLeft size={15}/>} disabled={step === 0 || busy} onClick={() => setStep(step - 1)}>Atrás</Button>
            {step < 3
              ? <Button type="submit" variant="primary" disabled={!valid[step]}>Siguiente <ArrowRight size={15}/></Button>
              : <Button variant="primary" icon={<Rocket size={15}/>} loading={busy} onClick={create}>Crear servicio</Button>}
          </div>
        </form>
      </Card>
    </div>
  )
}

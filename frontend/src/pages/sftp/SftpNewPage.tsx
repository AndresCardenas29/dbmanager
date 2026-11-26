import {useState} from 'react'
import {FolderOpen, Rocket} from 'lucide-react'
import {api, errMsg} from '../../api'
import {api2} from '../../api/v2'
import {useApp} from '../../store'
import {useProgress} from '../../hooks/useProgress'
import {useToast} from '../../components/Toast'
import {Button, Card, Checkbox, cx, ErrorBanner, Field, Input, Notice, PageHeader, ProgressBar} from '../../components/ui'

export default function SftpNewPage() {
  const {go, refresh} = useApp()
  const toast = useToast()
  const {event, start} = useProgress('sftp:progress')
  const [name, setName] = useState('')
  const [image, setImage] = useState('atmoz/sftp:alpine')
  const [port, setPort] = useState('')
  const [dataMode, setDataMode] = useState<'volume' | 'bind'>('volume')
  const [dataPath, setDataPath] = useState('')
  const [expose, setExpose] = useState(false)
  const [review, setReview] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const portNum = port.trim() === '' ? 0 : Number(port)
  const portOk = port.trim() === '' || (Number.isInteger(portNum) && portNum >= 1 && portNum <= 65535)
  const nameOk = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$/.test(name)
  const valid = nameOk && image.trim() !== '' && portOk && (dataMode === 'volume' || !!dataPath)

  const pick = async () => {
    try { const d = await api.pickDirectory('Carpeta para los datos del servicio SFTP'); if (d) setDataPath(d) } catch (e) { toast.error(e) }
  }
  const create = async () => {
    setBusy(true); setError('')
    const requestId = start()
    try {
      const s = await api2.createSftpService({requestId, name: name.trim(), image: image.trim(), port: portNum, dataMode, dataPath: dataPath.trim(), exposeOnNetwork: expose})
      toast.success(`Servicio SFTP «${s.name}» creado.`)
      await refresh(); go({page: 'sftp', id: s.id})
    } catch (e) { setError(errMsg(e)); toast.error(e); setBusy(false) }
  }

  return (
    <div className="mx-auto max-w-2xl">
      <PageHeader title="Nuevo servicio SFTP" subtitle="Crea un servidor SFTP en un contenedor de Docker."/>
      <Card>
        {!review ? (
          <form className="space-y-4" onSubmit={e => { e.preventDefault(); if (valid) setReview(true) }}>
            <Field label="Nombre del servicio" hint="Letras, números, punto, guion y guion bajo." error={name && !nameOk ? 'Nombre no válido.' : ''}>
              {id => <Input id={id} autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="sftp-intercambio"/>}
            </Field>
            <Field label="Imagen">{id => <Input id={id} value={image} onChange={e => setImage(e.target.value)}/>}</Field>
            <Field label="Puerto en tu equipo (opcional)" hint="Vacío = automático (desde 2224 en adelante, el primero libre)." error={!portOk ? 'Puerto entre 1 y 65535.' : ''}>
              {id => <Input id={id} inputMode="numeric" value={port} onChange={e => setPort(e.target.value.replace(/\D/g, ''))} placeholder="Automático"/>}
            </Field>
            <fieldset>
              <legend className="mb-1 text-xs font-medium text-muted">Almacenamiento de datos</legend>
              <div className="grid gap-2 sm:grid-cols-2">
                {([['volume', 'Volumen de Docker', 'Gestionado por Docker.'], ['bind', 'Carpeta de mi equipo', 'Los archivos quedan en una carpeta que eliges.']] as const).map(([k, t, d]) => (
                  <label key={k} className={cx('flex cursor-pointer items-start gap-2 rounded-md border p-3', dataMode === k ? 'border-accent bg-accent/5' : 'border-line')}>
                    <input type="radio" name="sftpData" checked={dataMode === k} onChange={() => { setDataMode(k); setDataPath('') }} className="mt-1 accent-[rgb(var(--accent))]"/>
                    <span><span className="block font-medium">{t}</span><span className="text-xs text-muted">{d}</span></span>
                  </label>
                ))}
              </div>
              {dataMode === 'bind'
                ? <div className="mt-3 flex gap-2"><Input aria-label="Carpeta de datos" readOnly value={dataPath} placeholder="Ninguna carpeta seleccionada"/>
                    <Button icon={<FolderOpen size={15}/>} onClick={pick}>Elegir</Button></div>
                : <div className="mt-3"><Field label="Nombre del volumen (opcional)" hint="Vacío = se genera automáticamente.">
                    {id => <Input id={id} value={dataPath} onChange={e => setDataPath(e.target.value)}/>}</Field></div>}
            </fieldset>
            <Checkbox label="Permitir conexiones desde otros equipos de la red" checked={expose} onChange={e => setExpose(e.target.checked)}
              hint="Por defecto el servicio solo escucha en 127.0.0.1 (este equipo)."/>
            {expose && <Notice tone="warn">Cualquier equipo de la red local podrá alcanzar el servidor SFTP. Usa contraseñas robustas o llaves.</Notice>}
            <div className="flex justify-end border-t border-line pt-4"><Button type="submit" variant="primary" disabled={!valid}>Revisar</Button></div>
          </form>
        ) : (
          <div className="space-y-4">
            <dl className="grid grid-cols-[10rem_1fr] gap-y-2 text-sm">
              <dt className="text-muted">Nombre</dt><dd>{name}</dd>
              <dt className="text-muted">Imagen</dt><dd className="break-all font-mono text-xs leading-5">{image}</dd>
              <dt className="text-muted">Puerto</dt><dd>{portNum || 'Automático (desde 2224)'}</dd>
              <dt className="text-muted">Datos</dt><dd>{dataMode === 'volume' ? `Volumen de Docker${dataPath ? ` (${dataPath})` : ''}` : `Carpeta: ${dataPath}`}</dd>
              <dt className="text-muted">Acceso</dt><dd>{expose ? 'Toda la red local' : 'Solo este equipo (127.0.0.1)'}</dd>
            </dl>
            <p className="text-xs text-muted">Podrás añadir usuarios una vez creado el servicio. La primera vez se descargará la imagen.</p>
            {busy && <ProgressBar percent={event?.percent ?? 0} message={event?.message ?? 'Iniciando...'} error={event?.error}/>}
            <ErrorBanner message={error}/>
            <div className="flex justify-between border-t border-line pt-4">
              <Button disabled={busy} onClick={() => setReview(false)}>Atrás</Button>
              <Button variant="primary" icon={<Rocket size={15}/>} loading={busy} onClick={create}>Crear servicio</Button>
            </div>
          </div>
        )}
      </Card>
    </div>
  )
}

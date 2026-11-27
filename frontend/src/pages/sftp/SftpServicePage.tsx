import {useEffect, useState, type ReactNode} from 'react'
import {FileText, FolderOpen, FolderTree, Info, LogIn, Play, RefreshCw, RotateCw, Square, Trash2, Users} from 'lucide-react'
import {useApp, type SftpTab} from '../../store'
import {api2, type SftpService} from '../../api/v2'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {Badge, Button, Card, Checkbox, ConfirmDialog, CopyButton, EmptyState, ErrorBanner, fmtDate, Notice, PageHeader, Select, Tabs} from '../../components/ui'
import {StatusDot} from '../../components/Sidebar'
import SftpUsersTab from './SftpUsersTab'
import SftpFoldersTab from './SftpFoldersTab'
import SftpTestTab from './SftpTestTab'

export default function SftpServicePage({id, tab}: {id: number; tab?: SftpTab}) {
  const {sftpServices, sftpStatuses} = useApp()
  const [current, setCurrent] = useState<SftpTab | 'folders'>(tab ?? 'summary')
  useEffect(() => { if (tab) setCurrent(tab) }, [tab])
  const svc = sftpServices.find(s => s.id === id)
  if (!svc) return <Card><EmptyState title="Servicio no encontrado">Puede haber sido eliminado. Elige otro en la barra lateral.</EmptyState></Card>
  const managed = svc.mode === 'managed'
  const state = sftpStatuses[id]?.state
  const tabs: Array<{key: SftpTab | 'folders'; label: string; icon: ReactNode}> = [
    {key: 'summary', label: 'Resumen', icon: <Info size={15}/>},
    {key: 'users' as SftpTab, label: 'Usuarios', icon: <Users size={15}/>},
    {key: 'folders', label: 'Carpetas', icon: <FolderTree size={15}/>},
    {key: 'test', label: 'Probar acceso', icon: <LogIn size={15}/>},
    {key: 'logs', label: 'Logs', icon: <FileText size={15}/>},
  ]
  const active = tabs.some(t => t.key === current) ? current : 'summary'
  return (
    <div>
      <PageHeader title={<span className="flex items-center gap-2"><StatusDot state={state}/>{svc.name}</span>}
        subtitle={<span className="flex items-center gap-2"><Badge tone="accent">SFTP</Badge>{managed ? 'Gestionado' : <Badge>externo</Badge>} · {svc.host || 'localhost'}:{svc.port}</span>}/>
      <Tabs tabs={tabs} value={active} onChange={setCurrent}/>
      <div className="pt-5">
        {active === 'summary' && <Summary svc={svc}/>}
        {active === 'users' && (managed ? <SftpUsersTab svc={svc}/>
          : <Card><EmptyState title="Usuarios no gestionados">Este servicio es externo (importado): su configuración de usuarios y llaves no la gestiona Andres Tools. Solo puedes iniciarlo, detenerlo, ver sus logs y probar el acceso.</EmptyState></Card>)}
        {active === 'folders' && (managed ? <SftpFoldersTab svc={svc}/>
          : <Card><EmptyState title="Carpetas no gestionadas">Este servicio es externo (importado): las carpetas compartidas no las gestiona Andres Tools.</EmptyState></Card>)}
        {active === 'test' && <SftpTestTab svc={svc}/>}
        {active === 'logs' && <Logs svc={svc}/>}
      </div>
    </div>
  )
}

const stateLabel: Record<string, string> = {running: 'En ejecución', stopped: 'Detenido', missing: 'Contenedor no encontrado'}

function Summary({svc}: {svc: SftpService}) {
  const {refresh, go} = useApp()
  const toast = useToast()
  const status = useAsync(() => api2.sftpStatus(svc.id), [svc.id])
  const fps = useAsync(() => api2.sftpFingerprints(svc.id), [svc.id])
  const keys = useAsync(() => api2.sftpHostKeys(svc.id), [svc.id])
  const [busy, setBusy] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [rmContainer, setRmContainer] = useState(true)
  const [rmData, setRmData] = useState(false)
  const state = status.data?.state
  const host = svc.host || 'localhost'

  const act = async (k: 'start' | 'stop' | 'restart') => {
    setBusy(k)
    try {
      await (k === 'start' ? api2.startSftp(svc.id) : k === 'stop' ? api2.stopSftp(svc.id) : api2.restartSftp(svc.id))
      toast.success(k === 'start' ? 'Servicio iniciado.' : k === 'stop' ? 'Servicio detenido.' : 'Servicio reiniciado.')
      await status.reload(); await refresh()
    } catch (e) { toast.error(e) } finally { setBusy('') }
  }
  const openFiles = async () => {
    try { const p = await api2.openSftpFolder(svc.id, 'service', ''); toast.success(`Carpeta abierta: ${p}`) } catch (e) { toast.error(e) }
  }
  const knownHosts = keys.data ?? []
  const sftpCmd = `sftp -P ${svc.port} -i RUTA_A_LA_LLAVE USUARIO@${host}`

  return (
    <div className="space-y-4">
      <Card title="Estado" actions={<Button size="sm" icon={<RefreshCw size={14}/>} loading={status.loading && !!status.data} onClick={() => void status.reload()}>Actualizar</Button>}>
        <ErrorBanner message={status.error} onRetry={() => void status.reload()}/>
        {status.data && <p className="text-sm"><Badge tone={state === 'running' ? 'success' : state === 'missing' ? 'danger' : 'neutral'}>{stateLabel[state ?? ''] ?? state}</Badge>
          {status.data.message && <span className="ml-2 text-muted">{status.data.message}</span>}</p>}
        <div className="mt-4 flex flex-wrap gap-2 border-t border-line pt-4">
          <Button icon={<Play size={15}/>} loading={busy === 'start'} disabled={!!busy || state === 'running' || state === 'missing'} onClick={() => act('start')}>Iniciar</Button>
          <Button icon={<Square size={15}/>} loading={busy === 'stop'} disabled={!!busy || state !== 'running'} onClick={() => act('stop')}>Detener</Button>
          <Button icon={<RotateCw size={15}/>} loading={busy === 'restart'} disabled={!!busy || state === 'missing'} onClick={() => act('restart')}>Reiniciar</Button>
        </div>
        {svc.mode === 'external' && <div className="mt-3"><Notice tone="accent">Servicio externo: solo se puede iniciar, detener e inspeccionar. Sus usuarios no los gestiona la app.</Notice></div>}
      </Card>

      <Card title="Conexión">
        <dl className="grid grid-cols-[11rem_1fr] gap-y-2 text-sm">
          <dt className="text-muted">Host</dt><dd className="font-mono text-xs leading-5">{host}</dd>
          <dt className="text-muted">Puerto</dt><dd className="font-mono text-xs leading-5">{svc.port}</dd>
          <dt className="text-muted">Contenedor</dt><dd className="font-mono text-xs leading-5">{svc.containerName}</dd>
          <dt className="text-muted">Imagen</dt><dd className="break-all font-mono text-xs leading-5">{svc.image}</dd>
          <dt className="text-muted">Datos</dt><dd className="break-all">{svc.dataMode === 'bind' ? `Carpeta: ${svc.dataPath}` : svc.dataMode === 'volume' ? `Volumen: ${svc.dataPath || '(automático)'}` : '—'}</dd>
          <dt className="text-muted">Red</dt><dd>{svc.exposeOnNetwork ? 'Accesible desde la red local' : 'Solo este equipo'}</dd>
          <dt className="text-muted">Creado</dt><dd>{fmtDate(svc.createdAt)}</dd>
        </dl>

        {svc.mode === 'managed' && <div className="mt-3"><Button icon={<FolderOpen size={15}/>} onClick={openFiles}>Abrir carpeta de archivos</Button></div>}

        <h4 className="mb-1 mt-5 text-sm font-semibold">Huellas del servidor</h4>
        <ErrorBanner message={fps.error} onRetry={() => void fps.reload()}/>
        {(fps.data ?? []).map(f => (
          <div key={f} className="mb-1 flex items-center gap-2 rounded-md border border-line bg-surface2 px-3 py-1.5">
            <code className="min-w-0 flex-1 break-all font-mono text-xs">{f}</code><CopyButton text={f} label="Copiar huella"/>
          </div>
        ))}
        <ErrorBanner message={keys.error} onRetry={() => void keys.reload()}/>
        {knownHosts.length > 0 && <>
          <h4 className="mb-1 mt-3 text-sm font-semibold">Línea para known_hosts</h4>
          {knownHosts.map(k => (
            <div key={k} className="mb-1 flex items-center gap-2 rounded-md border border-line bg-surface2 px-3 py-1.5">
              <code className="min-w-0 flex-1 break-all font-mono text-xs">{k}</code><CopyButton text={k} label="Copiar línea"/>
            </div>
          ))}
          <div className="flex items-center gap-2"><CopyButton text={knownHosts.join('\n')} label="Copiar todas las líneas"/><span className="text-xs text-muted">Copiar todo</span></div>
        </>}

        <h4 className="mb-1 mt-5 text-sm font-semibold">Ejemplos</h4>
        <div className="flex items-center gap-2 rounded-md border border-line bg-surface2 px-3 py-1.5">
          <code className="min-w-0 flex-1 break-all font-mono text-xs">{sftpCmd}</code><CopyButton text={sftpCmd} label="Copiar comando"/>
        </div>
        <p className="mt-2 text-xs text-muted">WinSCP: Protocolo SFTP · Servidor <b>{host}</b> · Puerto <b>{svc.port}</b> · Usuario · Contraseña, o en Avanzado → SSH → Autenticación, el archivo .ppk de la llave.</p>
      </Card>

      <Card title="Administrar">
        <Button variant="danger" icon={<Trash2 size={15}/>} onClick={() => setDeleting(true)}>{svc.mode === 'managed' ? 'Eliminar servicio' : 'Quitar servicio'}</Button>
      </Card>

      {deleting && (
        <ConfirmDialog title="Eliminar servicio SFTP" danger typeToConfirm={svc.name} confirmLabel="Eliminar" onClose={() => setDeleting(false)}
          onConfirm={async () => {
            try { await api2.deleteSftp(svc.id, rmContainer, rmContainer && rmData); toast.success(`«${svc.name}» eliminado.`); await refresh(); go({page: 'home'}) }
            catch (e) { toast.error(e); throw e }
          }}>
          <Checkbox label="Eliminar también el contenedor de Docker" checked={rmContainer} onChange={e => { setRmContainer(e.target.checked); if (!e.target.checked) setRmData(false) }}/>
          <Checkbox label="Eliminar también los archivos (volumen o carpeta)" disabled={!rmContainer} checked={rmData} onChange={e => setRmData(e.target.checked)} hint="Borra los archivos subidos de forma irreversible."/>
          {rmData && <Notice tone="danger">Se perderán todos los archivos de este servicio. No se puede deshacer.</Notice>}
        </ConfirmDialog>
      )}
    </div>
  )
}

function Logs({svc}: {svc: SftpService}) {
  const [tail, setTail] = useState(200)
  const logs = useAsync(() => api2.sftpLogs(svc.id, tail), [svc.id, tail])
  return (
    <Card title="Logs del contenedor" actions={<>
      <Select aria-label="Líneas" className="!h-8 !w-auto text-xs" value={tail} onChange={e => setTail(Number(e.target.value))}>
        {[100, 200, 500, 1000].map(n => <option key={n} value={n}>Últimas {n} líneas</option>)}</Select>
      <Button size="sm" icon={<RefreshCw size={14}/>} onClick={() => void logs.reload()}>Actualizar</Button>
    </>}>
      <ErrorBanner message={logs.error} onRetry={() => void logs.reload()}/>
      {logs.data !== undefined && (logs.data.trim() === ''
        ? <EmptyState title="Sin logs"/>
        : <pre className="h-[28rem] overflow-auto whitespace-pre-wrap break-all rounded-md border border-line bg-surface2 p-3 font-mono text-xs leading-5">{logs.data}</pre>)}
    </Card>
  )
}

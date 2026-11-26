import {useEffect, useState} from 'react'
import {FileSearch, RefreshCw, RotateCcw, Upload} from 'lucide-react'
import {api, errMsg, type BackupInfo, type FileFilter, type Server} from '../../api'
import {useAsync} from '../../hooks/useAsync'
import {useProgress} from '../../hooks/useProgress'
import {useToast} from '../../components/Toast'
import {
  Badge, Button, Card, Checkbox, EmptyState, ErrorBanner, Field, fmtDate, IconButton, Input, Notice, ProgressBar,
  Spinner, statusLabel, statusTone, Table, Td, Th,
} from '../../components/ui'

const FILTERS: Record<string, FileFilter[]> = {
  mssql: [{displayName: 'Backups de SQL Server (*.bak)', pattern: '*.bak'}],
  postgres: [{displayName: 'Backups de PostgreSQL (*.dump;*.backup;*.sql)', pattern: '*.dump;*.backup;*.sql'}],
}
const baseName = (p: string) => p.split(/[\\/]/).pop() ?? p
const guessDb = (info: BackupInfo | null, file: string) =>
  (info?.databaseName || baseName(file).replace(/\.[^.]+$/, '')).replace(/[^A-Za-z0-9_-]/g, '_')

export default function RestoresTab({server, initialFile, onConsumed}: {server: Server; initialFile?: string; onConsumed: () => void}) {
  const toast = useToast()
  const mssql = server.engine === 'mssql'
  const remoteMssql = mssql && server.mode === 'remote'
  const [typed, setTyped] = useState('')
  const list = useAsync(() => api.listRestores(server.id), [server.id])
  const {event, start, reset} = useProgress('restore:progress')
  const [file, setFile] = useState('')
  const [info, setInfo] = useState<BackupInfo | null>(null)
  const [inspecting, setInspecting] = useState(false)
  const [inspectErr, setInspectErr] = useState('')
  const [target, setTarget] = useState('')
  const [dataDir, setDataDir] = useState('/var/opt/mssql/data')
  const [logDir, setLogDir] = useState('/var/opt/mssql/data')
  const [replace, setReplace] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const inspect = async (f: string) => {
    setFile(f); setInfo(null); setInspectErr(''); setInspecting(true); reset()
    try {
      const i = await api.inspectBackup(server.id, f)
      setInfo(i); setTarget(guessDb(i, f))
    } catch (e) { setInspectErr(errMsg(e)); setTarget(guessDb(null, f)) }
    finally { setInspecting(false) }
  }

  useEffect(() => {
    if (initialFile) { void inspect(initialFile); onConsumed() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialFile])

  const pick = async () => {
    try {
      const f = await api.pickFile('Selecciona el archivo de backup', FILTERS[server.engine])
      if (f) await inspect(f)
    } catch (e) { toast.error(e) }
  }

  const run = async () => {
    setBusy(true); setError(''); const requestId = start()
    try {
      await api.restoreBackup({serverId: server.id, filePath: file, targetDatabase: target.trim(), dataDir: mssql ? dataDir.trim() : '', logDir: mssql ? logDir.trim() : '', replace, requestId})
      toast.success(`Base de datos «${target}» restaurada.`)
    } catch (e) { setError(errMsg(e)); toast.error(e) }
    finally { setBusy(false); void list.reload() }
  }

  const ready = !!file && !!target.trim() && (!mssql || (dataDir.trim() && logDir.trim()))

  return (
    <div className="space-y-4">
      <Card title="Restaurar un backup">
        <div className="space-y-5">
          <section>
            <h4 className="mb-2 text-sm font-semibold">1. Archivo de backup</h4>
            {remoteMssql && <p className="mb-2 text-xs text-muted">La ruta es del propio servidor SQL Server remoto, no de tu equipo.</p>}
            <div className="flex gap-2">
              {remoteMssql ? <>
                <Input aria-label="Ruta del archivo .bak en el servidor" value={typed} onChange={e => setTyped(e.target.value)} placeholder="/var/opt/mssql/backup/mi.bak" className="font-mono"/>
                <Button icon={<FileSearch size={15}/>} disabled={busy || !typed.trim()} onClick={() => void inspect(typed.trim())}>Inspeccionar</Button>
              </> : <>
                <Input aria-label="Archivo de backup" readOnly value={file} placeholder={mssql ? 'Ningún archivo .bak seleccionado' : 'Ningún archivo .dump / .backup / .sql seleccionado'}/>
                <Button icon={<FileSearch size={15}/>} disabled={busy} onClick={pick}>Elegir archivo</Button>
              </>}
            </div>
          </section>

          {file && (
            <section>
              <h4 className="mb-2 text-sm font-semibold">2. Contenido del backup</h4>
              {inspecting && <Spinner label="Inspeccionando backup..."/>}
              <ErrorBanner message={inspectErr}/>
              {info && (
                <div className="space-y-2 text-sm">
                  <p><span className="text-muted">Formato:</span> {info.format || '—'} {info.databaseName && <>· <span className="text-muted">Base original:</span> {info.databaseName}</>}</p>
                  {(info.files ?? []).length > 0 && (
                    <Table>
                      <thead><tr><Th>Nombre lógico</Th><Th>Archivo físico original</Th><Th>Tipo</Th></tr></thead>
                      <tbody>{(info.files ?? []).map(f => (
                        <tr key={f.logicalName}><Td className="font-mono text-xs">{f.logicalName}</Td><Td className="break-all font-mono text-xs">{f.physicalName}</Td>
                          <Td><Badge tone={f.type === 'log' ? 'neutral' : 'accent'}>{f.type === 'log' ? 'registro' : 'datos'}</Badge></Td></tr>
                      ))}</tbody>
                    </Table>
                  )}
                </div>
              )}
            </section>
          )}

          {file && !inspecting && (
            <section className="space-y-4">
              <h4 className="text-sm font-semibold">3. Destino</h4>
              <Field label="Nombre de la base de datos destino" hint={server.engine === 'postgres' ? 'Si no existe, se creará.' : undefined}>
                {id => <Input id={id} value={target} disabled={busy} onChange={e => setTarget(e.target.value)} autoComplete="off"/>}
              </Field>
              {mssql && (
                <div>
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label="Carpeta de datos (.mdf)">{id => <Input id={id} value={dataDir} disabled={busy} onChange={e => setDataDir(e.target.value)} className="font-mono"/>}</Field>
                    <Field label="Carpeta de logs (.ldf)">{id => <Input id={id} value={logDir} disabled={busy} onChange={e => setLogDir(e.target.value)} className="font-mono"/>}</Field>
                  </div>
                  <p className="mt-1 text-xs text-muted">Son rutas <b>dentro del servidor / contenedor</b>, no de tu equipo. Por defecto: /var/opt/mssql/data.</p>
                </div>
              )}
              <Checkbox label="Reemplazar si existe" checked={replace} disabled={busy} onChange={e => setReplace(e.target.checked)}/>
              {replace && <Notice tone="warn"><b>Atención:</b> si «{target || 'la base de datos'}» ya existe, se sobrescribirá y se perderán sus datos actuales.</Notice>}
              {(busy || event) && <ProgressBar percent={event?.percent ?? 0} message={event?.message ?? 'Iniciando...'} error={event?.error}/>}
              <ErrorBanner message={error}/>
              <Button variant="primary" icon={<Upload size={15}/>} loading={busy} disabled={!ready} onClick={run}>Restaurar</Button>
            </section>
          )}
        </div>
      </Card>

      <Card title="Historial de restauraciones" actions={<IconButton label="Actualizar" onClick={() => void list.reload()}><RefreshCw size={15} className={list.loading ? 'spin' : ''}/></IconButton>}>
        <ErrorBanner message={list.error} onRetry={() => void list.reload()}/>
        {list.loading && !list.data && <Spinner/>}
        {list.data && list.data.length === 0 && <EmptyState icon={<RotateCcw size={28}/>} title="Aún no hay restauraciones"/>}
        {list.data && list.data.length > 0 && (
          <Table>
            <thead><tr><Th>Base destino</Th><Th>Origen</Th><Th>Inicio</Th><Th>Fin</Th><Th>Estado</Th></tr></thead>
            <tbody>{list.data.map(r => (
              <tr key={r.id}>
                <Td className="font-medium">{r.targetDatabase}</Td>
                <Td title={r.sourceFile} className="max-w-[18rem]"><span className="block truncate font-mono text-xs">{r.sourceFile}</span></Td>
                <Td className="whitespace-nowrap">{fmtDate(r.startedAt)}</Td>
                <Td className="whitespace-nowrap">{fmtDate(r.finishedAt)}</Td>
                <Td><Badge tone={statusTone(r.status)}>{statusLabel(r.status)}</Badge>{r.status === 'error' && r.message && <span className="mt-1 block max-w-[14rem] text-xs text-danger">{r.message}</span>}</Td>
              </tr>
            ))}</tbody>
          </Table>
        )}
      </Card>
    </div>
  )
}


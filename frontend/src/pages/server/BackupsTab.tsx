import {useEffect, useState} from 'react'
import {Archive, FolderOpen, Play, RefreshCw, RotateCcw} from 'lucide-react'
import {api, errMsg, type Server} from '../../api'
import {useAsync} from '../../hooks/useAsync'
import {useProgress} from '../../hooks/useProgress'
import {useToast} from '../../components/Toast'
import {
  Badge, Button, Card, EmptyState, ErrorBanner, Field, fmtBytes, fmtDate, IconButton, Input, ProgressBar, Select,
  Spinner, statusLabel, statusTone, Table, Td, Th,
} from '../../components/ui'

export default function BackupsTab({server, onRestore}: {server: Server; onRestore: (file: string) => void}) {
  const toast = useToast()
  const remoteMssql = server.mode === 'remote' && server.engine === 'mssql'
  const dbs = useAsync(() => api.listDatabases(server.id), [server.id])
  const list = useAsync(() => api.listBackups(server.id), [server.id])
  const {event, start} = useProgress('backup:progress')
  const [db, setDb] = useState('')
  const [dir, setDir] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api.getSettings().then(s => setDir(d => d || (remoteMssql ? '' : s.defaultBackupDir || s.lastBackupDir || s.lastDirectory || ''))).catch(() => {})
  }, [])

  const userDbs = (dbs.data ?? []).filter(d => !d.system)
  useEffect(() => { if (!db && userDbs.length) setDb(userDbs[0].name) }, [userDbs, db])

  const pick = async () => {
    try { const d = await api.pickDirectory('Carpeta de destino del backup'); if (d) setDir(d) } catch (e) { toast.error(e) }
  }
  const run = async () => {
    setBusy(true); setError(''); const requestId = start()
    try {
      const r = await api.createBackup({serverId: server.id, database: db, destDir: dir, requestId})
      toast.success(`Backup creado: ${r.filePath}`)
      } catch (e) { setError(errMsg(e)); toast.error(e) }
    finally { setBusy(false); void list.reload() }
  }

  return (
    <div className="space-y-4">
      <Card title="Crear backup">
        <div className="grid gap-4 md:grid-cols-[1fr_2fr_auto] md:items-end">
          <Field label="Base de datos" error={dbs.error}>
            {id => <Select id={id} value={db} disabled={busy} onChange={e => setDb(e.target.value)}>
              {userDbs.length === 0 && <option value="">{dbs.loading ? 'Cargando...' : 'Sin bases de datos'}</option>}
              {userDbs.map(d => <option key={d.name}>{d.name}</option>)}
            </Select>}
          </Field>
          <Field label={remoteMssql ? 'Ruta de destino en el servidor remoto' : 'Carpeta de destino'}
            hint={remoteMssql ? 'La ruta se interpreta en el propio servidor SQL Server.' : undefined}>
            {id => <div className="flex gap-2">
              <Input id={id} value={dir} disabled={busy} onChange={e => setDir(e.target.value)} placeholder={remoteMssql ? '/var/opt/mssql/backup' : 'Elige una carpeta'}/>
              {!remoteMssql && <Button icon={<FolderOpen size={15}/>} disabled={busy} onClick={pick}>Elegir</Button>}
            </div>}
          </Field>
          <Button variant="primary" icon={<Play size={15}/>} loading={busy} disabled={!db || !dir.trim()} onClick={run}>Crear backup</Button>
        </div>
        {(busy || event) && <div className="mt-4"><ProgressBar percent={busy ? (event?.percent ?? 0) : (event?.percent ?? 0)} message={event?.message ?? 'Iniciando...'} error={event?.error}/></div>}
        <div className="mt-3"><ErrorBanner message={error}/></div>
      </Card>

      <Card title="Historial de backups" actions={<IconButton label="Actualizar" onClick={() => void list.reload()}><RefreshCw size={15} className={list.loading ? 'spin' : ''}/></IconButton>}>
        <ErrorBanner message={list.error} onRetry={() => void list.reload()}/>
        {list.loading && !list.data && <Spinner/>}
        {list.data && list.data.length === 0 && <EmptyState icon={<Archive size={28}/>} title="Aún no hay backups">Los backups que crees aparecerán aquí.</EmptyState>}
        {list.data && list.data.length > 0 && (
          <Table>
            <thead><tr><Th>Base de datos</Th><Th>Archivo</Th><Th>Tamaño</Th><Th>Fecha</Th><Th>Estado</Th><Th><span className="sr-only">Acciones</span></Th></tr></thead>
            <tbody>
              {list.data.map(b => (
                <tr key={b.id}>
                  <Td className="font-medium">{b.database}</Td>
                  <Td title={b.filePath} className="max-w-[20rem]"><span className="block truncate font-mono text-xs">{b.filePath}</span></Td>
                  <Td>{fmtBytes(b.sizeBytes)}</Td>
                  <Td className="whitespace-nowrap">{fmtDate(b.createdAt)}</Td>
                  <Td><Badge tone={statusTone(b.status)} >{statusLabel(b.status)}</Badge>{b.status === 'error' && b.message && <span className="mt-1 block max-w-[14rem] text-xs text-danger">{b.message}</span>}</Td>
                  <Td>{b.status === 'ok' && <Button size="sm" icon={<RotateCcw size={13}/>} onClick={() => onRestore(b.filePath)}>Restaurar este backup</Button>}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  )
}

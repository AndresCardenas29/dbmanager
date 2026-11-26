import {useState} from 'react'
import {RefreshCw} from 'lucide-react'
import {api, type Server} from '../../api'
import {useAsync} from '../../hooks/useAsync'
import {Button, Card, EmptyState, ErrorBanner, Select, Spinner} from '../../components/ui'

export default function LogsTab({server}: {server: Server}) {
  const [tail, setTail] = useState(200)
  const logs = useAsync(() => api.serverLogs(server.id, tail), [server.id, tail])

  return (
    <Card title="Logs del contenedor" actions={<>
      <Select aria-label="Líneas" className="!h-8 !w-auto text-xs" value={tail} onChange={e => setTail(Number(e.target.value))}>
        {[100, 200, 500, 1000, 5000].map(n => <option key={n} value={n}>Últimas {n} líneas</option>)}
      </Select>
      <Button size="sm" icon={<RefreshCw size={14} className={logs.loading ? 'spin' : ''}/>} onClick={() => void logs.reload()}>Actualizar</Button>
    </>}>
      <ErrorBanner message={logs.error} onRetry={() => void logs.reload()}/>
      {logs.loading && logs.data === undefined && <Spinner/>}
      {logs.data !== undefined && (logs.data.trim() === ''
        ? <EmptyState title="Sin logs">El contenedor no ha escrito nada todavía.</EmptyState>
        : <pre className="h-[28rem] overflow-auto whitespace-pre-wrap break-all rounded-md border border-line bg-surface2 p-3 font-mono text-xs leading-5">{logs.data}</pre>)}
    </Card>
  )
}

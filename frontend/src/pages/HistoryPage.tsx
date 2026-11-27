import {useMemo, useState} from 'react'
import {History, RefreshCw} from 'lucide-react'
import {api} from '../api'
import {useApp} from '../store'
import {useAsync} from '../hooks/useAsync'
import {Badge, Button, Card, EmptyState, ErrorBanner, fmtDate, Input, PageHeader, Select, Spinner, statusLabel, statusTone, Table, Td, Th} from '../components/ui'

export default function HistoryPage() {
  const {servers} = useApp()
  const ops = useAsync(() => api.listOperations(500), [])
  const [status, setStatus] = useState('')
  const [kind, setKind] = useState('')
  const [serverId, setServerId] = useState('')
  const [q, setQ] = useState('')

  const kinds = useMemo(() => Array.from(new Set((ops.data ?? []).map(o => o.kind))).sort(), [ops.data])
  const rows = (ops.data ?? []).filter(o =>
    (!status || o.status === status) && (!kind || o.kind === kind) && (!serverId || String(o.serverId) === serverId) &&
    (!q || `${o.target} ${o.message}`.toLowerCase().includes(q.toLowerCase())))
  const serverName = (id: number) => servers.find(s => s.id === id)?.name ?? (id ? `#${id}` : '—')

  return (
    <div>
      <PageHeader title="Historial" subtitle="Registro de operaciones realizadas desde Andres Tools."
        actions={<Button icon={<RefreshCw size={15}/>} loading={ops.loading} onClick={() => void ops.reload()}>Actualizar</Button>}/>
      <Card>
        <div className="mb-4 grid gap-3 sm:grid-cols-4">
          <Input aria-label="Buscar" placeholder="Buscar en destino o mensaje" value={q} onChange={e => setQ(e.target.value)}/>
          <Select aria-label="Estado" value={status} onChange={e => setStatus(e.target.value)}>
            <option value="">Todos los estados</option><option value="ok">Correcto</option><option value="error">Error</option></Select>
          <Select aria-label="Tipo" value={kind} onChange={e => setKind(e.target.value)}>
            <option value="">Todos los tipos</option>{kinds.map(k => <option key={k}>{k}</option>)}</Select>
          <Select aria-label="Servidor" value={serverId} onChange={e => setServerId(e.target.value)}>
            <option value="">Todos los servidores</option>{servers.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</Select>
        </div>
        <ErrorBanner message={ops.error} onRetry={() => void ops.reload()}/>
        {ops.loading && !ops.data && <Spinner/>}
        {ops.data && rows.length === 0 && <EmptyState icon={<History size={28}/>} title="Sin operaciones">
          {ops.data.length ? 'Ningún registro coincide con los filtros.' : 'Aquí aparecerán tus acciones.'}</EmptyState>}
        {rows.length > 0 && (
          <Table>
            <thead><tr><Th>Fecha</Th><Th>Tipo</Th><Th>Servidor</Th><Th>Destino</Th><Th>Estado</Th><Th>Mensaje</Th></tr></thead>
            <tbody>{rows.map(o => (
              <tr key={o.id}>
                <Td className="whitespace-nowrap">{fmtDate(o.createdAt)}</Td>
                <Td><Badge>{o.kind}</Badge></Td>
                <Td>{serverName(o.serverId)}</Td>
                <Td className="max-w-[14rem] break-words">{o.target}</Td>
                <Td><Badge tone={statusTone(o.status)}>{statusLabel(o.status)}</Badge></Td>
                <Td className="max-w-[22rem] break-words text-muted">{o.message}</Td>
              </tr>
            ))}</tbody>
          </Table>
        )}
      </Card>
    </div>
  )
}

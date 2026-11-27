import {RefreshCw, FileKey} from 'lucide-react'
import {api2} from '../../api/v2'
import {useAsync} from '../../hooks/useAsync'
import {Badge, Button, Card, EmptyState, ErrorBanner, fmtDate, PageHeader, Spinner, Table, Td, Th} from '../../components/ui'

export default function CertHistoryPage() {
  const h = useAsync(() => api2.listCertHistory(200), [])
  return (
    <div>
      <PageHeader title="Historial de certificados" subtitle="Archivos .p12 generados desde Andres Tools."
        actions={<Button icon={<RefreshCw size={15}/>} loading={h.loading} onClick={() => void h.reload()}>Actualizar</Button>}/>
      <Card>
        <ErrorBanner message={h.error} onRetry={() => void h.reload()}/>
        {h.loading && !h.data && <Spinner/>}
        {h.data && h.data.length === 0 && <EmptyState icon={<FileKey size={28}/>} title="Aún no has generado ningún .p12"/>}
        {h.data && h.data.length > 0 && (
          <Table>
            <thead><tr><Th>Alias</Th><Th>CN</Th><Th>Vencimiento</Th><Th>Cadena</Th><Th>Verificado</Th><Th>Ruta</Th><Th>Fecha</Th></tr></thead>
            <tbody>{h.data.map(c => (
              <tr key={c.id}>
                <Td className="font-medium">{c.alias}</Td>
                <Td className="break-all">{c.commonName}</Td>
                <Td className="whitespace-nowrap">{fmtDate(c.notAfter)}</Td>
                <Td>{c.chainLength}</Td>
                <Td><Badge tone={c.verified ? 'success' : 'danger'}>{c.verified ? 'Sí' : 'No'}</Badge></Td>
                <Td title={c.outputPath} className="max-w-[16rem]"><span className="block truncate font-mono text-xs">{c.outputPath}</span></Td>
                <Td className="whitespace-nowrap">{fmtDate(c.createdAt)}</Td>
              </tr>))}</tbody>
          </Table>
        )}
      </Card>
    </div>
  )
}

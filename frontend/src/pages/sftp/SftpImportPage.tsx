import {useState} from 'react'
import {Box, RefreshCw} from 'lucide-react'
import {errMsg, type ContainerInfo} from '../../api'
import {api2} from '../../api/v2'
import {useApp} from '../../store'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {Button, Card, cx, EmptyState, ErrorBanner, Notice, PageHeader, Spinner} from '../../components/ui'

export default function SftpImportPage() {
  const {go, refresh} = useApp()
  const toast = useToast()
  const list = useAsync(() => api2.listImportableSftp(), [])
  const [sel, setSel] = useState<ContainerInfo | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const submit = async () => {
    if (!sel) return
    setBusy(true); setError('')
    try {
      const s = await api2.importSftp(sel.name)
      toast.success(`Contenedor «${s.name}» importado.`)
      await refresh(); go({page: 'sftp', id: s.id})
    } catch (e) { setError(errMsg(e)); setBusy(false) }
  }

  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader title="Importar contenedor SFTP" subtitle="Registra un contenedor SFTP que ya existe en Docker."
        actions={<Button icon={<RefreshCw size={15}/>} loading={list.loading} onClick={() => void list.reload()}>Actualizar</Button>}/>
      <div className="space-y-4">
        <Notice tone="accent">Los servicios importados son <b>externos</b>: solo puedes iniciarlos, detenerlos, ver su estado y logs. Sus usuarios y llaves no los gestiona esta aplicación y el contenedor nunca se recrea.</Notice>
        <ErrorBanner message={list.error} onRetry={() => void list.reload()}/>
        {list.loading && !list.data && <Spinner/>}
        {list.data && list.data.length === 0 && <Card><EmptyState icon={<Box size={28}/>} title="No hay contenedores SFTP importables">Solo se muestran contenedores SFTP aún no registrados.</EmptyState></Card>}
        {list.data && list.data.length > 0 && (
          <Card title="Contenedores disponibles">
            <ul className="space-y-2" role="radiogroup" aria-label="Contenedores">
              {list.data.map(c => (
                <li key={c.id}>
                  <button role="radio" aria-checked={sel?.id === c.id} onClick={() => setSel(c)}
                    className={cx('flex w-full items-center gap-3 rounded-md border p-3 text-left', sel?.id === c.id ? 'border-accent bg-accent/5' : 'border-line hover:bg-surface2')}>
                    <Box size={18} className="text-muted"/>
                    <span className="min-w-0 flex-1"><span className="block font-medium">{c.name}</span>
                      <span className="block truncate text-xs text-muted">{c.image} · puerto {c.hostPort || '—'} · {c.status || c.state}</span></span>
                  </button>
                </li>
              ))}
            </ul>
            <div className="mt-4 space-y-3"><ErrorBanner message={error}/><Button variant="primary" disabled={!sel} loading={busy} onClick={submit}>Importar</Button></div>
          </Card>
        )}
      </div>
    </div>
  )
}

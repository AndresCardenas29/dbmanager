import {useState} from 'react'
import {Box, RefreshCw} from 'lucide-react'
import {api, errMsg, type ContainerInfo} from '../api'
import {useApp} from '../store'
import {useAsync} from '../hooks/useAsync'
import {useToast} from '../components/Toast'
import {Button, Card, cx, EmptyState, ErrorBanner, Field, Input, PageHeader, PasswordInput, Spinner} from '../components/ui'
import {EngineBadge} from '../components/Sidebar'

export default function ImportPage() {
  const {go, refresh} = useApp()
  const toast = useToast()
  const list = useAsync(() => api.listImportable(), [])
  const [sel, setSel] = useState<ContainerInfo | null>(null)
  const [user, setUser] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const choose = (c: ContainerInfo) => { setSel(c); setUser(c.engine === 'mssql' ? 'sa' : 'postgres'); setError('') }

  const submit = async () => {
    if (!sel) return
    setBusy(true); setError('')
    try {
      const s = await api.importContainer({containerName: sel.name, adminUser: user.trim(), adminPassword: password})
      toast.success(`Contenedor «${s.name}» importado.`)
      await refresh(); go({page: 'server', id: s.id})
    } catch (e) { setError(errMsg(e)); setBusy(false) }
  }

  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader title="Importar contenedor" subtitle="Registra un contenedor de SQL Server o PostgreSQL que ya existe en Docker."
        actions={<Button icon={<RefreshCw size={15}/>} onClick={() => void list.reload()} loading={list.loading}>Actualizar</Button>}/>
      <div className="space-y-4">
        <ErrorBanner message={list.error} onRetry={() => void list.reload()}/>
        {list.loading && !list.data && <Spinner/>}
        {list.data && list.data.length === 0 && (
          <Card><EmptyState icon={<Box size={28}/>} title="No hay contenedores importables">
            Solo se muestran contenedores de SQL Server o PostgreSQL que aún no estén registrados.
          </EmptyState></Card>
        )}
        {list.data && list.data.length > 0 && (
          <Card title="Contenedores disponibles">
            <ul className="space-y-2" role="radiogroup" aria-label="Contenedores">
              {list.data.map(c => (
                <li key={c.id}>
                  <button role="radio" aria-checked={sel?.id === c.id} onClick={() => choose(c)}
                    className={cx('flex w-full items-center gap-3 rounded-md border p-3 text-left', sel?.id === c.id ? 'border-accent bg-accent/5' : 'border-line hover:bg-surface2')}>
                    <Box size={18} className="text-muted"/>
                    <span className="min-w-0 flex-1">
                      <span className="block font-medium">{c.name}</span>
                      <span className="block truncate text-xs text-muted">{c.image} · puerto {c.hostPort || '—'} · {c.status || c.state}</span>
                    </span>
                    <EngineBadge engine={c.engine}/>
                  </button>
                </li>
              ))}
            </ul>
          </Card>
        )}
        {sel && (
          <Card title={`Credenciales de «${sel.name}»`}>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Usuario administrador">{id => <Input id={id} value={user} onChange={e => setUser(e.target.value)}/>}</Field>
              <Field label="Contraseña">{id => <PasswordInput id={id} autoComplete="off" value={password} onChange={setPassword}/>}</Field>
            </div>
            <div className="mt-4 space-y-3">
              <ErrorBanner message={error}/>
              <Button variant="primary" loading={busy} disabled={!user.trim() || !password} onClick={submit}>Importar</Button>
            </div>
          </Card>
        )}
      </div>
    </div>
  )
}

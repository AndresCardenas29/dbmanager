import {useState} from 'react'
import {CheckCircle2, XCircle} from 'lucide-react'
import {api, errMsg, type Engine} from '../api'
import {useApp} from '../store'
import {useToast} from '../components/Toast'
import {Button, Card, Checkbox, ErrorBanner, Field, Input, PageHeader, PasswordInput, Select} from '../components/ui'

/** Formulario compartido para crear (y reutilizado para editar) un servidor remoto. */
export default function RemotePage() {
  const {go, refresh} = useApp()
  const toast = useToast()
  const [engine, setEngine] = useState<Engine>('mssql')
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [port, setPort] = useState('1433')
  const [user, setUser] = useState('sa')
  const [password, setPassword] = useState('')
  const [tls, setTls] = useState(false)
  const [trust, setTrust] = useState(true)
  const [busy, setBusy] = useState(false)
  const [testing, setTesting] = useState(false)
  const [error, setError] = useState('')
  const [testResult, setTestResult] = useState<{ok: boolean; text: string} | null>(null)

  const changeEngine = (e: Engine) => {
    setEngine(e)
    if (port === '1433' || port === '5432') setPort(e === 'mssql' ? '1433' : '5432')
    if (user === 'sa' || user === 'postgres') setUser(e === 'mssql' ? 'sa' : 'postgres')
  }
  const portNum = Number(port)
  const valid = name.trim() && host.trim() && Number.isInteger(portNum) && portNum > 0 && portNum < 65536 && user.trim() && password

  const req = () => ({name: name.trim(), engine, host: host.trim(), port: portNum, adminUser: user.trim(), adminPassword: password, tls, trustServerCert: trust})

  const test = async () => {
    setTesting(true); setTestResult(null); setError('')
    try {
      const st = await api.testRemoteConnection(req())
      setTestResult({ok: st.reachable, text: st.reachable ? `Conexión correcta${st.version ? ` · ${st.version}` : ''}` : (st.message || 'No se pudo conectar.')})
    } catch (e) {
      setTestResult({ok: false, text: errMsg(e)})
    } finally { setTesting(false) }
  }

  const save = async () => {
    setBusy(true); setError('')
    try {
      const s = await api.addRemoteServer(req())
      toast.success(`Servidor «${s.name}» registrado.`)
      await refresh(); go({page: 'server', id: s.id})
    } catch (e) { setError(errMsg(e)); setBusy(false) }
  }

  return (
    <div className="mx-auto max-w-2xl">
      <PageHeader title="Conectar servidor remoto" subtitle="Registra un servidor existente. Solo se gestiona la conexión, no Docker."/>
      <Card>
        <form className="space-y-4" onSubmit={e => { e.preventDefault(); if (valid && !busy) void save() }}>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Motor">{id => <Select id={id} value={engine} onChange={e => changeEngine(e.target.value as Engine)}>
              <option value="mssql">SQL Server</option><option value="postgres">PostgreSQL</option></Select>}</Field>
            <Field label="Nombre">{id => <Input id={id} autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="Producción"/>}</Field>
            <Field label="Host" className="sm:col-span-1">{id => <Input id={id} value={host} onChange={e => setHost(e.target.value)} placeholder="db.ejemplo.com"/>}</Field>
            <Field label="Puerto">{id => <Input id={id} inputMode="numeric" value={port} onChange={e => setPort(e.target.value.replace(/\D/g, ''))}/>}</Field>
            <Field label="Usuario administrador">{id => <Input id={id} value={user} onChange={e => setUser(e.target.value)} autoComplete="off"/>}</Field>
            <Field label="Contraseña">{id => <PasswordInput id={id} autoComplete="off" value={password} onChange={setPassword}/>}</Field>
          </div>
          <div className="space-y-2">
            <Checkbox label="Usar TLS / cifrado" checked={tls} onChange={e => setTls(e.target.checked)}/>
            <Checkbox label="Confiar en el certificado del servidor" hint="Útil con certificados autofirmados. Reduce la seguridad." checked={trust} onChange={e => setTrust(e.target.checked)}/>
          </div>
          {testResult && (
            <div role="status" className={`flex items-start gap-2 rounded-md border p-3 text-sm ${testResult.ok ? 'border-success/40 bg-success/10' : 'border-danger/40 bg-danger/10'}`}>
              {testResult.ok ? <CheckCircle2 size={18} className="text-success"/> : <XCircle size={18} className="text-danger"/>}
              <span className="break-words">{testResult.text}</span>
            </div>
          )}
          <ErrorBanner message={error}/>
          <div className="flex justify-end gap-2 border-t border-line pt-4">
            <Button loading={testing} disabled={!valid || busy} onClick={test}>Probar conexión</Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!valid || testing}>Guardar servidor</Button>
          </div>
        </form>
      </Card>
    </div>
  )
}

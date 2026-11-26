import {useState} from 'react'
import {CheckCircle2, FileSearch, XCircle} from 'lucide-react'
import {api, errMsg} from '../../api'
import {api2, type SftpLoginResult, type SftpService} from '../../api/v2'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {Button, Card, cx, Field, Input, PasswordInput, Select} from '../../components/ui'

export default function SftpTestTab({svc}: {svc: SftpService}) {
  const toast = useToast()
  const managed = svc.mode === 'managed'
  const users = useAsync(async () => (managed ? api2.listSftpUsers(svc.id) : []), [svc.id])
  const [user, setUser] = useState('')
  const [method, setMethod] = useState<'password' | 'key'>('password')
  const [password, setPassword] = useState('')
  const [keyPath, setKeyPath] = useState('')
  const [pass, setPass] = useState('')
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<SftpLoginResult | null>(null)

  const pick = async () => {
    try { const f = await api.pickFile('Selecciona la llave privada', [{displayName: 'Todos los archivos (*)', pattern: '*'}]); if (f) setKeyPath(f) } catch (e) { toast.error(e) }
  }
  const run = async () => {
    setBusy(true); setRes(null)
    try {
      setRes(await api2.testSftpLogin({serviceId: svc.id, user: user.trim(), password: method === 'password' ? password : '', privateKeyPath: method === 'key' ? keyPath : '', passphrase: method === 'key' ? pass : ''}))
    } catch (e) { setRes({ok: false, message: errMsg(e), hostFingerprint: '', entries: null}) }
    finally { setBusy(false) }
  }
  const ready = user.trim() && (method === 'password' ? password : keyPath)

  return (
    <Card title="Probar acceso">
      <form className="space-y-4" onSubmit={e => { e.preventDefault(); if (ready && !busy) void run() }}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Usuario">
            {id => managed && (users.data ?? []).length > 0
              ? <Select id={id} value={user} onChange={e => setUser(e.target.value)}><option value="">Elige un usuario</option>{(users.data ?? []).map(u => <option key={u.name}>{u.name}</option>)}</Select>
              : <Input id={id} value={user} onChange={e => setUser(e.target.value)} autoComplete="off"/>}
          </Field>
          <Field label="Método">{id => <Select id={id} value={method} onChange={e => setMethod(e.target.value as 'password' | 'key')}>
            <option value="password">Contraseña</option><option value="key">Llave privada</option></Select>}</Field>
        </div>
        {method === 'password'
          ? <Field label="Contraseña">{id => <PasswordInput id={id} autoComplete="off" value={password} onChange={setPassword}/>}</Field>
          : <>
            <Field label="Archivo de llave privada">{id => <div className="flex gap-2"><Input id={id} readOnly value={keyPath} placeholder="Ningún archivo seleccionado"/>
              <Button icon={<FileSearch size={15}/>} onClick={pick}>Elegir</Button></div>}</Field>
            <Field label="Frase de paso (si la llave la tiene)">{id => <PasswordInput id={id} autoComplete="off" value={pass} onChange={setPass}/>}</Field>
          </>}
        <Button type="submit" variant="primary" loading={busy} disabled={!ready}>Probar acceso</Button>
      </form>
      {res && (
        <div role="status" className={cx('mt-4 space-y-2 rounded-md border p-3 text-sm', res.ok ? 'border-success/40 bg-success/10' : 'border-danger/40 bg-danger/10')}>
          <p className="flex items-start gap-2 font-medium">{res.ok ? <CheckCircle2 size={18} className="text-success"/> : <XCircle size={18} className="text-danger"/>}<span className="break-words">{res.message || (res.ok ? 'Acceso correcto' : 'Acceso denegado')}</span></p>
          {res.hostFingerprint && <p className="text-xs"><span className="text-muted">Huella del servidor:</span> <code className="break-all font-mono">{res.hostFingerprint}</code></p>}
          {res.ok && <div><p className="text-xs text-muted">Contenido del directorio:</p>
            {(res.entries ?? []).length === 0 ? <p className="text-xs">(vacío)</p>
              : <ul className="mt-1 max-h-48 overflow-auto font-mono text-xs">{(res.entries ?? []).map(e => <li key={e}>{e}</li>)}</ul>}</div>}
        </div>
      )}
    </Card>
  )
}

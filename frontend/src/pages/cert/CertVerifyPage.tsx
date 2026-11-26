import {useState} from 'react'
import {FileSearch, ShieldCheck} from 'lucide-react'
import {api, errMsg} from '../../api'
import {api2, type Pkcs12Verification} from '../../api/v2'
import {useToast} from '../../components/Toast'
import {VerificationPanel} from '../../components/CertParts'
import {Button, Card, ErrorBanner, Field, Input, PageHeader, PasswordInput} from '../../components/ui'

export default function CertVerifyPage() {
  const toast = useToast()
  const [path, setPath] = useState('')
  const [pw, setPw] = useState('')
  const [alias, setAlias] = useState('tomcat')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [res, setRes] = useState<{v: Pkcs12Verification; alias: string} | null>(null)

  const pick = async () => {
    try { const f = await api.pickFile('Selecciona el archivo .p12', [{displayName: 'PKCS#12 (*.p12;*.pfx)', pattern: '*.p12;*.pfx'}]); if (f) setPath(f) } catch (e) { toast.error(e) }
  }
  const run = async () => {
    setBusy(true); setError(''); setRes(null)
    try { setRes({v: await api2.verifyPkcs12(path, pw, alias.trim()), alias: alias.trim()}) }
    catch (e) { setError(errMsg(e)) } finally { setBusy(false) }
  }
  return (
    <div className="mx-auto max-w-4xl">
      <PageHeader title="Verificar .p12" subtitle="Comprueba el contenido de un keystore PKCS#12 existente."/>
      <div className="space-y-4">
        <Card>
          <form className="space-y-4" onSubmit={e => { e.preventDefault(); if (path && pw && !busy) void run() }}>
            <Field label="Archivo .p12">{id => <div className="flex gap-2"><Input id={id} readOnly value={path} placeholder="Ningún archivo seleccionado"/>
              <Button icon={<FileSearch size={15}/>} onClick={pick}>Elegir</Button></div>}</Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Contraseña">{id => <PasswordInput id={id} autoComplete="off" value={pw} onChange={setPw}/>}</Field>
              <Field label="Alias esperado" hint="Vacío = no comprobar el alias.">{id => <Input id={id} value={alias} onChange={e => setAlias(e.target.value)}/>}</Field>
            </div>
            <ErrorBanner message={error}/>
            <Button type="submit" variant="primary" icon={<ShieldCheck size={15}/>} loading={busy} disabled={!path || !pw}>Verificar</Button>
          </form>
        </Card>
        {res && <VerificationPanel v={res.v} expectedAlias={res.alias}/>}
      </div>
    </div>
  )
}

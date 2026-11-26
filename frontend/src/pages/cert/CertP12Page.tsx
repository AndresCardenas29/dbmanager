import {useState} from 'react'
import {FileSearch, FileKey, Save} from 'lucide-react'
import {api, errMsg} from '../../api'
import {api2, type CertFileInfo, type Pkcs12Result} from '../../api/v2'
import {useToast} from '../../components/Toast'
import {FileSummary, VerificationPanel} from '../../components/CertParts'
import {Button, Card, ConfirmDialog, CopyButton, ErrorBanner, Field, Input, PageHeader, PasswordInput, Select, Spinner} from '../../components/ui'

type Slot = 'key' | 'cert' | 'inter' | 'root'
interface SlotState {path: string; info: CertFileInfo | null; error: string; loading: boolean; pass: string}
const empty = (): SlotState => ({path: '', info: null, error: '', loading: false, pass: ''})
const PEM = [{displayName: 'Certificados y llaves (*.pem;*.crt;*.cer;*.key;*.der)', pattern: '*.pem;*.crt;*.cer;*.key;*.der'}, {displayName: 'Todos (*)', pattern: '*'}]
const LABEL: Record<Slot, {t: string; req: boolean}> = {
  key: {t: 'Llave privada', req: true}, cert: {t: 'Certificado de dominio', req: true},
  inter: {t: 'Certificado intermedio', req: true}, root: {t: 'Certificado raíz', req: false},
}
const q = (s: string) => (/\s/.test(s) ? `"${s}"` : s)

export default function CertP12Page() {
  const toast = useToast()
  const [slots, setSlots] = useState<Record<Slot, SlotState>>({key: empty(), cert: empty(), inter: empty(), root: empty()})
  const [alias, setAlias] = useState('tomcat')
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  const [format, setFormat] = useState<'modern' | 'legacy'>('modern')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState<Pkcs12Result | null>(null)
  const [done, setDone] = useState<{alias: string; req: string} | null>(null)

  const patch = (k: Slot, p: Partial<SlotState>) => setSlots(s => ({...s, [k]: {...s[k], ...p}}))
  const inspect = async (k: Slot, path: string, pass: string) => {
    patch(k, {path, loading: true, error: '', info: null})
    try { patch(k, {info: await api2.inspectCert(path, pass), loading: false}) }
    catch (e) { patch(k, {error: errMsg(e), loading: false}) }
  }
  const pick = async (k: Slot) => {
    try { const f = await api.pickFile(`Selecciona: ${LABEL[k].t.toLowerCase()}`, PEM); if (f) await inspect(k, f, slots[k].pass) } catch (e) { toast.error(e) }
  }
  const [out, setOut] = useState('')
  const [outFromDialog, setOutFromDialog] = useState(false)
  const chooseOut = async () => {
    try { const f = await api2.saveFile('Guardar archivo .p12', `${alias || 'tomcat'}.p12`, [{displayName: 'PKCS#12 (*.p12)', pattern: '*.p12'}]); if (f) { setOut(f); setOutFromDialog(true) } } catch (e) { toast.error(e) }
  }

  const keyEncNeedsPass = slots.key.info?.encrypted && !slots.key.pass
  const pwOk = pw.length >= 6 && pw === pw2
  const ready = slots.key.info && slots.cert.info && slots.inter.info && alias.trim() && pwOk && out && !keyEncNeedsPass

  const [confirmReplace, setConfirmReplace] = useState(false)
  const run = async (overwrite = outFromDialog) => {
    setBusy(true); setError(''); setResult(null)
    try {
      const req = {privateKeyPath: slots.key.path, keyPassphrase: slots.key.pass, certPath: slots.cert.path, intermediatePath: slots.inter.path,
        rootPath: slots.root.path, alias: alias.trim(), password: pw, outputPath: out, format, overwrite}
      const r = await api2.buildPkcs12(req)
      setResult(r); setDone({alias: alias.trim(), req: JSON.stringify(req)})
      toast.success('Archivo .p12 generado.')
    } catch (e) {
      if (!overwrite && /ya existe/i.test(errMsg(e))) setConfirmReplace(true)
      else { setError(errMsg(e)); toast.error(e) }
    } finally { setBusy(false) }
  }

  const chainFiles = [slots.inter.path, slots.root.path].filter(Boolean)
  const cmds = [
    chainFiles.length > 1 ? `cat ${chainFiles.map(q).join(' ')} > cadena.pem` : '',
    `openssl pkcs12 -export -inkey ${q(slots.key.path || 'privkey.pem')} -in ${q(slots.cert.path || 'cert.pem')} -certfile ${chainFiles.length > 1 ? 'cadena.pem' : q(chainFiles[0] || 'intermedio.pem')} -name ${alias || 'tomcat'} -out ${q(out || `${alias || 'tomcat'}.p12`)} -passout pass:TU_CONTRASEÑA`,
    `keytool -list -v -keystore ${q(out || `${alias || 'tomcat'}.p12`)} -storetype PKCS12 -storepass TU_CONTRASEÑA`,
  ].filter(Boolean).join('\n')

  return (
    <div className="mx-auto max-w-4xl">
      <PageHeader title="Generar .p12" subtitle="Crea un keystore PKCS#12 a partir de la llave privada y la cadena de certificados."/>
      <div className="space-y-4">
        <Card title="1. Archivos">
          <div className="space-y-5">
            {(['key', 'cert', 'inter', 'root'] as Slot[]).map(k => {
              const s = slots[k]
              return (
                <div key={k}>
                  <Field label={`${LABEL[k].t}${LABEL[k].req ? '' : ' (opcional)'}`}>
                    {id => <div className="flex gap-2"><Input id={id} readOnly value={s.path} placeholder="Ningún archivo seleccionado"/>
                      <Button icon={<FileSearch size={15}/>} onClick={() => pick(k)}>Elegir</Button>
                      {!LABEL[k].req && s.path && <Button onClick={() => patch(k, empty())}>Quitar</Button>}</div>}
                  </Field>
                  {s.loading && <Spinner label="Inspeccionando..."/>}
                  {s.error && <div className="mt-2"><ErrorBanner message={s.error}/></div>}
                  {s.info?.encrypted && k === 'key' && (
                    <div className="mt-2 flex gap-2"><div className="flex-1"><PasswordInput autoComplete="off" placeholder="Frase de paso de la llave" value={s.pass} onChange={v => patch(k, {pass: v})}/></div>
                      <Button disabled={!s.pass} onClick={() => inspect(k, s.path, s.pass)}>Descifrar</Button></div>
                  )}
                  {s.info && <FileSummary info={s.info}/>}
                </div>
              )
            })}
          </div>
        </Card>

        <Card title="2. Opciones">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Alias" hint={/[A-Z]/.test(alias) ? 'keytool mostrará el alias en minúsculas (p. ej. «miAlias» → «mialias»). Si tu configuración (server.xml / application.properties) usa el alias, escríbelo en minúsculas.' : undefined}>{id => <Input id={id} value={alias} onChange={e => setAlias(e.target.value)}/>}</Field>
            <Field label="Formato" hint={format === 'modern' ? 'AES-256. Java 8u301+, 11 y superiores.' : '3DES/RC2. Para Java/Tomcat antiguos.'}>
              {id => <Select id={id} value={format} onChange={e => setFormat(e.target.value as 'modern' | 'legacy')}>
                <option value="modern">Moderno (recomendado)</option><option value="legacy">Compatible con Java antiguo</option></Select>}</Field>
            <Field label="Contraseña del .p12" hint="Mínimo 6 caracteres (requisito de keytool).">{id => <PasswordInput id={id} value={pw} onChange={setPw}/>}</Field>
            <Field label="Confirmar contraseña" error={pw2 && pw !== pw2 ? 'No coinciden.' : ''}>{id => <PasswordInput id={id} value={pw2} onChange={setPw2}/>}</Field>
          </div>
          <div className="mt-4"><Field label="Archivo de salida">
            {id => <div className="flex gap-2"><Input id={id} readOnly value={out} placeholder={`${alias || 'tomcat'}.p12`}/><Button icon={<Save size={15}/>} onClick={chooseOut}>Guardar como...</Button></div>}</Field></div>
          <div className="mt-4 space-y-3"><ErrorBanner message={error}/>
            <Button variant="primary" icon={<FileKey size={15}/>} loading={busy} disabled={!ready} onClick={() => run()}>Generar</Button>
            {!ready && <p className="text-xs text-muted">Faltan: {[!slots.key.info && 'llave', !slots.cert.info && 'certificado', !slots.inter.info && 'intermedio', keyEncNeedsPass && 'frase de paso', !pwOk && 'contraseña válida', !out && 'ruta de salida'].filter(Boolean).join(', ')}.</p>}</div>
        </Card>

        {confirmReplace && (
          <ConfirmDialog title="¿Reemplazar el archivo existente?" confirmLabel="Reemplazar" danger onClose={() => setConfirmReplace(false)} onConfirm={() => run(true)}>
            <p>Ya existe un archivo en <code className="break-all font-mono text-xs">{out}</code>. Se sobrescribirá.</p>
          </ConfirmDialog>
        )}
        {result && done && <VerificationPanel v={result.verification} expectedAlias={done.alias} warnings={result.warnings} outputPath={result.outputPath}/>}

        <Card title="Comandos equivalentes" actions={<CopyButton text={cmds} label="Copiar comandos"/>}>
          <pre className="overflow-auto whitespace-pre-wrap break-all rounded-md border border-line bg-surface2 p-3 font-mono text-xs leading-5">{cmds}</pre>
          <p className="mt-2 text-xs text-muted">Sustituye TU_CONTRASEÑA por la contraseña elegida.</p>
        </Card>
      </div>
    </div>
  )
}

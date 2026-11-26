import {useState} from 'react'
import {CheckCircle2, ChevronDown, ChevronRight, XCircle, AlertTriangle} from 'lucide-react'
import type {CertFileInfo, CertSummary, Pkcs12Verification} from '../api/v2'
import {Badge, Card, cx, fmtDate, Notice} from './ui'

export function validity(c: CertSummary) {
  const end = new Date(c.notAfter).getTime()
  if (isNaN(end)) return {tone: 'neutral' as const, label: 'Sin fecha'}
  const days = Math.floor((end - Date.now()) / 86400000)
  if (days < 0) return {tone: 'danger' as const, label: 'Vencido'}
  if (days < 30) return {tone: 'warn' as const, label: `Vence en ${days} d`}
  return {tone: 'success' as const, label: 'Vigente'}
}

export function CertLine({c}: {c: CertSummary}) {
  const v = validity(c)
  return (
    <div className="space-y-0.5 text-sm">
      <p className="flex flex-wrap items-center gap-2"><b className="break-all">{c.commonName || c.subject}</b>
        {c.isCA && <Badge>CA</Badge>}{c.selfSigned && <Badge>autofirmado</Badge>}<Badge tone={v.tone}>{v.label}</Badge></p>
      <p className="break-all text-xs text-muted">Emisor: {c.issuer}</p>
      <p className="text-xs text-muted">Válido: {fmtDate(c.notBefore)} → {fmtDate(c.notAfter)}</p>
      {(c.sans ?? []).length > 0 && <p className="break-all text-xs text-muted">SAN: {(c.sans ?? []).join(', ')}</p>}
    </div>
  )
}

const kindLabel = {privateKey: 'Llave privada', certificate: 'Certificado', bundle: 'Paquete de certificados'}

export function FileSummary({info}: {info: CertFileInfo}) {
  return (
    <div className="mt-2 space-y-2 rounded-md border border-line bg-surface2 p-3">
      <p className="flex flex-wrap items-center gap-2 text-sm font-medium">{kindLabel[info.kind] ?? info.kind}
        {info.keyType && <Badge tone="accent">{info.keyType}</Badge>}{info.encrypted && <Badge tone="warn">cifrada</Badge>}</p>
      {(info.certs ?? []).map(c => <CertLine key={c.sha256 || c.serial} c={c}/>)}
    </div>
  )
}

export function Chain({chain}: {chain: CertSummary[]}) {
  const names = ['Dominio', 'Intermedio', 'Raíz']
  return (
    <ol className="space-y-0">
      {chain.map((c, i) => (
        <li key={c.sha256 || i} className="relative pl-6">
          {i < chain.length - 1 && <span className="absolute left-[7px] top-4 h-full w-px bg-line" aria-hidden/>}
          <span className="absolute left-0 top-1.5 h-3.5 w-3.5 rounded-full border-2 border-accent bg-surface" aria-hidden/>
          <div className="pb-3"><p className="text-xs font-semibold uppercase text-muted">{i === chain.length - 1 && chain.length > 1 && c.selfSigned ? 'Raíz' : names[i] ?? `Nivel ${i + 1}`}</p><CertLine c={c}/></div>
        </li>
      ))}
    </ol>
  )
}

function Check({ok, children, warn}: {ok: boolean; children: React.ReactNode; warn?: boolean}) {
  return (
    <li className="flex items-start gap-2 text-sm">
      {ok ? <CheckCircle2 size={17} className="mt-0.5 shrink-0 text-success"/> : warn ? <AlertTriangle size={17} className="mt-0.5 shrink-0 text-warn"/> : <XCircle size={17} className="mt-0.5 shrink-0 text-danger"/>}
      <span className="min-w-0 break-words">{children}</span>
    </li>
  )
}

export function VerificationPanel({v, expectedAlias, warnings, outputPath}: {v: Pkcs12Verification; expectedAlias: string; warnings?: string[] | null; outputPath?: string}) {
  const [open, setOpen] = useState(false)
  return (
    <Card title={<span className="flex items-center gap-2">{v.ok ? <CheckCircle2 size={18} className="text-success"/> : <XCircle size={18} className="text-danger"/>}
      {v.ok ? 'Verificación correcta' : 'La verificación encontró problemas'}</span>}>
      <div className="space-y-5">
        {outputPath && <p className="break-all text-sm"><span className="text-muted">Archivo:</span> <code className="font-mono text-xs">{outputPath}</code></p>}
        <ul className="space-y-1.5 font-mono">
          <Check ok={v.entryType === 'PrivateKeyEntry'}>Entry type: {v.entryType || '—'}</Check>
          <Check ok={!expectedAlias || v.alias.toLowerCase() === expectedAlias.toLowerCase()}>Alias name: {v.alias || '—'}</Check>
          <Check ok={v.chainLength >= 2}>Certificate chain length: {v.chainLength}</Check>
          <Check ok={v.keyMatchesCert}>Llave corresponde al certificado</Check>
          {(v.checks ?? []).map(c => <Check key={c.name} ok={c.severity ? c.severity === 'ok' : c.ok} warn={c.severity === 'warning'}><b>{c.name}</b>{c.detail ? <span className="text-muted"> — {c.detail}</span> : null}</Check>)}
          {(warnings ?? []).map(w => <Check key={w} ok={false} warn>{w}</Check>)}
        </ul>
        {(v.chain ?? []).length > 0 && <div><h4 className="mb-2 text-sm font-semibold">Cadena de certificados</h4><Chain chain={v.chain ?? []}/></div>}
        {v.keytoolAvailable ? (
          <div>
            <button onClick={() => setOpen(o => !o)} aria-expanded={open} className="flex items-center gap-1 text-sm font-medium">
              {open ? <ChevronDown size={16}/> : <ChevronRight size={16}/>} Salida de keytool
              <Badge tone={v.keytoolOk ? 'success' : 'danger'} className="ml-2">{v.keytoolOk ? 'keytool: correcto' : 'keytool: con errores'}</Badge>
            </button>
            {open && <pre className={cx('mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-md border border-line bg-surface2 p-3 font-mono text-xs leading-5')}>{v.keytoolOutput}</pre>}
          </div>
        ) : <Notice tone="accent">No se encontró Java/keytool en este equipo; solo se hizo la verificación interna.</Notice>}
      </div>
    </Card>
  )
}

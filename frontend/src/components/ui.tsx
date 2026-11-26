import {
  forwardRef, useEffect, useId, useRef, useState,
  type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes,
} from 'react'
import {AlertCircle, Check, Copy, Eye, EyeOff, Loader2, X} from 'lucide-react'

export const cx = (...a: Array<string | false | null | undefined>) => a.filter(Boolean).join(' ')

/* ---------- Botones ---------- */
type Variant = 'primary' | 'secondary' | 'danger' | 'ghost'
interface BtnProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant; loading?: boolean; icon?: ReactNode; size?: 'sm' | 'md'
}
const variants: Record<Variant, string> = {
  primary: 'bg-accent text-accentfg hover:opacity-90',
  secondary: 'bg-surface text-fg border border-line hover:bg-surface2',
  danger: 'bg-danger text-white hover:opacity-90',
  ghost: 'text-fg hover:bg-surface2',
}
export function Button({variant = 'secondary', loading, icon, size = 'md', className, children, disabled, ...p}: BtnProps) {
  return (
    <button
      type="button"
      {...p}
      disabled={disabled || loading}
      className={cx(
        'inline-flex items-center justify-center gap-2 rounded-md font-medium transition-colors whitespace-nowrap',
        'disabled:opacity-50 disabled:cursor-not-allowed',
        size === 'sm' ? 'h-8 px-2.5 text-xs' : 'h-9 px-3.5 text-sm',
        variants[variant], className,
      )}>
      {loading ? <Loader2 size={15} className="spin"/> : icon}
      {children}
    </button>
  )
}

export function IconButton({label, children, className, ...p}: ButtonHTMLAttributes<HTMLButtonElement> & {label: string}) {
  return (
    <button type="button" aria-label={label} title={label} {...p}
      className={cx('inline-flex h-8 w-8 items-center justify-center rounded-md text-muted hover:bg-surface2 hover:text-fg disabled:opacity-50', className)}>
      {children}
    </button>
  )
}

/* ---------- Formularios ---------- */
const inputCls = 'h-9 w-full rounded-md border border-line bg-surface px-3 text-sm text-fg placeholder:text-muted/70 disabled:opacity-60 disabled:bg-surface2'

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({className, ...p}, ref) => <input ref={ref} {...p} className={cx(inputCls, className)}/>)
Input.displayName = 'Input'

export function Select({className, children, ...p}: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...p} className={cx(inputCls, 'pr-8', className)}>{children}</select>
}

export function Field({label, hint, error, children, className}: {
  label: string; hint?: ReactNode; error?: string; children: (id: string) => ReactNode; className?: string
}) {
  const id = useId()
  return (
    <div className={cx('block', className)}>
      <label htmlFor={id} className="mb-1 block text-xs font-medium text-muted">{label}</label>
      {children(id)}
      {error ? <p className="mt-1 text-xs text-danger">{error}</p> : hint ? <p className="mt-1 text-xs text-muted">{hint}</p> : null}
    </div>
  )
}

export function Checkbox({label, hint, ...p}: Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> & {label: ReactNode; hint?: ReactNode}) {
  return (
    <label className="flex cursor-pointer items-start gap-2">
      <input type="checkbox" {...p} className="mt-0.5 h-4 w-4 shrink-0 accent-[rgb(var(--accent))]"/>
      <span><span className="text-sm">{label}</span>{hint && <span className="block text-xs text-muted">{hint}</span>}</span>
    </label>
  )
}

export function PasswordInput({id, value, onChange, placeholder, autoFocus, autoComplete = 'new-password'}: {
  id?: string; value: string; onChange: (v: string) => void; placeholder?: string; autoFocus?: boolean; autoComplete?: string
}) {
  const [show, setShow] = useState(false)
  return (
    <div className="relative">
      <Input id={id} type={show ? 'text' : 'password'} value={value} autoFocus={autoFocus} autoComplete={autoComplete}
        placeholder={placeholder} onChange={e => onChange(e.target.value)} className="pr-10"/>
      <button type="button" onClick={() => setShow(s => !s)} aria-label={show ? 'Ocultar contraseña' : 'Mostrar contraseña'}
        className="absolute right-1 top-1 inline-flex h-7 w-7 items-center justify-center rounded text-muted hover:text-fg">
        {show ? <EyeOff size={16}/> : <Eye size={16}/>}
      </button>
    </div>
  )
}

/* ---------- Presentación ---------- */
export function Card({title, actions, children, className}: {title?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string}) {
  return (
    <section className={cx('rounded-lg border border-line bg-surface', className)}>
      {(title || actions) && (
        <header className="flex min-h-[3rem] items-center justify-between gap-3 border-b border-line px-4 py-2">
          <h3 className="text-sm font-semibold">{title}</h3>
          <div className="flex items-center gap-2">{actions}</div>
        </header>
      )}
      <div className="p-4">{children}</div>
    </section>
  )
}

type Tone = 'neutral' | 'accent' | 'success' | 'danger' | 'warn'
const tones: Record<Tone, string> = {
  neutral: 'bg-surface2 text-muted border-line',
  accent: 'bg-accent/10 text-accent border-accent/30',
  success: 'bg-success/10 text-success border-success/30',
  danger: 'bg-danger/10 text-danger border-danger/30',
  warn: 'bg-warn/10 text-warn border-warn/30',
}
export function Badge({tone = 'neutral', children, className}: {tone?: Tone; children: ReactNode; className?: string}) {
  return <span className={cx('inline-flex items-center rounded border px-1.5 py-0.5 text-[11px] font-medium leading-none', tones[tone], className)}>{children}</span>
}

export function statusTone(status: string): Tone {
  if (status === 'ok') return 'success'
  if (status === 'error') return 'danger'
  if (status === 'running') return 'accent'
  return 'neutral'
}
export function statusLabel(status: string) {
  return ({ok: 'Correcto', error: 'Error', running: 'En curso'} as Record<string, string>)[status] ?? status
}

export function Spinner({label = 'Cargando...'}: {label?: string}) {
  return <div role="status" className="flex items-center gap-2 py-8 text-muted justify-center"><Loader2 size={18} className="spin"/>{label}</div>
}

export function EmptyState({icon, title, children}: {icon?: ReactNode; title: string; children?: ReactNode}) {
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-10 text-center">
      {icon && <div className="text-muted">{icon}</div>}
      <p className="font-medium">{title}</p>
      {children && <div className="max-w-md text-sm text-muted">{children}</div>}
    </div>
  )
}

export function ErrorBanner({message, onRetry}: {message: string; onRetry?: () => void}) {
  if (!message) return null
  return (
    <div role="alert" className="flex items-start gap-3 rounded-md border border-danger/40 bg-danger/10 p-3 text-sm">
      <AlertCircle size={18} className="mt-0.5 shrink-0 text-danger"/>
      <p className="min-w-0 flex-1 break-words">{message}</p>
      {onRetry && <Button size="sm" onClick={onRetry}>Reintentar</Button>}
    </div>
  )
}

export function Notice({tone = 'warn', children}: {tone?: 'warn' | 'accent' | 'danger'; children: ReactNode}) {
  return <div className={cx('rounded-md border p-3 text-sm', tones[tone])}><div className="text-fg">{children}</div></div>
}

export function ProgressBar({percent, message, error}: {percent: number; message?: string; error?: boolean}) {
  const p = Math.max(0, Math.min(100, percent))
  return (
    <div>
      <div role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={p}
        className="h-2 w-full overflow-hidden rounded-full bg-surface2">
        <div className={cx('h-full transition-all', error ? 'bg-danger' : 'bg-accent')} style={{width: `${p}%`}}/>
      </div>
      <div className="mt-1 flex justify-between gap-3 text-xs text-muted">
        <span className="truncate">{message}</span><span>{p}%</span>
      </div>
    </div>
  )
}

export function Tabs<K extends string>({tabs, value, onChange}: {tabs: Array<{key: K; label: string; icon?: ReactNode}>; value: K; onChange: (k: K) => void}) {
  return (
    <div role="tablist" className="flex gap-1 overflow-x-auto border-b border-line">
      {tabs.map(t => (
        <button key={t.key} role="tab" aria-selected={value === t.key} onClick={() => onChange(t.key)}
          className={cx('-mb-px inline-flex items-center gap-2 whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium',
            value === t.key ? 'border-accent text-accent' : 'border-transparent text-muted hover:text-fg')}>
          {t.icon}{t.label}
        </button>
      ))}
    </div>
  )
}

export function CopyButton({text, label = 'Copiar'}: {text: string; label?: string}) {
  const [ok, setOk] = useState(false)
  return (
    <IconButton label={ok ? 'Copiado' : label} onClick={async () => {
      try { await navigator.clipboard.writeText(text); setOk(true); setTimeout(() => setOk(false), 1500) } catch { /* ignorar */ }
    }}>{ok ? <Check size={15} className="text-success"/> : <Copy size={15}/>}</IconButton>
  )
}

/* ---------- Tablas ---------- */
export function Table({children}: {children: ReactNode}) {
  return <div className="overflow-x-auto"><table className="w-full border-collapse text-left text-sm">{children}</table></div>
}
export const Th = ({children, className}: {children?: ReactNode; className?: string}) =>
  <th className={cx('border-b border-line px-3 py-2 text-xs font-semibold uppercase tracking-wide text-muted', className)}>{children}</th>
export const Td = ({children, className, title}: {children?: ReactNode; className?: string; title?: string}) =>
  <td title={title} className={cx('border-b border-line px-3 py-2 align-middle', className)}>{children}</td>

/* ---------- Modal ---------- */
export function Modal({title, onClose, children, footer, width = 'max-w-lg', dismissible = true}: {
  title: string; onClose: () => void; children: ReactNode; footer?: ReactNode; width?: string; dismissible?: boolean
}) {
  const ref = useRef<HTMLDivElement>(null)
  const titleId = useId()
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null
    const el = ref.current
    const first = el?.querySelector<HTMLElement>('input,select,textarea,button:not([data-close])')
    ;(first ?? el)?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && dismissible) { e.stopPropagation(); onClose() }
      if (e.key === 'Tab' && el) {
        const f = Array.from(el.querySelectorAll<HTMLElement>('a[href],button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),[tabindex="0"]'))
        if (!f.length) return
        const a = f[0], z = f[f.length - 1]
        if (e.shiftKey && document.activeElement === a) { e.preventDefault(); z.focus() }
        else if (!e.shiftKey && document.activeElement === z) { e.preventDefault(); a.focus() }
      }
    }
    document.addEventListener('keydown', onKey)
    return () => { document.removeEventListener('keydown', onKey); prev?.focus?.() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/50 p-4"
      onMouseDown={e => { if (e.target === e.currentTarget && dismissible) onClose() }}>
      <div ref={ref} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1}
        className={cx('flex max-h-[90vh] w-full flex-col rounded-lg border border-line bg-surface shadow-xl', width)}>
        <header className="flex items-center justify-between border-b border-line px-5 py-3">
          <h2 id={titleId} className="text-base font-semibold">{title}</h2>
          {dismissible && <IconButton label="Cerrar" data-close onClick={onClose}><X size={16}/></IconButton>}
        </header>
        <div className="flex-1 overflow-y-auto p-5">{children}</div>
        {footer && <footer className="flex justify-end gap-2 border-t border-line px-5 py-3">{footer}</footer>}
      </div>
    </div>
  )
}

/** Confirmación; con `typeToConfirm` exige escribir el texto exacto. */
export function ConfirmDialog({title, children, confirmLabel = 'Confirmar', danger, typeToConfirm, onConfirm, onClose}: {
  title: string; children: ReactNode; confirmLabel?: string; danger?: boolean; typeToConfirm?: string
  onConfirm: () => Promise<void> | void; onClose: () => void
}) {
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const ok = !typeToConfirm || typed === typeToConfirm
  const run = async () => {
    if (!ok || busy) return
    setBusy(true)
    try { await onConfirm(); onClose() } catch { setBusy(false) }
  }
  return (
    <Modal title={title} onClose={() => !busy && onClose()} width="max-w-md" footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant={danger ? 'danger' : 'primary'} disabled={!ok} loading={busy} onClick={run}>{confirmLabel}</Button>
    </>}>
      <div className="space-y-3 text-sm">
        {children}
        {typeToConfirm && (
          <Field label={`Escribe "${typeToConfirm}" para confirmar`}>
            {id => <Input id={id} value={typed} autoComplete="off" onChange={e => setTyped(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') void run() }}/>}
          </Field>
        )}
      </div>
    </Modal>
  )
}

export function PageHeader({title, subtitle, actions}: {title: ReactNode; subtitle?: ReactNode; actions?: ReactNode}) {
  return (
    <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0"><h1 className="truncate text-xl font-semibold">{title}</h1>{subtitle && <p className="mt-0.5 text-sm text-muted">{subtitle}</p>}</div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  )
}

/* ---------- Utilidades de formato ---------- */
export function fmtDate(s: string) {
  if (!s) return '—'
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString('es')
}
export function fmtBytes(n: number) {
  if (!n) return '—'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0, v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(i ? 1 : 0)} ${u[i]}`
}
export function fmtMB(n: number) {
  return n >= 1024 ? `${(n / 1024).toFixed(2)} GB` : `${n.toFixed(1)} MB`
}
export const engineLabel = (e: string) => (e === 'mssql' ? 'SQL Server' : e === 'postgres' ? 'PostgreSQL' : e)

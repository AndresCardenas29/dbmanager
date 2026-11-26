import {createContext, useCallback, useContext, useMemo, useState, type ReactNode} from 'react'
import {CheckCircle2, AlertCircle, X} from 'lucide-react'
import {errMsg} from '../api'

interface ToastItem {id: number; kind: 'ok' | 'error'; text: string}
interface Ctx {success: (t: string) => void; error: (e: unknown) => void}

const ToastCtx = createContext<Ctx>({success: () => {}, error: () => {}})
export const useToast = () => useContext(ToastCtx)

export function ToastProvider({children}: {children: ReactNode}) {
  const [items, setItems] = useState<ToastItem[]>([])
  const dismiss = (id: number) => setItems(l => l.filter(i => i.id !== id))
  const push = useCallback((kind: 'ok' | 'error', text: string) => {
    const id = Date.now() + Math.random()
    setItems(l => [...l.slice(-3), {id, kind, text}])
    setTimeout(() => setItems(l => l.filter(i => i.id !== id)), kind === 'ok' ? 4000 : 9000)
  }, [])
  const value = useMemo<Ctx>(() => ({
    success: t => push('ok', t),
    error: e => push('error', errMsg(e)),
  }), [push])

  return (
    <ToastCtx.Provider value={value}>
      {children}
      <div className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-[22rem] max-w-[calc(100vw-2rem)] flex-col gap-2"
        aria-live="polite">
        {items.map(i => (
          <div key={i.id} role={i.kind === 'error' ? 'alert' : 'status'}
            className="pointer-events-auto flex items-start gap-2 rounded-md border border-line bg-surface p-3 shadow-lg">
            {i.kind === 'ok'
              ? <CheckCircle2 size={18} className="mt-0.5 shrink-0 text-success"/>
              : <AlertCircle size={18} className="mt-0.5 shrink-0 text-danger"/>}
            <p className="min-w-0 flex-1 break-words text-sm">{i.text}</p>
            <button onClick={() => dismiss(i.id)} aria-label="Cerrar notificación" className="text-muted hover:text-fg"><X size={14}/></button>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  )
}

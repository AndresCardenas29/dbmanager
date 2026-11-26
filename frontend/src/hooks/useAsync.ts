import {useCallback, useEffect, useRef, useState} from 'react'
import {errMsg} from '../api'

export interface AsyncState<T> {
  data: T | undefined
  error: string
  loading: boolean
  reload: () => Promise<void>
  setData: (v: T | undefined | ((p: T | undefined) => T | undefined)) => void
}

/** Carga datos asíncronos; mantiene los datos previos mientras recarga (sin saltos de layout). */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [data, setData] = useState<T | undefined>(undefined)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const seq = useRef(0)
  const fnRef = useRef(fn)
  fnRef.current = fn

  const reload = useCallback(async () => {
    const my = ++seq.current
    setLoading(true)
    try {
      const r = await fnRef.current()
      if (my === seq.current) { setData(r); setError('') }
    } catch (e) {
      if (my === seq.current) setError(errMsg(e))
    } finally {
      if (my === seq.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    setData(undefined)
    void reload()
    return () => { seq.current++ }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return {data, error, loading, reload, setData}
}

import {useCallback, useEffect, useRef, useState} from 'react'
import {onProgress, type ProgressChannel} from '../api/events'
import type {ProgressEvent} from '../api'

/** Último evento de progreso de un canal. */
export function useProgress(channel: ProgressChannel) {
  const [event, setEvent] = useState<ProgressEvent | null>(null)
  const rid = useRef('')
  useEffect(() => onProgress(channel, e => { if (rid.current && e.requestId === rid.current) setEvent(e) }), [channel])
  /** Inicia una operación nueva y devuelve su requestId. */
  const start = useCallback(() => { rid.current = crypto.randomUUID(); setEvent(null); return rid.current }, [])
  const reset = useCallback(() => { rid.current = ''; setEvent(null) }, [])
  return {event, start, reset}
}

import {EventsOn} from '../../wailsjs/runtime/runtime'
import type {ProgressEvent} from './types'

export type ProgressChannel = 'service:progress' | 'backup:progress' | 'restore:progress' | 'sftp:progress'

/** Suscribe a un canal de progreso; devuelve la función para cancelar. */
export function onProgress(channel: ProgressChannel, cb: (e: ProgressEvent) => void): () => void {
  try {
    return EventsOn(channel, (e: ProgressEvent) => cb(e))
  } catch {
    return () => {}
  }
}

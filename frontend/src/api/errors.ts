/** Extrae un mensaje legible de cualquier valor rechazado. */
export function errMsg(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  if (e && typeof e === 'object' && 'message' in e) return String((e as {message: unknown}).message)
  return 'Ocurrió un error inesperado.'
}

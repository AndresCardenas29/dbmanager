import {createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode} from 'react'
import {api, type EnvStatus, type Server, type ServerStatus} from './api'
import {useInterval} from './hooks/useInterval'
import {api2, type SftpService} from './api/v2'
import {useTheme, type ThemePref} from './hooks/useTheme'

export type TabKey = 'summary' | 'databases' | 'users' | 'permissions' | 'backups' | 'restores' | 'logs'
export type SftpTab = 'summary' | 'users' | 'test' | 'logs'
export type View =
  | {page: 'home'}
  | {page: 'server'; id: number; tab?: TabKey; restoreFile?: string}
  | {page: 'new'} | {page: 'import'} | {page: 'remote'}
  | {page: 'sftpNew'} | {page: 'sftpImport'} | {page: 'sftp'; id: number; tab?: SftpTab}
  | {page: 'certP12'} | {page: 'certVerify'} | {page: 'certHistory'}
  | {page: 'history'} | {page: 'settings'}

interface AppCtx {
  view: View
  go: (v: View) => void
  servers: Server[]
  serversLoaded: boolean
  serversError: string
  statuses: Record<number, ServerStatus>
  sftpServices: SftpService[]
  sftpStatuses: Record<number, ServerStatus>
  refresh: () => Promise<void>
  env: EnvStatus | null
  envError: string
  recheckEnv: () => Promise<void>
  theme: ThemePref
  setTheme: (t: ThemePref) => void
}

const Ctx = createContext<AppCtx>(null as never)
export const useApp = () => useContext(Ctx)

export function AppProvider({children}: {children: ReactNode}) {
  const [view, setView] = useState<View>({page: 'home'})
  const [servers, setServers] = useState<Server[]>([])
  const [serversLoaded, setLoaded] = useState(false)
  const [serversError, setServersError] = useState('')
  const [statuses, setStatuses] = useState<Record<number, ServerStatus>>({})
  const [sftpServices, setSftp] = useState<SftpService[]>([])
  const [sftpStatuses, setSftpStatuses] = useState<Record<number, ServerStatus>>({})
  const sftpRef = useRef<SftpService[]>([])
  const [env, setEnv] = useState<EnvStatus | null>(null)
  const [envError, setEnvError] = useState('')
  const {pref, set} = useTheme()
  const serversRef = useRef<Server[]>([])
  serversRef.current = servers

  const pollStatuses = useCallback(async (list: Server[]) => {
    const locals = list.filter(s => s.mode === 'local')
    const res = await Promise.all(locals.map(async s => {
      try { return [s.id, await api.serverStatus(s.id)] as const } catch { return null }
    }))
    setStatuses(prev => {
      const next: Record<number, ServerStatus> = {}
      for (const s of list) if (prev[s.id]) next[s.id] = prev[s.id]
      for (const r of res) if (r) next[r[0]] = r[1]
      return next
    })
  }, [])

  const pollSftp = useCallback(async (list: SftpService[]) => {
    const res = await Promise.all(list.map(async s => {
      try { return [s.id, await api2.sftpStatus(s.id)] as const } catch { return null }
    }))
    setSftpStatuses(prev => {
      const next: Record<number, ServerStatus> = {}
      for (const s of list) if (prev[s.id]) next[s.id] = prev[s.id]
      for (const r of res) if (r) next[r[0]] = r[1]
      return next
    })
  }, [])

  const refresh = useCallback(async () => {
    try {
      const sl = await api2.listSftpServices()
      setSftp(sl); sftpRef.current = sl; void pollSftp(sl)
    } catch { /* se ignora: la sección SFTP puede no estar lista */ }
    try {
      const l = await api.listServers()
      setServers(l); setServersError('')
      await pollStatuses(l)
    } catch (e) {
      setServersError(e instanceof Error ? e.message : String(e))
    } finally { setLoaded(true) }
  }, [pollStatuses, pollSftp])

  const recheckEnv = useCallback(async () => {
    try { setEnv(await api.checkEnvironment()); setEnvError('') }
    catch (e) { setEnvError(e instanceof Error ? e.message : String(e)) }
  }, [])

  useEffect(() => { void refresh(); void recheckEnv() }, [refresh, recheckEnv])
  useInterval(() => { void pollStatuses(serversRef.current); void pollSftp(sftpRef.current) }, 10000)

  const value = useMemo<AppCtx>(() => ({
    view, go: setView, servers, serversLoaded, serversError, statuses, sftpServices, sftpStatuses, refresh, env, envError, recheckEnv,
    theme: pref, setTheme: set,
  }), [view, servers, serversLoaded, serversError, statuses, sftpServices, sftpStatuses, refresh, env, envError, recheckEnv, pref, set])

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

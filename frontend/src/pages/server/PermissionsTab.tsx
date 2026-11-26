import {useMemo, useState} from 'react'
import {Grid3x3, RefreshCw} from 'lucide-react'
import {api, type AccessLevel, type Server} from '../../api'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {Card, cx, EmptyState, ErrorBanner, IconButton, Spinner} from '../../components/ui'

const LEVELS: Array<{value: AccessLevel; label: string; mssql: string; pg: string; tone: string}> = [
  {value: 'owner', label: 'Propietario', mssql: 'Rol db_owner: control total de la base de datos.', pg: 'Dueño de la base de datos.', tone: 'text-accent'},
  {value: 'readwrite', label: 'Lectura y escritura', mssql: 'Roles db_datareader + db_datawriter.', pg: 'CONNECT, USAGE y SELECT/INSERT/UPDATE/DELETE en el esquema public (también en tablas futuras).', tone: 'text-success'},
  {value: 'readonly', label: 'Solo lectura', mssql: 'Rol db_datareader.', pg: 'CONNECT, USAGE y SELECT en el esquema public (también en tablas futuras).', tone: 'text-warn'},
  {value: 'none', label: 'Sin acceso', mssql: 'Se quitan los roles del usuario en la base de datos.', pg: 'Se revocan los privilegios sobre la base de datos.', tone: 'text-muted'},
]

export default function PermissionsTab({server}: {server: Server}) {
  const toast = useToast()
  const m = useAsync(() => api.getPermissionMatrix(server.id), [server.id])
  const [pending, setPending] = useState<Record<string, boolean>>({})
  const key = (l: string, d: string) => `${l}\u0000${d}`

  const levels = useMemo(() => {
    const map = new Map<string, AccessLevel>()
    for (const c of m.data?.cells ?? []) map.set(key(c.login, c.database), c.level)
    return map
  }, [m.data])

  const change = async (login: string, database: string, level: AccessLevel) => {
    const k = key(login, database)
    const prev = levels.get(k) ?? 'none'
    if (prev === level) return
    const apply = (lv: AccessLevel) => m.setData(d => {
      if (!d) return d
      const cells = (d.cells ?? []).filter(c => !(c.login === login && c.database === database))
      cells.push({login, database, level: lv})
      return {...d, cells}
    })
    apply(level)
    setPending(p => ({...p, [k]: true}))
    try {
      await api.setDatabaseAccess(server.id, login, database, level)
    } catch (e) {
      apply(prev)
      toast.error(e)
    } finally {
      setPending(p => { const n = {...p}; delete n[k]; return n })
    }
  }

  const logins = m.data?.logins ?? []
  const dbs = m.data?.databases ?? []

  return (
    <div className="space-y-4">
      <Card title="Permisos por base de datos" actions={
        <IconButton label="Actualizar" onClick={() => void m.reload()}><RefreshCw size={15} className={m.loading ? 'spin' : ''}/></IconButton>}>
        <ErrorBanner message={m.error} onRetry={() => void m.reload()}/>
        {m.loading && !m.data && <Spinner/>}
        {m.data && (logins.length === 0 || dbs.length === 0) && (
          <EmptyState icon={<Grid3x3 size={28}/>} title="No hay nada que mostrar">
            Se necesita al menos un usuario y una base de datos de usuario para configurar permisos.
          </EmptyState>
        )}
        {m.data && logins.length > 0 && dbs.length > 0 && (
          <div className="overflow-auto rounded-md border border-line">
            <table className="border-collapse text-sm">
              <thead>
                <tr>
                  <th className="sticky left-0 z-10 border-b border-r border-line bg-surface2 px-3 py-2 text-left text-xs font-semibold uppercase tracking-wide text-muted">Usuario \ Base de datos</th>
                  {dbs.map(d => <th key={d} className="min-w-[11rem] border-b border-line bg-surface2 px-3 py-2 text-left text-xs font-semibold">{d}</th>)}
                </tr>
              </thead>
              <tbody>
                {logins.map(l => (
                  <tr key={l}>
                    <th scope="row" className="sticky left-0 z-10 whitespace-nowrap border-b border-r border-line bg-surface px-3 py-2 text-left font-medium">{l}</th>
                    {dbs.map(d => {
                      const k = key(l, d)
                      const lv = levels.get(k) ?? 'none'
                      return (
                        <td key={d} className="border-b border-line p-1.5">
                          <select aria-label={`Acceso de ${l} a ${d}`} value={lv} disabled={!!pending[k]}
                            onChange={e => void change(l, d, e.target.value as AccessLevel)}
                            className={cx('h-8 w-full rounded border border-line bg-surface px-2 text-xs disabled:opacity-60', LEVELS.find(x => x.value === lv)?.tone)}>
                            {LEVELS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
                          </select>
                        </td>
                      )
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Card title="Leyenda de niveles">
        <dl className="grid gap-3 sm:grid-cols-2">
          {LEVELS.map(l => (
            <div key={l.value}>
              <dt className={cx('font-medium', l.tone)}>{l.label}</dt>
              <dd className="text-xs text-muted">{server.engine === 'mssql' ? l.mssql : l.pg}</dd>
            </div>
          ))}
        </dl>
      </Card>
    </div>
  )
}

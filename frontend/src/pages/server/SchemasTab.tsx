import {useEffect, useMemo, useState} from 'react'
import {AlertTriangle, ChevronDown, ChevronRight, Layers, Plus, RefreshCw, UserPlus} from 'lucide-react'
import {api, errMsg, type AccessLevel, type ObjectGrant, type Privilege, type SchemaInfo, type Server} from '../../api'
import {useAsync} from '../../hooks/useAsync'
import {useToast} from '../../components/Toast'
import {
  Badge, Button, Card, Checkbox, cx, EmptyState, ErrorBanner, Field, IconButton, Input, Modal, Notice, Select, Spinner,
} from '../../components/ui'
import {LEVELS} from './PermissionsTab'

const NAME_RE = /^[A-Za-z_][A-Za-z0-9_-]{0,127}$/
const ALL_PRIVS: Privilege[] = ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'EXECUTE', 'ALTER']
const TABLE_PRIVS: Privilege[] = ['SELECT', 'INSERT', 'UPDATE', 'DELETE']
const LETTER: Record<Privilege, string> = {SELECT: 'L', INSERT: 'I', UPDATE: 'M', DELETE: 'B', EXECUTE: 'E', ALTER: 'D'}
const VERB: Record<Privilege, string> = {
  SELECT: 'leer', INSERT: 'insertar', UPDATE: 'modificar', DELETE: 'borrar', EXECUTE: 'ejecutar procedimientos/funciones', ALTER: 'crear/alterar/borrar objetos (DDL)',
}
const LABEL: Record<Privilege, {t: string; short: string}> = {
  SELECT: {t: 'Leer', short: 'Leer'},
  INSERT: {t: 'Insertar', short: 'Insertar'},
  UPDATE: {t: 'Modificar', short: 'Modificar'},
  DELETE: {t: 'Borrar', short: 'Borrar'},
  EXECUTE: {t: 'Ejecutar procedimientos/funciones', short: 'Ejecutar'},
  ALTER: {t: 'Crear/alterar/borrar objetos (DDL)', short: 'DDL'},
}
const sortPrivs = (p: Privilege[]) => ALL_PRIVS.filter(x => p.includes(x))

export default function SchemasTab({server}: {server: Server}) {
  const toast = useToast()
  const dbs = useAsync(async () => (await api.listDatabases(server.id)).filter(d => !d.system), [server.id])
  const [db, setDb] = useState('')
  useEffect(() => {
    const names = (dbs.data ?? []).map(d => d.name)
    if (names.length && !names.includes(db)) setDb(names[0])
  }, [dbs.data, db])

  return (
    <div className="space-y-4">
      <Card title="Base de datos">
        <ErrorBanner message={dbs.error} onRetry={() => void dbs.reload()}/>
        {dbs.loading && !dbs.data && <Spinner/>}
        {dbs.data && dbs.data.length === 0 && <EmptyState icon={<Layers size={28}/>} title="No hay bases de datos">Crea una base de datos para administrar sus esquemas.</EmptyState>}
        {dbs.data && dbs.data.length > 0 && (
          <Select aria-label="Base de datos" className="max-w-xs" value={db} onChange={e => setDb(e.target.value)}>
            {dbs.data.map(d => <option key={d.name} value={d.name}>{d.name}</option>)}
          </Select>
        )}
      </Card>
      {db && <DbSchemas key={db} server={server} db={db} toast={toast}/>}
    </div>
  )
}

function DbSchemas({server, db, toast}: {server: Server; db: string; toast: ReturnType<typeof useToast>}) {
  const schemasQ = useAsync(() => api.listSchemas(server.id, db), [server.id, db])
  const grantsQ = useAsync(() => api.listObjectGrants(server.id, db), [server.id, db])
  const matrixQ = useAsync(() => api.getPermissionMatrix(server.id), [server.id, db])
  const loginsQ = useAsync(() => api.listLogins(server.id), [server.id])
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const [creating, setCreating] = useState(false)
  const [extra, setExtra] = useState<string[]>([])
  const [adding, setAdding] = useState(false)
  const [edit, setEdit] = useState<{login: string; schema: SchemaInfo} | null>(null)
  const [who, setWho] = useState('')
  const [fixing, setFixing] = useState('')

  const isRoot = (l: string) => l.toLowerCase() === server.adminUser.toLowerCase()
  const schemas = useMemo(() => (schemasQ.data ?? []).filter(s => !s.system), [schemasQ.data])
  const grants = grantsQ.data ?? []
  const levelOf = useMemo(() => {
    const m = new Map<string, AccessLevel>()
    for (const c of matrixQ.data?.cells ?? []) if (c.database === db) m.set(c.login, c.level)
    return m
  }, [matrixQ.data, db])

  const users = useMemo(() => {
    const set = new Set<string>()
    for (const [l, lv] of levelOf) if (lv !== 'none' && !isRoot(l)) set.add(l)
    for (const g of grantsQ.data ?? []) if (!isRoot(g.login)) set.add(g.login)
    for (const e of extra) if (!isRoot(e)) set.add(e)
    return [...set].sort((a, b) => a.localeCompare(b))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [levelOf, grantsQ.data, extra, server.adminUser])

  useEffect(() => { if (!who && users.length) setWho(users[0]) }, [users, who])

  const reloadAll = async () => { await Promise.all([schemasQ.reload(), grantsQ.reload(), matrixQ.reload()]) }
  const schemaGrant = (l: string, s: string) => grants.find(g => g.login === l && g.schema === s && g.table === '')
  const tableGrants = (l: string, s: string) => grants.filter(g => g.login === l && g.schema === s && g.table !== '' && (g.privileges ?? []).length > 0)

  const makeCustom = async (login: string) => {
    setFixing(login)
    try { await api.setDatabaseAccess(server.id, login, db, 'custom'); toast.success(`«${login}» ahora usa permisos por esquema/tabla.`); await matrixQ.reload() }
    catch (e) { toast.error(e) } finally { setFixing('') }
  }

  const candidates = (loginsQ.data ?? []).filter(l => !l.isSystem && !isRoot(l.name) && !users.includes(l.name))

  return (
    <>
      <Card title="Esquemas" actions={<>
        <IconButton label="Actualizar" onClick={() => void reloadAll()}><RefreshCw size={15} className={schemasQ.loading ? 'spin' : ''}/></IconButton>
        <Button size="sm" variant="primary" icon={<Plus size={14}/>} onClick={() => setCreating(true)}>Crear esquema</Button>
      </>}>
        <ErrorBanner message={schemasQ.error} onRetry={() => void schemasQ.reload()}/>
        {schemasQ.loading && !schemasQ.data && <Spinner/>}
        {schemasQ.data && schemas.length === 0 && <EmptyState icon={<Layers size={28}/>} title="Sin esquemas">Esta base de datos no tiene esquemas propios todavía.</EmptyState>}
        {schemas.length > 0 && (
          <ul className="divide-y divide-line rounded-md border border-line">
            {schemas.map(s => {
              const t = s.tables ?? []
              const o = open[s.name]
              return (
                <li key={s.name} className="px-3 py-2 text-sm">
                  <div className="flex items-center gap-2">
                    <button type="button" aria-label={o ? 'Contraer' : 'Expandir'} disabled={t.length === 0} onClick={() => setOpen(p => ({...p, [s.name]: !o}))}
                      className="text-muted disabled:opacity-30">{o ? <ChevronDown size={15}/> : <ChevronRight size={15}/>}</button>
                    <span className="font-medium">{s.name}</span>
                    <span className="text-xs text-muted">dueño: {s.owner || '—'}</span>
                    <Badge className="ml-auto">{t.length} {t.length === 1 ? 'tabla' : 'tablas'}</Badge>
                  </div>
                  {o && <ul className="ml-7 mt-2 flex flex-wrap gap-1.5">{t.map(n => <li key={n}><Badge>{n}</Badge></li>)}</ul>}
                </li>
              )
            })}
          </ul>
        )}
      </Card>

      <Card title="Permisos por esquema" actions={
        <Button size="sm" icon={<UserPlus size={14}/>} onClick={() => setAdding(true)}>Agregar usuario a la matriz</Button>}>
        <p className="mb-3 text-xs text-muted">
          El usuario root «{server.adminUser}» tiene acceso total a todo y no se muestra. Sin permisos concedidos, un usuario no puede ver ni modificar nada en el esquema.
          Pulsa una celda para editar. Letras: L=Leer, I=Insertar, M=Modificar, B=Borrar, E=Ejecutar, D=DDL (crear/alterar). T = tablas con permisos propios.
        </p>
        <ErrorBanner message={grantsQ.error || matrixQ.error} onRetry={() => void reloadAll()}/>
        {(grantsQ.loading || matrixQ.loading) && !grantsQ.data && <Spinner/>}
        {users.length === 0 && schemas.length > 0 && matrixQ.data && <p className="text-sm text-muted">No hay usuarios con acceso a esta base. Agrega uno a la matriz.</p>}
        {users.length > 0 && schemas.length > 0 && (
          <div className="overflow-auto rounded-md border border-line">
            <table className="border-collapse text-sm">
              <thead><tr>
                <th className="sticky left-0 z-10 border-b border-r border-line bg-surface2 px-3 py-2 text-left text-xs font-semibold uppercase tracking-wide text-muted">Usuario \ Esquema</th>
                {schemas.map(s => <th key={s.name} className="min-w-[8rem] border-b border-line bg-surface2 px-3 py-2 text-left text-xs font-semibold">{s.name}</th>)}
              </tr></thead>
              <tbody>
                {users.map(u => {
                  const lv = levelOf.get(u) ?? 'none'
                  const global = lv === 'owner' || lv === 'readwrite' || lv === 'readonly'
                  const lvInfo = LEVELS.find(x => x.value === lv)
                  return (
                    <tr key={u}>
                      <th scope="row" className="sticky left-0 z-10 border-b border-r border-line bg-surface px-3 py-2 text-left font-medium">
                        <div className="flex items-center gap-2 whitespace-nowrap">
                          {u}
                          <Badge tone={lv === 'custom' ? 'accent' : global ? 'warn' : 'neutral'}>{lvInfo?.label ?? lv}</Badge>
                          {global && (
                            <span title={`Tiene un rol global de la base (${lvInfo?.label}): ya puede ${lv === 'readonly' ? 'leer' : lv === 'owner' ? 'hacer todo' : 'leer y escribir'} en todos los esquemas; para que solo valgan los permisos por esquema cambia su nivel a 'Por esquema/tabla'`}>
                              <AlertTriangle size={14} className="text-warn"/>
                            </span>
                          )}
                          {global && <Button size="sm" loading={fixing === u} onClick={() => void makeCustom(u)}>Usar por esquema</Button>}
                        </div>
                      </th>
                      {schemas.map(s => {
                        const p = sortPrivs(schemaGrant(u, s.name)?.privileges ?? [])
                        const tg = tableGrants(u, s.name)
                        return (
                          <td key={s.name} className="border-b border-line p-1">
                            <button type="button" onClick={() => setEdit({login: u, schema: s})} aria-label={`Permisos de ${u} en ${s.name}`}
                              className="flex min-h-8 w-full items-center gap-1 rounded px-1.5 py-1 text-left hover:bg-surface2">
                              {p.length === 0 ? <span className="text-muted">—</span>
                                : p.map(x => <span key={x} title={VERB[x]} className="rounded bg-accent/10 px-1 text-[11px] font-semibold text-accent">{LETTER[x]}</span>)}
                              {tg.length > 0 && <span title={`${tg.length} tabla(s) con permisos propios`} className="ml-auto text-[11px] font-semibold text-warn">T{tg.length}</span>}
                            </button>
                          </td>
                        )
                      })}
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Summary server={server} users={users} who={who} setWho={setWho} schemas={schemas} grants={grants} level={levelOf.get(who) ?? 'none'}/>

      {creating && <CreateSchemaModal server={server} db={db} onClose={() => setCreating(false)} onDone={async () => { toast.success('Esquema creado.'); await schemasQ.reload() }}/>}
      {adding && (
        <Modal title="Agregar usuario a la matriz" onClose={() => setAdding(false)} footer={<Button onClick={() => setAdding(false)}>Cerrar</Button>}>
          {candidates.length === 0 ? <p className="text-sm text-muted">No hay más usuarios disponibles.</p> : (
            <div className="space-y-2">
              <p className="text-xs text-muted">El usuario recibirá acceso a «{db}» cuando guardes un permiso para él.</p>
              <ul className="divide-y divide-line rounded-md border border-line">
                {candidates.map(l => (
                  <li key={l.name} className="flex items-center justify-between px-3 py-2 text-sm">
                    {l.name}
                    <Button size="sm" onClick={() => { setExtra(e => [...e, l.name]); setWho(l.name); setAdding(false) }}>Agregar</Button>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </Modal>
      )}
      {edit && (
        <GrantModal server={server} db={db} login={edit.login} schema={edit.schema} grants={grants} level={levelOf.get(edit.login) ?? 'none'}
          onClose={() => setEdit(null)} onSaved={async () => { toast.success('Permisos guardados.'); await reloadAll() }}/>
      )}
    </>
  )
}

function CreateSchemaModal({server, db, onClose, onDone}: {server: Server; db: string; onClose: () => void; onDone: () => Promise<void>}) {
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const ok = NAME_RE.test(name)
  const create = async () => {
    setBusy(true); setError('')
    try { await api.createSchema(server.id, db, name); await onDone(); onClose() }
    catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title={`Crear esquema en ${db}`} onClose={onClose} footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" disabled={!ok} loading={busy} onClick={create}>Crear</Button>
    </>}>
      <form className="space-y-4" onSubmit={e => { e.preventDefault(); if (ok && !busy) void create() }}>
        <Field label="Nombre" hint="Empieza con letra o _; letras, números, _ y -. Máx. 128 caracteres." error={name && !ok ? 'Nombre no válido.' : ''}>
          {id => <Input id={id} value={name} autoComplete="off" autoFocus onChange={e => setName(e.target.value)}/>}
        </Field>
        <ErrorBanner message={error}/>
      </form>
    </Modal>
  )
}

const PRESETS: Array<{label: string; privs: Privilege[]}> = [
  {label: 'Ninguno', privs: []},
  {label: 'Solo lectura', privs: ['SELECT']},
  {label: 'Solo insertar (logs)', privs: ['INSERT']},
  {label: 'Lectura/escritura', privs: ['SELECT', 'INSERT', 'UPDATE', 'DELETE']},
  {label: 'Total', privs: ALL_PRIVS},
]

function GrantModal({server, db, login, schema, grants, level, onClose, onSaved}: {
  server: Server; db: string; login: string; schema: SchemaInfo; grants: ObjectGrant[]; level: AccessLevel
  onClose: () => void; onSaved: () => Promise<void>
}) {
  const pg = server.engine === 'postgres'
  const tables = schema.tables ?? []
  const cur = (t: string) => sortPrivs(grants.find(g => g.login === login && g.schema === schema.name && g.table === t)?.privileges ?? [])
  const [sp, setSp] = useState<Privilege[]>(() => cur(''))
  const [tp, setTp] = useState<Record<string, Privilege[]>>(() => Object.fromEntries(tables.map(t => [t, cur(t)])))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const tablePrivs: Privilege[] = pg ? TABLE_PRIVS : [...TABLE_PRIVS, 'ALTER']
  const same = (a: Privilege[], b: Privilege[]) => a.length === b.length && a.every(x => b.includes(x))
  const toggle = (arr: Privilege[], p: Privilege) => sortPrivs(arr.includes(p) ? arr.filter(x => x !== p) : [...arr, p])

  const save = async () => {
    setBusy(true); setError('')
    try {
      if (level === 'none') await api.setDatabaseAccess(server.id, login, db, 'custom')
      if (!same(sp, cur(''))) await api.setObjectGrant(server.id, db, {login, schema: schema.name, table: '', privileges: sp})
      for (const t of tables) {
        const v = tp[t] ?? []
        if (!same(v, cur(t))) await api.setObjectGrant(server.id, db, {login, schema: schema.name, table: t, privileges: v})
      }
      await onSaved(); onClose()
    } catch (e) { setError(errMsg(e)); setBusy(false) }
  }

  return (
    <Modal title={`Permisos de ${login} en ${schema.name}`} width="max-w-2xl" onClose={onClose} footer={<>
      <Button onClick={onClose} disabled={busy}>Cancelar</Button>
      <Button variant="primary" loading={busy} onClick={save}>Guardar</Button>
    </>}>
      <div className="space-y-4">
        {level === 'none' && <Notice tone="accent">«{login}» aún no tiene acceso a {db}; se le dará acceso «Por esquema/tabla» al guardar.</Notice>}
        <div>
          <p className="mb-2 text-sm font-medium">Permisos en todo el esquema</p>
          <div className="mb-3 flex flex-wrap gap-2">
            {PRESETS.map(p => <Button key={p.label} size="sm" onClick={() => setSp(p.privs)}>{p.label}</Button>)}
          </div>
          <div className="space-y-2">
            {ALL_PRIVS.map(p => (
              <Checkbox key={p} label={LABEL[p].t} checked={sp.includes(p)} onChange={() => setSp(toggle(sp, p))}
                hint={`${p}${pg && p !== 'EXECUTE' && p !== 'ALTER' ? ' · se aplica a todas las tablas actuales y futuras del esquema' : ''}`}/>
            ))}
          </div>
        </div>
        <div>
          <p className="mb-2 text-sm font-medium">Permisos por tabla</p>
          {tables.length === 0 ? <p className="text-sm text-muted">El esquema no tiene tablas.</p> : (
            <div className="overflow-auto rounded-md border border-line">
              <table className="w-full border-collapse text-sm">
                <thead><tr>
                  <th className="border-b border-line bg-surface2 px-3 py-2 text-left text-xs font-semibold">Tabla</th>
                  {tablePrivs.map(p => <th key={p} title={p} className="border-b border-line bg-surface2 px-2 py-2 text-xs font-semibold">{LABEL[p].short}</th>)}
                </tr></thead>
                <tbody>{tables.map(t => (
                  <tr key={t}>
                    <td className="border-b border-line px-3 py-1.5 font-mono text-xs">{t}</td>
                    {tablePrivs.map(p => (
                      <td key={p} className="border-b border-line px-2 py-1.5 text-center">
                        <input type="checkbox" aria-label={`${LABEL[p].short} en ${t}`} checked={(tp[t] ?? []).includes(p)}
                          onChange={() => setTp(s => ({...s, [t]: toggle(s[t] ?? [], p)}))}/>
                      </td>
                    ))}
                  </tr>
                ))}</tbody>
              </table>
            </div>
          )}
        </div>
        <ErrorBanner message={error}/>
      </div>
    </Modal>
  )
}

function Summary({server, users, who, setWho, schemas, grants, level}: {
  server: Server; users: string[]; who: string; setWho: (v: string) => void
  schemas: SchemaInfo[]; grants: ObjectGrant[]; level: AccessLevel
}) {
  const lvInfo = LEVELS.find(l => l.value === level)
  const global = level === 'owner' || level === 'readwrite' || level === 'readonly'
  return (
    <Card title="¿Qué puede hacer cada usuario?">
      <p className="mb-3 text-xs text-muted">
        Sin permisos concedidos en un esquema, el usuario no puede ver ni modificar nada en él. El usuario root «{server.adminUser}» puede hacerlo todo.
      </p>
      {users.length === 0 ? <p className="text-sm text-muted">No hay usuarios para mostrar.</p> : (
        <div className="space-y-3">
          <Select aria-label="Usuario" className="max-w-xs" value={who} onChange={e => setWho(e.target.value)}>
            {users.map(u => <option key={u} value={u}>{u}</option>)}
          </Select>
          <p className="text-sm">Nivel en la base: <Badge tone={level === 'custom' ? 'accent' : 'neutral'}>{lvInfo?.label ?? level}</Badge></p>
          {global && <Notice>Por su nivel «{lvInfo?.label}» ya tiene permisos en todos los esquemas, además de lo indicado abajo.</Notice>}
          <ul className="space-y-2 text-sm">
            {schemas.map(s => {
              const sp = sortPrivs(grants.find(g => g.login === who && g.schema === s.name && g.table === '')?.privileges ?? [])
              const no = ALL_PRIVS.filter(p => !sp.includes(p))
              const tg = grants.filter(g => g.login === who && g.schema === s.name && g.table !== '' && (g.privileges ?? []).length > 0)
              return (
                <li key={s.name} className="rounded-md border border-line px-3 py-2">
                  <p className="font-medium">{s.name}</p>
                  <p className={cx(sp.length ? 'text-success' : 'text-muted')}>Puede: {sp.length ? sp.map(p => VERB[p]).join(', ') : 'nada'}</p>
                  {sp.length > 0 && no.length > 0 && <p className="text-muted">No puede: {no.map(p => VERB[p]).join(', ')}</p>}
                  {tg.map(g => <p key={g.table} className="text-xs">Tabla {s.name}.{g.table}: {sortPrivs(g.privileges ?? []).map(p => VERB[p]).join(', ')}</p>)}
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </Card>
  )
}

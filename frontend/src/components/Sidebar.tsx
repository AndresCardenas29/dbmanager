import {Database, FileKey, FolderSync, History, Home, Import, Moon, Plus, Settings, Sun, Globe, Monitor, ShieldCheck, ListChecks} from 'lucide-react'
import type {ReactNode} from 'react'
import {useApp} from '../store'
import {Badge, cx, engineLabel, IconButton} from './ui'
import type {ServerState} from '../api'

const dot: Record<ServerState, {cls: string; label: string}> = {
  running: {cls: 'bg-success', label: 'En ejecución'},
  stopped: {cls: 'bg-muted', label: 'Detenido'},
  missing: {cls: 'bg-danger', label: 'Contenedor no encontrado'},
  remote: {cls: 'bg-accent', label: 'Remoto'},
}

export function StatusDot({state}: {state?: ServerState}) {
  const d = state ? dot[state] : undefined
  return <span role="img" aria-label={d?.label ?? 'Desconocido'} title={d?.label ?? 'Comprobando...'}
    className={cx('inline-block h-2.5 w-2.5 shrink-0 rounded-full', d?.cls ?? 'bg-line')}/>
}

export const EngineBadge = ({engine}: {engine: string}) =>
  <Badge tone={engine === 'mssql' ? 'warn' : 'accent'}>{engineLabel(engine)}</Badge>

function NavBtn({active, onClick, icon, children}: {active?: boolean; onClick: () => void; icon: ReactNode; children: ReactNode}) {
  return (
    <button onClick={onClick} aria-current={active ? 'page' : undefined}
      className={cx('flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm',
        active ? 'bg-accent/10 font-medium text-accent' : 'text-fg hover:bg-surface2')}>
      {icon}{children}
    </button>
  )
}

export default function Sidebar() {
  const {view, go, servers, serversLoaded, statuses, sftpServices, sftpStatuses, theme, setTheme} = useApp()
  const locals = servers.filter(s => s.mode === 'local')
  const remotes = servers.filter(s => s.mode === 'remote')
  const selected = view.page === 'server' ? view.id : -1
  const selSftp = view.page === 'sftp' ? view.id : -1

  const group = (title: string, list: typeof servers) => (
    <div className="mb-3">
      <h2 className="px-2.5 pb-1 text-[11px] font-semibold uppercase tracking-wider text-muted">{title}</h2>
      {list.length === 0
        ? <p className="px-2.5 py-1 text-xs text-muted">{serversLoaded ? 'Ninguno todavía' : 'Cargando...'}</p>
        : list.map(s => (
          <button key={s.id} onClick={() => go({page: 'server', id: s.id})} aria-current={selected === s.id ? 'page' : undefined}
            className={cx('flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left',
              selected === s.id ? 'bg-accent/10' : 'hover:bg-surface2')}>
            <StatusDot state={s.mode === 'remote' ? 'remote' : statuses[s.id]?.state}/>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium">{s.name}</span>
              <span className="block truncate text-xs text-muted">{s.host || 'localhost'}:{s.port}</span>
            </span>
            <EngineBadge engine={s.engine}/>
          </button>
        ))}
    </div>
  )

  const Section = ({icon, title, children}: {icon: ReactNode; title: string; children: ReactNode}) => (
    <section className="mb-4">
      <h2 className="mb-1 flex items-center gap-2 px-2.5 text-xs font-semibold uppercase tracking-wider text-fg">{icon}{title}</h2>
      {children}
    </section>
  )
  const next = theme === 'dark' ? 'light' : theme === 'light' ? 'system' : 'dark'
  return (
    <aside className="flex h-full w-72 shrink-0 flex-col border-r border-line bg-surface">
      <div className="flex items-center justify-between px-4 py-3">
        <button onClick={() => go({page: 'home'})} className="flex items-center gap-2 font-semibold">
          <Database size={20} className="text-accent"/> DB Manager
        </button>
        <IconButton label={`Tema: ${theme === 'system' ? 'automático' : theme === 'dark' ? 'oscuro' : 'claro'} (cambiar)`}
          onClick={() => setTheme(next)}>
          {theme === 'dark' ? <Moon size={16}/> : theme === 'light' ? <Sun size={16}/> : <Monitor size={16}/>}
        </IconButton>
      </div>
      <nav className="flex-1 overflow-y-auto px-2 pb-2" aria-label="Secciones">
        <NavBtn active={view.page === 'home'} onClick={() => go({page: 'home'})} icon={<Home size={16}/>}>Inicio</NavBtn>
        <div className="mt-3">
          <Section icon={<Database size={14} className="text-accent"/>} title="Bases de datos">
            {group('Locales', locals)}{group('Remotos', remotes)}
            <NavBtn active={view.page === 'new'} onClick={() => go({page: 'new'})} icon={<Plus size={16}/>}>Nuevo servicio</NavBtn>
            <NavBtn active={view.page === 'import'} onClick={() => go({page: 'import'})} icon={<Import size={16}/>}>Importar contenedor</NavBtn>
            <NavBtn active={view.page === 'remote'} onClick={() => go({page: 'remote'})} icon={<Globe size={16}/>}>Conectar servidor remoto</NavBtn>
          </Section>
          <Section icon={<FolderSync size={14} className="text-accent"/>} title="SFTP">
            <div className="mb-1">
              {sftpServices.length === 0
                ? <p className="px-2.5 py-1 text-xs text-muted">Ningún servicio SFTP</p>
                : sftpServices.map(s => (
                  <button key={s.id} onClick={() => go({page: 'sftp', id: s.id})} aria-current={selSftp === s.id ? 'page' : undefined}
                    className={cx('flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left', selSftp === s.id ? 'bg-accent/10' : 'hover:bg-surface2')}>
                    <StatusDot state={sftpStatuses[s.id]?.state}/>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">{s.name}</span>
                      <span className="block truncate text-xs text-muted">{s.host || 'localhost'}:{s.port}</span>
                    </span>
                    {s.mode === 'external' && <Badge>externo</Badge>}
                  </button>
                ))}
            </div>
            <NavBtn active={view.page === 'sftpNew'} onClick={() => go({page: 'sftpNew'})} icon={<Plus size={16}/>}>Nuevo servicio SFTP</NavBtn>
            <NavBtn active={view.page === 'sftpImport'} onClick={() => go({page: 'sftpImport'})} icon={<Import size={16}/>}>Importar contenedor SFTP</NavBtn>
          </Section>
          <Section icon={<FileKey size={14} className="text-accent"/>} title="Certificados">
            <NavBtn active={view.page === 'certP12'} onClick={() => go({page: 'certP12'})} icon={<FileKey size={16}/>}>Generar .p12</NavBtn>
            <NavBtn active={view.page === 'certVerify'} onClick={() => go({page: 'certVerify'})} icon={<ShieldCheck size={16}/>}>Verificar .p12</NavBtn>
            <NavBtn active={view.page === 'certHistory'} onClick={() => go({page: 'certHistory'})} icon={<ListChecks size={16}/>}>Historial</NavBtn>
          </Section>
        </div>
      </nav>
      <div className="flex gap-1 border-t border-line p-2">
        <NavBtn active={view.page === 'history'} onClick={() => go({page: 'history'})} icon={<History size={16}/>}>Historial</NavBtn>
        <NavBtn active={view.page === 'settings'} onClick={() => go({page: 'settings'})} icon={<Settings size={16}/>}>Ajustes</NavBtn>
      </div>
    </aside>
  )
}

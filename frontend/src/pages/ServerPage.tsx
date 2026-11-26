import {useEffect, useState} from 'react'
import {Archive, Database, FileText, Grid3x3, Info, RotateCcw, Users} from 'lucide-react'
import {useApp, type TabKey} from '../store'
import {Card, EmptyState, PageHeader, Tabs} from '../components/ui'
import {EngineBadge, StatusDot} from '../components/Sidebar'
import SummaryTab from './server/SummaryTab'
import DatabasesTab from './server/DatabasesTab'
import UsersTab from './server/UsersTab'
import PermissionsTab from './server/PermissionsTab'
import BackupsTab from './server/BackupsTab'
import RestoresTab from './server/RestoresTab'
import LogsTab from './server/LogsTab'

export default function ServerPage({id, tab, restoreFile}: {id: number; tab?: TabKey; restoreFile?: string}) {
  const {servers, serversLoaded, statuses} = useApp()
  const [current, setCurrent] = useState<TabKey>(tab ?? 'summary')
  const [pendingRestore, setPendingRestore] = useState(restoreFile)
  const server = servers.find(s => s.id === id)

  useEffect(() => { if (tab) setCurrent(tab) }, [tab])

  if (!server) {
    return serversLoaded
      ? <Card><EmptyState title="Servidor no encontrado">Puede haber sido eliminado. Elige otro en la barra lateral.</EmptyState></Card>
      : null
  }
  const local = server.mode === 'local'
  const st = statuses[id]
  const state = local ? st?.state : 'remote'
  const tabs: Array<{key: TabKey; label: string; icon: React.ReactNode}> = [
    {key: 'summary', label: 'Resumen', icon: <Info size={15}/>},
    {key: 'databases', label: 'Bases de datos', icon: <Database size={15}/>},
    {key: 'users', label: 'Usuarios', icon: <Users size={15}/>},
    {key: 'permissions', label: 'Permisos', icon: <Grid3x3 size={15}/>},
    {key: 'backups', label: 'Backups', icon: <Archive size={15}/>},
    {key: 'restores', label: 'Restauraciones', icon: <RotateCcw size={15}/>},
    ...(local ? [{key: 'logs' as TabKey, label: 'Logs', icon: <FileText size={15}/>}] : []),
  ]
  const active = tabs.some(t => t.key === current) ? current : 'summary'
  const needsRunning = local && state && state !== 'running' && active !== 'summary' && active !== 'logs'

  return (
    <div>
      <PageHeader
        title={<span className="flex items-center gap-2"><StatusDot state={state}/>{server.name}</span>}
        subtitle={<span className="flex items-center gap-2"><EngineBadge engine={server.engine}/>{server.host || 'localhost'}:{server.port} · {local ? 'Local (Docker)' : 'Remoto'}</span>}/>
      <Tabs tabs={tabs} value={active} onChange={setCurrent}/>
      <div className="pt-5">
        {active === 'summary' && <SummaryTab server={server}/>}
        {needsRunning
          ? <Card><EmptyState title="El servidor no está en ejecución">Inícialo desde la pestaña Resumen para usar esta sección.</EmptyState></Card>
          : <>
            {active === 'databases' && <DatabasesTab server={server}/>}
            {active === 'users' && <UsersTab server={server}/>}
            {active === 'permissions' && <PermissionsTab server={server}/>}
            {active === 'backups' && <BackupsTab server={server} onRestore={f => { setPendingRestore(f); setCurrent('restores') }}/>}
            {active === 'restores' && <RestoresTab server={server} initialFile={pendingRestore} onConsumed={() => setPendingRestore(undefined)}/>}
          </>}
        {active === 'logs' && <LogsTab server={server}/>}
      </div>
    </div>
  )
}

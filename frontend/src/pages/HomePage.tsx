import {AlertTriangle, CheckCircle2, Database, FileKey, FolderSync, ShieldCheck, Globe, Import, Plus, RefreshCw, Server as ServerIcon} from 'lucide-react'
import {useState} from 'react'
import {useApp} from '../store'
import {Button, Card, ErrorBanner, Notice, PageHeader, Spinner} from '../components/ui'
import {EngineBadge, StatusDot} from '../components/Sidebar'

function DockerHelp() {
  return (
    <Card title="Docker no está disponible">
      <p className="mb-4 text-sm">DB Manager necesita Docker para crear y gestionar servidores locales. Instálalo o inícialo según tu sistema:</p>
      <div className="grid gap-4 md:grid-cols-3">
        <div className="rounded-md border border-line p-3">
          <h4 className="mb-1 font-semibold">Windows</h4>
          <ol className="list-decimal space-y-1 pl-5 text-sm text-muted">
            <li>Instala <b className="text-fg">Docker Desktop</b> desde docker.com.</li>
            <li>Ábrelo y espera a que indique «Engine running».</li>
            <li>Si se te pide, habilita WSL 2 y reinicia el equipo.</li>
          </ol>
        </div>
        <div className="rounded-md border border-line p-3">
          <h4 className="mb-1 font-semibold">macOS</h4>
          <ol className="list-decimal space-y-1 pl-5 text-sm text-muted">
            <li>Instala <b className="text-fg">Docker Desktop</b> (Apple Silicon o Intel).</li>
            <li>Ábrelo desde Aplicaciones y espera a que termine de iniciar.</li>
            <li>En Mac con chip Apple, SQL Server requiere Rosetta.</li>
          </ol>
        </div>
        <div className="rounded-md border border-line p-3">
          <h4 className="mb-1 font-semibold">Linux</h4>
          <ol className="list-decimal space-y-1 pl-5 text-sm text-muted">
            <li>Instala <b className="text-fg">Docker Engine</b> desde la documentación oficial.</li>
            <li>Inícialo: <code className="font-mono text-fg">sudo systemctl enable --now docker</code></li>
            <li>Permite tu usuario: <code className="font-mono text-fg">sudo usermod -aG docker $USER</code> y vuelve a iniciar sesión.</li>
          </ol>
        </div>
      </div>
      <p className="mt-4 text-sm text-muted">Mientras tanto, aún puedes conectar servidores remotos.</p>
    </Card>
  )
}

export default function HomePage() {
  const {env, envError, recheckEnv, servers, statuses, sftpServices, go} = useApp()
  const [checking, setChecking] = useState(false)
  const recheck = async () => { setChecking(true); await recheckEnv(); setChecking(false) }

  return (
    <div>
      <PageHeader title="Inicio" subtitle="Gestiona SQL Server y PostgreSQL en Docker o en servidores remotos."
        actions={<Button icon={<RefreshCw size={15}/>} loading={checking} onClick={recheck}>Volver a comprobar</Button>}/>
      <div className="space-y-4">
        {envError && <ErrorBanner message={envError} onRetry={recheck}/>}
        {!env && !envError && <Spinner label="Comprobando entorno..."/>}
        {env && !env.dockerAvailable && <DockerHelp/>}
        {env?.dockerAvailable && (
          <Card>
            <div className="flex items-center gap-3">
              <CheckCircle2 className="text-success" size={22}/>
              <div>
                <p className="font-medium">Docker disponible</p>
                <p className="text-sm text-muted">Versión {env.dockerVersion} · {env.os}/{env.arch}</p>
              </div>
            </div>
          </Card>
        )}
        {env?.rosettaWarning && (
          <Notice tone="warn">
            <div className="flex gap-2"><AlertTriangle size={18} className="mt-0.5 shrink-0 text-warn"/>
              <p><b>Rosetta recomendado.</b> Las imágenes de SQL Server son <code className="font-mono">linux/amd64</code>. En Mac con chip Apple,
                activa «Use Rosetta for x86_64/amd64 emulation» en los ajustes de Docker Desktop (o ejecuta <code className="font-mono">softwareupdate --install-rosetta</code>).</p></div>
          </Notice>
        )}
        {env?.messages?.filter(Boolean).map((m, i) => <Notice key={i} tone="accent">{m}</Notice>)}

                <div className="grid gap-4 md:grid-cols-3">
          <Card title={<span className="flex items-center gap-2"><Database size={16} className="text-accent"/>Bases de datos</span>}>
            <p className="mb-3 text-sm text-muted">SQL Server y PostgreSQL en Docker o remotos. {servers.length} servidor(es) registrado(s).</p>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="primary" icon={<Plus size={14}/>} onClick={() => go({page: 'new'})}>Nuevo servicio</Button>
              <Button size="sm" icon={<Globe size={14}/>} onClick={() => go({page: 'remote'})}>Remoto</Button>
            </div>
          </Card>
          <Card title={<span className="flex items-center gap-2"><FolderSync size={16} className="text-accent"/>SFTP</span>}>
            <p className="mb-3 text-sm text-muted">Servidores SFTP en Docker con usuarios y llaves. {sftpServices.length} servicio(s).</p>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="primary" icon={<Plus size={14}/>} onClick={() => go({page: 'sftpNew'})}>Nuevo servicio SFTP</Button>
              <Button size="sm" icon={<Import size={14}/>} onClick={() => go({page: 'sftpImport'})}>Importar</Button>
            </div>
          </Card>
          <Card title={<span className="flex items-center gap-2"><FileKey size={16} className="text-accent"/>Certificados</span>}>
            <p className="mb-3 text-sm text-muted">Genera y verifica keystores PKCS#12 (.p12) para Tomcat y Java.</p>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="primary" icon={<FileKey size={14}/>} onClick={() => go({page: 'certP12'})}>Generar .p12</Button>
              <Button size="sm" icon={<ShieldCheck size={14}/>} onClick={() => go({page: 'certVerify'})}>Verificar</Button>
            </div>
          </Card>
        </div>

        <Card title="Servidores de bases de datos" actions={<>
          <Button size="sm" icon={<Plus size={14}/>} variant="primary" onClick={() => go({page: 'new'})}>Nuevo servicio</Button>
        </>}>
          {servers.length === 0 ? (
            <div className="py-6 text-center">
              <ServerIcon size={32} className="mx-auto mb-2 text-muted"/>
              <p className="font-medium">Aún no hay servidores</p>
              <p className="mb-4 text-sm text-muted">Crea un servicio nuevo, importa un contenedor existente o conecta un servidor remoto.</p>
              <div className="flex flex-wrap justify-center gap-2">
                <Button variant="primary" icon={<Plus size={15}/>} onClick={() => go({page: 'new'})}>Nuevo servicio</Button>
                <Button icon={<Import size={15}/>} onClick={() => go({page: 'import'})}>Importar contenedor</Button>
                <Button icon={<Globe size={15}/>} onClick={() => go({page: 'remote'})}>Conectar servidor remoto</Button>
              </div>
            </div>
          ) : (
            <ul className="grid gap-2 md:grid-cols-2">
              {servers.map(s => (
                <li key={s.id}>
                  <button onClick={() => go({page: 'server', id: s.id})}
                    className="flex w-full items-center gap-3 rounded-md border border-line p-3 text-left hover:bg-surface2">
                    <StatusDot state={s.mode === 'remote' ? 'remote' : statuses[s.id]?.state}/>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-medium">{s.name}</span>
                      <span className="block truncate text-xs text-muted">{s.host || 'localhost'}:{s.port} · {s.mode === 'local' ? 'Local' : 'Remoto'}</span>
                    </span>
                    <EngineBadge engine={s.engine}/>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>
    </div>
  )
}

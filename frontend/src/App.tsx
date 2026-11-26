import {AppProvider, useApp} from './store'
import {ToastProvider} from './components/Toast'
import Sidebar from './components/Sidebar'
import HomePage from './pages/HomePage'
import NewServicePage from './pages/NewServicePage'
import ImportPage from './pages/ImportPage'
import RemotePage from './pages/RemotePage'
import ServerPage from './pages/ServerPage'
import HistoryPage from './pages/HistoryPage'
import SettingsPage from './pages/SettingsPage'
import SftpNewPage from './pages/sftp/SftpNewPage'
import SftpImportPage from './pages/sftp/SftpImportPage'
import SftpServicePage from './pages/sftp/SftpServicePage'
import CertP12Page from './pages/cert/CertP12Page'
import CertVerifyPage from './pages/cert/CertVerifyPage'
import CertHistoryPage from './pages/cert/CertHistoryPage'

function Router() {
  const {view} = useApp()
  switch (view.page) {
    case 'home': return <HomePage/>
    case 'new': return <NewServicePage/>
    case 'import': return <ImportPage/>
    case 'remote': return <RemotePage/>
    case 'server': return <ServerPage key={view.id} id={view.id} tab={view.tab} restoreFile={view.restoreFile}/>
    case 'sftpNew': return <SftpNewPage/>
    case 'sftpImport': return <SftpImportPage/>
    case 'sftp': return <SftpServicePage key={view.id} id={view.id} tab={view.tab}/>
    case 'certP12': return <CertP12Page/>
    case 'certVerify': return <CertVerifyPage/>
    case 'certHistory': return <CertHistoryPage/>
    case 'history': return <HistoryPage/>
    case 'settings': return <SettingsPage/>
  }
}

export default function App() {
  return (
    <ToastProvider>
      <AppProvider>
        <div className="flex h-full">
          <Sidebar/>
          <main className="min-w-0 flex-1 overflow-y-auto">
            <div className="mx-auto max-w-6xl p-6"><Router/></div>
          </main>
        </div>
      </AppProvider>
    </ToastProvider>
  )
}

import { App as AntApp } from 'antd'
import { Navigate, Route, Routes } from 'react-router-dom'
import { DeviceStateProvider } from './state/DeviceStateProvider'
import { AppLayout } from './components/AppLayout'
import { DashboardPage } from './pages/DashboardPage'
import { HistoryPage } from './pages/HistoryPage'
import { ProfilesPage } from './pages/ProfilesPage'
import { EventsPage } from './pages/EventsPage'
import { AutomationPage } from './pages/AutomationPage'
import { SequencesPage } from './pages/SequencesPage'
import { ChargePage } from './pages/ChargePage'
import { SettingsPage } from './pages/SettingsPage'

function App() {
  return (
    <AntApp>
      <DeviceStateProvider>
        <Routes>
          <Route element={<AppLayout />}>
            <Route index element={<DashboardPage />} />
            <Route path="history" element={<HistoryPage />} />
            <Route path="profiles" element={<ProfilesPage />} />
            <Route path="events" element={<EventsPage />} />
            <Route path="automation" element={<AutomationPage />} />
            <Route path="sequences" element={<SequencesPage />} />
            <Route path="charge" element={<ChargePage />} />
            <Route path="settings" element={<SettingsPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </DeviceStateProvider>
    </AntApp>
  )
}

export default App

import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { RequireAuth, RequireGuest } from './components/RouteGuards';
import { AppShell } from './components/AppShell';

import LoginPage from './routes/login';
import RegisterPage from './routes/register';
import ForgotPasswordPage from './routes/forgot-password';
import ResetPasswordPage from './routes/reset-password';
import DashboardPage from './routes/dashboard';
import NetworksIndexPage from './routes/networks/index';
import NetworkDetailPage from './routes/networks/[id]';
import DevicesIndexPage from './routes/devices/index';
import AuditLogsPage from './routes/logs/audit';
import ConnectionLogsPage from './routes/logs/connections';
import AccountSettingsPage from './routes/settings/account';
import NotFoundPage from './routes/not-found';

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<RequireGuest />}>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/register" element={<RegisterPage />} />
          <Route path="/forgot-password" element={<ForgotPasswordPage />} />
          <Route path="/reset-password" element={<ResetPasswordPage />} />
        </Route>

        <Route element={<RequireAuth />}>
          <Route element={<AppShell />}>
            <Route path="/dashboard" element={<DashboardPage />} />
            <Route path="/networks" element={<NetworksIndexPage />} />
            <Route path="/networks/:id" element={<NetworkDetailPage />} />
            <Route path="/devices" element={<DevicesIndexPage />} />
            <Route path="/logs/audit" element={<AuditLogsPage />} />
            <Route path="/logs/connections" element={<ConnectionLogsPage />} />
            <Route path="/settings/account" element={<AccountSettingsPage />} />
          </Route>
        </Route>

        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </BrowserRouter>
  );
}

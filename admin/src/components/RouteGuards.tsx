import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '@/lib/auth-context';
import { Spinner } from './Feedback';

function FullPageSpinner() {
  return (
    <div className="flex h-screen items-center justify-center bg-gray-50 dark:bg-gray-950">
      <Spinner label="Loading NexusVPN Admin…" />
    </div>
  );
}

/** Only reachable when signed in; otherwise redirect to /login preserving intended destination. */
export function RequireAuth() {
  const { isAuthenticated, isBootstrapping } = useAuth();
  const location = useLocation();

  if (isBootstrapping) return <FullPageSpinner />;
  if (!isAuthenticated) {
    return <Navigate to="/login" replace state={{ from: location }} />;
  }
  return <Outlet />;
}

/** Only reachable when signed out (login/register/forgot/reset); otherwise redirect to /dashboard. */
export function RequireGuest() {
  const { isAuthenticated, isBootstrapping } = useAuth();

  if (isBootstrapping) return <FullPageSpinner />;
  if (isAuthenticated) {
    return <Navigate to="/dashboard" replace />;
  }
  return <Outlet />;
}

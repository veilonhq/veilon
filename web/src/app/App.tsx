import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MotionConfig } from 'framer-motion';
import { useEffect } from 'react';
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { ApiError } from '@/api/client';
import { prefetchPageData, useMe } from '@/api/hooks';
import { Backdrop } from '@/components/Backdrop';
import { ConfirmHost, Toaster } from '@/components/Feedback';
import { Shell } from '@/components/Shell';
import { Guard } from '@/components/Guard';
import { Skeleton } from '@/components/misc';
import { lazyPage } from '@/components/RouteBoundary';
import { useSession } from '@/store/session';
import { BASENAME } from './base';
import LoginPage from '@/pages/Login';
import DashboardPage from '@/pages/Dashboard';

// Route chunks. Keeping the factories lets the shell warm them up after
// sign-in, so a tap in the dock never has to wait on the network.
const pageImports = {
  users: () => import('@/pages/Users'),
  inbounds: () => import('@/pages/Inbounds'),
  certs: () => import('@/pages/Certs'),
  settings: () => import('@/pages/Settings'),
  admins: () => import('@/pages/Admins'),
  groups: () => import('@/pages/Groups'),
  audit: () => import('@/pages/Audit'),
  logs: () => import('@/pages/Logs'),
  donate: () => import('@/pages/Donate'),
  notFound: () => import('@/pages/NotFound'),
};
const UsersPage = lazyPage(pageImports.users);
const InboundsPage = lazyPage(pageImports.inbounds);
const CertsPage = lazyPage(pageImports.certs);
const SettingsPage = lazyPage(pageImports.settings);
const AdminsPage = lazyPage(pageImports.admins);
const GroupsPage = lazyPage(pageImports.groups);
const AuditPage = lazyPage(pageImports.audit);
const LogsPage = lazyPage(pageImports.logs);
const DonatePage = lazyPage(pageImports.donate);
const NotFound = lazyPage(pageImports.notFound);
/** Warm every route chunk and the data the dock pages open with. */
export function preloadPages() {
  for (const c of [UsersPage, InboundsPage, CertsPage, SettingsPage, LogsPage, AdminsPage, GroupsPage, AuditPage, NotFound]) c.preload().catch(() => undefined);
  prefetchPageData(qc);
}

const qc = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      refetchOnWindowFocus: false,
      retry: (n, e) => !(e instanceof ApiError && e.status >= 400 && e.status < 500) && n < 2,
    },
  },
});

function RequireAuth({ children }: { children: React.ReactNode }) {
  const token = useSession((s) => s.token);
  const setAdmin = useSession((s) => s.setAdmin);
  const loc = useLocation();
  const me = useMe(!!token);
  useEffect(() => {
    if (me.data) setAdmin(me.data);
  }, [me.data, setAdmin]);
  if (!token) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />;
  return <>{children}</>;
}

function PageFallback() {
  return (
    <div style={{ display: 'grid', gap: 16, paddingTop: 20 }} aria-busy="true">
      <Skeleton w="40%" h={52} r={16} />
      <Skeleton w="25%" h={18} />
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(260px,1fr))', gap: 16, marginTop: 12 }}>
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} h={180} r={24} />
        ))}
      </div>
    </div>
  );
}

export function App() {
  return (
    <QueryClientProvider client={qc}>
      <MotionConfig reducedMotion="user">
        <BrowserRouter basename={BASENAME || '/'} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
          <Backdrop />
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route
              element={
                <RequireAuth>
                  <Shell onReady={preloadPages} fallback={<PageFallback />} />
                </RequireAuth>
              }
            >
              <Route index element={<DashboardPage />} />
              <Route path="users" element={<Guard need="users_view" title="nav.users"><UsersPage /></Guard>} />
              <Route path="inbounds" element={<Guard need="inbounds" title="nav.inbounds"><InboundsPage /></Guard>} />
              <Route path="certs" element={<Guard need="certs" title="nav.certs"><CertsPage /></Guard>} />
              <Route path="settings" element={<SettingsPage />} />
              <Route path="groups" element={<Guard need="groups" title="nav.groups"><GroupsPage /></Guard>} />
              <Route path="admins" element={<AdminsPage />} />
              <Route path="audit" element={<Guard need="logs" title="nav.audit"><AuditPage /></Guard>} />
              <Route path="logs" element={<Guard need="logs" title="nav.logs"><LogsPage /></Guard>} />
              <Route path="donate" element={<DonatePage />} />
              <Route path="*" element={<NotFound />} />
            </Route>
          </Routes>
          <Toaster />
          <ConfirmHost />
        </BrowserRouter>
      </MotionConfig>
    </QueryClientProvider>
  );
}

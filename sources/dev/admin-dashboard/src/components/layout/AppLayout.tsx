import { useState } from 'react';
import { Outlet, useLocation } from 'react-router';
import Sidebar from './Sidebar';
import Header from './Header';

export default function AppLayout() {
  const location = useLocation();
  const [sidebarState, setSidebarState] = useState({ open: false, pathname: location.pathname });
  const sidebarOpen = sidebarState.open && sidebarState.pathname === location.pathname;

  return (
    <div className="flex min-h-dvh bg-gray-50">
      <Sidebar
        open={sidebarOpen}
        onClose={() => setSidebarState({ open: false, pathname: location.pathname })}
      />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header onMenuClick={() => setSidebarState({ open: true, pathname: location.pathname })} />
        <main className="flex-1 overflow-auto px-4 py-4 sm:px-6 sm:py-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

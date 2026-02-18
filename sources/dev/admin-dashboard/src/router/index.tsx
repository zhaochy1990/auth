import { BrowserRouter, Routes, Route } from 'react-router';
import AppLayout from '../components/layout/AppLayout';
import ProtectedRoute from './ProtectedRoute';
import LoginPage from '../pages/LoginPage';
import DashboardPage from '../pages/DashboardPage';
import ApplicationListPage from '../pages/applications/ListPage';
import ApplicationCreatePage from '../pages/applications/CreatePage';
import ApplicationDetailPage from '../pages/applications/DetailPage';
import UserListPage from '../pages/users/ListPage';
import UserDetailPage from '../pages/users/DetailPage';
import NotFoundPage from '../pages/NotFoundPage';

export default function AppRouter() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          element={
            <ProtectedRoute>
              <AppLayout />
            </ProtectedRoute>
          }
        >
          <Route index element={<DashboardPage />} />
          <Route path="applications" element={<ApplicationListPage />} />
          <Route path="applications/new" element={<ApplicationCreatePage />} />
          <Route path="applications/:id" element={<ApplicationDetailPage />} />
          <Route path="users" element={<UserListPage />} />
          <Route path="users/:id" element={<UserDetailPage />} />
        </Route>
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </BrowserRouter>
  );
}

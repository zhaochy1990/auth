import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router';
import { AppWindow, Users, Plus, Search } from 'lucide-react';
import { getStats } from '../api/admin';
import Spinner from '../components/ui/Spinner';

export default function DashboardPage() {
  const { t } = useTranslation('dashboard');
  const navigate = useNavigate();

  const { data: stats, isLoading } = useQuery({
    queryKey: ['stats'],
    queryFn: getStats,
  });

  if (isLoading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Spinner className="h-6 w-6 text-gray-400" />
      </div>
    );
  }

  return (
    <div>
      <h1 className="text-2xl font-semibold text-gray-900">{t('title')}</h1>

      <div className="mt-6 grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3">
        {/* App Stats */}
        <div className="rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200">
          <div className="flex items-center gap-2 text-gray-500">
            <AppWindow size={20} />
            <h2 className="font-medium">{t('apps.title')}</h2>
          </div>
          <div className="mt-4 grid grid-cols-3 gap-4 text-center">
            <div>
              <div className="text-2xl font-semibold text-gray-900">{stats?.applications.total ?? 0}</div>
              <div className="text-xs text-gray-500">{t('apps.total')}</div>
            </div>
            <div>
              <div className="text-2xl font-semibold text-green-600">{stats?.applications.active ?? 0}</div>
              <div className="text-xs text-gray-500">{t('apps.active')}</div>
            </div>
            <div>
              <div className="text-2xl font-semibold text-red-600">{stats?.applications.inactive ?? 0}</div>
              <div className="text-xs text-gray-500">{t('apps.inactive')}</div>
            </div>
          </div>
        </div>

        {/* User Stats */}
        <div className="rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200">
          <div className="flex items-center gap-2 text-gray-500">
            <Users size={20} />
            <h2 className="font-medium">{t('users.title')}</h2>
          </div>
          <div className="mt-4 grid grid-cols-2 gap-4 text-center">
            <div>
              <div className="text-2xl font-semibold text-gray-900">{stats?.users.total ?? 0}</div>
              <div className="text-xs text-gray-500">{t('users.total')}</div>
            </div>
            <div>
              <div className="text-2xl font-semibold text-blue-600">{stats?.users.recent ?? 0}</div>
              <div className="text-xs text-gray-500">{t('users.recent')}</div>
            </div>
          </div>
        </div>

        {/* Quick Actions */}
        <div className="rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200">
          <h2 className="font-medium text-gray-500">{t('quickActions.title')}</h2>
          <div className="mt-4 space-y-2">
            <button
              onClick={() => navigate('/applications/new')}
              className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm text-gray-700 hover:bg-gray-100"
            >
              <Plus size={16} />
              {t('quickActions.createApp')}
            </button>
            <button
              onClick={() => navigate('/users')}
              className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm text-gray-700 hover:bg-gray-100"
            >
              <Search size={16} />
              {t('quickActions.searchUser')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

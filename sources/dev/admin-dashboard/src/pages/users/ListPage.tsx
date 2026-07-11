import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router';
import { ChevronLeft, ChevronRight, Plus } from 'lucide-react';
import { listUsers, updateUser } from '../../api/admin';
import type { UserType } from '../../api/types';
import StatusBadge from '../../components/shared/StatusBadge';
import Badge from '../../components/ui/Badge';
import Spinner from '../../components/ui/Spinner';

export default function UserListPage() {
  const { t } = useTranslation('users');
  const { t: tc } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [page, setPage] = useState(1);
  const [search, setSearch] = useState('');
  const [searchInput, setSearchInput] = useState('');
  const [userType, setUserType] = useState<'' | UserType>('');
  const perPage = 20;

  const { data, isLoading } = useQuery({
    queryKey: ['users', page, perPage, search, userType],
    queryFn: () => listUsers({ page, per_page: perPage, search: search || undefined, user_type: userType || undefined }),
  });

  const toggleMutation = useMutation({
    mutationFn: ({ id, is_active }: { id: string; is_active: boolean }) =>
      updateUser(id, { is_active }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
  });

  const handleSearch = () => {
    setPage(1);
    setSearch(searchInput);
  };

  const totalPages = data ? Math.ceil(data.total / perPage) : 0;
  const users = data?.users || [];

  if (isLoading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Spinner className="h-6 w-6 text-gray-400" />
      </div>
    );
  }

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <h1 className="text-xl font-semibold text-gray-900 sm:text-2xl">{t('title')}</h1>
        <button
          onClick={() => navigate('/users/new')}
          className="flex w-full items-center justify-center gap-1 rounded-md bg-blue-600 px-3 py-2 text-sm text-white hover:bg-blue-700 sm:w-auto"
        >
          <Plus size={16} />
          {t('createBtn')}
        </button>
      </div>

      <div className="mt-4 flex flex-col gap-2 sm:flex-row">
        <input
          type="text"
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
          placeholder={t('searchPlaceholder')}
          className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 sm:max-w-xs"
        />
        <select
          value={userType}
          onChange={(e) => { setPage(1); setUserType(e.target.value as '' | UserType); }}
          className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 sm:w-40"
        >
          <option value="">{t('userType.all')}</option>
          <option value="regular">{t('userType.regular')}</option>
          <option value="testing">{t('userType.testing')}</option>
        </select>
        <button
          onClick={handleSearch}
          className="rounded-md bg-gray-100 px-3 py-2 text-sm text-gray-700 hover:bg-gray-200 sm:w-auto"
        >
          {tc('actions.search')}
        </button>
      </div>

      <div className="mt-4 space-y-3 md:hidden">
        {users.length === 0 ? (
          <div className="rounded-lg bg-white px-4 py-8 text-center text-sm text-gray-500 shadow-sm ring-1 ring-gray-200">
            {tc('status.empty')}
          </div>
        ) : (
          users.map((user) => (
            <div key={user.id} className="rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-200">
              <div className="flex items-start justify-between gap-3">
                <Link to={`/users/${user.id}`} className="min-w-0 text-sm font-medium text-blue-600 hover:underline">
                  <span className="break-all">{user.email || '-'}</span>
                </Link>
                <button
                  onClick={() => toggleMutation.mutate({ id: user.id, is_active: !user.is_active })}
                  className="shrink-0 cursor-pointer"
                >
                  <StatusBadge active={user.is_active} />
                </button>
              </div>

              <dl className="mt-3 grid grid-cols-1 gap-3 text-sm">
                <div>
                  <dt className="text-xs font-medium text-gray-500">{t('table.name')}</dt>
                  <dd className="mt-1 text-gray-900">{user.name || '-'}</dd>
                </div>
                <div>
                  <dt className="text-xs font-medium text-gray-500">{t('table.role')}</dt>
                  <dd className="mt-1">
                    <Badge variant={user.role === 'admin' ? 'yellow' : 'gray'}>
                      {t(`role.${user.role}`)}
                    </Badge>
                  </dd>
                </div>
                <div>
                  <dt className="text-xs font-medium text-gray-500">{t('table.userType')}</dt>
                  <dd className="mt-1">
                    <Badge variant={user.user_type === 'testing' ? 'blue' : 'gray'}>
                      {t(`userType.${user.user_type}`)}
                    </Badge>
                  </dd>
                </div>
                <div>
                  <dt className="text-xs font-medium text-gray-500">{t('table.membership')}</dt>
                  <dd className="mt-1">
                    <Badge variant={user.membership === 'regular' ? 'gray' : 'purple'}>
                      {t(`membership.${user.membership}`)}
                    </Badge>
                  </dd>
                </div>
                <div>
                  <dt className="text-xs font-medium text-gray-500">{t('table.createdAt')}</dt>
                  <dd className="mt-1 text-gray-500">{new Date(user.created_at).toLocaleDateString()}</dd>
                </div>
                <div>
                  <dt className="text-xs font-medium text-gray-500">{t('table.lastLoginAt')}</dt>
                  <dd className="mt-1 text-gray-500">
                    {user.last_login_at
                      ? new Date(`${user.last_login_at}Z`).toLocaleString(undefined, { timeZone: 'Asia/Shanghai' })
                      : '-'}
                  </dd>
                </div>
              </dl>
            </div>
          ))
        )}
      </div>

      <div className="mt-4 hidden overflow-hidden rounded-lg bg-white shadow-sm ring-1 ring-gray-200 md:block">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.email')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.name')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.role')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.userType')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.membership')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.status')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.createdAt')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.lastLoginAt')}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200">
            {users.length === 0 ? (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center text-sm text-gray-500">
                  {tc('status.empty')}
                </td>
              </tr>
            ) : (
              users.map((user) => (
                <tr key={user.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3">
                    <Link to={`/users/${user.id}`} className="text-sm font-medium text-blue-600 hover:underline">
                      {user.email || '-'}
                    </Link>
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-700">{user.name || '-'}</td>
                  <td className="px-4 py-3">
                    <Badge variant={user.role === 'admin' ? 'yellow' : 'gray'}>
                      {t(`role.${user.role}`)}
                    </Badge>
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={user.user_type === 'testing' ? 'blue' : 'gray'}>
                      {t(`userType.${user.user_type}`)}
                    </Badge>
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={user.membership === 'regular' ? 'gray' : 'purple'}>
                      {t(`membership.${user.membership}`)}
                    </Badge>
                  </td>
                  <td className="px-4 py-3">
                    <button
                      onClick={() => toggleMutation.mutate({ id: user.id, is_active: !user.is_active })}
                      className="cursor-pointer"
                    >
                      <StatusBadge active={user.is_active} />
                    </button>
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-500">
                    {new Date(user.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-500">
                    {user.last_login_at
                      ? new Date(`${user.last_login_at}Z`).toLocaleString(undefined, { timeZone: 'Asia/Shanghai' })
                      : '-'}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <span className="text-sm text-gray-500">
            {tc('pagination.total', { total: data?.total ?? 0 })}
          </span>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page === 1}
              className="rounded-md p-1 text-gray-500 hover:bg-gray-100 disabled:opacity-30"
            >
              <ChevronLeft size={20} />
            </button>
            <span className="text-sm text-gray-700">
              {page} / {totalPages}
            </span>
            <button
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page >= totalPages}
              className="rounded-md p-1 text-gray-500 hover:bg-gray-100 disabled:opacity-30"
            >
              <ChevronRight size={20} />
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

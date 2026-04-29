import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router';
import { Plus, Copy, Check } from 'lucide-react';
import { listApplications, updateApplication } from '../../api/admin';
import StatusBadge from '../../components/shared/StatusBadge';
import Badge from '../../components/ui/Badge';
import Spinner from '../../components/ui/Spinner';

export default function ApplicationListPage() {
  const { t } = useTranslation('applications');
  const { t: tc } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState('');
  const [copiedId, setCopiedId] = useState<string | null>(null);

  const { data: apps, isLoading } = useQuery({
    queryKey: ['applications'],
    queryFn: listApplications,
  });

  const toggleMutation = useMutation({
    mutationFn: ({ id, is_active }: { id: string; is_active: boolean }) =>
      updateApplication(id, { is_active }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['applications'] });
    },
  });

  const filtered = (apps || []).filter((app) =>
    app.name.toLowerCase().includes(search.toLowerCase()),
  );

  const copyClientId = async (clientId: string) => {
    await navigator.clipboard.writeText(clientId);
    setCopiedId(clientId);
    setTimeout(() => setCopiedId(null), 2000);
  };

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
          onClick={() => navigate('/applications/new')}
          className="flex w-full items-center justify-center gap-1 rounded-md bg-blue-600 px-3 py-2 text-sm text-white hover:bg-blue-700 sm:w-auto"
        >
          <Plus size={16} />
          {t('createBtn')}
        </button>
      </div>

      <div className="mt-4">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('searchPlaceholder')}
          className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 sm:max-w-xs"
        />
      </div>

      <div className="mt-4 space-y-3 md:hidden">
        {filtered.length === 0 ? (
          <div className="rounded-lg bg-white px-4 py-8 text-center text-sm text-gray-500 shadow-sm ring-1 ring-gray-200">
            {tc('status.empty')}
          </div>
        ) : (
          filtered.map((app) => (
            <div key={app.id} className="rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-200">
              <div className="flex items-start justify-between gap-3">
                <Link to={`/applications/${app.id}`} className="min-w-0 text-sm font-medium text-blue-600 hover:underline">
                  <span className="break-words">{app.name}</span>
                </Link>
                <button
                  onClick={() =>
                    toggleMutation.mutate({ id: app.id, is_active: !app.is_active })
                  }
                  className="shrink-0 cursor-pointer"
                >
                  <StatusBadge active={app.is_active} />
                </button>
              </div>

              <div className="mt-3">
                <div className="text-xs font-medium text-gray-500">{t('table.clientId')}</div>
                <div className="mt-1 flex items-start gap-2 rounded-md bg-gray-50 px-3 py-2">
                  <code className="min-w-0 flex-1 break-all text-xs text-gray-600">{app.client_id}</code>
                  <button
                    onClick={() => copyClientId(app.client_id)}
                    aria-label={tc('actions.copy')}
                    className="shrink-0 text-gray-400 hover:text-gray-600"
                  >
                    {copiedId === app.client_id ? (
                      <Check size={14} className="text-green-600" />
                    ) : (
                      <Copy size={14} />
                    )}
                  </button>
                </div>
              </div>

              <div className="mt-3">
                <div className="text-xs font-medium text-gray-500">{t('table.scopes')}</div>
                <div className="mt-1 flex flex-wrap gap-1">
                  {app.allowed_scopes.map((s) => (
                    <Badge key={s} variant="blue">{s}</Badge>
                  ))}
                </div>
              </div>

              <div className="mt-3 text-xs text-gray-500">
                {t('table.createdAt')}: {new Date(app.created_at).toLocaleDateString()}
              </div>
            </div>
          ))
        )}
      </div>

      <div className="mt-4 hidden overflow-hidden rounded-lg bg-white shadow-sm ring-1 ring-gray-200 md:block">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.name')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.clientId')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.status')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.scopes')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.createdAt')}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200">
            {filtered.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-sm text-gray-500">
                  {tc('status.empty')}
                </td>
              </tr>
            ) : (
              filtered.map((app) => (
                <tr key={app.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3">
                    <Link to={`/applications/${app.id}`} className="text-sm font-medium text-blue-600 hover:underline">
                      {app.name}
                    </Link>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-1">
                      <code className="text-xs text-gray-600">{app.client_id}</code>
                      <button
                        onClick={() => copyClientId(app.client_id)}
                        aria-label={tc('actions.copy')}
                        className="text-gray-400 hover:text-gray-600"
                      >
                        {copiedId === app.client_id ? (
                          <Check size={14} className="text-green-600" />
                        ) : (
                          <Copy size={14} />
                        )}
                      </button>
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <button
                      onClick={() =>
                        toggleMutation.mutate({ id: app.id, is_active: !app.is_active })
                      }
                      className="cursor-pointer"
                    >
                      <StatusBadge active={app.is_active} />
                    </button>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {app.allowed_scopes.map((s) => (
                        <Badge key={s} variant="blue">{s}</Badge>
                      ))}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-500">
                    {new Date(app.created_at).toLocaleDateString()}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

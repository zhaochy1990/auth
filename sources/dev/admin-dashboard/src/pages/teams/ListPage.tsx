import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { NavLink } from 'react-router';
import { Plus, Users } from 'lucide-react';
import { listTeams } from '../../api/admin';
import Spinner from '../../components/ui/Spinner';

export default function TeamsListPage() {
  const { t } = useTranslation('teams');
  const { t: tc } = useTranslation();

  const { data, isLoading } = useQuery({
    queryKey: ['teams'],
    queryFn: listTeams,
  });
  const teams = data || [];

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
        <NavLink
          to="/teams/new"
          className="flex w-full items-center justify-center gap-1 rounded-md bg-blue-600 px-3 py-2 text-sm text-white hover:bg-blue-700 sm:w-auto"
        >
          <Plus size={16} />
          {t('createBtn')}
        </NavLink>
      </div>

      <div className="mt-4 space-y-3 md:hidden">
        {teams.length === 0 ? (
          <div className="rounded-lg bg-white px-4 py-8 text-center text-sm text-gray-500 shadow-sm ring-1 ring-gray-200">
            {tc('status.empty')}
          </div>
        ) : (
          teams.map((team) => (
            <div key={team.id} className="rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-200">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <h2 className="break-words text-sm font-medium text-gray-900">{team.name}</h2>
                  {team.description && (
                    <p className="mt-1 line-clamp-2 text-xs text-gray-500">{team.description}</p>
                  )}
                </div>
                <span className="inline-flex shrink-0 items-center gap-1 text-sm text-gray-700">
                  <Users size={14} className="text-gray-400" />
                  {team.member_count}
                </span>
              </div>

              <dl className="mt-3 grid grid-cols-1 gap-3 text-sm">
                <div>
                  <dt className="text-xs font-medium text-gray-500">{t('table.owner')}</dt>
                  <dd className="mt-1">
                    <code className="rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs text-gray-600">
                      {team.owner_user_id.slice(0, 8)}…
                    </code>
                  </dd>
                </div>
                <div>
                  <dt className="text-xs font-medium text-gray-500">{t('table.createdAt')}</dt>
                  <dd className="mt-1 text-gray-500">{new Date(team.created_at).toLocaleDateString()}</dd>
                </div>
              </dl>

              <NavLink
                to={`/teams/${team.id}`}
                className="mt-3 inline-flex text-sm font-medium text-blue-600 hover:text-blue-800"
              >
                {t('actions.viewDetail')}
              </NavLink>
            </div>
          ))
        )}
      </div>

      <div className="mt-4 hidden overflow-hidden rounded-lg bg-white shadow-sm ring-1 ring-gray-200 md:block">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.name')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.owner')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.members')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.createdAt')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.actions')}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200">
            {teams.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-sm text-gray-500">
                  {tc('status.empty')}
                </td>
              </tr>
            ) : (
              teams.map((team) => (
                <tr key={team.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3 text-sm">
                    <div className="font-medium text-gray-900">{team.name}</div>
                    {team.description && (
                      <div className="mt-0.5 line-clamp-1 text-xs text-gray-500">{team.description}</div>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <code className="rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs text-gray-600">
                      {team.owner_user_id.slice(0, 8)}…
                    </code>
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-700">
                    <span className="inline-flex items-center gap-1">
                      <Users size={14} className="text-gray-400" />
                      {team.member_count}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-500">
                    {new Date(team.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-sm">
                    <NavLink
                      to={`/teams/${team.id}`}
                      className="text-blue-600 hover:text-blue-800"
                    >
                      {t('actions.viewDetail')}
                    </NavLink>
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

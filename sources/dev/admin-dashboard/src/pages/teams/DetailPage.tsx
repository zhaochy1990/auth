import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink, useParams } from 'react-router';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Trash2, UserPlus } from 'lucide-react';
import { AxiosError } from 'axios';
import toast from 'react-hot-toast';
import {
  getTeam, getTeamMembers,
  adminAddTeamMember, adminRemoveTeamMember,
} from '../../api/admin';
import type { TeamMember } from '../../api/types';
import Spinner from '../../components/ui/Spinner';

export default function TeamDetailPage() {
  const { t } = useTranslation('teams');
  const { id = '' } = useParams<{ id: string }>();
  const queryClient = useQueryClient();

  const [newUserId, setNewUserId] = useState('');
  const [newRole, setNewRole] = useState<'member' | 'owner'>('member');

  const teamQuery = useQuery({ queryKey: ['team', id], queryFn: () => getTeam(id), enabled: !!id });
  const membersQuery = useQuery({
    queryKey: ['team', id, 'members'],
    queryFn: () => getTeamMembers(id),
    enabled: !!id,
  });

  const addMutation = useMutation({
    mutationFn: () => adminAddTeamMember(id, { user_id: newUserId.trim(), role: newRole }),
    onSuccess: () => {
      setNewUserId('');
      setNewRole('member');
      queryClient.invalidateQueries({ queryKey: ['team', id] });
      queryClient.invalidateQueries({ queryKey: ['team', id, 'members'] });
    },
    onError: (err: AxiosError<{ message?: string; error?: string }>) => {
      const detail = err.response?.data?.message || err.response?.data?.error || t('errors.addFailed');
      toast.error(detail);
    },
  });

  const removeMutation = useMutation({
    mutationFn: (userId: string) => adminRemoveTeamMember(id, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['team', id] });
      queryClient.invalidateQueries({ queryKey: ['team', id, 'members'] });
    },
    onError: (err: AxiosError<{ message?: string; error?: string }>) => {
      const detail = err.response?.data?.message || err.response?.data?.error || t('errors.removeFailed');
      toast.error(detail);
    },
  });

  if (teamQuery.isLoading || membersQuery.isLoading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Spinner className="h-6 w-6 text-gray-400" />
      </div>
    );
  }

  const team = teamQuery.data;
  const members: TeamMember[] = membersQuery.data || [];

  if (!team) {
    return (
      <div>
        <NavLink to="/teams" className="text-sm text-blue-600 hover:text-blue-800">
          {t('detail.back')}
        </NavLink>
      </div>
    );
  }

  const handleAdd = (e: React.FormEvent) => {
    e.preventDefault();
    if (!newUserId.trim()) return;
    addMutation.mutate();
  };

  const isOwner = (m: TeamMember) => m.user_id === team.owner_user_id;

  return (
    <div className="space-y-6">
      <NavLink to="/teams" className="text-sm text-blue-600 hover:text-blue-800">
        {t('detail.back')}
      </NavLink>

      {/* Team header */}
      <div className="rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold text-gray-900">{team.name}</h1>
          {team.is_open && (
            <span className="rounded bg-green-50 px-2 py-0.5 text-xs font-medium text-green-700">
              {t('detail.openBadge')}
            </span>
          )}
        </div>
        {team.description && (
          <p className="mt-2 text-sm text-gray-600">{team.description}</p>
        )}
        <p className="mt-3 text-xs font-mono text-gray-400">{team.id}</p>
        <p className="mt-1 text-xs text-gray-500">
          {t('detail.memberCount', { count: team.member_count })}
        </p>
      </div>

      {/* Add member */}
      <form
        onSubmit={handleAdd}
        className="rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200"
      >
        <h2 className="text-base font-semibold text-gray-900">{t('detail.addMemberTitle')}</h2>
        <div className="mt-4 grid gap-3 sm:grid-cols-[1fr_140px_auto]">
          <div>
            <label className="block text-xs font-medium text-gray-600">{t('detail.userIdLabel')}</label>
            <input
              type="text"
              value={newUserId}
              onChange={(e) => setNewUserId(e.target.value)}
              placeholder="00000000-0000-4000-8000-000000000000"
              className="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 font-mono text-xs focus:border-blue-500 focus:outline-none"
            />
            <p className="mt-1 text-xs text-gray-400">{t('detail.userIdHelp')}</p>
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-600">{t('detail.roleLabel')}</label>
            <select
              value={newRole}
              onChange={(e) => setNewRole(e.target.value as 'member' | 'owner')}
              className="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none"
            >
              <option value="member">{t('detail.roleMember')}</option>
              <option value="owner">{t('detail.roleOwner')}</option>
            </select>
          </div>
          <div className="flex items-end">
            <button
              type="submit"
              disabled={!newUserId.trim() || addMutation.isPending}
              className="flex items-center gap-1 rounded-md bg-blue-600 px-3 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
            >
              <UserPlus size={16} />
              {t('detail.addBtn')}
            </button>
          </div>
        </div>
      </form>

      {/* Members table */}
      <div className="overflow-hidden rounded-lg bg-white shadow-sm ring-1 ring-gray-200">
        <div className="border-b border-gray-200 px-4 py-3">
          <h2 className="text-base font-semibold text-gray-900">{t('detail.membersTitle')}</h2>
        </div>
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">User</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('detail.roleLabel')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">Joined</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200">
            {members.map((m) => (
              <tr key={m.user_id} className="hover:bg-gray-50">
                <td className="px-4 py-3 text-sm">
                  <div className="font-medium text-gray-900">
                    {m.name || m.email || '—'}
                  </div>
                  <code className="text-xs text-gray-400">{m.user_id.slice(0, 8)}…</code>
                </td>
                <td className="px-4 py-3 text-sm">
                  {isOwner(m) ? (
                    <span className="rounded bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700">
                      {t('detail.ownerBadge')}
                    </span>
                  ) : (
                    <span className="text-gray-500">{m.role}</span>
                  )}
                </td>
                <td className="px-4 py-3 text-sm text-gray-500">
                  {new Date(m.joined_at).toLocaleDateString()}
                </td>
                <td className="px-4 py-3 text-sm">
                  {isOwner(m) ? (
                    <span className="text-xs text-gray-400" title={t('detail.removeOwnerBlocked')}>—</span>
                  ) : (
                    <button
                      onClick={() => removeMutation.mutate(m.user_id)}
                      disabled={removeMutation.isPending}
                      className="flex items-center gap-1 text-red-600 hover:text-red-800 disabled:opacity-50"
                    >
                      <Trash2 size={14} />
                      {t('actions.remove')}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

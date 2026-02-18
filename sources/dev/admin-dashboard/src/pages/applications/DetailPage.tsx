import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams, useNavigate } from 'react-router';
import { ArrowLeft, Copy, Check, Trash2 } from 'lucide-react';
import {
  listApplications,
  updateApplication,
  rotateSecret,
  listProviders,
  addProvider,
  removeProvider,
} from '../../api/admin';
import type { RotateSecretResponse } from '../../api/types';
import TagInput from '../../components/ui/TagInput';
import SecretDisplay from '../../components/shared/SecretDisplay';
import ConfirmDialog from '../../components/ui/ConfirmDialog';
import Spinner from '../../components/ui/Spinner';
import toast from 'react-hot-toast';

export default function ApplicationDetailPage() {
  const { t } = useTranslation('applications');
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  // Fetch app from list (cached)
  const { data: apps, isLoading: appsLoading } = useQuery({
    queryKey: ['applications'],
    queryFn: listApplications,
  });
  const app = apps?.find((a) => a.id === id);

  // Providers
  const { data: providers, isLoading: providersLoading } = useQuery({
    queryKey: ['providers', id],
    queryFn: () => listProviders(id!),
    enabled: !!id,
  });

  // Local form state
  const [name, setName] = useState('');
  const [redirectUris, setRedirectUris] = useState<string[]>([]);
  const [scopes, setScopes] = useState<string[]>([]);
  const [formInit, setFormInit] = useState(false);

  if (app && !formInit) {
    setName(app.name);
    setRedirectUris(app.redirect_uris);
    setScopes(app.allowed_scopes);
    setFormInit(true);
  }

  // Secret display
  const [secretData, setSecretData] = useState<RotateSecretResponse | null>(null);
  const [showRotateConfirm, setShowRotateConfirm] = useState(false);
  const [copiedClientId, setCopiedClientId] = useState(false);

  // Add provider dialog
  const [showAddProvider, setShowAddProvider] = useState(false);
  const [newProviderId, setNewProviderId] = useState('password');
  const [newProviderConfig, setNewProviderConfig] = useState('{}');

  // Remove provider confirm
  const [removeProviderId, setRemoveProviderId] = useState<string | null>(null);

  // Mutations
  const saveMutation = useMutation({
    mutationFn: () => updateApplication(id!, { name, redirect_uris: redirectUris, allowed_scopes: scopes }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['applications'] });
      toast.success(t('detail.saveSuccess'));
    },
  });

  const rotateMutation = useMutation({
    mutationFn: () => rotateSecret(id!),
    onSuccess: (data) => {
      setSecretData(data);
      setShowRotateConfirm(false);
    },
  });

  const addProviderMutation = useMutation({
    mutationFn: () => {
      const config = JSON.parse(newProviderConfig);
      return addProvider(id!, { provider_id: newProviderId, config });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers', id] });
      setShowAddProvider(false);
      setNewProviderId('password');
      setNewProviderConfig('{}');
      toast.success(t('detail.providerAdded'));
    },
  });

  const removeProviderMutation = useMutation({
    mutationFn: (providerId: string) => removeProvider(id!, providerId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers', id] });
      setRemoveProviderId(null);
      toast.success(t('detail.providerRemoved'));
    },
  });

  if (appsLoading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Spinner className="h-6 w-6 text-gray-400" />
      </div>
    );
  }

  if (!app) {
    return <div className="text-gray-500">Application not found</div>;
  }

  if (secretData) {
    return (
      <SecretDisplay
        clientId={secretData.client_id}
        clientSecret={secretData.client_secret}
        onAcknowledge={() => setSecretData(null)}
      />
    );
  }

  const handleSave = (e: FormEvent) => {
    e.preventDefault();
    saveMutation.mutate();
  };

  const copyClientId = async () => {
    await navigator.clipboard.writeText(app.client_id);
    setCopiedClientId(true);
    setTimeout(() => setCopiedClientId(false), 2000);
  };

  return (
    <div className="mx-auto max-w-2xl">
      <button
        onClick={() => navigate('/applications')}
        className="flex items-center gap-1 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft size={16} />
        {t('common:actions.back')}
      </button>

      <h1 className="mt-4 text-2xl font-semibold text-gray-900">{app.name}</h1>

      {/* Basic Settings */}
      <form onSubmit={handleSave} className="mt-6 space-y-4 rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200">
        <h2 className="font-medium text-gray-900">{t('detail.basicSettings')}</h2>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.name')}</label>
          <input
            type="text"
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.redirectUris')}</label>
          <div className="mt-1">
            <TagInput value={redirectUris} onChange={setRedirectUris} />
          </div>
        </div>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.scopes')}</label>
          <div className="mt-1">
            <TagInput value={scopes} onChange={setScopes} />
          </div>
        </div>

        <div className="flex justify-end">
          <button
            type="submit"
            disabled={saveMutation.isPending}
            className="rounded-md bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
          >
            {t('common:actions.save')}
          </button>
        </div>
      </form>

      {/* Credentials */}
      <div className="mt-6 rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200">
        <h2 className="font-medium text-gray-900">{t('detail.credentials')}</h2>

        <div className="mt-4">
          <label className="text-xs font-medium text-gray-500">Client ID</label>
          <div className="flex items-center gap-2 rounded-md bg-gray-50 px-3 py-2">
            <code className="flex-1 text-sm">{app.client_id}</code>
            <button onClick={copyClientId} className="text-gray-400 hover:text-gray-600">
              {copiedClientId ? <Check size={16} className="text-green-600" /> : <Copy size={16} />}
            </button>
          </div>
        </div>

        <button
          onClick={() => setShowRotateConfirm(true)}
          className="mt-3 rounded-md bg-amber-600 px-3 py-1.5 text-sm text-white hover:bg-amber-700"
        >
          {t('detail.rotateSecret')}
        </button>
      </div>

      {/* Providers */}
      <div className="mt-6 rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200">
        <div className="flex items-center justify-between">
          <h2 className="font-medium text-gray-900">{t('detail.providers')}</h2>
          <button
            onClick={() => setShowAddProvider(true)}
            className="text-sm text-blue-600 hover:underline"
          >
            {t('detail.addProvider')}
          </button>
        </div>

        {providersLoading ? (
          <Spinner className="mt-4 h-5 w-5 text-gray-400" />
        ) : (providers || []).length === 0 ? (
          <p className="mt-4 text-sm text-gray-500">{t('common:status.empty')}</p>
        ) : (
          <div className="mt-4 divide-y divide-gray-100">
            {(providers || []).map((p) => (
              <div key={p.id} className="flex items-center justify-between py-2">
                <div>
                  <span className="text-sm font-medium">{p.provider_id}</span>
                  <span className="ml-2 text-xs text-gray-500">{new Date(p.created_at).toLocaleDateString()}</span>
                </div>
                <button
                  onClick={() => setRemoveProviderId(p.provider_id)}
                  className="text-red-500 hover:text-red-700"
                >
                  <Trash2 size={16} />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Add Provider Dialog */}
      {showAddProvider && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="w-full max-w-sm rounded-lg bg-white p-6 shadow-xl">
            <h3 className="text-lg font-semibold">{t('detail.addProvider')}</h3>
            <div className="mt-4 space-y-3">
              <div>
                <label className="block text-sm font-medium text-gray-700">{t('detail.providerType')}</label>
                <select
                  value={newProviderId}
                  onChange={(e) => setNewProviderId(e.target.value)}
                  className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                >
                  <option value="password">password</option>
                  <option value="wechat">wechat</option>
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700">{t('detail.providerConfig')}</label>
                <textarea
                  value={newProviderConfig}
                  onChange={(e) => setNewProviderConfig(e.target.value)}
                  rows={4}
                  className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm font-mono"
                />
              </div>
            </div>
            <div className="mt-4 flex justify-end gap-2">
              <button
                onClick={() => setShowAddProvider(false)}
                className="rounded-md px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100"
              >
                {t('common:actions.cancel')}
              </button>
              <button
                onClick={() => addProviderMutation.mutate()}
                disabled={addProviderMutation.isPending}
                className="rounded-md bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
              >
                {t('common:actions.create')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Rotate Confirm */}
      <ConfirmDialog
        open={showRotateConfirm}
        message={t('detail.rotateConfirm')}
        onConfirm={() => rotateMutation.mutate()}
        onCancel={() => setShowRotateConfirm(false)}
        loading={rotateMutation.isPending}
      />

      {/* Remove Provider Confirm */}
      <ConfirmDialog
        open={!!removeProviderId}
        message={t('detail.removeProviderConfirm')}
        onConfirm={() => removeProviderId && removeProviderMutation.mutate(removeProviderId)}
        onCancel={() => setRemoveProviderId(null)}
        loading={removeProviderMutation.isPending}
      />
    </div>
  );
}

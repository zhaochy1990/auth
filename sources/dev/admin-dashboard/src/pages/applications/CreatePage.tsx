import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router';
import { ArrowLeft } from 'lucide-react';
import { createApplication } from '../../api/admin';
import type { CreateApplicationResponse } from '../../api/types';
import TagInput from '../../components/ui/TagInput';
import SecretDisplay from '../../components/shared/SecretDisplay';
import Spinner from '../../components/ui/Spinner';

export default function ApplicationCreatePage() {
  const { t } = useTranslation('applications');
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [name, setName] = useState('');
  const [redirectUris, setRedirectUris] = useState<string[]>([]);
  const [scopes, setScopes] = useState<string[]>([]);
  const [createdApp, setCreatedApp] = useState<CreateApplicationResponse | null>(null);

  const mutation = useMutation({
    mutationFn: createApplication,
    onSuccess: (data) => {
      setCreatedApp(data);
      queryClient.invalidateQueries({ queryKey: ['applications'] });
    },
  });

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    mutation.mutate({
      name,
      redirect_uris: redirectUris,
      allowed_scopes: scopes,
    });
  };

  if (createdApp) {
    return (
      <SecretDisplay
        clientId={createdApp.client_id}
        clientSecret={createdApp.client_secret}
        onAcknowledge={() => navigate(`/applications/${createdApp.id}`)}
      />
    );
  }

  return (
    <div className="mx-auto max-w-lg">
      <button
        onClick={() => navigate('/applications')}
        className="flex items-center gap-1 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft size={16} />
        {t('common:actions.back')}
      </button>

      <h1 className="mt-4 text-2xl font-semibold text-gray-900">{t('create.title')}</h1>

      <form onSubmit={handleSubmit} className="mt-6 space-y-4 rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200">
        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.name')}</label>
          <input
            type="text"
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('create.namePlaceholder')}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.redirectUris')}</label>
          <div className="mt-1">
            <TagInput
              value={redirectUris}
              onChange={setRedirectUris}
              placeholder={t('create.redirectUrisPlaceholder')}
            />
          </div>
        </div>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.scopes')}</label>
          <div className="mt-1">
            <TagInput
              value={scopes}
              onChange={setScopes}
              placeholder={t('create.scopesPlaceholder')}
            />
          </div>
        </div>

        <div className="flex justify-end">
          <button
            type="submit"
            disabled={mutation.isPending}
            className="flex items-center gap-2 rounded-md bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
          >
            {mutation.isPending && <Spinner className="h-4 w-4" />}
            {t('create.submit')}
          </button>
        </div>
      </form>
    </div>
  );
}

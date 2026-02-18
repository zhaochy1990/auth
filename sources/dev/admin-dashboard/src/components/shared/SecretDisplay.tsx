import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Copy, Check } from 'lucide-react';

interface Props {
  clientId: string;
  clientSecret: string;
  onAcknowledge: () => void;
}

export default function SecretDisplay({ clientId, clientSecret, onAcknowledge }: Props) {
  const { t } = useTranslation('applications');
  const [copiedField, setCopiedField] = useState<string | null>(null);
  const [acknowledged, setAcknowledged] = useState(false);

  const copy = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedField(field);
    setTimeout(() => setCopiedField(null), 2000);
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div className="w-full max-w-lg rounded-lg bg-white p-6 shadow-xl">
        <h3 className="text-lg font-semibold text-gray-900">{t('secret.title')}</h3>
        <p className="mt-1 text-sm text-amber-600">{t('secret.message')}</p>

        <div className="mt-4 space-y-3">
          <div>
            <label className="text-xs font-medium text-gray-500">{t('secret.clientId')}</label>
            <div className="flex items-center gap-2 rounded-md bg-gray-50 px-3 py-2">
              <code className="flex-1 break-all text-sm">{clientId}</code>
              <button onClick={() => copy(clientId, 'id')} className="text-gray-400 hover:text-gray-600">
                {copiedField === 'id' ? <Check size={16} className="text-green-600" /> : <Copy size={16} />}
              </button>
            </div>
          </div>
          <div>
            <label className="text-xs font-medium text-gray-500">{t('secret.clientSecret')}</label>
            <div className="flex items-center gap-2 rounded-md bg-gray-50 px-3 py-2">
              <code className="flex-1 break-all text-sm">{clientSecret}</code>
              <button onClick={() => copy(clientSecret, 'secret')} className="text-gray-400 hover:text-gray-600">
                {copiedField === 'secret' ? <Check size={16} className="text-green-600" /> : <Copy size={16} />}
              </button>
            </div>
          </div>
        </div>

        <div className="mt-4">
          <label className="flex items-center gap-2 text-sm text-gray-700">
            <input
              type="checkbox"
              checked={acknowledged}
              onChange={(e) => setAcknowledged(e.target.checked)}
              className="rounded border-gray-300"
            />
            {t('secret.acknowledge')}
          </label>
        </div>

        <div className="mt-4 flex justify-end">
          <button
            onClick={onAcknowledge}
            disabled={!acknowledged}
            className="rounded-md bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
          >
            {t('common:actions.confirm')}
          </button>
        </div>
      </div>
    </div>
  );
}

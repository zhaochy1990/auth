import { Loader2 } from 'lucide-react';

export default function Spinner({ className = '' }: { className?: string }) {
  return <Loader2 className={`animate-spin ${className}`} />;
}

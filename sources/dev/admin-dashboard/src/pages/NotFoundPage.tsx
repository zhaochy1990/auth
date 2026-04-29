import { Link } from 'react-router';

export default function NotFoundPage() {
  return (
    <div className="flex min-h-dvh flex-col items-center justify-center px-4 text-center">
      <h1 className="text-6xl font-bold text-gray-300">404</h1>
      <p className="mt-2 text-gray-500">Page not found</p>
      <Link to="/" className="mt-4 text-sm text-blue-600 hover:underline">
        Go to Dashboard
      </Link>
    </div>
  );
}

import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { AuthLayout } from '@/components/AuthLayout';
import { Input } from '@/components/Input';
import { Button } from '@/components/Button';
import { ErrorBanner } from '@/components/Feedback';
import { api, ApiError } from '@/lib/api';

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export default function ForgotPasswordPage() {
  const [email, setEmail] = useState('');
  const [error, setError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitted, setSubmitted] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setFormError(null);
    if (!email.trim()) {
      setError('Email is required.');
      return;
    }
    if (!EMAIL_RE.test(email)) {
      setError('Enter a valid email address.');
      return;
    }
    setError(undefined);
    setIsSubmitting(true);
    try {
      await api.auth.forgotPassword(email.trim());
      setSubmitted(true);
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Something went wrong. Please try again.');
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthLayout title="Reset your password" subtitle="We'll email you a reset link">
      {submitted ? (
        <div className="flex flex-col gap-4 text-center">
          <p className="text-sm text-gray-600 dark:text-gray-300">
            If an account exists for <strong>{email}</strong>, we&apos;ve sent a password reset
            link. Check your inbox.
          </p>
          <Link to="/login" className="text-sm font-medium text-brand-600 hover:text-brand-700 dark:text-brand-400">
            Back to sign in
          </Link>
        </div>
      ) : (
        <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
          {formError && <ErrorBanner message={formError} />}
          <Input
            label="Email"
            type="email"
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            error={error}
            required
          />
          <Button type="submit" isLoading={isSubmitting} className="w-full">
            Send reset link
          </Button>
          <Link to="/login" className="text-center text-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200">
            Back to sign in
          </Link>
        </form>
      )}
    </AuthLayout>
  );
}

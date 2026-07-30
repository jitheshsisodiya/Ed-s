import { useState, type FormEvent } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { AuthLayout } from '@/components/AuthLayout';
import { Input } from '@/components/Input';
import { Button } from '@/components/Button';
import { ErrorBanner } from '@/components/Feedback';
import { useToast } from '@/components/Toast';
import { api, ApiError } from '@/lib/api';

export default function ResetPasswordPage() {
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token');
  const navigate = useNavigate();
  const toast = useToast();

  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [errors, setErrors] = useState<{ password?: string; confirmPassword?: string }>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  if (!token) {
    return (
      <AuthLayout title="Invalid reset link">
        <div className="flex flex-col gap-4 text-center">
          <ErrorBanner message="This password reset link is missing or invalid. Please request a new one." />
          <Link to="/forgot-password" className="text-sm font-medium text-brand-600 hover:text-brand-700 dark:text-brand-400">
            Request a new link
          </Link>
        </div>
      </AuthLayout>
    );
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setFormError(null);
    const fieldErrors: typeof errors = {};
    if (!password) fieldErrors.password = 'Password is required.';
    else if (password.length < 12) fieldErrors.password = 'Password must be at least 12 characters.';
    if (confirmPassword !== password) fieldErrors.confirmPassword = 'Passwords do not match.';
    setErrors(fieldErrors);
    if (Object.keys(fieldErrors).length > 0) return;

    setIsSubmitting(true);
    try {
      await api.auth.resetPassword(token as string, password);
      toast.success('Password updated. Please sign in.');
      navigate('/login', { replace: true });
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Unable to reset password. The link may have expired.');
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthLayout title="Choose a new password">
      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
        {formError && <ErrorBanner message={formError} />}
        <Input
          label="New password"
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={errors.password}
          hint="At least 12 characters."
          required
        />
        <Input
          label="Confirm new password"
          type="password"
          autoComplete="new-password"
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          error={errors.confirmPassword}
          required
        />
        <Button type="submit" isLoading={isSubmitting} className="w-full">
          Reset password
        </Button>
      </form>
    </AuthLayout>
  );
}

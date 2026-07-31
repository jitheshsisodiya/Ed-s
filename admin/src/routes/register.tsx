import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { AuthLayout } from '@/components/AuthLayout';
import { Input } from '@/components/Input';
import { Button } from '@/components/Button';
import { ErrorBanner } from '@/components/Feedback';
import { useAuth } from '@/lib/auth-context-value';
import { useToast } from '@/components/toast-context';
import { ApiError } from '@/lib/api';

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

interface FieldErrors {
  email?: string;
  password?: string;
  confirmPassword?: string;
  displayName?: string;
}

export default function RegisterPage() {
  const { register } = useAuth();
  const navigate = useNavigate();
  const toast = useToast();

  const [email, setEmail] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  function validate(): boolean {
    const errors: FieldErrors = {};
    if (!displayName.trim()) errors.displayName = 'Display name is required.';
    if (!email.trim()) errors.email = 'Email is required.';
    else if (!EMAIL_RE.test(email)) errors.email = 'Enter a valid email address.';
    if (!password) errors.password = 'Password is required.';
    else if (password.length < 12) errors.password = 'Password must be at least 12 characters.';
    if (confirmPassword !== password) errors.confirmPassword = 'Passwords do not match.';
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setFormError(null);
    if (!validate()) return;

    setIsSubmitting(true);
    try {
      await register({ email: email.trim(), password, displayName: displayName.trim() });
      toast.success('Account created. Please sign in.');
      navigate('/login', { replace: true });
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setFieldErrors((prev) => ({ ...prev, email: 'An account with this email already exists.' }));
      } else {
        setFormError(err instanceof ApiError ? err.message : 'Unable to create account. Please try again.');
      }
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthLayout title="Create your account" subtitle="Start managing your NexusVPN mesh">
      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
        {formError && <ErrorBanner message={formError} />}

        <Input
          label="Display name"
          autoComplete="name"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          error={fieldErrors.displayName}
          required
        />
        <Input
          label="Email"
          type="email"
          autoComplete="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          error={fieldErrors.email}
          required
        />
        <Input
          label="Password"
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={fieldErrors.password}
          hint="At least 12 characters."
          required
        />
        <Input
          label="Confirm password"
          type="password"
          autoComplete="new-password"
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          error={fieldErrors.confirmPassword}
          required
        />

        <Button type="submit" isLoading={isSubmitting} className="w-full">
          Create account
        </Button>
      </form>

      <p className="mt-6 text-center text-sm text-gray-500 dark:text-gray-400">
        Already have an account?{' '}
        <Link to="/login" className="font-medium text-brand-600 hover:text-brand-700 dark:text-brand-400">
          Sign in
        </Link>
      </p>
    </AuthLayout>
  );
}

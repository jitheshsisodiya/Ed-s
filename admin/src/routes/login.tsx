import { useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { AuthLayout } from '@/components/AuthLayout';
import { Input } from '@/components/Input';
import { Button } from '@/components/Button';
import { ErrorBanner } from '@/components/Feedback';
import { useAuth } from '@/lib/auth-context';
import { ApiError } from '@/lib/api';

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

interface LocationState {
  from?: { pathname: string };
}

export default function LoginPage() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [mfaCode, setMfaCode] = useState('');
  const [step, setStep] = useState<'credentials' | 'mfa'>('credentials');
  const [fieldErrors, setFieldErrors] = useState<{ email?: string; password?: string; mfaCode?: string }>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const redirectTo = (location.state as LocationState | null)?.from?.pathname ?? '/dashboard';

  function validateCredentials(): boolean {
    const errors: typeof fieldErrors = {};
    if (!email.trim()) errors.email = 'Email is required.';
    else if (!EMAIL_RE.test(email)) errors.email = 'Enter a valid email address.';
    if (!password) errors.password = 'Password is required.';
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  }

  function validateMfa(): boolean {
    const errors: typeof fieldErrors = {};
    if (!mfaCode.trim()) errors.mfaCode = 'Enter the 6-digit code from your authenticator app.';
    else if (!/^\d{6}$/.test(mfaCode.trim())) errors.mfaCode = 'Code must be 6 digits.';
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setFormError(null);

    if (step === 'credentials') {
      if (!validateCredentials()) return;
      setIsSubmitting(true);
      try {
        const result = await login({ email: email.trim(), password });
        if (result.mfaRequired) {
          setStep('mfa');
        } else {
          navigate(redirectTo, { replace: true });
        }
      } catch (err) {
        setFormError(err instanceof ApiError ? err.message : 'Unable to sign in. Please try again.');
      } finally {
        setIsSubmitting(false);
      }
      return;
    }

    if (!validateMfa()) return;
    setIsSubmitting(true);
    try {
      await login({ email: email.trim(), password, mfaCode: mfaCode.trim() });
      navigate(redirectTo, { replace: true });
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Invalid verification code.');
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthLayout
      title="Sign in to NexusVPN"
      subtitle={step === 'mfa' ? 'Enter your two-factor authentication code' : 'Manage your networks and devices'}
    >
      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
        {formError && <ErrorBanner message={formError} />}

        {step === 'credentials' ? (
          <>
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
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              error={fieldErrors.password}
              required
            />
            <div className="flex justify-end">
              <Link to="/forgot-password" className="text-sm font-medium text-brand-600 hover:text-brand-700 dark:text-brand-400">
                Forgot password?
              </Link>
            </div>
          </>
        ) : (
          <Input
            label="Verification code"
            inputMode="numeric"
            autoComplete="one-time-code"
            maxLength={6}
            value={mfaCode}
            onChange={(e) => setMfaCode(e.target.value)}
            error={fieldErrors.mfaCode}
            autoFocus
            required
          />
        )}

        <Button type="submit" isLoading={isSubmitting} className="w-full">
          {step === 'mfa' ? 'Verify' : 'Sign in'}
        </Button>

        {step === 'mfa' && (
          <button
            type="button"
            onClick={() => {
              setStep('credentials');
              setMfaCode('');
              setFormError(null);
            }}
            className="text-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
          >
            &larr; Back
          </button>
        )}
      </form>

      <p className="mt-6 text-center text-sm text-gray-500 dark:text-gray-400">
        Don&apos;t have an account?{' '}
        <Link to="/register" className="font-medium text-brand-600 hover:text-brand-700 dark:text-brand-400">
          Create one
        </Link>
      </p>
    </AuthLayout>
  );
}

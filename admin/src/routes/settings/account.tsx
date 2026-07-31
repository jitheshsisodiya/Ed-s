import { useEffect, useState, type FormEvent } from 'react';
import QRCode from 'qrcode';
import { useAuth } from '@/lib/auth-context';
import { api, ApiError } from '@/lib/api';
import { useToast } from '@/components/Toast';
import { Card, ErrorBanner } from '@/components/Feedback';
import { Button } from '@/components/Button';
import { Input } from '@/components/Input';

function MfaSetup({ onEnabled }: { onEnabled: () => void }) {
  const toast = useToast();
  const [secret, setSecret] = useState<string | null>(null);
  const [otpauthUrl, setOtpauthUrl] = useState<string | null>(null);
  const [qrDataUrl, setQrDataUrl] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [codeError, setCodeError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [isStarting, setIsStarting] = useState(false);
  const [isVerifying, setIsVerifying] = useState(false);

  useEffect(() => {
    if (!otpauthUrl) {
      setQrDataUrl(null);
      return;
    }
    let cancelled = false;
    QRCode.toDataURL(otpauthUrl, { margin: 1, width: 220 })
      .then((url) => {
        if (!cancelled) setQrDataUrl(url);
      })
      .catch(() => {
        if (!cancelled) setQrDataUrl(null);
      });
    return () => {
      cancelled = true;
    };
  }, [otpauthUrl]);

  async function startSetup() {
    setFormError(null);
    setIsStarting(true);
    try {
      const res = await api.auth.mfaEnable();
      setSecret(res.secret);
      setOtpauthUrl(res.otpauthUrl);
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Failed to start MFA setup.');
    } finally {
      setIsStarting(false);
    }
  }

  async function handleVerify(e: FormEvent) {
    e.preventDefault();
    setFormError(null);
    if (!/^\d{6}$/.test(code.trim())) {
      setCodeError('Enter the 6-digit code from your authenticator app.');
      return;
    }
    setCodeError(undefined);
    setIsVerifying(true);
    try {
      await api.auth.mfaVerify(code.trim());
      toast.success('Two-factor authentication enabled.');
      onEnabled();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Invalid code. Please try again.');
    } finally {
      setIsVerifying(false);
    }
  }

  if (!otpauthUrl) {
    return (
      <div className="flex flex-col gap-3">
        {formError && <ErrorBanner message={formError} />}
        <p className="text-sm text-gray-500 dark:text-gray-400">
          Protect your account with a time-based one-time password (TOTP) app such as Google
          Authenticator or 1Password.
        </p>
        <div>
          <Button onClick={startSetup} isLoading={isStarting}>
            Enable two-factor authentication
          </Button>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={handleVerify} noValidate className="flex flex-col gap-4">
      {formError && <ErrorBanner message={formError} />}
      <p className="text-sm text-gray-500 dark:text-gray-400">
        Scan this QR code with your authenticator app, or enter the secret manually, then confirm
        with a generated code.
      </p>
      {qrDataUrl && (
        <img
          src={qrDataUrl}
          alt="MFA setup QR code — scan with your authenticator app"
          className="h-[220px] w-[220px] self-start rounded-md border border-gray-200 dark:border-gray-800"
        />
      )}
      <div>
        <p className="text-xs font-medium text-gray-500 dark:text-gray-400">Manual entry secret</p>
        <p className="mt-1 select-all font-mono text-sm text-gray-900 dark:text-gray-100">{secret}</p>
      </div>
      <Input
        label="Verification code"
        inputMode="numeric"
        maxLength={6}
        value={code}
        onChange={(e) => setCode(e.target.value)}
        error={codeError}
        required
      />
      <div>
        <Button type="submit" isLoading={isVerifying}>
          Verify &amp; enable
        </Button>
      </div>
    </form>
  );
}

export default function AccountSettingsPage() {
  const { user, patchUser } = useAuth();
  const toast = useToast();
  const [isSendingReset, setIsSendingReset] = useState(false);
  const [resetError, setResetError] = useState<string | null>(null);

  async function handleSendResetEmail() {
    if (!user?.email) return;
    setResetError(null);
    setIsSendingReset(true);
    try {
      await api.auth.forgotPassword(user.email);
      toast.success(`Password reset email sent to ${user.email}.`);
    } catch (err) {
      setResetError(err instanceof ApiError ? err.message : 'Failed to send reset email.');
    } finally {
      setIsSendingReset(false);
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <h2 className="text-sm font-semibold text-gray-700 dark:text-gray-200">Profile</h2>
        <dl className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <dt className="text-xs font-medium text-gray-400 dark:text-gray-500">Display name</dt>
            <dd className="mt-1 text-sm text-gray-900 dark:text-gray-100">{user?.displayName || '—'}</dd>
          </div>
          <div>
            <dt className="text-xs font-medium text-gray-400 dark:text-gray-500">Email</dt>
            <dd className="mt-1 text-sm text-gray-900 dark:text-gray-100">{user?.email || '—'}</dd>
          </div>
        </dl>
        <p className="mt-4 text-xs text-gray-400 dark:text-gray-500">
          Profile details are read-only in this panel; the control plane API does not currently
          expose an endpoint to update them.
        </p>
      </Card>

      <Card>
        <h2 className="text-sm font-semibold text-gray-700 dark:text-gray-200">Password</h2>
        {resetError && <div className="mt-3"><ErrorBanner message={resetError} /></div>}
        <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">
          We&apos;ll email you a secure link to set a new password.
        </p>
        <div className="mt-4">
          <Button variant="secondary" onClick={handleSendResetEmail} isLoading={isSendingReset}>
            Send password reset email
          </Button>
        </div>
      </Card>

      <Card>
        <h2 className="text-sm font-semibold text-gray-700 dark:text-gray-200">Two-factor authentication</h2>
        <div className="mt-4">
          {user?.mfaEnabled ? (
            <p className="inline-flex items-center gap-2 text-sm text-emerald-700 dark:text-emerald-400">
              <span className="h-2 w-2 rounded-full bg-emerald-500" aria-hidden="true" />
              Two-factor authentication is enabled on your account.
            </p>
          ) : (
            <MfaSetup onEnabled={() => patchUser({ mfaEnabled: true })} />
          )}
        </div>
      </Card>
    </div>
  );
}

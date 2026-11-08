import { motion } from 'framer-motion';
import { ArrowLeft, ArrowRight, Eye, EyeOff, Loader2 } from 'lucide-react';
import { useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { ApiError } from '@/api/client';
import { login } from '@/api/hooks';
import { IS_MOCK } from '@/app/base';
import { Logo } from '@/components/Logo';
import { ThemeLangToggles } from '@/components/Shell';
import { useI18n } from '@/i18n';
import { useSession } from '@/store/session';
import s from './Login.module.css';

const BLIPS = [
  { r: 0.62, a: 28, d: 0.2 },
  { r: 0.38, a: 110, d: 1.1 },
  { r: 0.82, a: 168, d: 2.0 },
  { r: 0.5, a: 232, d: 2.7 },
  { r: 0.72, a: 300, d: 3.4 },
  { r: 0.26, a: 340, d: 3.9 },
];

function Radar() {
  return (
    <div className={s.radar} aria-hidden="true">
      {[1, 0.78, 0.56, 0.34].map((r) => (
        <span key={r} className={s.ring} style={{ inset: `${(1 - r) * 50}%` }} />
      ))}
      <span className={s.cross} />
      <span className={s.crossV} />
      <span className={s.sweep} />
      {BLIPS.map((b, i) => {
        const rad = (b.a * Math.PI) / 180;
        return (
          <span
            key={i}
            className={s.blip}
            style={{ left: `${50 + 50 * b.r * Math.cos(rad)}%`, top: `${50 + 50 * b.r * Math.sin(rad)}%`, animationDelay: `${b.d}s` }}
          />
        );
      })}
      <span className={s.center} />
    </div>
  );
}

export default function LoginPage() {
  const { t, dir } = useI18n();
  const token = useSession((x) => x.token);
  const doLogin = useSession((x) => x.login);
  const nav = useNavigate();
  const loc = useLocation();
  const [u, setU] = useState('');
  const [p, setP] = useState('');
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const from = (loc.state as { from?: string } | null)?.from ?? '/';

  if (token) return <Navigate to={from} replace />;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!u || !p) return;
    setBusy(true);
    setErr(null);
    try {
      const r = await login(u.trim(), p);
      doLogin(r.token, r.expires_at, r.admin);
      nav(from, { replace: true });
    } catch (e) {
      const ae = e as ApiError;
      setErr(ae.status === 429 ? t('login.rateLimited') : ae.status === 401 || ae.status === 400 ? t('login.failed') : ae.message || t('common.errorBody'));
    } finally {
      setBusy(false);
    }
  };

  const Arrow = dir === 'rtl' ? ArrowLeft : ArrowRight;
  const lines = t('login.headline').split('\n');

  return (
    <div className={s.page}>
      <div className={s.top}>
        <div className={s.brand}>
          <Logo size={36} />
          <strong className="display">Veilon</strong>
        </div>
        <div className={s.toggles}>
          <ThemeLangToggles />
        </div>
      </div>

      <section className={s.hero}>
        <Radar />
        <div className={s.heroText}>
          <motion.span className={`eyebrow ${s.status}`} initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: 0.2 }}>
            <span className={s.dot} /> {t('login.status')}
          </motion.span>
          <h1 className={`${s.headline} display`}>
            {lines.map((l, i) => (
              <motion.span
                key={i}
                className={s.line}
                initial={{ y: '110%', opacity: 0 }}
                animate={{ y: 0, opacity: 1 }}
                transition={{ delay: 0.15 + i * 0.12, duration: 0.8, ease: [0.22, 1, 0.36, 1] }}
              >
                {i === lines.length - 1 ? <em>{l}</em> : l}
              </motion.span>
            ))}
          </h1>
          <motion.p className={s.sub} initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.5 }}>
            {t('login.sub')}
          </motion.p>
        </div>
      </section>

      <motion.form
        className={s.form}
        onSubmit={submit}
        initial={{ opacity: 0, y: 24 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ delay: 0.35, duration: 0.7, ease: [0.22, 1, 0.36, 1] }}
        aria-labelledby="login-title"
      >
        <h2 id="login-title" className={`${s.formTitle} display`}>
          {t('login.title')}
        </h2>
        <label className={s.field}>
          <span>{t('login.username')}</span>
          <input
            className={`${s.input} ltr`}
            value={u}
            onChange={(e) => setU(e.target.value)}
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            required
            autoFocus
          />
        </label>
        <label className={s.field}>
          <span>{t('login.password')}</span>
          <div className={s.pwWrap}>
            <input
              className={`${s.input} ltr`}
              type={show ? 'text' : 'password'}
              value={p}
              onChange={(e) => setP(e.target.value)}
              autoComplete="current-password"
              required
              aria-invalid={!!err}
              aria-describedby={err ? 'login-err' : undefined}
            />
            <button type="button" className={s.eye} onClick={() => setShow((x) => !x)} aria-label={show ? t('login.hidePassword') : t('login.showPassword')}>
              {show ? <EyeOff size={18} /> : <Eye size={18} />}
            </button>
          </div>
        </label>
        {err && (
          <motion.p id="login-err" role="alert" className={s.err} initial={{ opacity: 0, x: -6 }} animate={{ opacity: 1, x: [8, -6, 4, 0] }}>
            {err}
          </motion.p>
        )}
        <button type="submit" className={`btn btn--primary btn--lg ${s.submit}`} disabled={busy}>
          {busy ? <Loader2 size={20} className="spin" /> : null}
          <span>{busy ? t('login.submitting') : t('login.submit')}</span>
          {!busy && <Arrow size={20} />}
        </button>
        {IS_MOCK && <p className={s.hint}>{t('login.mockHint')}</p>}
      </motion.form>
    </div>
  );
}

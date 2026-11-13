import { Globe, Power, RotateCw } from 'lucide-react';
import { useEffect, useMemo, useRef } from 'react';
import { useRestartCore } from '@/api/hooks';
import type { CoreInfo } from '@/api/types';
import { type CoreScope, useCorePower, useInstallXray, useUpdateGeo, useXrayInfo, useXrayReleases } from '@/api/xray';
import { Select, type SelectOption } from '@/components/Select';
import { engineName } from '@/lib/protocols';
import { useI18n } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import s from './XrayCore.module.css';
import { cmpVer, tagOf, versionItems } from './xrayVersions';


/**
 * On/off switch for the cores. Switching off asks whether only Xray or every
 * core goes off; switching on brings back every core that was switched off.
 * A switched-off core stays off until switched on, even after a panel restart.
 */
export function CorePowerSwitch({ core, stoppedCores }: { core?: CoreInfo; stoppedCores?: string[] }) {
  const { t } = useI18n();
  const power = useCorePower();
  const on = !!core && !core.stopped;
  const off = stoppedCores ?? (core?.stopped ? [core.name] : []);
  const toggle = async () => {
    if (!core) return;
    let scope: CoreScope | undefined;
    if (on) {
      const ok = await confirmAction({
        title: t('xray.stopConfirm'),
        body: t('xray.stopBody'),
        confirmLabel: t('xray.stop'),
        danger: true,
        choiceInput: {
          label: t('xray.stopScope'),
          initial: 0,
          options: [
            { value: 0, label: t('xray.scopeXray') },
            { value: 1, label: t('xray.scopeAll') },
          ],
        },
      });
      if (!ok) return;
      scope = ok.value === 1 ? 'all' : 'xray';
    }
    power.mutate(
      { on: !on, scope },
      {
        onSuccess: () => toast.success(on ? (scope === 'all' ? t('xray.stoppedAll') : t('xray.stopped')) : off.length > 1 ? t('xray.startedAll') : t('xray.started')),
        onError: (e) => toast.error(t('common.error'), (e as Error).message),
      },
    );
  };
  return (
    <>
      <label className={s.power}>
        <Power size={14} aria-hidden="true" />
        <span>{on ? t('xray.on') : t('xray.off')}</span>
        <button
          type="button"
          role="switch"
          className="switch"
          aria-checked={on}
          aria-label={t('xray.power')}
          disabled={!core || power.isPending || !!core.busy}
          onClick={toggle}
        />
      </label>
      {off.length > 0 && (
        <span className={s.note}>
          {t('xray.offCores')}: <span className="ltr">{off.map(engineName).join(', ')}</span>
        </span>
      )}
    </>
  );
}

/**
 * The Xray version picker shared by the main server's Xray card and the
 * nodes: the newest stable official releases plus the installed version
 * (kept and marked current). Choosing another one asks for confirmation and
 * calls onInstall. Returns the field and a note line (installing, GitHub rate
 * limit, release list error) for the caller to place.
 */
export function XrayVersionSelect({
  current,
  disabled,
  busyNote,
  onInstall,
  confirmTitle,
  confirmBody,
  className,
}: {
  /** installed tag ("v26.10.31") or '' when unknown */
  current: string;
  disabled?: boolean;
  /** shown instead of the release notes while an install runs */
  busyNote?: string | null;
  onInstall: (tag: string) => void;
  /** confirmation title (default: install Xray {v}) */
  confirmTitle?: (tag: string) => string;
  /** confirmation text (default: the main server's) */
  confirmBody?: (older: boolean) => string;
  className?: string;
}) {
  const { t, duration } = useI18n();
  const releases = useXrayReleases();

  const options = useMemo<SelectOption<string>[]>(() => {
    return versionItems(releases.data?.releases ?? [], current).map((v) => ({
      value: v.tag,
      label: v.tag,
      text: v.tag,
      hint: v.current ? t('xray.current') : undefined,
    }));
  }, [releases.data, current, t]);

  const choose = async (tag: string) => {
    if (!tag || tag === current) return;
    const older = !!current && cmpVer(tag, current) < 0;
    const ok = await confirmAction({
      title: confirmTitle ? confirmTitle(tag) : t('xray.installConfirm', { v: tag }),
      body: confirmBody ? confirmBody(older) : older ? t('xray.installOlder') : t('xray.installBody'),
      confirmLabel: t('xray.install'),
      danger: older,
    });
    if (!ok) return;
    onInstall(tag);
  };

  const limitedFor = releases.data?.rate_limited_until ? new Date(releases.data.rate_limited_until).getTime() - Date.now() : 0;
  let note: string | null = null;
  if (busyNote) note = busyNote;
  else if (limitedFor > 0) note = t('xray.rateLimited', { time: duration(limitedFor) });
  else if (releases.isError) note = t('xray.releasesError');

  return (
    <>
      <Select<string>
        ltr
        mono
        className={className ?? s.versionSelect}
        valueHint={false}
        value={current || null}
        placeholder="—"
        onChange={choose}
        options={options}
        ariaLabel={t('xray.chooseVersion')}
        disabled={disabled}
      />
      {note && <span className={s.note}>{note}</span>}
    </>
  );
}

/** The version line as a picker: choosing a release installs it. */
export function CoreVersionField({ core }: { core?: CoreInfo }) {
  const { t } = useI18n();
  const info = useXrayInfo(!!core?.busy);
  const install = useInstallXray();
  const current = tagOf(core?.version);
  const job = info.data?.job;
  const installing = (job?.running && job.kind === 'install') || core?.busy === 'install';

  return (
    <div className={s.version}>
      <span className={s.versionLabel}>{t('dash.version')}</span>
      <XrayVersionSelect
        current={current}
        disabled={!core || installing || install.isPending}
        busyNote={installing ? t('xray.installing', { v: job?.version ?? '' }) : null}
        onInstall={(tag) =>
          install.mutate(tag, {
            onSuccess: () => toast.info(t('xray.installing', { v: tag })),
            onError: (e) => toast.error(t('common.error'), (e as Error).message),
          })
        }
      />
    </div>
  );
}

/** Restart and geo-file update buttons, plus toasts when a background job ends. */
export function CoreActions({ core }: { core?: CoreInfo }) {
  const { t, date } = useI18n();
  const restart = useRestartCore();
  const geo = useUpdateGeo();
  const info = useXrayInfo(!!core?.busy);
  const job = info.data?.job;
  const geoRunning = (job?.running && job.kind === 'geo') || core?.busy === 'geo';
  const stopped = !!core?.stopped;

  // Announce the outcome of a job that was seen running.
  const seen = useRef<string | null>(null);
  useEffect(() => {
    if (!job) return;
    const key = `${job.kind}:${job.started_at}`;
    if (job.running) {
      seen.current = key;
      return;
    }
    if (seen.current !== key) return;
    seen.current = null;
    if (job.error) {
      toast.error(job.kind === 'install' ? t('xray.installFailed') : t('xray.geoFailed'), job.rolled_back ? `${job.error} — ${t('xray.rolledBack')}` : job.error);
    } else {
      toast.success(job.kind === 'install' ? t('xray.installed', { v: job.version ?? '' }) : t('xray.geoUpdated'));
    }
  }, [job, t]);

  const doRestart = async () => {
    const ok = await confirmAction({ title: t('dash.restartConfirm'), body: t('dash.restartBody'), confirmLabel: t('dash.restart') });
    if (!ok) return;
    restart.mutate(undefined, {
      onSuccess: () => toast.success(t('dash.restarted')),
      onError: (e) => toast.error(t('common.error'), (e as Error).message),
    });
  };
  const doGeo = async () => {
    const ok = await confirmAction({ title: t('xray.geoConfirm'), body: t('xray.geoBody'), confirmLabel: t('xray.geoUpdate') });
    if (!ok) return;
    geo.mutate(undefined, {
      onSuccess: () => toast.info(t('xray.geoUpdating')),
      onError: (e) => toast.error(t('common.error'), (e as Error).message),
    });
  };
  const geoDate = info.data?.geo?.find((g) => g.name === 'geosite.dat')?.updated_at;

  return (
    <div className={s.actions}>
      <button className="btn btn--sm" onClick={doRestart} disabled={restart.isPending || stopped || !!core?.busy} title={stopped ? t('xray.restartOff') : undefined}>
        <RotateCw size={15} className={restart.isPending ? 'spin' : undefined} />
        {restart.isPending ? t('dash.restarting') : t('dash.restart')}
      </button>
      <button className="btn btn--sm" onClick={doGeo} disabled={geo.isPending || geoRunning || !!core?.busy} title={geoDate ? t('xray.geoDate', { date: date(geoDate) }) : undefined}>
        <Globe size={15} className={geoRunning ? 'spin' : undefined} />
        {geoRunning ? t('xray.geoUpdating') : t('xray.geoUpdate')}
      </button>
    </div>
  );
}

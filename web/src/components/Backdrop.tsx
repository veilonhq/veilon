import s from './Backdrop.module.css';

/** Fixed ink background with a slow aurora and animated film grain. Purely decorative. */
export function Backdrop() {
  return (
    <div className={s.root} aria-hidden="true">
      <div className={s.aurora}>
        <span className={s.a1} />
        <span className={s.a2} />
        <span className={s.a3} />
      </div>
      <div className={s.grid} />
      <div className={s.grain} />
    </div>
  );
}

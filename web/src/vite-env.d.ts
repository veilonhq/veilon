/// <reference types="vite/client" />
interface ImportMetaEnv {
  readonly VITE_MOCK?: string;
}
declare module '*.module.css' {
  const classes: Record<string, string>;
  export default classes;
}

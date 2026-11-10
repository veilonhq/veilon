import {
  Activity,
  Heart,
  Layers,
  Route,
  LayoutGrid,
  ScrollText,
  Settings as SettingsIcon,
  ShieldCheck,
  Users,
  UserCog,
  Waypoints,
  type LucideIcon,
} from 'lucide-react';
import type { Admin } from '@/api/types';
import type { MessageKey } from '@/i18n';
import { can, type Need } from '@/lib/perms';

export interface NavItem {
  to: string;
  key: MessageKey;
  /** shorter label for the dock */
  short?: MessageKey;
  icon: LucideIcon;
  /** what the signed-in admin needs to see this page */
  need?: Need;
}
export const PRIMARY: NavItem[] = [
  { to: '/', key: 'nav.dashboard', short: 'nav.home', icon: LayoutGrid },
  { to: '/users', key: 'nav.users', icon: Users, need: 'users_view' },
  { to: '/inbounds', key: 'nav.inbounds', icon: Waypoints, need: 'inbounds' },
  { to: '/logs', key: 'nav.logs', short: 'nav.logsShort', icon: ScrollText, need: 'logs' },
];
export const SECONDARY: NavItem[] = [
  { to: '/certs', key: 'nav.certs', icon: ShieldCheck, need: 'certs' },
  { to: '/settings', key: 'nav.settings', icon: SettingsIcon },
  { to: '/xray', key: 'nav.xray', icon: Route, need: 'sudo' },
  { to: '/admins', key: 'nav.admins', icon: UserCog, need: 'sudo' },
  { to: '/audit', key: 'nav.audit', icon: Activity, need: 'logs' },
  { to: '/groups', key: 'nav.groups', icon: Layers, need: 'groups' },
  { to: '/donate', key: 'nav.donate', icon: Heart },
];

/** The nav items the admin may open. */
export const allowed = (items: NavItem[], admin: Admin | null | undefined) => items.filter((it) => can(admin, it.need));


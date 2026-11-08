import type { Admin, Perm } from '@/api/types';

/** Every sub-admin flag, in the order the admin editor lists them. */
export const USER_PERMS: Perm[] = ['users_view', 'users_create', 'users_edit', 'users_delete', 'all_users'];
export const AREA_PERMS: Perm[] = ['inbounds', 'groups', 'settings', 'certs', 'logs'];
export const ALL_PERMS: Perm[] = [...USER_PERMS, ...AREA_PERMS];

/** What a page or action needs: one flag, any of several, or sudo. */
export type Need = Perm | Perm[] | 'sudo';

/**
 * Whether the signed-in admin may use something. Sudo admins hold every
 * flag; an admin whose flags are unknown (not loaded yet, or an older server
 * without permissions) is treated as unrestricted, so nothing hides by mistake.
 */
export function can(a: Admin | null | undefined, need?: Need): boolean {
  if (!need) return true;
  if (!a) return true;
  if (a.is_sudo) return true;
  if (need === 'sudo') return false;
  if (!a.permissions) return true;
  const list = Array.isArray(need) ? need : [need];
  return list.some((p) => a.permissions!.includes(p));
}

import type { SubData } from './format';

/** Development sample (vite dev at /sub.html only; never in the build output's data path). */
export const sample: SubData = {
  username: 'sara_k',
  status: 'active',
  up: 3.4 * 1024 ** 3,
  down: 18.9 * 1024 ** 3,
  used: 22.3 * 1024 ** 3,
  limit: 50 * 1024 ** 3,
  expire_at: new Date(Date.now() + 19 * 86_400_000).toISOString(),
  last_online_at: new Date(Date.now() - 3_600_000).toISOString(),
  online: false,
  sub_url: 'https://sub.example.com/sub/Q2xP9vKb7mZr4TnW1sYd8LcF3hJ6gA0e',
  title: 'Veilon',
  update_hours: 12,
  timezone: 'Asia/Tehran',
  links: [
    { name: 'Frankfurt Reality', protocol: 'vless', network: 'tcp', security: 'reality', uri: 'vless://00000000-0000-4000-8000-000000000000@203.0.113.10:443?security=reality&type=tcp#Frankfurt%20Reality' },
    { name: 'CDN WS', protocol: 'vmess', network: 'ws', security: 'tls', uri: 'vmess://eyJ2IjoiMiJ9' },
  ],
};

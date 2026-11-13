# Telegram bot

The Telegram bot lets you run the panel from inside Telegram: users, inbounds, nodes, server status, updates, backups, Xray settings, panel settings, admins and the event log. Important notifications (user data and expiry, nodes going offline, new releases) arrive there too. The bot runs inside the panel itself; it needs no separate program or server.

Only full (sudo) admins see the bot page in the panel. The bot's menus are in Persian.

## 1. Create a bot with BotFather

1. Open [@BotFather](https://t.me/BotFather) in Telegram and send `/newbot`.
2. Give a display name, then a username ending in `bot` (for example `myveilon_bot`).
3. BotFather gives you a token like `123456789:AAH...`. This token is the bot's key; do not share it.

## 2. Get your numeric Telegram id

The bot answers only the numeric ids you enter in the panel (a username is not enough, because it can change).

- Open an id bot such as [@userinfobot](https://t.me/userinfobot) and press `/start`; the number shown as Id is your id.
- Do the same for every other admin who should use the bot.

## 3. Turn the bot on in the panel

1. In the panel, open **More → Telegram bot**.
2. Enter the **Bot token**.
3. In **Admin numeric ids**, separate the ids with commas (for example `111111111, 222222222`). Persian digits and the Persian comma work too. At most 20 ids.
4. If the server cannot reach Telegram, set a **Proxy**: `socks5://host:port` or `http://host:port`.
5. Switch **Bot on** on and press **Save**.
6. Each admin presses `/start` in the bot once. Then press **Send test message**: for each id you see whether the message was delivered (when it was not, that admin usually has not pressed `/start` yet or has blocked the bot).

The **Bot status** card shows whether the bot is running, its username and the last error (for example a wrong token or no connection to Telegram).

## 4. Bot sections

The first time you press `/start`, the bot asks whether it should speak Persian or English. Each admin's choice is saved separately and can be changed at any time with the **🌐 Language** button in the main menu (or `/lang`); notifications are sent in that admin's language too.

`/start` or `/menu` opens the main menu:

- **👥 Users:** list with search and filters, create, edit, renew (days and data), reset usage, enable and disable, group and inbounds, connections, subscription link and QR, new subscription link, and delete with confirmation.
- **📥 Inbounds:** list, create, rename and change port, link address, where it runs (main server or nodes), full JSON edit, turn on and off, and delete with confirmation.
- **🌐 Nodes:** list with status, details, add a node and its install command, a new install command, agent update with progress, enable and disable, and delete with confirmation.
- **📊 Server status:** CPU, memory, disk, uptime, traffic, online users, core versions, logs (long logs as a file) and core restart with confirmation.
- **⬆️ Update:** check for a new release, release notes, and install with confirmation and progress.
- **💾 Backup:** download a database backup as a file, and restore it (see below).
- **🧭 Xray settings:** outbounds (from a share link or JSON) and routing rules: add, edit, turn on and off, reorder and delete. Changes stay in a draft until you **Apply** and confirm. You can also send the whole config as a JSON file; it is checked before it is applied.
- **⚙️ Panel settings:** the same fields as the web Settings page. Port, panel path, subscription path and certificate changes ask first. The bot token can only be changed in the web panel. Each notification type is switched on or off here as well.
- **👤 Admins:** list, add, role, permissions, quotas, password, disable, and delete with confirmation.
- **🗂 Event log:** panel events, 10 per page, filtered by admin.

## 5. Notifications

Notifications go to every admin id. Each type can be switched on or off on the bot page in the panel (**Notifications**) or in the bot (**Panel settings**):

- **User data:** when a user has used 90% and 100% of their data.
- **User expiry:** less than a day before expiry, and when it expires.
- **Node status:** when a node is unreachable on two checks in a row, and when it comes back.
- **New panel version:** when a new release is out.

Each event is announced once. If a user is renewed and reaches the same point again, it is announced again. Events from the same check are grouped in one message. A user notification has buttons to open the user and to add 30 days (with confirmation).

## 6. Backups and the 20 MB limit

- **Backup:** the bot makes a consistent copy of the database (SQLite or PostgreSQL) and sends it as a file. The file holds all panel data; keep it somewhere safe. Telegram sends files up to 50 MB.
- **Restore:** send the backup file to the bot. It is checked first, then applied after you confirm: a copy of the current database is kept in `<data>/backups/before-restore-*` and the panel restarts once on its own.
- **20 MB limit:** Telegram bots can only receive files up to **20 MB**. A larger backup has to be restored on the server itself.

## 7. Who the bot acts as

The bot does everything through the panel's own API, as the **first enabled full admin**, so the same checks and limits as in the web panel apply. In the **event log** its actions show as `telegram:<Telegram id>` with the IP `telegram`, so you can tell which Telegram admin did what. The bot cannot edit, disable or delete the admin it acts as.

If that admin is disabled, the bot acts as the next enabled full admin. The last enabled full admin cannot be disabled. The web **Admins** page also has an enable/disable switch next to each admin; a disabled admin cannot sign in and their current sessions end.

## 8. Security

- The bot token is stored only on the server. The panel and the API never return it in full; they show only the bot id and the last four characters (like `123456789:••••abcd`). It is not written to the log or the event log either. Leaving the token field empty keeps the current token; **Remove token** clears it and turns the bot off.
- Only the numeric ids in the admin list can use the bot. Messages from anyone else and from groups are ignored.
- Anyone with access to one of these Telegram accounts has full-admin access to the panel. Turn on Telegram two-step verification for them and enter only the ids you need.
- If you think the token has leaked, get a new one with `/revoke` in BotFather and save it in the panel.
- Risky actions (deleting, core restart, update, restoring a backup, applying the Xray config, and changing the panel port or path) always ask for confirmation first.

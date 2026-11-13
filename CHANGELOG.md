# Changelog

Each release's notes, Persian first. Older releases: https://github.com/veilonhq/veilon/releases

## Unreleased

### فارسی

- ورودی تازه **SOCKS** (پراکسی SOCKS5 تنها، موتور Xray): ورود با نام کاربری و رمز یا بدون احراز هویت و کلید UDP. روی سرور اصلی و نودها اجرا می شود و لینک `socks://`، Clash (`socks5`)، sing-box و اشتراک JSON دارد. بدون احراز هویت مصرف کاربران جدا شمرده نمی شود و محدودیت های کاربران روی آن کار نمی کند (هشدار در فرم ورودی). [راهنما](docs/fa/socks-proxy-login.md)
- ورود پراکسی دلخواه برای هر کاربر: نام کاربری و رمز اختیاری برای ورودی های HTTP، Mixed و SOCKS در فرم کاربر (فقط وقتی یکی از این ورودی ها انتخاب شده باشد نشان داده می شود)، در API (`proxy_username` و `proxy_password`) و ربات تلگرام. خالی یعنی همان ورود خودکار قبلی. نام کاربری بین کاربران یکتا است و مصرف، آنلاین بودن و محدودیت های IP و سرعت همچنان به همان کاربر نسبت داده می شوند. روی نودها برنامه نود همین نسخه لازم است
- **رمزگذاری VLESS** (VLESS Encryption، Xray نسخه ۲۵.۹.۵ به بعد) در فرم ورودی VLESS: کلید سرور X25519 یا ML-KEM-768 (پساکوانتومی)، ظاهر native، xorpub یا random، اتصال دوباره 0-RTT (با عمر بلیت) یا 1-RTT، ساخت کلید در پنل با Xray نصب شده، و Vision روی همه انتقال ها وقتی رمزگذاری روشن است. لینک ها، Clash و JSON رشته encryption را دارند؛ sing-box این قابلیت را ندارد و این ورودی ها در خروجی sing-box نمی آیند. روی نودها هم کار می کند، در ربات تلگرام و API (`vless_encryption`، `POST /inbounds/vless-enc-keys`) هم هست. بدون تغییر پایگاه داده. [راهنما](docs/fa/vless-encryption.md)
- **تنظیمات پیشرفته ورودی** (امنیت و انتقال Xray) در بخش بسته **تنظیمات پیشرفته** مرحله های اتصال و امنیت فرم ورودی؛ دست نخورده همان تنظیمات قبلی می ماند: فال بک ها (VLESS و Trojan روی RAW با TLS یا Reality)، TLS (کمترین و بیشترین نسخه، cipher ها، رد SNI ناشناس، پین گواهی در تنظیمات کلاینت، ECH با ساخت کلید)، Reality (ML-DSA-65 با ساخت کلید، نسخه کلاینت، اختلاف ساعت، محدودیت سرعت فال بک)، سوکت و PROXY protocol، پوشش هدر HTTP روی RAW، هدر ها و heartbeat در WS و HTTPUpgrade، تنظیمات gRPC، XHTTP (padding، اندازه POST، xmux، دانلود از آدرس دیگر مثل CDN)، انتقال تازه mKCP، UDP و انتقال های تازه برای Shadowsocks، Hysteria 2 (کنترل ازدحام و masquerade)، TUIC، ماسک های fragment و noise سمت سرور، پروکسی معکوس VLESS (پل) با دانلود تنظیمات پل و جزئیات شنود. لینک ها، Clash، sing-box و JSON هر چه کلاینت پشتیبانی می کند را دارند. روی نودها هم کار می کند، در ربات تلگرام (**⚙️ تنظیمات پیشرفته**) و API (`advanced`، `POST /inbounds/mldsa65-keys`، `POST /inbounds/ech-keys`، `GET /inbounds/{id}/reverse-bridge`) هم هست. بدون تغییر پایگاه داده. [راهنما](docs/fa/xray-advanced.md)
- بررسی نسخه Xray-core برای تنظیمات پیشرفته و رمزگذاری VLESS: اگر Xray-core سرور اصلی یا یک نود برای تنظیمی که اضافه می کنید قدیمی باشد ذخیره نمی شود، و اگر بعدا معلوم شود قدیمی است فقط همان ورودی روی همان سرور کنار گذاشته می شود و بقیه ورودی ها کار می کنند. هشدار در فرم ورودی، کارت ورودی و کارت نود، و در ربات تلگرام (صفحه ورودی و نود)؛ در API `xray_needs` و `xray_too_old`. [راهنما](docs/fa/xray-advanced.md)
- مهاجرت پایگاه داده: schema 12 (ستون های users.proxy_username و users.proxy_password و ایندکس یکتای نام های غیر خالی)، فقط افزودنی

### English

- New **SOCKS** inbound (plain SOCKS5 proxy on Xray): sign in with username and password or no authentication, plus a UDP switch. Runs on the main server and on nodes, with `socks://` links, Clash (`socks5`), sing-box and JSON subscription entries. Without authentication users cannot be counted individually and their limits do not apply (the inbound form warns about it). [Guide](docs/en/socks-proxy-login.md)
- Custom proxy login per user: an optional username and password for HTTP, Mixed and SOCKS inbounds in the user form (shown only while one of these inbounds is selected), in the API (`proxy_username`, `proxy_password`) and in the Telegram bot. Empty keeps the previous automatic login. The username is unique across users, and traffic, online state and IP and speed limits still belong to the same user. Nodes need this release's node agent
- **VLESS Encryption** (Xray 25.9.5+) in the VLESS inbound form: X25519 or ML-KEM-768 (post-quantum) server key, native, xorpub or random appearance, 0-RTT (with a ticket lifetime) or 1-RTT reconnects, in-panel key generation with the installed Xray, and Vision on every transport while encryption is on. Links, Clash and JSON carry the encryption string; sing-box has no support, so these inbounds are left out of its output. Works on nodes, in the Telegram bot and the API (`vless_encryption`, `POST /inbounds/vless-enc-keys`). No database change. [Guide](docs/en/vless-encryption.md)
- **Advanced inbound settings** (Xray security and transport) in a collapsed **Advanced settings** block on the connection and security steps of the inbound form; untouched, the config stays as before: fallbacks (VLESS and Trojan on RAW with TLS or Reality), TLS (min and max version, ciphers, reject unknown SNI, certificate pin in client configs, ECH with key generation), Reality (ML-DSA-65 with key generation, client versions, clock difference, fallback rate limits), socket and PROXY protocol, HTTP header obfuscation on RAW, WS and HTTPUpgrade headers and heartbeat, gRPC options, XHTTP (padding, POST sizes, xmux, download through another address such as a CDN), the new mKCP transport, UDP switch and new transports for Shadowsocks, Hysteria 2 congestion and masquerade, TUIC options, server-side fragment and noise masks, VLESS reverse proxy (bridge) with a bridge config download, and sniffing details. Links, Clash, sing-box and JSON carry what each client supports. Works on nodes, in the Telegram bot (**⚙️ Advanced settings**) and the API (`advanced`, `POST /inbounds/mldsa65-keys`, `POST /inbounds/ech-keys`, `GET /inbounds/{id}/reverse-bridge`). No database change. [Guide](docs/en/xray-advanced.md)
- Xray-core version check for advanced settings and VLESS Encryption: saving is refused when the main server or a node is known to run an Xray-core too old for a setting being added, and a server found too old later leaves out only that inbound while its other inbounds keep running. Warnings in the inbound form, the inbound card and the node card, and in the Telegram bot (inbound and node screens); `xray_needs` and `xray_too_old` in the API. [Guide](docs/en/xray-advanced.md)
- Database migration: schema 12 (users.proxy_username and users.proxy_password columns and a unique index on non-empty names), additive only

## v0.3.10 (2026-11-13)

### فارسی

- ربات تلگرام انگلیسی هم صحبت می کند: اولین `/start` می پرسد زبان فارسی باشد یا انگلیسی، انتخاب هر مدیر جدا ذخیره می شود و از دکمه **🌐 زبان** در منوی اصلی (یا `/lang`) عوض می شود. در انگلیسی تاریخ ها میلادی و عدد ها لاتین هستند و اعلان ها هم به زبان هر مدیر فرستاده می شوند

### English

- The Telegram bot now speaks English too: the first `/start` asks for Persian or English, each admin's choice is saved separately and can be changed with the **🌐 Language** button in the main menu (or `/lang`). In English, dates are Gregorian and digits are Latin, and notifications go out in each admin's language

## v0.3.9 (2026-11-12)

### فارسی

- محدودیت سرعت برای کاربران AmneziaWG: همان محدودیت دانلود و آپلود فرم کاربر اکنون روی ورودی های AmneziaWG هم اعمال می شود. هر کاربر AmneziaWG آدرس داخلی تونل خودش را دارد، پس محدودیت فقط بر اساس همان آدرس اعمال می شود: دانلود با tc روی رابط همان ورودی (`vawg<شماره ورودی>`) و آپلود با یک محدودکننده nftables برای هر کاربر (`table inet veilon_awgshape`) که بین همه ورودی های AmneziaWG آن کاربر مشترک است. کاربر بدون محدودیت روی همان ورودی با سرعت کامل کار می کند. محدودیت سرعت Xray و TUIC مثل قبل کار می کند و جداست. برای سرور اصلی و نودها (برنامه نود این نسخه؛ نودهای قدیمی تر کاربران AmneziaWG را محدود نمی کنند). MTProto همچنان محدودیت سرعت ندارد
- ربات تلگرام برای مدیریت پنل: کاربران، ورودی ها، نودها، وضعیت سرور، بروزرسانی پنل، پشتیبان و بازگردانی، تنظیمات Xray، تنظیمات پنل، مدیران و گزارش فعالیت، با تایید برای کارهای خطرناک. ربات داخل خود پنل اجرا می شود و فقط به شناسه های عددی مدیران جواب می دهد. [راهنما](docs/fa/telegram-bot.md)
- اعلان ها در تلگرام: ۹۰ و ۱۰۰ درصد حجم کاربر، کمتر از یک روز تا انقضا و منقضی شدن، قطع و وصل شدن نود، نسخه تازه پنل. هر نوع جدا روشن یا خاموش می شود و هر رویداد یک بار اعلام می شود
- صفحه تازه **بیشتر ← ربات تلگرام** (فقط مدیر ارشد): توکن (فقط بخشی از آن نشان داده می شود و خالی گذاشتن آن توکن فعلی را نگه می دارد)، شناسه مدیران (عدد فارسی هم پذیرفته می شود)، پروکسی اختیاری، روشن و خاموش، اعلان ها، دکمه پیام آزمایشی با نتیجه برای هر مدیر و وضعیت زنده ربات
- پشتیبان پایگاه داده از ربات (SQLite و PostgreSQL) و بازگردانی با بررسی فایل، نسخه ایمنی از پایگاه داده فعلی و راه اندازی دوباره خودکار پنل؛ سقف دریافت فایل در ربات تلگرام ۲۰ مگابایت است
- مدیران را می توان غیرفعال کرد (کلید در صفحه مدیران وب و در ربات): مدیر غیرفعال نمی تواند وارد شود و توکن های او پذیرفته نمی شوند؛ آخرین مدیر ارشد فعال را نمی توان غیرفعال کرد
- گزارش فعالیت را می توان بر اساس مدیر فیلتر کرد (API: `GET /audit?admin=`)؛ کارهای ربات با نام `telegram:<شناسه>` ثبت می شوند
- مهاجرت پایگاه داده: schema 10 (جدول telegram_notices برای جلوگیری از اعلان تکراری) و schema 11 (ستون admins.disabled)، هر دو فقط افزودنی

### English

- Speed limits for AmneziaWG users: the user form's download and upload limits now apply on AmneziaWG inbounds too. Every AmneziaWG user has its own tunnel address, so the limit is applied by that address only: download by tc on the inbound's interface (`vawg<inbound id>`), upload by one nftables limiter per user (`table inet veilon_awgshape`) shared across all of that user's AmneziaWG inbounds. A user with no limit on the same inbound runs at full speed. Xray and TUIC speed limits work as before and separately. Main server and nodes (this release's node agent; older agents do not limit AmneziaWG users). MTProto still has no speed limits
- Telegram bot to manage the panel: users, inbounds, nodes, server status, panel update, backup and restore, Xray settings, panel settings, admins and the event log, with confirmation for risky actions. The bot runs inside the panel and answers only the admins' numeric ids. [Guide](docs/en/telegram-bot.md)
- Telegram notifications: a user at 90% and 100% of their data, less than a day before expiry and expired, a node going offline and coming back, a new panel release. Each type can be switched on or off, and each event is announced once
- New page **More → Telegram bot** (full admins only): token (only a hint is shown; leaving it empty keeps the current one), admin ids (Persian digits work), optional proxy, on/off, notifications, a test message button with a result per admin, and the bot's live status
- Database backup from the bot (SQLite and PostgreSQL) and restore with a file check, a safety copy of the current database and an automatic panel restart; Telegram bots can receive files up to 20 MB
- Admins can be disabled (a switch on the web Admins page and in the bot): a disabled admin cannot sign in and their tokens are refused; the last enabled full admin cannot be disabled
- The event log can be filtered by admin (API: `GET /audit?admin=`); the bot's actions are logged as `telegram:<id>`
- Database migrations: schema 10 (telegram_notices table, so a notification is not sent twice) and schema 11 (admins.disabled column), both additive only

## v0.3.8 (2026-11-12)

### فارسی

- رفع ایراد مهم محدودیت سرعت: محدودیت سرعت یک کاربر به کاربران دیگر روی همان اینباند هم می رسید. علت: محدودیت بر اساس IP دستگاه اعمال می شد، پس هر کاربر دیگری که از همان IP وصل می شد (دو پروفایل روی یک گوشی، یک مودم یا NAT اپراتور) هم کند می شد؛ حتی ترافیک دیگر آن IP مثل AmneziaWG. اکنون هر اتصال (IP و پورت دستگاه از لاگ دسترسی) به کاربر خودش نسبت داده می شود: اتصال های کاربر محدود با nftables نشانه گذاری می شوند (`table inet veilon_shape`)، دانلود با tc روی همان نشانه و آپلود با یک قانون nftables برای هر کاربر محدود می شود. کاربر بدون محدودیت حتی روی همان اینباند و از همان IP با سرعت کامل کار می کند. برای سرور اصلی و نودها (برنامه نود این نسخه؛ نودهای قدیمی تر تا به روزرسانی مثل قبل بر اساس IP محدود می کنند). Xray و TUIC (sing-box)
- روی سرور، محدودیت سرعت اکنون به nftables هم نیاز دارد (نصب کننده آن را نصب می کند؛ بدون آن یک هشدار در لاگ ثبت می شود و محدودیت سرعت اعمال نمی شود)
- انقضای کاربر: کادر تعداد روز در «چند روز از امروز» و «شروع با اولین اتصال» در فرم کاربر جدید و ویرایش کاربر اکنون آزادانه قابل تایپ است، هر عدد صحیح از ۱ (مثلا ۱ روز)، با صفحه کلید فارسی یا انگلیسی و صفحه کلید عددی در گوشی؛ راهنمای «تعداد روز» و واحد «روز» دارد و خط «منقضی می شود در ...» همان لحظه به روز می شود. قبلا کادر عدد فارسی را نمی پذیرفت و پس از پاک کردن «۰» نشان می داد
- ورودی های AmneziaWG و MTProto (پروکسی تلگرام) حالا مثل ورودی های Xray روی نودها اجرا می شوند: در فرم ورودی «محل اجرا» را روی نودها بگذارید (گزینه «روی سرور اصلی هم اجرا شود» هم مثل قبل). نود همان موتورهای سرور اصلی را اجرا می کند (سرور داخلی AmneziaWG و mtg-multi که بار اول روی نود دانلود می شود)
- لینک ها، QR و فایل `.conf` کاربران برای این ورودی ها آدرس نود را دارند؛ ترافیک هر کاربر روی نود مثل Xray در مصرف کاربر و ترافیک نود حساب می شود؛ وضعیت آنلاین این دو با قاعده ترافیک است (فهرست اتصال باز ندارند)
- محدودیت IP روی نود برای این دو هم کار می کند: IP کاربر (endpoint های AmneziaWG که ترافیک دارند و IP های زنده MTProto) در «اتصال ها» دیده می شود و در محدودیت IP همه سرورها حساب می شود، و IP اضافه با همان قانون nftables روی پورت های نود مسدود می شود
- این قابلیت برنامه نود همین نسخه را می خواهد. نودهای v0.3.7 و قدیمی تر در فرم ورودی با «نیاز به بروزرسانی» نشان داده می شوند و برای این دو پروتکل انتخاب نمی شوند (API: خطای ۴۰۹ `node_agent_old`)؛ ورودی ای که از قبل روی چنین نودی باشد تا بروزرسانی نود روی آن اجرا نمی شود و فرم نود همین را می گوید. ورودی های Xray روی نودهای قدیمی مثل قبل کار می کنند
- بررسی پورت تکراری برای هر سرور شامل این ورودی ها هم هست
- ورودی ها و کاربران موجود سرور اصلی تغییری نمی کنند و پایگاه داده مهاجرت تازه ای ندارد (schema همان 9)

### English

- Critical speed limit fix: one user's speed limit also slowed other users on the same inbound. Cause: limits were applied by device IP, so every other user connecting from the same IP (two profiles on one phone, one router, carrier NAT) was slowed too, even other traffic of that IP such as AmneziaWG. Now each connection (client IP and port from the access log) belongs to its own user: a limited user's connections are marked with nftables (`table inet veilon_shape`), download is shaped by tc on that mark and upload is policed by one nftables rule per user. A user with no limit runs at full speed even on the same inbound and from the same IP. Main server and nodes (this release's node agent; older agents keep shaping by IP until updated). Xray and TUIC (sing-box)
- Speed limits now need nftables on the server as well (the installer installs it; without it the log shows one warning and speed limits are not applied)
- User expiry: the number of days in "Days from today" and "Start on first connect" in the new user and edit user forms can now be typed freely, any whole number from 1 (e.g. 1 day), with Persian or Latin digits and the numeric keypad on phones; it has a "Number of days" placeholder and a "days" unit, and the "Expires on ..." line updates as you type. Before, the box rejected Persian digits and showed "0" once cleared
- AmneziaWG and MTProto (Telegram proxy) inbounds can now run on nodes like Xray inbounds: set "Runs on" to nodes in the inbound form ("Also run on the main server" works as before). The node runs the same engines as the main server (the built-in AmneziaWG server and mtg-multi, downloaded on the node on first use)
- Users' links, QR codes and `.conf` files for these inbounds point at the node; each user's traffic on the node counts towards their usage and the node's traffic like Xray's; their online status uses the traffic rule (these engines have no open-connection list)
- IP limits work on nodes for these two as well: the user's IPs (AmneziaWG endpoints that send traffic, MTProto's live client addresses) show in "Connections" and count in the IP limit across all servers, and an extra IP is dropped on the node's ports with the same nftables rule
- This needs this release's node agent. Nodes on v0.3.7 or older are marked "needs update" in the inbound form and cannot be picked for these two protocols (API: `409 node_agent_old`); an inbound already placed on such a node is skipped there until the node is updated, and the node's form says so. Xray inbounds on older nodes keep working as before
- The per-server port check covers these inbounds too
- Existing inbounds and users on the main server are unchanged, and there is no new database migration (schema stays 9)

## v0.3.7 (2026-11-11)

### فارسی

- بخش «اتصال ها» در جزئیات کاربر سریع تر به روز می شود: IP تازه حدود ۲ ثانیه پس از دیده شدن در سرور اصلی ثبت می شود (قبلا تا یک دقیقه) و IP های نودها همراه پرسش ۵ ثانیه ای وضعیت آنلاین می رسند (برنامه نود این نسخه؛ نودهای قدیمی تر مثل قبل هر ۱۰ ثانیه). آخرین دیده شدن و تعداد اتصال IP های قبلی مثل قبل هر دقیقه نوشته می شود. این بخش وقتی باز است هر ۵ ثانیه تازه می شود (قبلا ۱۵ ثانیه) تا دستگاه های تازه (HWID) هم زود دیده شوند

### English

- The user's "Connections" section updates faster: a new IP is written about 2 seconds after the main server sees it (was up to a minute), and node IPs arrive with the 5-second online poll (this release's node agent; older agents keep the 10-second stats round). Last seen and hits of known IPs are still written once a minute. The section refreshes every 5 seconds while open (was 15), so new HWID devices show up quickly too

## v0.3.6 (2026-11-11)

### فارسی

- وضعیت آنلاین سریع: کاربر چند ثانیه پس از اتصال «آنلاین» می شود و حدود ۵ تا ۱۰ ثانیه پس از قطع اتصال به «آخرین اتصال» با تاریخ و ساعت درست برمی گردد؛ در کارت کاربران، جزئیات کاربر، فیلتر «آنلاین»، شمار آنلاین داشبورد و صفحه اشتراک. سرور اصلی و نودها هر ۵ ثانیه فهرست اتصال های باز Xray را می خوانند (`statsUserOnline`)، به جای قاعده قبلی «ترافیک ارسالی در ۶۰ ثانیه گذشته» که کاربری را که فقط دانلود می کرد گاهی آفلاین نشان می داد و پس از قطع تا یک دقیقه آنلاین نگه می داشت
- «آخرین اتصال» لحظه ای است که کاربر آخرین بار آنلاین دیده شد. پایگاه داده فقط هنگام وصل و قطع شدن و در طول اتصال حداکثر هر ۶۰ ثانیه یک بار نوشته می شود، نه در هر بار خواندن آمار
- نودها: برنامه نود این نسخه فهرست کاربران آنلاین را به سرور اصلی می دهد (`GET /v1/online`، قابلیت `online`). نودهای با برنامه v0.3.5 و قدیمی تر، AmneziaWG، MTProto و Xray قدیمی تر از 26.3 همان قاعده ترافیک ۶۰ ثانیه را دارند؛ کاربرانی که از مسیر داخلی وصل می شوند (TUIC از راه sing-box) هم با ترافیک سنجیده می شوند
- پنل مدیریت وقتی صفحه دیده می شود هر ۵ ثانیه وضعیت را تازه می کند و صفحه اشتراک باز هم هر ۵ ثانیه بدون بارگذاری دوباره به روز می شود (`?format=live`: داده سبک بدون ساختن کانفیگ ها)
- پس از به روزرسانی، کانفیگ Xray سرور اصلی و نودها یک بار دوباره اعمال می شود و اتصال های باز یک بار قطع و دوباره وصل می شوند

### English

- Fast online status: a user shows "Online" a few seconds after connecting and switches to "Last online" with the right date and time about 5 to 10 seconds after disconnecting, in the users cards, user details, the "Online" filter, the dashboard's online count and the subscription page. The main server and the nodes read Xray's list of open connections every 5 seconds (`statsUserOnline`) instead of the earlier rule "sent traffic in the last 60 seconds", which could show a user who was only downloading as offline and kept a user online for up to a minute after they left
- "Last online" is the moment the user was last seen online. The database is written only when a user comes online or goes offline, and at most once every 60 seconds while they stay connected, not on every stats poll
- Nodes: this release's node agent gives the main server its online users (`GET /v1/online`, capability `online`). Nodes with a v0.3.5 or older agent, AmneziaWG, MTProto and Xray older than 26.3 keep the 60-second traffic rule; users reaching Xray through the internal hop (TUIC via sing-box) are judged by traffic too
- The admin panel refreshes the status every 5 seconds while the page is visible, and an open subscription page updates itself every 5 seconds without a reload (`?format=live`: a light answer that does not render the configs)
- After the update, the Xray config of the main server and the nodes is applied once more, so open connections drop and reconnect once

## v0.3.5 (2026-11-11)

### فارسی

- بروزرسانی نود با یک دکمه: کارت هر نود نسخه برنامه نود را نشان می دهد و اگر از نسخه سرور اصلی قدیمی تر باشد، دکمه «بروزرسانی نود» دارد؛ «بروزرسانی همه نودهای قدیمی» هم همه را با هم بروزرسانی می کند (فقط مدیران کل). نود فایل مخصوص پردازنده اش را از همان منبع بروزرسانی پنل دانلود می کند، با checksum انتشار بررسی می کند، از نسخه فعلی پشتیبان می گیرد، جایگزین می کند و دوباره راه اندازی می شود؛ اگر نسخه جدید تا ۹۰ ثانیه دوباره وصل نشود، نسخه قبلی خودکار برمی گردد. گواهی نود دست نمی خورد. مراحل (دانلود، بررسی، راه اندازی، انجام شد یا خطا با دلیل) با زمان به وقت پنل نمایش داده می شوند و در گزارش فعالیت ها ثبت می شوند. API: `POST /nodes/{id}/update` و `POST /nodes/update-outdated`
- نودهایی که برنامه نود قدیمی تر از این نسخه دارند یک بار باید با «دستور نصب تازه» بروزرسانی شوند؛ کارت آن ها همین را می گوید
- حذف نود (یا برداشتن ورودی از آخرین نودش) دیگر ورودی هایی را که فقط روی نود بودند بدون بررسی پورت به سرور اصلی برنمی گرداند: اگر پورت روی سرور اصلی گرفته شده باشد، با خطای ۴۰۹ و نام ورودی و پورت رد می شود
- صفحه اشتراک: کاشی «آخرین اتصال» وقتی کاربر همین الان وصل است «آنلاین» را با نقطه سبز نشان می دهد (همان قاعده آنلاین پنل مدیریت، با احتساب اتصال از نودها)؛ در غیر این صورت مثل قبل تاریخ و ساعت آخرین اتصال. داده صفحه و `?format=status` فیلد `online` دارند

### English

- One-button node update: each node card shows its agent version and, when it is older than the main server's, an "Update node" button; "Update all outdated nodes" updates them all (sudo admins only). The node downloads the release file for its CPU from the panel's update source, checks it against the release checksums, keeps a backup, swaps the binary and restarts; if the new version does not reconnect within 90 seconds, the previous version is restored automatically. The node's certificate is untouched. Progress (downloading, verifying, restarting, done or failed with the reason) shows with times in the panel's time zone and is audit-logged. API: `POST /nodes/{id}/update` and `POST /nodes/update-outdated`
- Nodes running an agent older than this release need one last manual update with "New install command"; their card says so
- Deleting a node (or taking an inbound off its last node) no longer moves node-only inbounds back to the main server without a port check: a port already used on the main server is refused with a 409 naming the inbound and the port
- Subscription page: the "Last online" tile shows "Online" with a green dot while the user is connected (the admin panel's online rule, node traffic included); otherwise the last date and time as before. The page data and `?format=status` carry an `online` field

## v0.3.4 (2026-11-11)

### فارسی

- صفحه اشتراک برای مرورگر: وقتی کاربر لینک اشتراک را در مرورگر باز می کند، صفحه ای با طراحی پنل می بیند: وضعیت، حلقه مصرف (حجم مصرف شده از کل یا ∞)، آپلود، دانلود، روز باقی مانده، تاریخ انقضا (تقویم شمسی، به وقت پنل)، آخرین اتصال، لینک اشتراک با کپی و QR، همه کانفیگ ها با کپی و QR و «کپی همه»، و فاصله به روزرسانی خودکار. تم تیره و روشن، فارسی و انگلیسی
- برنامه های VPN (v2rayNG، Hiddify، Streisand، Shadowrocket، sing-box، کلش و ...) همان اشتراک قبلی را بدون هیچ تغییری می گیرند؛ با `?format=raw` در مرورگر هم خود اشتراک برگردانده می شود و `?format=status` داده صفحه را به صورت JSON می دهد
- کاربران غیرفعال، منقضی یا با حجم تمام شده هم صفحه را با وضعیت خودشان می بینند
- بروزرسانی پنل از تنظیمات به صفحه جداگانه «بروزرسانی پنل» در منوی «بیشتر» منتقل شد (فقط مدیران کل)؛ بنر به روزرسانی در همین صفحه نمایش داده نمی شود
- بسته شدن منوی «بیشتر» در موبایل درست شد: منو دیگر لحظه ای جابجا و جدا از نوار پایین دیده نمی شود و بسته شدن دقیقا برعکس باز شدن است. منوی حساب کاربری هم همین طور

### English

- Subscription page for browsers: opening a subscription link in a browser shows a page in the panel's design: status, a usage ring (used of total, or ∞), upload, download, days left, expiry date (Persian calendar in Persian, panel time zone), last online, the subscription link with copy and QR, every config with copy and QR plus "copy all", and the auto-update interval. Dark and light themes, Persian and English
- VPN clients (v2rayNG, Hiddify, Streisand, Shadowrocket, sing-box, clash and others) keep receiving the exact same subscription; `?format=raw` returns the subscription in a browser too, and `?format=status` returns the page data as JSON
- Disabled, expired and out-of-data users also see the page, with their status
- Panel update moved from Settings to its own "Panel update" page in the More menu (sudo admins only); the update banner is hidden on that page
- Fixed the More menu closing on mobile: it no longer flashes shifted and detached from the bottom bar, and closing is the exact reverse of opening. Same fix for the account menu

## v0.3.3 (2026-11-11)

### فارسی

- بررسی دستی به روزرسانی در تنظیمات؛ بررسی خودکار هر ۶ ساعت مثل قبل ادامه دارد
- بنر به روزرسانی جمع و جور و یک خطی شد و در صفحه تنظیمات نمایش داده نمی شود
- پیام موفقیت به روزرسانی بعد از ۱۰ ثانیه خودش بسته می شود و برای همان نسخه دوباره نمایش داده نمی شود
- پورت ورودی فقط روی همان سرور باید یکتا باشد: یک پورت (مثلا ۴۴۳) می تواند هم روی سرور اصلی و هم روی ورودی ای که فقط روی نود اجرا می شود باشد؛ دو ورودی روی یک سرور (سرور اصلی یا یک نود) با پورت یکسان پذیرفته نمی شوند و پیام خطا نام سرور را می گوید. پورت ارتباط هر نود با پنل (مثلا ۶۲۰۵۰) روی همان نود رزرو است
- `veilon firewall` و `veilon ports` فقط پورت ورودی هایی را باز می کنند که روی سرور اصلی اجرا می شوند

### English

- Manual update check in Settings; the automatic check every 6 hours stays
- Compact one-row update banner, hidden on the Settings page
- The update success notice closes by itself after 10 seconds and stays dismissed for that version
- Inbound ports only need to be unique per server: the same port (e.g. 443) can be used on the main server and by a node-only inbound; two inbounds on one server (the main server or the same node) with the same port are refused, and the error names the server. Each node's agent port (e.g. 62050) is reserved on that node
- `veilon firewall` and `veilon ports` only open ports of inbounds the main server runs

## v0.3.2 (2026-11-10)

### فارسی

- **محل اجرا** در فرم ورودی: هر ورودی روی سرور اصلی یا روی یک یا چند نود اجرا می شود؛ صفحه نودها فقط خود نودها را مدیریت می کند
- لینک های ورودی هایی که روی نود هستند به آدرس همان نود اشاره می کنند (یک لینک برای هر نود)
- محدودیت IP کاربر روی همه سرورها با هم شمرده می شود (سرور اصلی و همه نودها)
- ترافیک به تفکیک سرور در داشبورد و صفحه نودها؛ عدد کارت هر نود همان مصرف شمرده شده کاربران روی آن نود است
- محدودیت سرعت کاربران روی نودها هم اعمال می شود
- پورت ورودی جدید دیگر از پیش پر نمی شود و وارد کردنش لازم است
- حذف ادمین: `veilon admin delete`، و ادمینی که هنوز کاربر دارد حذف نمی شود
- هشدار واضح: پنل را روی سرور نود نصب نکنید
- یادداشت های به روزرسانی در پنجره به روزرسانی تمیز و فقط به زبان پنل نمایش داده می شوند
- منطقه زمانی: نمودار ترافیک و خروجی خط فرمان با منطقه زمانی پنل (با در نظر گرفتن تغییر ساعت)
- کلاینت های IPv6 محلی (link-local) هم در محدودیت IP شمرده می شوند
- رفع مشکلات ظاهری: پرش صفحه داشبورد هنگام بارگذاری و بریده شدن نسخه هسته Xray

**مهم:** برای محدودیت IP و محدودیت سرعت روی نودها، برنامه هر نود را هم به روز کنید: در صفحه نودها نود را باز کنید، **دستور نصب تازه** را بزنید و آن را روی سرور نود اجرا کنید. عدد ترافیک کارت نودها یک بار از صفر شروع می شود.

### English

- **Runs on** in the inbound form: each inbound runs on the main server or on one or more nodes; the Nodes page manages nodes only
- Links of inbounds on nodes use the node's address (one link per node)
- A user's IP limit counts across all servers (main server and every node)
- Traffic by server on the Dashboard and Nodes page; a node card shows the users' counted usage on that node, the same figure
- User speed limits apply on nodes too
- A new inbound's port is no longer pre-filled and is required
- Admin delete: `veilon admin delete`, and an admin who still owns users cannot be deleted
- Clear warning not to install the panel on a node server
- The update dialog shows the release notes cleanly, in the panel language
- Time zone fixes: traffic chart and CLI output use the panel time zone (DST-aware)
- IPv6 link-local clients now count for IP limits
- QA fixes: no Dashboard layout shift on load, Xray core version no longer cut off

**Important:** nodes must update their agent for IP limits and speed limits on nodes: open the node on the Nodes page, choose **New install command** and run it on the node server. Node card traffic totals start again from zero once.

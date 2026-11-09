<div dir="rtl">

# Veilon

**توسعه داده شده توسط [@lxhosein](https://t.me/lxhosein)**

Veilon یک پنل مدیریت پروکسی رایگان و متن باز است. یک فایل اجرایی Go همه کار را انجام می دهد: رابط وب، API، لینک اشتراک (Subscription) و مدیریت هسته های پروکسی. نصب با یک دستور انجام می شود و رابط کاربری فارسی و انگلیسی دارد.

[English version below](#english)

## فهرست

- [امکانات](#امکانات)
- [پیش نیازها](#پیش-نیازها)
- [نصب قدم به قدم برای تازه کارها](#نصب-قدم-به-قدم-برای-تازه-کارها)
- [باز کردن پورت ها در فایروال](#باز-کردن-پورت-ها-در-فایروال)
- [گواهی SSL: دامنه یا IP سرور](#گواهی-ssl-دامنه-یا-ip-سرور)
- [اولین ورود، ساخت ورودی و کاربر](#اولین-ورود-ساخت-ورودی-و-کاربر)
- [اتصال با برنامه های کلاینت](#اتصال-با-برنامه-های-کلاینت)
- [منوی مدیریت veilon](#منوی-مدیریت-veilon)
- [رفع مشکلات رایج](#رفع-مشکلات-رایج)
- [حمایت از توسعه دهنده](#حمایت-از-توسعه-دهنده)
- [مجوز](#مجوز)

## امکانات

- **پروتکل ها:** VLESS، VMess، Trojan، Shadowsocks (شامل 2022)، Hysteria2، TUIC، WireGuard، AmneziaWG، HTTP، Mixed (HTTP + SOCKS)، Tunnel، TUN، MTProto و MASQUE
- **امنیت اتصال:** Reality و TLS، انتقال TCP، WebSocket، gRPC، HTTPUpgrade و XHTTP
- **گواهی رایگان Let's Encrypt** برای دامنه (۹۰ روزه) یا مستقیم برای IP سرور (کوتاه مدت)، با تمدید خودکار
- **مدیریت کاربران:** سقف حجم، تاریخ انقضا، شروع انقضا از اولین اتصال، ریست دوره ای حجم (روزانه، هفتگی، ماهانه)، وضعیت آنلاین و آخرین اتصال، عملیات گروهی
- **لینک اشتراک** برای v2rayNG، Hiddify، Streisand، sing-box و Clash، به همراه کد QR و فایل `.conf` برای WireGuard و AmneziaWG
- **ورودی ها (Inbounds):** ساخت با راهنمای مرحله به مرحله؛ پورت و نام هر ورودی یکتاست و پنل همان لحظه هشدار می دهد
- **داشبورد زنده:** ترافیک لحظه ای واقعی کارت شبکه، کاربران آنلاین، مصرف CPU، حافظه و دیسک؛ مجموع ترافیک برابر با جمع مصرف کاربران است
- **کنترل هسته Xray از داشبورد:** روشن و خاموش کردن هسته (خاموش بودن بعد از ری استارت پنل هم می ماند)، انتخاب و نصب هر نسخه Xray از لیست نسخه های رسمی (با بررسی فایل و برگشت خودکار در صورت خطا) و به روزرسانی فایل های geo
- **منطقه زمانی پنل:** همه تاریخ ها، نمودارها، ریست های دوره ای و بازه های زمانی کاربران به وقت منطقه زمانی انتخابی در تنظیمات (مثلا تهران؛ پیش فرض UTC)
- **لاگ ها:** لاگ Xray، هسته های دیگر و خود پنل، جدا یا همه با هم، با زمان دقیق هر خط و سطح (info، warning، error)
- **محدودیت های هر کاربر:** تعداد IP همزمان، تعداد دستگاه (HWID)، سرعت دانلود و آپلود جدا، سقف حجم روزانه، ضریب مصرف، مسدود کردن تورنت، بازه زمانی مجاز (ساعت و روزهای هفته به وقت منطقه زمانی پنل)؛ برای TUIC هم کار می کند
- **گزارش اتصال ها:** IP ها و دستگاه های هر کاربر با اولین و آخرین زمان دیده شدن، و مسدود یا آزاد کردن هر IP یا دستگاه
- **گروه ها:** گروه بسازید، کاربران را در گروه بگذارید، برای گروه ورودی ها و مقادیر پیش فرض تعیین کنید و مسدودسازی تبلیغات را روشن کنید
- **چند مدیر با دسترسی جدا:** مدیر اصلی و مدیران زیرمجموعه؛ برای هر مدیر دیدن، ساختن، ویرایش و حذف کاربران و دسترسی به بخش ها جدا روشن یا خاموش می شود، به همراه سقف تعداد کاربر و حجم؛ ثبت رویدادها (Audit log)
- **پایگاه داده:** PostgreSQL (پیش فرض) یا SQLite
- **منوی خط فرمان `veilon`** برای به روزرسانی، ری استارت، لاگ ها، تغییر رمز، پورت، مسیر، SSL، BBR و فایروال
- رابط کاربری واکنش گرا برای موبایل و دسکتاپ، تم تیره و روشن، فارسی (پیش فرض) و انگلیسی

## پیش نیازها

- یک سرور مجازی (VPS) لازم است
- سیستم عامل: Ubuntu، Debian، CentOS Stream، AlmaLinux، Rocky Linux، Fedora یا Arch Linux، با systemd (پیشنهاد: Ubuntu 22.04 یا 24.04، یا Debian 12)
- پردازنده: معماری amd64 (x86_64) یا arm64، حداقل ۱ هسته
- رم: حداقل ۱ گیگابایت (۲ گیگابایت پیشنهاد می شود)
- دسترسی root (یا کاربری که بتواند `sudo` بزند)
- اختیاری: یک دامنه که رکورد DNS آن به IP سرور اشاره کند

## نصب قدم به قدم برای تازه کارها

### ۱. تهیه سرور

یک سرور مجازی (VPS) لازم است، با یکی از سیستم عامل های بخش پیش نیازها (پیشنهاد: **Ubuntu 24.04** یا **Debian 12**)، حداقل ۱ هسته پردازنده amd64 یا arm64 و حداقل ۱ گیگابایت رم. بعد از تحویل سرور این اطلاعات را دارید:

- **IP سرور** (مثلا `203.0.113.10`)
- **نام کاربری** (معمولا `root`)
- **رمز عبور**

### ۲. اتصال به سرور با SSH

روش اصلی ورود با رمز عبور است:

```bash
ssh root@SERVER_IP
```

به جای `SERVER_IP`، IP سرور خودتان را بنویسید (مثلا `ssh root@203.0.113.10`). بار اول سوال `Are you sure you want to continue connecting` می آید؛ `yes` را تایپ کنید و Enter بزنید. سپس رمز عبور را وارد کنید و Enter بزنید. هنگام تایپ رمز چیزی روی صفحه نمایش داده نمی شود؛ این طبیعی است.

**ویندوز (PowerShell):** کلید Windows را بزنید، `PowerShell` را تایپ و باز کنید و دستور بالا را بزنید.

**ویندوز با Termius:** اگر محیط گرافیکی را ترجیح می دهید، برنامه Termius را نصب کنید و مثل بخش اندروید جلو بروید.

**اندروید (Termius):** برنامه Termius را از Google Play نصب کنید، روی **New Host** بزنید، IP سرور را در **Address**، `root` را در **Username** و رمز را در **Password** وارد کنید و **Connect** را بزنید.

**macOS و Linux:** برنامه Terminal را باز کنید و همان دستور بالا را بزنید.

> **نکته (اختیاری): ورود با فایل کلید.** اگر به جای رمز، یک فایل کلید به شما داده شده است:
>
> ```bash
> ssh -i key root@SERVER_IP
> ```
>
> به جای `key` مسیر فایل کلید را بنویسید. در macOS و Linux اگر خطای دسترسی فایل دیدید، اول `chmod 600 key` را بزنید. در Termius فایل کلید را در بخش **Keys** اضافه کنید و برای Host انتخاب کنید.

### ۳. کاربر root شوید

اگر با کاربری غیر از root وارد شده اید:

```bash
sudo -i
```

### ۴. اجرای دستور نصب

```bash
bash <(curl -Ls https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh)
```

برای نصب یک نسخه مشخص:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh) v0.2.2
```

اگر `curl` نصب نیست:

```bash
apt update && apt install -y curl
```

### ۵. سوال های نصب و پاسخ پیشنهادی

نصب کننده چند سوال می پرسد. برای قبول مقدار پیش فرض فقط Enter بزنید.

| سوال | معنی | پیشنهاد |
| --- | --- | --- |
| `Panel port` | پورتی که پنل روی آن باز می شود | Enter (یک پورت تصادفی و آزاد انتخاب می شود) |
| `Admin username` | نام کاربری مدیر پنل | Enter (`admin`) یا یک نام دلخواه |
| `Admin password` | رمز مدیر | Enter (رمز قوی تصادفی ساخته می شود) |
| `Panel path` | مسیر مخفی پنل در آدرس | Enter (مسیر تصادفی؛ امنیت را بالا می برد) |
| `Panel certificate` | نوع گواهی HTTPS | `1` اگر دامنه دارید، `2` اگر ندارید (IP سرور) |
| `Domain` | فقط در گزینه ۱: دامنه شما | مثلا `panel.example.com` |
| `IP address for the certificate` | فقط در گزینه ۲: IP تشخیص داده شده | Enter برای تایید |

نصب کننده PostgreSQL، هسته Xray و سرویس systemd را نصب و راه اندازی می کند.

### ۶. آدرس، نام کاربری و رمز پنل

در پایان، آدرس پنل، نام کاربری و رمز نمایش داده می شود؛ مثلا:

```
URL:      https://203.0.113.10:24871/abcdefghijklmnop/
Username: admin
Password: ••••••••••••••••
```

این اطلاعات در فایل زیر هم ذخیره می شود:

```bash
cat /root/veilon-credentials.txt
```

اجرای دوباره همان دستور نصب روی سروری که Veilon دارد، آن را **به روزرسانی** می کند و اطلاعات و تنظیمات حفظ می شود.

## باز کردن پورت ها در فایروال

این پورت ها باید از اینترنت در دسترس باشند:

- **پورت پنل** (همان که در نصب انتخاب شد)
- **پورت 80/tcp** برای صدور و تمدید گواهی Let's Encrypt
- **پورت هر ورودی** که می سازید (مثلا 443)

پورت پنل را در فایروال سرور باز کنید، و اگر ارائه دهنده سرور شما در پنل خودش فایروال دارد، آنجا هم باز کنید. برای Hysteria2، TUIC و WireGuard پورت ورودی باید به صورت UDP باز باشد.

ساده ترین راه برای فایروال داخل سرور (ufw یا firewalld):

```bash
veilon firewall
```

یا دستی، با **ufw** (Ubuntu و Debian). به جای `PANEL_PORT` پورت پنل را بنویسید:

```bash
ufw allow PANEL_PORT/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw reload
```

با **firewalld** (CentOS Stream، AlmaLinux، Rocky Linux، Fedora):

```bash
firewall-cmd --permanent --add-port=PANEL_PORT/tcp
firewall-cmd --permanent --add-port=80/tcp
firewall-cmd --permanent --add-port=443/tcp
firewall-cmd --reload
```

برای پورت UDP، به جای `tcp` بنویسید `udp` (مثلا `ufw allow 8443/udp`).

## گواهی SSL: دامنه یا IP سرور

- **دامنه (گزینه ۱):** گواهی ۹۰ روزه Let's Encrypt با تمدید خودکار. قبل از نصب، یک رکورد `A` برای دامنه بسازید که به IP سرور اشاره کند (اگر سرویس DNS شما گزینه پروکسی یا CDN دارد، آن را برای این رکورد خاموش کنید).
- **IP سرور (گزینه ۲):** گواهی کوتاه مدت Let's Encrypt (حدود ۶ روز) مستقیم برای IP، که هر ۳ روز خودکار تمدید می شود. نیازی به دامنه نیست.
- **رد کردن (گزینه ۳):** پنل با HTTP ساده باز می شود و بعدا می توانید از منو گواهی بگیرید.

گرفتن یا تغییر گواهی بعد از نصب:

```bash
veilon cert issue --domain panel.example.com --panel
```

```bash
veilon cert issue --ip auto --panel
```

```bash
veilon cert list
```

در هر دو حالت پورت 80/tcp باید باز باشد.

## اولین ورود، ساخت ورودی و کاربر

1. آدرس پنل را در مرورگر باز کنید و با نام کاربری و رمز وارد شوید.
2. **ساخت ورودی:** به بخش **ورودی ها** بروید ← **ورودی جدید**. برای شروع **VLESS + Reality** پیشنهاد می شود: پروتکل VLESS، انتقال tcp، امنیت Reality، پورت 443. کلیدهای Reality خودکار ساخته می شوند. پورت و نام هر ورودی باید یکتا باشد؛ اگر تکراری باشد، پنل همان لحظه هشدار می دهد.
3. **ساخت کاربر:** به بخش **کاربران** بروید ← **کاربر جدید**. نام، سقف حجم (۰ یعنی نامحدود)، تاریخ انقضا و ورودی هایی که کاربر به آن ها دسترسی دارد را انتخاب کنید.
4. روی کاربر بزنید تا **لینک اشتراک**، **کد QR** و لینک های تکی را ببینید.
5. **محدودیت ها (اختیاری):** در فرم کاربر، بخش محدودیت ها را باز کنید. مثلا «IP همزمان» را ۲ بگذارید تا کاربر فقط با دو IP همزمان وصل شود، یا سرعت دانلود را محدود کنید. هر مقدار ۰ یعنی بدون محدودیت. در زبانه **اتصال ها** IP ها و دستگاه های کاربر را می بینید و می توانید هر کدام را مسدود کنید.
6. **گروه ها (اختیاری):** در بخش **گروه ها** یک گروه بسازید، ورودی ها و مقادیر پیش فرض آن را تعیین کنید و هنگام ساخت کاربر، گروه را انتخاب کنید.
7. **مدیران (اختیاری):** در بخش **مدیران** مدیر جدید بسازید و برای او مشخص کنید چه کارهایی مجاز است (مثلا فقط دیدن کاربران). دکمه هایی که مدیر اجازه آن را ندارد در پنل نمایش داده نمی شوند.
8. **TUIC:** در ورودی جدید پروتکل TUIC را انتخاب کنید (به گواهی TLS نیاز دارد، بخش SSL را ببینید). محدودیت های کاربر برای TUIC هم اعمال می شوند.
9. **هسته Xray:** در کارت هسته در **داشبورد** می توانید هسته را خاموش و روشن کنید، روی شماره نسخه بزنید و نسخه دیگری نصب کنید، یا فایل های geo را به روز کنید.
10. **منطقه زمانی:** در **تنظیمات** منطقه زمانی پنل را انتخاب کنید (مثلا تهران؛ پیش فرض UTC). زمان لاگ ها، نمودار ترافیک و ریست های روزانه، هفتگی و ماهانه طبق آن است.
11. **لاگ ها:** در بخش **لاگ ها** منبع را انتخاب کنید (Xray، پنل یا همه) تا هر خط را با زمان دقیق ببینید.

## اتصال با برنامه های کلاینت

| برنامه | سیستم عامل | روش |
| --- | --- | --- |
| **v2rayNG** | اندروید | لینک اشتراک را کپی کنید ← دکمه + ← Import from clipboard؛ یا کد QR را اسکن کنید |
| **Hiddify** | اندروید، iOS، ویندوز، macOS، لینوکس | New Profile ← لینک اشتراک را وارد کنید |
| **Streisand** | iOS و macOS | دکمه + ← لینک اشتراک یا اسکن QR |
| **AmneziaWG** | اندروید، iOS، ویندوز، macOS | برای ورودی های AmneziaWG: فایل `.conf` را از صفحه کاربر دانلود کنید و در برنامه Import from file بزنید |
| **WireGuard** | همه | برای ورودی های WireGuard: فایل `.conf` یا اسکن QR |

لینک اشتراک خودکار به روز می شود؛ اگر ورودی جدیدی به کاربر بدهید، کافی است در برنامه Update subscription را بزنید.

## منوی مدیریت veilon

با کاربر root این دستور را بزنید تا منوی مدیریت باز شود:

```bash
veilon
```

```
 1  Install / update            10  Status
 2  Uninstall                   11  Logs
 3  Reset username and password 12  Enable autostart
 4  Reset panel path            13  Disable autostart
 5  Change panel port           14  SSL certificates
 6  Show settings and URL       15  Enable BBR
 7  Start                       16  Firewall: open Veilon ports
 8  Stop                        17  Update geo files
 9  Restart                     18  Version
                                 0  Exit
```

همه گزینه ها به شکل دستور مستقیم هم هستند:

| کار | دستور |
| --- | --- |
| به روزرسانی | `veilon update` |
| ری استارت | `veilon restart` |
| وضعیت | `veilon status` |
| دیدن لاگ ها | `veilon logs -f` |
| آدرس و تنظیمات پنل | `veilon info` |
| تغییر نام کاربری و رمز | `veilon admin reset-credentials --username admin --password NEW_PASSWORD` |
| تغییر پورت پنل | `veilon set-port 2053` |
| تغییر مسیر پنل | `veilon set-path random` |
| تغییر زبان پیش فرض | `veilon set-language fa` یا `veilon set-language en` |
| گواهی ها | `veilon cert list` |
| فعال کردن BBR | `veilon bbr` |
| باز کردن پورت ها در فایروال | `veilon firewall` |
| حذف | `veilon uninstall` (با `--purge` اطلاعات و پایگاه داده هم حذف می شود) |

```bash
veilon help
```

مستندات API در [docs/api-v1.md](docs/api-v1.md) است.

## رفع مشکلات رایج

**پنل باز نمی شود.**
1. وضعیت سرویس را ببینید: `veilon status`
2. آدرس دقیق را ببینید (پورت و مسیر را درست وارد کنید، با `/` آخر): `veilon info`
3. پورت پنل را در فایروال سرور، و اگر ارائه دهنده سرور شما در پنل خودش فایروال دارد آنجا هم، باز کنید.
4. لاگ ها را بررسی کنید: `veilon logs -n 100`

**خطای گواهی یا «اتصال امن نیست».**
- اگر گواهی نگرفته اید، آدرس با `http://` باز می شود نه `https://`.
- پورت 80/tcp باید باز باشد، سپس دوباره گواهی بگیرید: `veilon cert issue --ip auto --panel`
- برای دامنه، مطمئن شوید رکورد DNS به IP همین سرور اشاره می کند و پروکسی یا CDN سرویس DNS برای آن خاموش است.
- لیست و وضعیت گواهی ها: `veilon cert list`

**رمز پنل را فراموش کرده ام.**

```bash
cat /root/veilon-credentials.txt
```

یا رمز تازه بسازید:

```bash
veilon admin reset-credentials --password NEW_PASSWORD
```

**پورت در حال استفاده است.**
- پورت پنل: یک پورت دیگر بدهید: `veilon set-port 2096`
- پورت ورودی: پنل اجازه نمی دهد دو ورودی یک پورت داشته باشند. اگر برنامه دیگری روی سرور از آن پورت استفاده می کند، ببینید کدام است:

```bash
ss -tulpn | grep :443
```

**کاربر وصل نمی شود.**
- پورت ورودی را در فایروال سرور، و اگر ارائه دهنده سرور شما در پنل خودش فایروال دارد آنجا هم، باز کنید (برای Hysteria2، TUIC و WireGuard به صورت UDP).
- ساعت گوشی و سرور باید درست باشد.
- در برنامه کلاینت Update subscription را بزنید.

## حمایت از توسعه دهنده

Veilon رایگان است و با وقت و علاقه ساخته می شود. اگر به کارتان آمده، هر حمایتی دلگرمی بزرگی است. همین اطلاعات در پنل، در منوی «بیشتر» ← «حمایت از توسعه دهنده» هم هست.

- **کیف پول:** Tonkeeper
- **ارزها:** TON و تتر (USDT) **فقط روی شبکه TON**
- **آدرس:**

```
UQCr7FXmeLsKLplHAgb1HIT0rxmQJpvAh4w2pST-DBGicbi2
```

> ⚠️ هر ارز را فقط با شبکه مخصوص همان آدرس بفرستید؛ ارسال با شبکه دیگر باعث از دست رفتن پول می شود. تتر را از شبکه های TRC20، ERC20 یا BEP20 به این آدرس نفرستید.

## مجوز

AGPL-3.0. متن کامل در [LICENSE](LICENSE).

</div>

---

<a id="english"></a>

# Veilon (English)

**Developed by [@lxhosein](https://t.me/lxhosein)**

Veilon is a free, open-source proxy panel. One Go binary serves the web UI, the API and subscriptions, and supervises the proxy cores. One-command install, Persian and English UI.

## Features

- **Protocols:** VLESS, VMess, Trojan, Shadowsocks (incl. 2022), Hysteria2, TUIC, WireGuard, AmneziaWG, HTTP, Mixed (HTTP + SOCKS), Tunnel, TUN, MTProto and MASQUE
- **Security and transports:** Reality and TLS; TCP, WebSocket, gRPC, HTTPUpgrade and XHTTP
- **Free Let's Encrypt certificates** for a domain (90 days) or directly for the server IP (short-lived), renewed automatically
- **Users:** data cap, expiry, expiry starting at first connection, periodic data reset (daily, weekly, monthly), online status and last connection, bulk actions
- **Subscriptions** for v2rayNG, Hiddify, Streisand, sing-box and Clash, with QR codes and `.conf` files for WireGuard and AmneziaWG
- **Inbounds:** step-by-step wizard; inbound ports and names are unique and checked live
- **Live dashboard:** real network traffic of the server, online users, CPU, memory and disk; the traffic total equals the sum of user usage
- **Xray core control from the dashboard:** switch the core on or off (stays off across panel restarts), pick and install any official Xray release (verified download, automatic rollback on failure), update geo files
- **Panel timezone:** every date, chart, periodic reset and user time window follows the timezone chosen in Settings (e.g. Tehran; default UTC)
- **Logs:** Xray, the other engines and the panel itself, one source or all merged, with the exact time and level (info, warning, error) of every line
- **Per-user limits:** concurrent IPs, devices (HWID), separate download and upload speed, daily data cap, traffic multiplier, torrent blocking, allowed time window (hours and weekdays, in the panel timezone); TUIC included
- **Connection log:** each user's IPs and devices with first and last seen, block or unblock any IP or device
- **Groups:** create groups, put users in them, set a group's inbounds and defaults, turn on ad blocking
- **Multiple admins with permissions:** per sub-admin toggles for viewing, creating, editing and deleting users and for each panel area, plus user and traffic quotas; audit log
- **Database:** PostgreSQL (default) or SQLite
- **`veilon` CLI menu** for update, restart, logs, credentials, port, path, SSL, BBR and firewall
- Responsive UI for phone and desktop, dark and light themes, Persian (default) and English

## Requirements

- A VPS is required
- OS: Ubuntu, Debian, CentOS Stream, AlmaLinux, Rocky Linux, Fedora or Arch Linux, with systemd (recommended: Ubuntu 22.04 or 24.04, or Debian 12)
- CPU: amd64 (x86_64) or arm64, at least 1 core
- RAM: at least 1 GB (2 GB recommended)
- Root access (or a user with `sudo`)
- Optional: a domain whose DNS record points to the server

## Install

1. A VPS is required (see Requirements; Ubuntu 24.04 or Debian 12 recommended). Note its IP, username (usually `root`) and password.
2. Connect with SSH using your password (the main way):

```bash
ssh root@SERVER_IP
```

Replace `SERVER_IP` with your server's IP. The first time, type `yes` to accept the host key, then enter your password (nothing is shown while you type; that is normal).

- **Windows:** open PowerShell and run the command above, or use Termius.
- **Android:** install Termius, tap **New Host**, enter the server IP as Address, `root` as Username and your password, then **Connect**.
- **macOS and Linux:** open Terminal and run the command above.

> **Optional: key login.** If your provider gave you a key file instead of a password:
>
> ```bash
> ssh -i key root@SERVER_IP
> ```
>
> Replace `key` with the path to the key file (on macOS and Linux run `chmod 600 key` first if SSH complains about permissions). In Termius, add the key under **Keys** and select it for the host.

3. If you logged in as a non-root user, become root:

```bash
sudo -i
```

4. Run the installer:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh)
```

A specific release:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh) v0.2.2
```

5. Answer the questions (Enter accepts the default): panel port (random free port), admin username (`admin`), admin password (random), panel path (random), then the certificate: `1` domain, `2` server IP (recommended without a domain), `3` skip (plain HTTP).
6. At the end the installer prints the panel URL, username and password, also saved to `/root/veilon-credentials.txt`. Running the same command again upgrades the panel and keeps all data.

Non-interactive options:

```
--port N  --admin NAME  --password PW  --path P  --lang fa|en
--cert domain|ip|none  --domain D  --ip ADDR|auto  --ipv6 ADDR|auto  --email E
--sqlite  --version TAG  --binary FILE  -y
install.sh uninstall [--purge] [-y]
```

## Firewall

Open the panel port in your server firewall, and in your provider's firewall panel if it has one. Also open 80/tcp (Let's Encrypt validation) and every inbound port (UDP for Hysteria2, TUIC and WireGuard).

`veilon firewall` opens the Veilon ports in ufw or firewalld. To do it by hand, replace `PANEL_PORT` with your panel port.

ufw (Ubuntu, Debian):

```bash
ufw allow PANEL_PORT/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw reload
```

firewalld (CentOS Stream, AlmaLinux, Rocky Linux, Fedora):

```bash
firewall-cmd --permanent --add-port=PANEL_PORT/tcp
firewall-cmd --permanent --add-port=80/tcp
firewall-cmd --permanent --add-port=443/tcp
firewall-cmd --reload
```

For a UDP port use `udp` instead of `tcp` (e.g. `ufw allow 8443/udp`).

## SSL

- **Domain:** Let's Encrypt, 90 days, auto renewed. Point an `A` record to the server first (turn off any DNS proxy or CDN option for that record).
- **Server IP:** Let's Encrypt short-lived certificate (about 6 days), renewed every ~3 days. No domain needed.

```bash
veilon cert issue --domain panel.example.com --panel
```

```bash
veilon cert issue --ip auto --panel
```

## First steps

1. Open the panel URL and sign in.
2. **Inbounds → New inbound.** VLESS + Reality on port 443 is a good start. Ports and names must be unique.
3. **Users → New user.** Set a data cap (0 = unlimited), expiry and the inbounds.
4. Open the user to get the subscription link, QR codes and per-inbound links.
5. **Limits (optional):** in the user form, open the limits section, e.g. concurrent IPs = 2 or a download speed cap. 0 always means no limit. The **Connections** tab lists the user's IPs and devices and lets you block any of them.
6. **Groups (optional):** create a group under **Groups**, set its inbounds and defaults, and pick it when creating users.
7. **Admins (optional):** create a sub-admin under **Admins** and choose what it may do (e.g. only view users). Buttons an admin may not use are hidden.
8. **TUIC:** pick TUIC in the inbound wizard (needs a TLS certificate, see SSL). User limits apply to TUIC as well.
9. **Xray core:** the core card on the **Dashboard** switches the core on or off, installs another version (tap the version number) and updates the geo files.
10. **Timezone:** choose the panel timezone in **Settings** (e.g. Tehran; default UTC). Log times, the traffic chart and daily, weekly and monthly resets follow it.
11. **Logs:** pick a source (Xray, panel or all) under **Logs** to see every line with its exact time.

## Client apps

- **v2rayNG** (Android), **Hiddify** (all platforms), **Streisand** (iOS, macOS): import the subscription link or scan the QR code.
- **AmneziaWG** and **WireGuard**: download the `.conf` file from the user page and import it.

## Management

```bash
veilon
```

| Task | Command |
| --- | --- |
| Update | `veilon update` |
| Restart | `veilon restart` |
| Status | `veilon status` |
| Logs | `veilon logs -f` |
| URL and settings | `veilon info` |
| Change username and password | `veilon admin reset-credentials --username admin --password NEW_PASSWORD` |
| Change panel port | `veilon set-port 2053` |
| Change panel path | `veilon set-path random` |
| Default language | `veilon set-language fa` or `veilon set-language en` |
| Certificates | `veilon cert list` |
| BBR | `veilon bbr` |
| Firewall | `veilon firewall` |
| Uninstall | `veilon uninstall` (add `--purge` to remove data and database) |

`veilon help` lists everything. The API is documented in [docs/api-v1.md](docs/api-v1.md).

## Troubleshooting

- **Panel does not open:** `veilon status`, `veilon info` (exact port and path, with the trailing `/`), open the panel port in your server firewall (and your provider's firewall panel if it has one), `veilon logs -n 100`.
- **Certificate errors:** without a certificate use `http://`; open 80/tcp and run `veilon cert issue --ip auto --panel`; for a domain check the DNS record and turn off any DNS proxy or CDN option.
- **Forgot the password:** `cat /root/veilon-credentials.txt` or `veilon admin reset-credentials --password NEW_PASSWORD`.
- **Port in use:** `veilon set-port 2096` for the panel; for inbounds find the other program with `ss -tulpn | grep :443`.

## Support the developer

Veilon is free and built with time and care. If it helps you, any support means a lot. The same details are in the panel under More → Support the developer.

- Wallet: Tonkeeper
- Accepts: TON and USDT (Tether) **on the TON network only**

```
UQCr7FXmeLsKLplHAgb1HIT0rxmQJpvAh4w2pST-DBGicbi2
```

> ⚠️ Send each coin only on the network of that address; sending on another network will lose the money. Do not send USDT from TRC20, ERC20 or BEP20.

## License

AGPL-3.0. See [LICENSE](LICENSE).

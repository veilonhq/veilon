# Changelog

Each release's notes, Persian first. Older releases: https://github.com/veilonhq/veilon/releases

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

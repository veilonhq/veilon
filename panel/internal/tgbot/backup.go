package tgbot

import "github.com/veilonhq/veilon/panel/internal/audit"

// Telegram limits: a bot downloads files up to 20 MB and sends up to 50 MB.
const (
	maxDownload = 20 << 20
	maxUpload   = 50 << 20
)

func init() {
	on("bk", backupView)
	on("bkc", backupCreate)
	on("bkr", func(r *req, _ []string) {
		if r.b.Backups == nil {
			backupView(r, nil)
			return
		}
		r.st.restore, r.st.restoreName = nil, ""
		r.ask("bkfile", nil, "بازگردانی پشتیبان",
			"فایل پشتیبان پایگاه داده (همان فایلی که ربات فرستاده است) را اینجا بفرستید.\n\n"+
				"⚠️ ربات های تلگرام فقط فایل های تا <b>۲۰ مگابایت</b> را می توانند دریافت کنند. فایل بزرگ تر را باید روی خود سرور بازگردانی کرد.",
			"bk")
	})
	onInput("bkfile", backupReceive)
	on("bky", backupApply)
	on("bkn", func(r *req, _ []string) {
		r.st.restore, r.st.restoreName = nil, ""
		backupView(r, nil)
	})
}

func backupView(r *req, _ []string) {
	t := header("💾", "پشتیبان")
	if r.b.Backups == nil {
		r.show(view{text: t + "پشتیبان گیری روی این پنل در دسترس نیست.", kb: Keyboard{nav("home")}})
		return
	}
	t += "📦 <b>گرفتن پشتیبان:</b> یک نسخه سالم از پایگاه داده (کاربران، ورودی ها، تنظیمات) ساخته و همینجا برای شما فرستاده می شود.\n\n"
	t += "♻️ <b>بازگردانی:</b> فایل پشتیبان را می فرستید؛ پیش از هر چیز از پایگاه داده فعلی یک نسخه ایمن روی سرور نگه داشته می شود، بعد پنل یک بار دوباره راه اندازی می شود و فایل شما سر جایش قرار می گیرد.\n\n"
	t += "ℹ️ محدودیت تلگرام: ربات فقط فایل های تا <b>۲۰ مگابایت</b> را دریافت می کند و تا ۵۰ مگابایت می فرستد.\n"
	if res := r.b.Backups.LastResult(); res != nil {
		t += "\nآخرین بازگردانی: " + jdate(res.AppliedAt, r.b.loc()) + " · "
		if res.OK {
			t += "✅ انجام شد"
		} else {
			t += "⚠️ انجام نشد: " + esc(cut(res.Error, 300))
		}
		t += "\n"
	}
	r.show(view{text: t, kb: Keyboard{
		{btn("📦 گرفتن پشتیبان و ارسال", "bkc")},
		{btn("♻️ بازگردانی از فایل", "bkr")},
		nav("home"),
	}})
}

func backupCreate(r *req, _ []string) {
	if r.b.Backups == nil {
		backupView(r, nil)
		return
	}
	r.progress("در حال گرفتن پشتیبان")
	name, data, err := r.b.Backups.Create(r.ctx)
	if err != nil {
		r.fail(err, "bk")
		return
	}
	if len(data) > maxUpload {
		r.fail(errorf("فایل پشتیبان "+size(int64(len(data)))+" است و از سقف ۵۰ مگابایت تلگرام بزرگ تر است. پشتیبان را روی خود سرور بگیرید."), "bk")
		return
	}
	caption := "💾 پشتیبان پایگاه داده Veilon\n" + jdate(r.b.now(), r.b.loc()) + " · " + size(int64(len(data))) +
		"\nاین فایل همه اطلاعات پنل را دارد؛ آن را جای امن نگه دارید."
	if err := r.cl.SendDocument(r.ctx, r.chat, name, data, caption); err != nil {
		r.fail(err, "bk")
		return
	}
	if a, err := r.b.actor(r.from); err == nil {
		audit.Log(r.b.DB, a, "backup.create", name, "telegram")
	}
	r.fresh(view{text: header("💾", "پشتیبان") + "✅ فایل پشتیبان فرستاده شد (" + size(int64(len(data))) + ").", kb: Keyboard{{btn("💾 پشتیبان", "bk")}, nav("home")}})
}

type textErr string

func (e textErr) Error() string { return string(e) }

func errorf(s string) error { return textErr(s) }

func backupReceive(r *req, _ []string, _ string, doc *Document) {
	again := func(warn string) {
		r.ask("bkfile", nil, "بازگردانی پشتیبان", "⚠️ "+warn+"\nفایل پشتیبان را بفرستید (حداکثر ۲۰ مگابایت):", "bk")
	}
	if doc == nil {
		again("یک فایل بفرستید، نه متن.")
		return
	}
	if doc.FileSize > maxDownload {
		again("این فایل " + size(doc.FileSize) + " است؛ تلگرام به ربات اجازه دریافت فایل بیشتر از ۲۰ مگابایت را نمی دهد.")
		return
	}
	r.progress("در حال دریافت و بررسی فایل")
	data, err := r.cl.Download(r.ctx, doc.FileID, maxDownload)
	if err != nil {
		again("دریافت فایل انجام نشد: " + esc(err.Error()))
		return
	}
	desc, err := r.b.Backups.Check(doc.FileName, data)
	if err != nil {
		again("این فایل قابل بازگردانی نیست: " + esc(err.Error()))
		return
	}
	r.st.restore, r.st.restoreName = data, doc.FileName
	q := "فایل: " + code(doc.FileName) + " · " + size(int64(len(data))) + "\n" + esc(desc) + "\n\n" +
		"⚠️ همه کاربران، ورودی ها و تنظیمات فعلی با محتوای این فایل جایگزین می شوند. پیش از آن یک نسخه ایمن از پایگاه داده فعلی روی سرور نگه داشته می شود.\n" +
		"پنل یک بار دوباره راه اندازی می شود و چند ثانیه از دسترس خارج است.\n\nبازگردانی انجام شود؟"
	r.show(view{text: header("♻️", "بازگردانی پشتیبان") + q, kb: Keyboard{{
		Button{Text: "✅ بله، بازگردانی کن", Data: "bky"}, Button{Text: "✖️ نه", Data: "bkn"},
	}}})
}

func backupApply(r *req, _ []string) {
	data, name := r.st.restore, r.st.restoreName
	r.st.restore, r.st.restoreName = nil, ""
	if r.b.Backups == nil || len(data) == 0 {
		r.toast("فایلی منتظر بازگردانی نیست")
		backupView(r, nil)
		return
	}
	r.progress("در حال آماده سازی بازگردانی")
	safety, err := r.b.Backups.Restore(name, data)
	if err != nil {
		r.fail(err, "bk")
		return
	}
	if a, err := r.b.actor(r.from); err == nil {
		audit.Log(r.b.DB, a, "backup.restore", name, "telegram")
	}
	t := header("♻️", "بازگردانی پشتیبان")
	t += "✅ فایل آماده شد و پنل تا چند ثانیه دیگر دوباره راه اندازی می شود تا آن را سر جایش بگذارد.\n\n"
	t += "نسخه ایمن پایگاه داده قبلی:\n" + code(safety) + "\n\n"
	t += "حدود یک دقیقه بعد /start را بفرستید. اگر شناسه تلگرام شما در پشتیبان جزو ادمین های ربات نباشد، ربات دیگر جواب شما را نمی دهد."
	r.show(view{text: t, kb: Keyboard{}})
}

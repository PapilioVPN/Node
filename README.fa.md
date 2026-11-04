<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/banner.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/banner-light.png">
    <img alt="PapilioVPN Node — سرویس مدیریت نودهای وی‌پی‌ان" src="assets/banner-light.png" width="100%">
  </picture>
</p>

<h1 align="center">PapilioVPN Node</h1>

<p align="center" dir="rtl">سرویس نوشته‌شده با Go برای نودهای تحت مدیریت پلتفرم PapilioVPN.</p>

<p align="center">
  <a href="https://github.com/papilio-platform/Node/actions/workflows/ci.yml"><img alt="وضعیت CI" src="https://github.com/papilio-platform/Node/actions/workflows/ci.yml/badge.svg?branch=main"></a>
  <a href="go.mod"><img alt="Go 1.27.3 یا جدیدتر" src="https://img.shields.io/badge/Go-1.27.3%2B-00ADD8?style=flat-square&amp;logo=go&amp;logoColor=white"></a>
  <img alt="وضعیت: ابتدای توسعه" src="https://img.shields.io/badge/status-early_development-8B5CF6?style=flat-square">
</p>

<p align="center"><a href="README.md" lang="en">English</a> · <strong lang="fa">فارسی</strong></p>

<p align="center" dir="rtl">
  <a href="#current-capabilities">قابلیت‌ها</a> ·
  <a href="#quick-start">شروع کار</a> ·
  <a href="#development">توسعه</a> ·
  <a href="CONTRIBUTING.md">مشارکت</a>
</p>

---

<div dir="rtl">

<a id="current-capabilities"></a>

## قابلیت‌های فعلی

این نسخه، پایهٔ اجرایی و رابط خط فرمان پروژه را فراهم می‌کند.
آداپتر داخلی Xray اعتبارسنجی بومی تنظیمات و شروع و توقف فرایند متعلق به Node
را انجام می‌دهد. عملیات هسته هنوز فرمان CLI یا endpoint کنترل شبکه ندارد.
سرویس داخلی instance عملیات چرخهٔ اجرا را هماهنگ می‌کند و revision تنظیمات،
نسخهٔ بومی و نتیجهٔ تأییدشدهٔ خروج و پاک‌سازی فرایند را گزارش می‌دهد.

| قابلیت موجود | رفتار |
| --- | --- |
| مشاهدهٔ فرمان‌ها | راهنما، فرمان‌هایی را نشان می‌دهد که همین فایل اجرایی ارائه می‌کند. |
| کنسول ترمینال | رابط با هویت بصری Papilio، ناوبری با کیبورد، اطلاعات ساخت و راهنمای فرمان‌ها؛ همراه با اسکرول و چیدمان متناسب با اندازهٔ پنجره. |
| بررسی نسخه | نسخه، نسخهٔ Go و پلتفرم هدف را به‌شکل خوانا نمایش می‌دهد. |
| اطلاعات ساخت ساختاریافته | خروجی JSON شامل revision و وضعیت تغییرات کد، در صورت درج توسط Go است. |
| بررسی بومی تنظیمات | اعتبارسنجی Xray با سقف منابع، امکان لغو و جزئیات خطای خصوصی؛ [دامنهٔ آزمایش‌شده و محدودیت‌ها](docs/engines/xray.md). |
| فرایندهای Xray متعلق به Node | تأیید آمادگی از API بومی روی سوکت Unix خصوصی، توقف با مهلت مشخص و پاک‌سازی منابع همان اجرا؛ فعلاً در آداپتر داخلی. [محدودیت‌های چرخهٔ اجرا](docs/engines/xray.md#owned-process-contract). |
| چرخهٔ اجرای instance | وضعیت و revisionها، نسخهٔ بومی و نتیجهٔ خروج و پاک‌سازی؛ عملیات start/stop/restart محدود با بازیابی صریح. تست با هستهٔ واقعی عبور دادهٔ محلی، بازیابی پس از crash و توقف با پایان عمر مالک را پوشش می‌دهد. خواندن وضعیت بدون I/O هسته انجام می‌شود. [قراردادهای instance](docs/instances.md). |
| لاگ کنسول هسته | دریافت اختیاری، محدود و زنده همراه با بازپخش، نشانگر ادامه، گزارش صریح خط‌های ازدست‌رفته و هویت هر اجرا. دریافت خروجی Xray با ترافیک واقعی و restart بررسی شده؛ مقصد فایل و تنظیم غیرفعال‌بودن لاگ حفظ می‌شوند. [قراردادهای لاگ](docs/engine-logs.md). |
| تنظیمات هسته | خواندن صریح و خصوصی ورودی مطلوب، اعتبارسنجی مؤثر بومی و جایگزینی کامل با شرط revision؛ ورودی بدون تغییر و اجرای سالم دوباره استفاده می‌شود. شکست جایگزین، تاریخچهٔ واقعی تنظیم مطلوب و اعمال‌شده را حفظ می‌کند. تغییر کامل نیازمند restart است؛ [قراردادهای تنظیمات](docs/configuration.md). |
| بررسی روی Linux | در CI، ساخت و تست روی amd64 و arm64 به‌صورت native انجام می‌شود؛ همراه با بررسی تنظیمات و فرایند Xray با نسخه و checksum ثابت. |

<a id="quick-start"></a>

## شروع کار

[Go نسخهٔ 1.27.3 یا جدیدتر](https://go.dev/dl/) را نصب کنید و سپس پروژه را از سورس بسازید:

<div dir="ltr">

```bash
git clone https://github.com/papilio-platform/Node.git
cd Node
go build -trimpath -o bin/node ./cmd/node
./bin/node help
./bin/node version
./bin/node version --json
./bin/node tui
```

</div>

این دستورها رابط خط فرمان نسخهٔ توسعه را می‌سازند. سرویس سیستمی نصب نمی‌کنند
و تنظیمات شبکه را تغییر نمی‌دهند. بسته‌های نصب نسخه‌دار هنوز منتشر نشده‌اند.

## راهنمای فرمان‌ها

| فرمان | نتیجه |
| --- | --- |
| <code dir="ltr">node help</code> | نمایش راهنما و فرمان‌های موجود؛ <code dir="ltr">-h</code> و <code dir="ltr">--help</code> هم پذیرفته می‌شوند. |
| <code dir="ltr">node version</code> | نمایش خوانای نسخه و پلتفرم. |
| <code dir="ltr">node version --json</code> | نمایش فیلدهای <code dir="ltr">version</code>، <code dir="ltr">revision</code>، <code dir="ltr">modified</code>، <code dir="ltr">go_version</code>، <code dir="ltr">os</code> و <code dir="ltr">arch</code>. |
| <code dir="ltr">node version --help</code> | نمایش گزینه‌های فرمان نسخه. |
| <code dir="ltr">node tui</code> | بازکردن کنسول تعاملی در ترمینال. |
| <code dir="ltr">node tui --help</code> | نمایش پیش‌نیازها و روش ناوبری کنسول. |
| <code dir="ltr">node --color=auto\|always\|never &lt;command&gt;</code> | انتخاب حالت رنگ؛ گزینه‌های عمومی قبل از فرمان نوشته می‌شوند. |

### کنسول تعاملی

با <code dir="ltr">node tui</code> صفحه‌های **Overview**، **Build** و **Commands** را مرور کنید.
کنسول اطلاعات همین فایل اجرایی را نشان می‌دهد؛ سرویس در حال اجرا را بررسی نمی‌کند
و مدیریت هسته‌ها را انجام نمی‌دهد. رابط تمام‌صفحه است و هنگام خروج، صفحهٔ قبلی،
نشانگر و حالت ورودی ترمینال را بازمی‌گرداند.

| کلیدها | عملکرد |
| --- | --- |
| <code dir="ltr">Tab / Shift+Tab</code>، <code dir="ltr">← / →</code> یا <code dir="ltr">h / l</code> | رفتن به صفحهٔ قبلی یا بعدی. |
| <code dir="ltr">1 / 2 / 3</code> | معرفی، جزئیات ساخت یا فرمان‌ها. |
| <code dir="ltr">↑ / ↓</code> یا <code dir="ltr">k / j</code> | اسکرول صفحه. |
| <code dir="ltr">PgUp / PgDown</code>، <code dir="ltr">Home / End</code> | حرکت صفحه‌ای یا رفتن به ابتدا و انتهای محتوا. |
| <code dir="ltr">?</code> | نمایش یا بستن راهنمای کیبورد. |
| <code dir="ltr">Esc</code> | بستن راهنما؛ در حالت عادی خروج. |
| <code dir="ltr">q / Ctrl+C</code> | خروج از کنسول. |

چیدمان با اندازهٔ پنجره و گزارش پس‌زمینهٔ روشن یا تاریک ترمینال سازگار می‌شود.
در اندازهٔ کمتر از **۴۸ ستون × ۱۶ سطر**، پیام تغییر اندازه نمایش داده می‌شود و
کلیدهای خروج فعال می‌مانند. ورودی و خروجی کنسول باید ترمینال باشند؛ در حالت
redirect یا <code dir="ltr">TERM=dumb</code>، بدون ورود به حالت تعاملی خطای استفاده گزارش می‌شود.

خروجی خواندنی روی ترمینال مناسب به‌طور خودکار رنگی است. هر مقدار غیرخالی برای
<code dir="ltr">NO_COLOR</code> قالب‌بندی خودکار را غیرفعال می‌کند.
گزینهٔ <code dir="ltr">--color=never</code> قالب‌بندی را حذف می‌کند و
<code dir="ltr">--color=always</code> آن را حتی برای خروجی خواندنی redirectشده فعال می‌کند.
گزینهٔ صریح بر <code dir="ltr">NO_COLOR</code> اولویت دارد. کدهای کنترل صفحه در TUI بدون رنگ
هم ضروری‌اند. خروجی JSON، مستقل از گزینهٔ رنگ، همیشه بدون قالب‌بندی و کد کنترل ترمینال است.

خروجی نسخه از اطلاعات ماژول درج‌شده توسط Go استفاده می‌کند؛ در ساخت توسعه ممکن است
یک pseudo-version براساس commit باشد. اگر نسخهٔ ماژول موجود نباشد، <code dir="ltr">devel</code> گزارش می‌شود.
اگر اطلاعات VCS هنگام ساخت درج نشده باشد، <code dir="ltr">revision</code> خالی است؛ فایل اجرایی در زمان اجرا Git را فراخوانی نمی‌کند.
فیلد <code dir="ltr">modified</code> وضعیت کد در زمان ساخت را نشان می‌دهد، نه وضعیت فایل‌های سیستم مقصد.

کد خروج **0** به‌معنی موفقیت، **1** به‌معنی خطای اجرا یا نوشتن خروجی و **2** به‌معنی
استفادهٔ نادرست است. خطای فرمان نامعتبر در stderr نوشته می‌شود.

<a id="development"></a>

## توسعه

رابط ترمینال از وابستگی‌های نسخه‌ثابت
[Bubble Tea](https://github.com/charmbracelet/bubbletea)،
[Bubbles](https://github.com/charmbracelet/bubbles) و
[Lip Gloss](https://github.com/charmbracelet/lipgloss) استفاده می‌کند.
بررسی‌های اصلی CI را می‌توانید به این شکل اجرا کنید:

<div dir="ltr">

```bash
gofmt -w cmd internal tests
go mod tidy
go mod verify
go vet ./...
go test -race -count=1 ./...
go build -trimpath -o bin/node ./cmd/node
```

</div>

CI همچنین یکسان‌بودن قالب‌بندی و وابستگی‌ها و آسیب‌پذیری‌های قابل‌دسترسی را با
نسخهٔ ثابت <code dir="ltr">govulncheck</code> بررسی می‌کند. نسخهٔ کامپایلر و ابزار بررسی در
[فایل CI](.github/workflows/ci.yml) مشخص شده‌اند. تست race به ابزار C پشتیبانی‌شده نیاز دارد.
تست‌های Linux با pseudo-terminal واقعی، ناوبری، تغییر اندازه، سیاست رنگ، لغو اجرا،
سیگنال‌ها و بازگرداندن حالت ترمینال را بررسی می‌کنند.
تست واقعی هسته به فایل اجرایی نسخهٔ ثابت Xray نیاز دارد؛
[دستورهای بررسی هسته](CONTRIBUTING.md#native-xray-tests) را ببینید.

## مشارکت

[راهنمای مشارکت](CONTRIBUTING.md) را بخوانید،
[مشکلی گزارش کنید](https://github.com/papilio-platform/Node/issues) یا
[درخواست ادغام بفرستید](https://github.com/papilio-platform/Node/pulls).
در گزارش خطا، اطلاعات ساخت و یک نمونهٔ قابل‌بازتولید را بنویسید.
هنگام تغییر رفتار، راهنمای انگلیسی و فارسی را با هم به‌روز کنید.

</div>

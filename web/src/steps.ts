export type DocStep = {
  title: string;
  file: string;
  method: string;
  phase: number;
  key: string;
  plain: string;
  how: string[];
  why: string;
  next: string;
};

export const steps: DocStep[] = [
  {
    title: "راه‌اندازی برنامه",
    file: "main.go",
    method: "main()",
    phase: 0,
    key: "تابع main فقط نقطهٔ شروع است؛ مغز استریم اینجا نیست.",
    plain: "وقتی برنامه را اجرا می‌کنی، سیستم‌عامل می‌گردد دنبال تابع main. اینجا فقط یک کار می‌کند: سرور HTTP را صدا بزند تا بقیهٔ سیستم راه بیفتد.",
    how: [
    "package main = این فایل مال برنامهٔ اجرایی است.",
    "func main() = اولین کدی که اجرا می‌شود.",
    "داخلش فقط: httpserver.Run() .",
    "هنوز نه session ساخته شده، نه سوکت UDP، نه WebRTC."
    ],
    why: "اگر همه چیز را داخل main بریزی، بعداً نمی‌فهمی کدام قسمت مال بوت است و کدام مال استریم.",
    next: "→ httpserver.Run()",
  },
  {
    title: "بوت سرور HTTP",
    file: "internal/httpserver/server.go",
    method: "Run()",
    phase: 1,
    key: "صفحهٔ کنترل HTTP مسیر فرمان React است؛ صفحهٔ رسانه (ویدیو) جدا و روی RTP است.",
    plain: "یک HTTP server روی پورت 8085 listen می‌کند. React از اینجا Start/Stop و offer می‌فرستد. خودِ تصویر دوربین از مسیر دیگری (RTP) می‌آید.",
    how: [
    "config.Load() تنظیمات را می‌خواند.",
    "gin.New() موتور وب را می‌سازد.",
    "Recovery اگر panic رخ دهد کل process crash نکند.",
    "cors اجازه می‌دهد فرانت از origin دیگر درخواست بزند.",
    "docs و swagger و api ثبت می‌شوند.",
    "r.Run(\":8085\") listen می‌کند و اینجا می‌ماند."
    ],
    why: "بدون این HTTP server، حتی اگر RTP برسد، React نمی‌تواند Start/Stop یا offer بفرستد.",
    next: "→ config.Load → api.Register",
  },
  {
    title: "خواندن تنظیمات",
    file: "internal/config/config.go",
    method: "Load()",
    phase: 1,
    key: "تنظیمات حساس بیرون از کد زندگی می‌کنند (.env).",
    plain: "رمز و آدرس دوربین آزمایشی از فایل .env خوانده می‌شود. اگر AUTH_SECRET نباشد، GET / باز می‌ماند و استریم قطع نمی‌شود.",
    how: [
    "godotenv.Load() فایل .env را می‌خواند.",
    "AUTH_SECRET در صورت وجود فقط GET / را قفل می‌کند.",
    "DEMO_CAMERA_URL آدرس دوربین آزمایشی سناریو است.",
    "GRPC_ADDR آدرس سرویس gRPC است."
    ],
    why: "در لپ‌تاپ و سرور واقعی رمزها فرق دارد؛ کد یکی می‌ماند.",
    next: "→ ادامه Run",
  },
  {
    title: "ثبت مسیرهای ریشه",
    file: "internal/api/hello.go",
    method: "Register()",
    phase: 2,
    key: "مسیر (Route) یعنی «اگر این آدرس آمد، این تابع را اجرا کن».",
    plain: "منوی اصلی API را به Gin معرفی می‌کند؛ بخش استریم را به تابع بعدی می‌سپارد.",
    how: [
    "GET / با middleware احراز هویت.",
    "GET /ptz WebSocket برای PTZ (مثل mobileservergo).",
    "سپس registerStream(r) برای همهٔ مسیرهای پخش."
    ],
    why: "جدا کردن root از stream خواندن کد را ساده می‌کند.",
    next: "→ registerStream",
  },
  {
    title: "ثبت API استریم",
    file: "internal/api/stream.go",
    method: "registerStream()",
    phase: 2,
    key: "اینجا قرارداد فرانت با سرور نوشته می‌شود.",
    plain: "تمام routeهای مربوط به شروع/توقف استریم و WebRTC اینجا به handlerها وصل می‌شوند. هنوز کسی Start نزده؛ فقط ثبت route انجام شده.",
    how: [
    "POST /stream/start و /stop",
    "POST /webrtc/:id/offer و batch",
    "GET /stream/meta وضعیت",
    "مسیرهای HLS/play کمکی"
    ],
    why: "فرانت باید بداند دقیقاً به کدام URL چه چیزی بفرستد.",
    next: "→ منتظر React",
  },
  {
    title: "React استارت می‌زند",
    file: "internal/api/stream.go",
    method: "POST /stream/start",
    phase: 3,
    key: "دستور Start یعنی session استریم را برای این منبع باز کن و URL پخش را برگردان.",
    plain: "فرانت یک JSON می‌فرستد: آدرس RTP از Record Server + نوع خروجی. سرور استریم را راه می‌اندازد و یک URL پخش برمی‌گرداند.",
    how: [
    "بدنه: { url, output_type }",
    "outPath نوع را چک می‌کند: ۶ → /hls/{id}/index.m3u8",
    "۷ → /webrtc/{id}/offer",
    "بعد stream.Start(url, type)",
    "بعد absURL آدرس کامل می‌سازد مثل http://host:8085/webrtc/…/offer"
    ],
    why: "فرانت React نباید مستقیم به multicast Record Server بچسبد؛ این سرویس پل امن/پایدار است.",
    next: "→ stream.Start",
  },
  {
    title: "Facade لایه stream",
    file: "internal/stream/stream.go",
    method: "Start()",
    phase: 3,
    key: "لایهٔ Facade یعنی در تمیز؛ api جزئیات داخلی session را نمی‌بیند.",
    plain: "تابع Start این پکیج فقط فراخوانی را به session.Start می‌سپارد؛ جزئیات پیاده‌سازی پشت facade می‌ماند.",
    how: [
    "stream.Start → session.Start",
    "همین الگو برای Stop، RTCAnswer، Overlay و …"
    ],
    why: "اگر پیاده‌سازی session عوض شود، importهای api کمتر می‌شکند.",
    next: "→ session.Start",
  },
  {
    title: "ساخت/Reuse نشست (session)",
    file: "internal/stream/stream.go",
    method: "Start()",
    phase: 4,
    key: "نشست (session) یعنی واحد runtime یک استریم برای یک منبع (یک url). state همان استریم داخل همین struct نگه داشته می‌شود.",
    plain: "سرور برای هر آدرس RTP یک session در map نگه می‌دارد. اگر همان url از قبل running باشد، reuse می‌کند. اگر جدید باشد، session می‌سازد و goroutineهای پس‌زمینه را start می‌کند.",
    how: [
    "url را با resolve نرمال می‌کند.",
    "کلید map: sessions[url]",
    "سقف هم‌زمان: ۶۴",
    "برای session جدید: setLaneLocked + go loop + (در صورت نیاز) idle watchdog",
    "gen نسل session است؛ بعد از Stop، gen بالا می‌رود تا goroutineهای قدیمی خارج شوند."
    ],
    why: "دیوار مانیتور مدام Start می‌زند؛ reuse جلوی انفجار سوکت/لاگ را می‌گیرد.",
    next: "→ resolve → setLaneLocked → go loop",
  },
  {
    title: "نرمال‌سازی URL",
    file: "internal/stream/stream.go",
    method: "resolve()",
    phase: 4,
    key: "Start و Stop باید دقیقاً از یک کلید یکسان استفاده کنند.",
    plain: "آدرس ورودی را تمیز می‌کند. برای udp/rtp فقط scheme+host+port را به‌عنوان کلید نگه می‌دارد تا query اضافی نقشه را خراب نکند.",
    how: [
    "اگر URL خالی باشد، مقدار پیش‌فرض DefaultURL استفاده می‌شود.",
    "schemeهای مجاز: rtsp ، udp ، rtp",
    "مثال تبدیل به کلید map: ورودی udp://224.2.0.1:5484?codec=h265 فقط scheme و host و port می‌ماند کلید map udp://224.2.0.1:5484",
    "علامت codec جدا در Start خوانده می‌شود (برای HEVC )."
    ],
    why: "اگر کلید map فرق کند، Stop همان session را پیدا نمی‌کند.",
    next: "بعد: idFor",
  },
  {
    title: "ساخت شناسه استریم",
    file: "internal/stream/stream.go",
    method: "idFor()",
    phase: 4,
    key: "شناسهٔ کوتاه و ثابت از روی آدرس منبع؛ برای مسیرهای پخش در مرورگر.",
    plain: "از روی آدرس منبع اثرانگشت می‌سازد و ۱۶ کاراکتر hex برمی‌دارد. همان شناسه داخل مسیر پخش می‌آید.",
    how: [
    "sha256(url) به ۸ بایت اول به hex",
    "ValidID همین طول و فرمت را چک می‌کند",
    "مسیر پخش: /webrtc/{id}/offer"
    ],
    why: "نمی‌خواهیم آدرس بلند multicast داخل مسیر مرورگر برود.",
    next: "بعد: setLaneLocked",
  },
  {
    title: "خط WebRTC چیست؟ (Lane)",
    file: "internal/stream/stream.go",
    method: "setLaneLocked()",
    phase: 5,
    key: "خط خروجی (lane) یعنی «مسیر خروجی رسانه» داخل همان session. ورودی یکی است؛ خروجی می‌تواند HLS یا WebRTC باشد.",
    plain: "وقتی output_type=7 ارسال شود، خروجی این session باید WebRTC باشد. lane مسیر egress رسانه داخل session است: اجزایی که پکت ویدیو را به مرورگر می‌رسانند. این با HTTP control plane فرق دارد.",
    how: [
    "output_type فقط ۶ یا ۷ قبول است.",
    "۷: wantRTC=true و ensureRTCLocked() — خط WebRTC را ایجاد یا فعال می‌کند.",
    "۶: startLLLocked() — خط LL-HLS.",
    "داخل session فیلد out نوع lane فعال را نگه می‌دارد.",
    "ingest (گرفتن از Record) مشترک است؛ lane فقط می‌گوید خروجی کجا برود.",
    "WebRTC lane شامل: rtcHub + rtcQ + drainRTC + بعداً peerهای مرورگر.",
    "Signaling (offer/answer) جدا از خود lane رسانه است؛ ولی به همان id وصل می‌شود."
    ],
    why: "خط خروجی را با ingest یکی نگیرید: Open ورودی است و lane خروجی.",
    next: "→ ensureRTCLocked()",
  },
  {
    title: "ساخت قطعات خط WebRTC",
    file: "internal/stream/stream.go",
    method: "ensureRTCLocked()",
    phase: 5,
    key: "تابع ensureRTCLocked اجزای WebRTC lane را داخل session آماده می‌کند؛ هنوز لزوماً پکت روی lane جریان ندارد.",
    plain: "اگر hub نبود می‌سازد، صف rtcQ را باز می‌کند، و goroutine مربوط به drainRTC را start می‌کند تا از صف بخواند و به hub بدهد. SPS/PPS را هم اگر از قبل یاد گرفته باشد به hub می‌دهد.",
    how: [
    "rtc.NewHub(...) مرکز توزیع به peerها",
    "rtcQ کانال با بافر ۸۱۹۲ — بین sniff و hub",
    "go drainRTC(rtcQ, hub) حلقهٔ خواندن از صف",
    "SetParams(SPS,PPS) برای دیکود درست در مرورگر",
    "اگر hub از قبل بود، فقط params را تازه می‌کند (reuse)"
    ],
    why: "اگر sniff مستقیماً به همهٔ peerها Write کند، تأخیر یک peer کل UDP Read را مسدود می‌کند.",
    next: "→ go loop (ورودی)",
  },
  {
    title: "loop دقیقاً چیست؟",
    file: "internal/stream/stream.go",
    method: "loop()",
    phase: 6,
    key: "تابع loop راه‌انداز ingest است؛ حلقهٔ بی‌نهایت Read نیست. حلقهٔ Read داخل sniff است که loop آن را start می‌کند.",
    plain: "بعد از Start، یک goroutine تابع loop را اجرا می‌کند. loop نوع منبع را بررسی می‌کند، مسیر ingest را انتخاب می‌کند، سپس sniff یا GStreamer را start می‌کند. خودش مدام Read نمی‌کند.",
    how: [
    "با gen/my بررسی می‌کند که نسل فعلی session همچنان معتبر است یا Stop شده است.",
    "اگر url از نوع udp/rtp باشد و HEVC اجباری نباشد: rtp.Open سوکت را باز می‌کند",
    "یک فایل SDP کمکی می‌نویسد",
    "go sniff(...) را start می‌کند ← حلقهٔ واقعی Read از اینجا آغاز می‌شود",
    "اگر Open شکست بخورد یا منبع RTSP/HEVC خاص باشد → startGStreamerRemux",
    "آخر کار لاگ می‌زند: lane چیست، ingest=sniff یا remux",
    "پس: loop = تصمیم + استارت ingest · sniff = حلقهٔ مستمر Read"
    ],
    why: "نام loop ممکن است گمراه‌کننده باشد: این تابع حلقهٔ خواندن پکت نیست، بلکه راه‌انداز ingest است.",
    next: "→ rtp.Open و/یا go sniff",
  },
  {
    title: "باز کردن سوکت RTP",
    file: "internal/protocols/rtp/rtp.go",
    method: "Open()",
    phase: 6,
    key: "تابع Open سوکت شبکه را به Record Server وصل می‌کند (ingest).",
    plain: "آدرس udp/rtp را می‌گیرد. اگر multicast باشد عضو گروه می‌شود تا پکت‌های گروهی را ببیند؛ اگر unicast باشد روی پورت گوش می‌دهد.",
    how: [
    "MediaURL چک می‌کند scheme و host و port درست باشد.",
    "multicast → JoinGroup روی چند NIC",
    "unicast → Listen روی پورت",
    "SO_REUSEADDR تا بعد از Stop دوباره بتوان همان پورت را باز کرد"
    ],
    why: "بدون join، Record ارسال می‌کند اما این پروسه پکتی دریافت نمی‌کند.",
    next: "→ sniff.Read",
  },
  {
    title: "sniff: حلقهٔ واقعی خواندن",
    file: "internal/stream/sniff.go",
    method: "sniff()",
    phase: 7,
    key: "تابع sniff همان حلقهٔ بی‌نهایت است: Read → تشخیص → در صورت ویدیو fanout.",
    plain: "از وقتی loop آن را start کند، مدام از سوکت UDP بسته می‌خواند. اگر شبکه ساکت شد، gap را تمیز می‌کند و در multicast دوباره join می‌کند. اگر بسته ویدیو بود، به lane خروجی می‌فرستد.",
    how: [
    "for { Read } تا زمانی که session با همان gen معتبر باشد",
    "deadline حدود ۲ ثانیه",
    "timeout → afterGap (FU نصفه دور) + Rejoin",
    "handlePacket تصمیم می‌گیرد ویدیو هست یا نه",
    "true → fanout به lane(های) فعال"
    ],
    why: "هر بلاک اضافه روی این حلقه = از دست رفتن پکت = فریز تصویر.",
    next: "→ handlePacket",
  },
  {
    title: "طبقه‌بندی بسته",
    file: "internal/stream/sniff.go",
    method: "handlePacket()",
    phase: 7,
    key: "یک سوکت ممکن است ویدیو + AI + TS را با هم حمل کند؛ باید demux/طبقه‌بندی شوند.",
    plain: "هر datagram را نگاه می‌کند: RTP ویدیوی H264 است؟ MPEG-TS است؟ JSON هوش مصنوعی است؟ HEVC است؟ فقط ویدیوی مناسب fanout می‌شود.",
    how: [
    "rtp.Parse — اگر OK نبود → مسیر TS/AI",
    "extension/payload ممکن است AI داشته باشد → صف جدا",
    "HEVC → مسیر ترنسکد (armHEVC)، نه H264 hub",
    "PT معمولاً ۹۶ + Unpack NAL + learn SPS/PPS + SEI",
    "touchPkt یعنی آخرین پکت اخیراً دریافت شده است"
    ],
    why: "دادهٔ هوش مصنوعی روی همان سوکت نباید sniff را block کند.",
    next: "→ Parse / Unpack",
  },
  {
    title: "پارس هدر RTP",
    file: "internal/protocols/rtp/rtp.go",
    method: "Parse()",
    phase: 7,
    key: "هدر RTP مثل برچسب روی بسته است؛ Parse همان برچسب را می‌خواند تا بقیهٔ مسیر بداند داخل بسته چیست.",
    plain: "از بایت خام درمی‌آورد: نوع payload (PT)، شماره ترتیب (seq)، timestamp روی ساعت ۹۰kHz، و خودِ بدنه. بدون این‌ها فقط آرایهٔ بایت بی‌معنی دارید.",
    how: [
    "PT معمولاً ۹۶ = ویدیوی H264 این پروژه",
    "timestamp روی ساعت ۹۰kHz",
    "extension اختیاری (گاهی AI)",
    "payload = دادهٔ فشردهٔ تصویر برای Unpack"
    ],
    why: "اشتباه رایج: فکر کردن که Parse خودِ تصویر را دیکود می‌کند — فقط برچسب را جدا می‌کند.",
    next: "→ Unpack",
  },
  {
    title: "باز کردن H264 به NAL",
    file: "internal/protocols/rtp/rtp.go",
    method: "Unpack()",
    phase: 7,
    key: "دیکودر NAL کامل می‌خواهد، نه تکه‌های RTP. Unpack تکه‌های FU-A را به یک NAL می‌چسباند.",
    plain: "فریم بزرگ روی شبکه تکه می‌شود: FU-A با پرچم‌های S (شروع) · M (میانی) · E (پایان). state در بافر fu می‌ماند. اگر وسط راه gap باشد، نصفه دور ریخته می‌شود تا franken-NAL ساخته نشود.",
    how: [
    "شکل‌ها: single NAL / STAP-A / FU-A",
    "FU-A: S=شروع · M=میانی · E=پایان",
    "بعد از gap نباید FU نصفه به فریم بعد بچسبد"
    ],
    why: "اشتباه رایج: چسباندن تکه‌های دو فریم مختلف = فریز عجیب در Chrome.",
    next: "→ learnVideoParams",
  },
  {
    title: "یادگیری SPS/PPS",
    file: "internal/stream/sniff.go",
    method: "learnVideoParams()",
    phase: 7,
    key: "SPS و PPS دفترچهٔ دیکود H264 هستند؛ بدون‌شان مرورگر معمولاً تصویر سیاه می‌بیند.",
    plain: "از بین NALها نوع ۷ (SPS) و ۸ (PPS) را برمی‌دارد و با hub.SetParams روی Hub می‌گذارد. وقتی هر دو آمدند videoReady روشن می‌شود و hot-path دیگر mutex سراسری نمی‌گیرد.",
    how: [
    "NAL type 7 = SPS · 8 = PPS",
    "هر دو → videoReady",
    "hub.SetParams برای Cap/SDP",
    "قبل از fanout؛ روی hot path بعد از آماده شدن، mu سراسری گرفته نمی‌شود"
    ],
    why: "اشتباه رایج: فکر کردن که سیاهی فقط از ICE است — اغلب SPS/PPS هنوز روی Hub نیست.",
    next: "→ fanout",
  },
  {
    title: "fanout به خط خروجی",
    file: "internal/stream/sniff.go",
    method: "fanout()",
    phase: 8,
    key: "fanout پکت را کپی می‌کند و غیرمسدود به صف lane می‌فرستد؛ sniff را هرگز معطل نمی‌کند.",
    plain: "بعد از snapshot کوتاه از hub/rtcQ/llCh، یک copy []byte ساخته می‌شود و با select/default به rtcQ (و اگر بود llCh) می‌رود. اگر صف پر باشد همان کپی drop می‌شود و فقط rtcGap=true می‌ماند.",
    how: [
    "snapshot کوتاه زیر mutex · کپی بیرون از lock",
    "select/default — بدون block",
    "پر بودن rtcQ → rtcGap=true (نه بستن سوکت)",
    "اولویت: ادامهٔ ingest؛ ترمیم با keyframe بعدی"
    ],
    why: "اشتباه رایج: فکر کردن که drop یعنی استریم مرد — فقط پرچم gap تا IDR بعدی.",
    next: "→ rtcQ",
  },
  {
    title: "صف rtcQ داخل خط WebRTC",
    file: "session.rtcQ",
    method: "channel buffer",
    phase: 9,
    key: "rtcQ انبار میانی است تا sniff سریع بنویسد و drain با سرعت egress بخواند.",
    plain: "نوع chan []byte با ظرفیت بزرگ (۸۱۹۲) برای IDR burst. بدون این صف، کندی یک peer کل Read/ingest را starve می‌کند. پر شدن = عقب‌افتادگی موقت تا keyframe.",
    how: [
    "نوع: chan []byte · ظرفیت ۸۱۹۲",
    "نویسنده: sniff / fanout · خواننده: drainRTC",
    "پر شدن → fanout فقط rtcGap می‌گذارد"
    ],
    why: "اشتباه رایج: یکی کردن نرخ دوربین با نرخ مرورگر — این صف همان جداسازی است.",
    next: "→ drainRTC",
  },
  {
    title: "drainRTC و Hub.Feed",
    file: "internal/stream/webrtc.go",
    method: "drainRTC() / Hub.Feed",
    phase: 10,
    key: "drainRTC از rtcQ می‌خواند؛ بعد از gap اول hub.Gap (needKey) می‌زند، بعد hub.Feed به peerها.",
    plain: "حلقه روی range rtcQ. اگر rtcGap بوده، hub.Gap() پرچم needKey را روشن می‌کند تا P-frame قبل از IDR بعدی push نشود. سپس hub.Feed(p) همان بسته را به همهٔ peerهای وصل‌شده می‌دهد.",
    how: [
    "range rtcQ",
    "rtcGap → hub.Gap() → needKey تا IDR",
    "hub.Feed(p) با ساعت wire RTP",
    "یک Feed، چند viewer"
    ],
    why: "اشتباه رایج: بعد از loss همهٔ بایت‌ها را کور push کردن — بدتر از صبر برای IDR.",
    next: "→ signaling / peers",
  },
  {
    title: "دست دادن WebRTC با React",
    file: "internal/api/stream.go",
    method: "POST /webrtc/:id/offer",
    phase: 11,
    key: "Offer/Answer روی HTTP فقط دست دادن است؛ بدون آن حتی با hub پر، <video> سیاه می‌ماند.",
    plain: "مرورگر SDP offer می‌فرستد. سرور RTCAnswer می‌سازد و با Soft ICE کاندیداها را داخل answer می‌گذارد (بدون trickle HTTP جدا). بعد رسانهٔ RTP از hub به peer می‌رسد و <video> زنده می‌شود.",
    how: [
    "path: /webrtc/{id}/offer",
    "بدنه: {type:\"offer\", sdp} → پاسخ {type:\"answer\", sdp}",
    "Soft ICE: gather داخل answer · بدون trickle HTTP",
    "رسانه جدا از signaling؛ فریم‌ها از قبل/همزمان روی lane‌اند"
    ],
    why: "اشتباه رایج: یکی دانستن signaling و رسانه — دو صفحهٔ جدا؛ هر دو لازم‌اند.",
    next: "→ پخش در React",
  },
  {
    title: "PTZ روی WebSocket",
    file: "internal/ptz/ws.go",
    method: "ServeHTTP()",
    phase: 11,
    key: "کنترل دوربین مسیر جدا از overlay است؛ overlay فقط HTTP poll می‌ماند.",
    plain: "بعد از Start، مرورگر به ws://host/ptz?id=… وصل می‌شود و JSON می‌فرستد. سرور ack برمی‌گرداند؛ در این رزومه فقط stub RPC در لاگ چاپ می‌شود.",
    how: [
    "Upgrade به WebSocket روی GET /ptz",
    "پیام: {type:\"ptz:move\", x, y, zoom}",
    "پاسخ: ptz:ack",
    "سناریو React دکمه‌های جهت + Zoom دارد"
    ],
    why: "PTZ کنترل plane است؛ نباید روی همان سوکت RTP یا overlay قاطی شود.",
    next: "→ Stop",
  },
  {
    title: "Stop و پاکسازی",
    file: "internal/stream/stream.go",
    method: "Stop()",
    phase: 12,
    key: "Stop جلسه را عمداً جمع می‌کند: gen++، بستن منابع، حذف از map.",
    plain: "سفارش توقف می‌آید. اول gen++ تا sniff/loop قدیمی خارج شوند؛ بعد UDP/hub/صف‌ها بسته و delete(sessions, url) می‌شود. این با sniff timeout فرق دارد — آنجا سوکت را Close نکن.",
    how: [
    "gen++ نسل را باطل می‌کند",
    "بستن mc · detach/close WebRTC/HLS · kill proc کمکی",
    "delete(sessions, url)",
    "multicast: Rejoin برای socketهای خواهر",
    "≠ sniff timeout (آنجا Close UDP ممنوع)"
    ],
    why: "اشتباه رایج: یکی دانستن Stop با afterGap — silence فقط Rejoin؛ Stop واقعاً می‌بندد.",
    next: "پایان lifecycle",
  }
];

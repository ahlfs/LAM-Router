# Changelog

## [Unreleased] — 2026-10-23

### API Usage per API key
- **Rollup pemakaian per model.** Tabel baru `api_key_model_usage` (`key_id`, `model`, `requests`,
  `total_tokens`, `cost`, `first_used`, `last_used`) menyimpan akumulasi per key per model, sehingga
  section usage tidak perlu memindai `request_logs` (yang dipangkas retensi) setiap kali dibuka.
- **Stamp pemakaian terakhir.** `api_keys.last_used_at` dan `last_model` diisi pada transaksi yang
  sama dengan akrual token/biaya, jadi "model terakhir" tidak pernah tertinggal dari counter.
- **Satu transaksi.** `IncrementApiKeyUsage` sekarang menulis counter lifetime, stamp, dan rollup
  dalam satu transaksi (sebelumnya satu UPDATE tanpa rollup). Biaya terukur +12 µs per request.
- **Backfill sekali jalan.** `BackfillKeyModelUsage` mengisi rollup dari `request_logs` saat upgrade
  pertama (hanya `api_key_id` yang benar-benar ada di `api_keys`, `status_code < 400`, nilai bukan
  model seperti `auth` dilewati). Idempoten: panggilan berikutnya no-op, tidak pernah dobel.
- **Endpoint baru.** `GET /v1/keys/usage` mengembalikan per key: `lastModel`, `lastUsedAt`,
  `usageTokens`, `usageCost`, `trackedTokens`, dan hingga 50 model (ditampilkan di modal).
  Biaya baca O(jumlah key).
- **Recount eksplisit (API).** `POST /v1/keys/usage/recount` membangun ulang counter lifetime dari
  rollup, untuk satu key (`{"id":"key_..."}`) atau semua key (body kosong). Tidak pernah otomatis,
  supaya reset manual tidak dibatalkan diam-diam, dan sengaja tidak diekspos sebagai tombol di UI.
- **Reset konsisten.** `resetUsage` juga menghapus rollup dan stamp, jadi breakdown tidak hidup lebih
  lama dari counter-nya. Menghapus key (`DELETE /v1/keys/{id}`) ikut menghapus baris rollup-nya,
  supaya tidak ada akumulator yatim yang luput dari retensi (retensi hanya memangkas
  `request_logs`/`requestDetails`).
- **Section "API Usage"** di halaman Endpoint & Keys (di bawah tabel key): per key menampilkan status,
  id, aktivitas terakhir (relatif + tanggal), empat metrik berlabel — tokens tracked (dengan tanggal
  mulai tracking), requests, biaya, dan model terakhir beserta kapan model itu dipakai — bar distribusi
  model (top 4 + sisanya, legenda persentase), bar pemakaian quota bila `quotaLimit > 0`, dan penanda
  `counter drift`. Rincian penuh per model dibuka di jendela gaya macOS (overlay gelap + blur, jendela
  `rounded-2xl`, tiga titik traffic light, tombol tutup, footer, tutup via Esc / klik luar) lewat tombol
  `View N models`; jendela ini dirender langsung tanpa portal/transisi sehingga tidak berkedip saat
  dibuka.

### Katalog model Antigravity disegarkan (diverifikasi ke upstream)
- **Penyebab 404:** `probeAntigravityModels()` bukan probe — ia menulis ulang daftar statis lama, jadi
  model yang sudah dihapus Google tetap ditawarkan dan setiap panggilan berakhir
  `404 Requested entity was not found`.
- **Hasil probe langsung (2026-10-23)** per id: `200` untuk `gemini-3.8-flash` beserta
  high/medium/low/thinking, `gemini-3.8-flash-cyber`, `gemini-3-flash-agent`; `429` (hidup, kuota
  habis) untuk `claude-3-7-sonnet`, `claude-sonnet-4-6`, `claude-opus-4-6-thinking`,
  `gpt-oss-120b-medium`; `404` untuk seluruh 3.7 dan 3.6, `gemini-3.1-pro-high/low`, `gemini-pro-agent`,
  `gemini-2.5-pro`, serta semua kandidat baru yang diuji (3.8-pro, 3.9-*, dst).
- **Template 22 → 11 model.** `gemini-3.8-flash-cyber` masuk (sebelumnya tidak terdaftar), model mati
  dikeluarkan. Sync kini juga **menghapus** baris `provider_models` dan `model_health` untuk id yang
  keluar katalog, supaya `/v1/models` berhenti menawarkannya.
- **Nama lama tidak lagi 404:** alias 3.7/3.6 → `gemini-3.8-flash-tiered` (rename, thinking level
  tetap); `gemini-2.5-pro`, `gemini-3.1-pro-*`, `gemini-3-pro-*`, `gemini-pro-agent`,
  `gemini-3.8-flash-agent` → `gemini-3-flash-agent`; `gpt-oss-120b-high` → `gpt-oss-120b-medium`.
- **Daftar statis di bundle UI** (dipakai picker combo) 19 → 11 entri, disamakan dengan katalog backend.

### Keamanan: model scope per API key akhirnya ditegakkan
- **`allowed_models` sebelumnya hanya disimpan, tidak pernah dicek.** Key "Restricted" tetap bisa
  memanggil model apa pun (permintaan diteruskan ke upstream), dan `/v1/models` tetap menampilkan
  seluruh katalog. Sekarang scope ditegakkan di `/v1/chat/completions`, `/v1/messages`, `/v1/embeddings`,
  `/v1/images/generations`, `/v1/audio/speech` (termasuk jalur MiMo TTS), dan endpoint media lain yang
  lewat `forwardMediaRequest`. Penolakan berupa `403` dengan `type=permission_error`,
  `code=model_not_allowed`, dan pesan yang menyebut model yang ditolak beserta daftar yang diizinkan.
- **Dicek sebelum cache dan sebelum upstream**, jadi response cache tidak bisa melewati batasan dan
  model terlarang tidak menghabiskan kuota provider.
- **Cocok untuk nama tanpa prefix**: entri `provider/model` juga menerima panggilan `model` saja, tapi
  entri ber-prefix tidak pernah cocok dengan provider lain. Perbandingan case-insensitive.
- **Combo**: lolos bila nama combo diizinkan, atau bila seluruh anggota combo ada di dalam scope.
- **`/v1/models` difilter** untuk key terbatas (route tetap publik; filter aktif bila client mengirim
  key). Sesi admin/dashboard tidak terpengaruh.
- **Scope rusak gagal-tertutup**: JSON yang tidak bisa dibaca membuat key menolak semua model, bukan
  berubah menjadi tanpa batasan.

### Table & List Density
- **Kontrol density sekarang benar-benar bekerja.** Sebelumnya `uiDensity` hanya tersimpan di
  localStorage tanpa consumer. Sekarang atribut `data-density` dipasang sebelum paint dari
  `index.html`, dan CSS memakai variabel padding pada kit tabel (`data-slot="table-cell"` /
  `table-head`) serta baris list (`lr-list-row`): sel 12px→18px, header 10px→14px, baris list
  8px→16px saat mode Cozy.

## [v2.3.1] — 2026-10-22

### Perbaikan audit Settings
- **Setelan yang benar-benar dipakai gateway.** Setelan logging, retensi log, dan pencatatan token
  sekarang tersimpan di server (`GET`/`POST /v1/settings/preferences`) dan dijalankan gateway:
  pemangkas audit (`request_logs`, `requestDetails`) berjalan saat start lalu tiap 6 jam, level
  `metadata` membuang isi request/response dari baris detail, level `disabled` tidak menulis baris
  audit sama sekali, dan `recordTokenUsage=false` menyimpan token/biaya nol serta melewati akrual
  kuota API key.
- **Playground defaults tersambung.** Nilai temperature, top-p, max tokens, system prompt, dan
  stream dari tab Playground Defaults sekarang dibaca halaman Playground (bukan sekadar disimpan),
  diverifikasi lewat render: slider 0.20, max tokens 1234, system prompt terisi.
- **Reset All to Defaults** kini juga mengembalikan preferensi server ke nilai pabrik.
- **Validasi input:** password admin minimal 8 karakter, `outboundProxy` harus URL http(s) absolut,
  TTL cache minimal 1 detik, import backup menolak payload asing, dan `GET /v1/settings` melaporkan
  versi biner yang benar-benar berjalan (bukan "1.0.0").
- **Label jujur:** kontrol yang belum ditegakkan gateway (request timeout, retry/backoff, token
  refresh lead, mask sensitive headers) kini bertanda "Browser preference only".

## [v2.3.0] — 2026-10-22

### Sorotan
- **feat(prompt-inject): mesin injeksi ditulis ulang dan mencakup empat bentuk wire.** Paket baru
  `internal/promptinject` menggantikan penambal string lama di `chat/inject.go`. Kontraknya:
  tiga variabel template (`{{combo_name}}`, `{{display_name}}`, `{{requested_model}}`), empat
  mode (`prepend`, `append`, `replace`, `hidden_user`), dan komposisi terpisah per format —
  OpenAI chat, Claude (blok `system` dengan `cache_control` utuh), Responses (`instructions`
  dilipat ke `input`), dan Gemini (`systemInstruction.parts`, `contents` tidak disentuh).
  Mode `append` menaruh instruksi setelah giliran system/developer terakhir, `replace` membuang
  semua giliran system/developer klien, dan body yang tidak perlu diubah dikembalikan apa adanya.
- **feat(prompt-inject): tombol on/off dan Public Identity per scope.** Baris lama tetap aktif
  (flag `NULL` berarti "prompt tidak kosong = aktif"), jadi menambahkan sakelar tidak mematikan
  prompt yang sudah tersimpan. API simpan kini bergaya patch: hanya field yang dikirim yang
  ditulis, `null`/string kosong menghapus, dan `"enabled":"true"` ditolak 400 alih-alih
  dikoersi. Prompt tidak lagi dipangkas saat disimpan.
- **feat(prompt-inject): halaman dashboard dirapikan.** Sakelar aktif per kartu, isian Public
  Identity, unggah `.txt` (BOM dibuang, kosong = null), dan pratinjau dihitung mesin server yang
  sama lewat `POST /v1/prompt-inject/preview` sehingga tidak bisa melenceng dari perilaku asli.
  Pratinjau tertutup secara default dan isinya dipotong 1.200 karakter per blok — prompt 150 KB
  tidak lagi memicu render ulang di setiap ketikan.
- **feat(prompt-inject): jalur Claude `/v1/messages` ikut disuntik.** Injeksi dijalankan pada body
  Claude asli sebelum translasi, jadi provider Claude native menerima blok `system` utuh dan
  provider OpenAI menerima hasil gabungan dari translator.
- **fix(cache): kunci cache menghitung body yang sudah disuntik.** Sebelumnya lookup terjadi
  sebelum injeksi, sehingga dua combo dengan member sama dan prompt berbeda bisa saling menyajikan
  jawaban cache.
- **fix(quota): kartu provider tertutup secara default.** Kondisi render dan label chevron
  sebelumnya terbalik, jadi semua kartu terbuka saat halaman dibuka dan tombol Expand/Collapse
  bertukar arti.

### Ditambahkan
- `internal/promptinject/`: `Render`, `Validate`, `Apply` beserta test tabel untuk empat format,
  mode, variabel template, dan jaminan no-op.
- `internal/handlers/chat/prompt.go`: satu titik komposisi (global lalu combo) yang dipanggil
  sebelum cache dan sebelum dispatch combo/fallback.
- `internal/handlers/routerapi/prompt_inject.go`: field `enabled`, `enabledExplicit`,
  `displayIdentity`, patch-style set, dan endpoint preview.
- Kolom `prompt_injection_enabled` + `display_identity` pada `combos` dan `fallback_rules`, serta
  kunci `global_prompt_enabled` pada `system_settings`. Semua additive; baris lama aman.

### Dihapus
- `internal/handlers/chat/inject.go` dan `inject_test.go` — digantikan oleh paket
  `promptinject`; seluruh kasus ujinya (termasuk `hidden_user`) dipindahkan ke test baru.


## [v2.2.0] — 2026-10-21

### Sorotan
- **feat(prompt-inject): system prompt bisa disuntikkan per combo maupun global, tanpa menyentuh
  klien.** Empat mode: `prepend` (di atas system prompt klien), `append` (di bawahnya), `replace`
  (menimpa system prompt klien), dan `hidden_user` (dikirim sebagai giliran `user` + `assistant:
  "Understood."` sebelum pesan pertama klien — untuk model yang mengabaikan peran `system` atau
  klien yang mengunci system prompt-nya). Prompt global dijalankan lebih dulu, prompt combo
  menumpuk di atasnya, dan cakupan combo mengikuti `fallback_rules.source_model`. Disimpan di
  `fallback_rules.system_prompt` / `inject_mode` serta `system_settings.global_system_prompt` /
  `global_inject_mode`, dengan API `GET /v1/prompt-inject` dan `POST /v1/prompt-inject/set`.
  Halaman **Prompt Inject** di dashboard memakai tema dan sidebar LAM Cyberlab, lengkap dengan
  dropdown mode kustom, penghitung karakter, dan pratinjau hasil injeksi.
- **feat(logs): setiap baris request log kini mencatat API key mana yang dipakai.** Kolom Cost
  diganti kolom **API Key** (biaya pindah ke detail baris), `request_logs` menyimpan
  `api_key_id` + `api_key_name` hasil pencocokan hash key, dan key yang tidak dikenal dicatat
  lewat `unknownKeyFingerprint` — nilai key mentah tidak pernah masuk database maupun log.
- **fix(context): jendela konteks DeepSeek 4.1 terbaca 128k padahal 1M.** Metadata konteks dari
  upstream sekarang menang atas heuristik lokal, nilai yang sudah diketahui lebih besar tidak
  pernah diturunkan, dan override manual tidak pernah ditimpa saat sinkronisasi. Sumber tiap nilai
  disimpan di `provider_models.context_source` (`heuristic` / `upstream` / `manual`) dan
  ditampilkan sebagai chip di katalog model, dengan peringatan saat sinkronisasi gagal.

### Ditambahkan
- `internal/handlers/chat/inject.go`: mesin injeksi prompt (4 mode, penumpukan global → combo)
  yang dipanggil setelah `resolveModel()`, plus `inject_test.go`.
- `internal/handlers/routerapi/prompt_inject.go`: handler GET/SET prompt inject beserta validasi
  mode, dan `prompt-inject.html` yang dirender di dalam shell dashboard.
- Guard test `logs_api_key_test.go` dan `model_context_test.go` untuk atribusi API key serta
  aturan provenance konteks.

### Diperbaiki
- Jarak antar kolom tabel log dirapikan agar nama API key yang panjang tidak mendorong kolom lain.
- Label versi statis di bundel dashboard disinkronkan dengan versi paket npm (`v2.2.0 Live`).

## [v2.1.0] — 2026-10-21

### Sorotan
- **fix(translator): tool call tanpa argumen tidak lagi menjadi `null`, dan riwayat yang sudah
  teracuni tidak lagi memicu 400 pada model Claude.** Akarnya: `json.Unmarshal` terhadap literal
  `null` berhasil tanpa error namun menyisakan map `nil`, lalu `json.Marshal` atas map `nil`
  menghasilkan `"null"` — juga tanpa error — sehingga penjaga `if err != nil` tak pernah aktif.
  Akibatnya `functionCall` berjalan keluar sebagai `"args":null` pada request ke antigravity dan
  sebagai `arguments:"null"` pada respons ke klien; klien menyimpannya sebagai riwayat, dan saat
  model diganti ke Claude di sesi yang sama Vertex menolak seluruh request dengan
  `messages.N.content.X.tool_use.input: Field required`. Kini argumen kosong/`null`/bukan objek
  dinormalkan menjadi `{}` di sisi request, di kedua jalur respons (non-stream dan stream), di
  penjaga terakhir `WrapForAntigravity`, serta di `sanitizeToolArgs` untuk arah keluar format
  Claude. Sesi baru selalu tampak normal karena riwayatnya belum memuat tool call tersebut.
- **fix(api): permintaan yang ditolak middleware auth tidak lagi dihitung sebagai model.**
  Baris log untuk permintaan yang ditolak kini menyimpan `provider_id='auth'` tanpa model, dan
  statistik mengeluarkannya dari rincian per model dan per provider lalu melaporkannya lewat blok
  `blocked` (`{total, byStatus}`). Sebelumnya placeholder itu muncul sebagai model bernama "Auth"
  dengan 0 token di kartu Usage by model dan Provider Distribution.

### Ditambahkan
- Kartu **Provider Distribution** di dashboard: distribusi token masuk/keluar, jumlah
  permintaan, jumlah model, dan pangsa tiap provider - dirender di antara
  "Model Traffic & Volume" dan "Usage by model".
- Filter periode pada kartu statistik: Today / 24h / 7D / 30D / 60D / All time, dihitung
  di server lewat `/v1/logs/stats?period=` (dashboard melakukan polling tiap detik, jadi
  pemfilteran di klien akan memaksa data mentah ikut terkirim).
- Setiap baris `byModel` kini membawa `providerId` dan `providerName` asli, plus
  `byProvider` pada respons statistik.
- Ikon `web/dist/icons/providers/auth.svg`: id provider `auth` sebelumnya tidak punya
  ikon sehingga badge-nya jatuh ke inisial "AU".

### Diperbaiki
- Badai ~104 error konsol pada setiap muat halaman dingin. Akarnya: `index.html` memuat
  script entry sebagai `index-<hash>.js?v=health1` sementara 48 impor antar-chunk memakai
  `"./index-<hash>.js"` tanpa query. Identitas modul ES adalah URL penuh, jadi modul entry
  dievaluasi dua kali, React terduplikasi, dan `createRoot()` berjalan dua kali pada `#root`
  yang sama (terbukti: kontainer menyimpan dua kunci `__reactContainer$`). Root kedua
  membersihkan isi kontainer saat mount, root pertama lalu merekonsiliasi node yang sudah
  lenyap: 51 `removeChild` + 51 `insertBefore` NotFoundError, ditambah React #321 dan #185.
  URL entry kini disamakan dengan gaya standar Vite (nama berkas sudah content-hash).
- Label provider palsu: `qa()` meng-hardcode `"gateway"` bila id model tidak memuat `/`,
  sehingga setiap model tampil dengan badge "GA". Kini memakai provider sebenarnya; model
  yang dilayani beberapa provider memakai provider pemilik token terbanyak.
- **fix(api): metrik pemakaian per provider di halaman Quotas & Limits selalu kosong.**
  `getProviderUsageMetrics` menanyakan kolom `provider` yang tidak ada di `request_logs`,
  jadi query-nya selalu gagal dan panel "Usage Consumption History" terus menampilkan
  "No requests logged yet" padahal ada 1047 permintaan tercatat. Kini membaca `provider_id`,
  mencocokkan nama keluarga provider maupun id kredensial yang membawa akhiran
  (`antigravity` vs `antigravity-1791105810882`), mengabaikan baris permintaan yang ditolak,
  dan kegagalan query/scan dicatat ke log alih-alih hilang tanpa jejak.
  `lastUsedAt` juga dikirim sebagai epoch milidetik (angka) seperti field waktu lain - sebagai
  string, panel kuota gagal mengurainya lalu menampilkan deretan digit mentah.

## [v2.0.2] — 2026-10-20

### Perbaikan tema terang
- Token `--muted` pada 18 tema terang diisi abu-abu pekat; di enam tema nilainya
  identik dengan `--muted-foreground` (warna teks). Setiap `var(--muted)` di
  stylesheet ternyata dipakai sebagai `background-color` dengan alpha 10-80%,
  sehingga panel Universal Base URL, Cloudflare Tunnel, serta header tabel dan
  bar filter di halaman Endpoint & Keys tampil sebagai lempeng kelabu yang
  terbaca seperti nonaktif.
- Nilai `--muted` tiap tema terang kini diturunkan dari palet tema itu sendiri
  (65% `--secondary` + 35% `--muted-foreground`), jadi karakter hue tiap tema
  tetap terjaga. Tema gelap tidak tersentuh (diverifikasi: nol blok gelap
  berubah antara HEAD dan hasil akhir).
- Penjaga regresi baru memastikan `--muted` tema terang selalu berupa permukaan
  terang dan tidak pernah sama dengan `--muted-foreground`, disertai tes
  pembalik yang membuktikan penjaga itu benar-benar bisa gagal.

## [v2.0.0] — 2026-10-20

### Instalasi & update penuh lewat npm
- Instalasi resmi lewat registry npm (`npm i -g lam-router`), launcher Pattern A
  (`optionalDependencies`) untuk linux-x64, linux-arm64, darwin-arm64, darwin-x64.
- win32-x64 tidak punya paket platform: npm menolak nama barunya
  (`403 Package name triggered spam detection`, kebijakan sisi npm, bukan soal
  token). Rilis tidak digagalkan oleh penolakan itu — launcher menutup celahnya
  dengan mengunduh `lam-router-windows-amd64.exe` dari GitHub Release yang sama,
  memverifikasi sha256 terhadap `checksums.txt`, lalu menyimpannya di
  `node_modules/lam-router/cache/<versi>/` sehingga kanal tetap dilaporkan `npm`.
- `lam-router version` melaporkan versi terpasang, kanal pemasangan
  (`npm` atau `manual`) dan rilis terbaru di registry; `lam-router update`
  mendelegasikan ke npm dan tidak pernah menulis ke dalam node_modules.
- Pengecekan versi membaca registry npm (bukan tag GitHub) dengan timeout 8 detik
  dan batas baca 256KB. Notifikasi latar belakang memakai `--update-check`
  (matikan dengan `UPDATE_CHECK=false`), menggantikan `--auto-update`.

### Dihapus total: self-updater yang menulis binernya sendiri
- `updater.PerformSelfUpdate`, `restartSelf`, dan manifest `version.json` dihapus.
  Jalur lama (unduh, `chmod 0755`, timpa executable yang sedang berjalan) tidak
  punya verifikasi integritas maupun salinan rollback, dan bertabrakan dengan
  process supervisor yang sudah menjalankan biner tersebut.
- Endpoint `POST /api/version/update` dihapus, sehingga API key konsumen tidak
  lagi bisa memicu penggantian biner di server.

### Panel System Diagnostics
- Versi terpasang dan status rilis diambil dari backend `/version` (yang membaca
  npm), bukan konstanta `v0.1.3` yang dibandingkan dengan tag GitHub.
- Perintah yang ditampilkan untuk disalin menjadi `npm i -g lam-router@latest`.

### Rilis yang bisa diaudit
- `scripts/build-npm.sh` menolak build/publish kecuali HEAD berada tepat di tag
  rilis dan working tree bersih; `ALLOW_UNTAGGED=1` hanya untuk dry run lokal yang
  tetap dilarang publish.
- Satu nama platform yang ditolak npm tidak lagi menggagalkan seluruh rilis:
  penolakan name-screening dilewati dengan peringatan, sedangkan kegagalan lain
  (token, registry) tetap menghentikan publish sebelum root terbit.
- Mode `--verify` mengunduh artefak yang benar-benar dilayani registry dan gagal
  bila binernya tidak lagi cocok dengan kode — guard untuk regresi 1.8.5. Ia juga
  membedakan "nama ditolak npm" (celah yang diterima, ditutup launcher) dari
  "artefak hilang", dan menolak rilis bila launcher tidak membawa jalur unduhan
  terverifikasi untuk platform yang paketnya tidak bisa ada.
- Workflow rilis menerbitkan `checksums.txt`, memakai npm provenance, dan punya
  job verifikasi pasca-publish.

### Perbaikan yang ikut terbawa
- Fix kuota Antigravity (User-Agent `antigravity/hub/2.9.1`) yang belum sampai ke
  artefak npm 1.8.5.

## [v1.8.4] — 2026-08-14

### 🐛 Bug Fixes & Resilience

- **Combo Cycle Graceful Recovery & Fault Tolerance** — `flattenComboModels` now gracefully skips recursive / self-referencing combo branches with a warning log instead of failing hard with HTTP 400 (`combo cycle detected`), ensuring chatbot requests continue executing remaining valid models seamlessly. (`internal/handlers/chat/resolution.go`)
- **Safe Model Resolution on Leaf Models** — eliminates potential slice index-out-of-range edge cases when resolving combo leaf models that do not contain a provider prefix. (`internal/handlers/chat/resolution.go`)
- **Multi-Level Nested Combo Support** — verified recursive cascading combo expansion (e.g. `super-combo` → `mid-combo` → `base-combo` → leaf models) so all reachable models participate in round-robin, sticky, and fallback strategies. (`internal/handlers/chat/resolution.go`, `internal/handlers/chat/resolution_test.go`)

## [v1.8.3] — 2026-08-14

### ✨ Features

- **Antigravity Gemini 3.7 Flash Model Mapping** — canonical model IDs and aliases for `gemini-3.7-flash`, `gemini-3.7-flash-high`, `gemini-3.7-flash-agent`, `gemini-3.7-flash-medium`, `gemini-3.7-flash-low`, `gemini-3.7-flash-extra-low`, and `gemini-3.7-flash-thinking` correctly mapped to Google Antigravity backend model IDs (`gemini-3-flash-agent` / `gemini-3.5-flash-low`), fixing upstream 404 errors. (`internal/translator/antigravity.go`)
- **Enriched Prompt-Injection Guard** — enhanced prompt-injection detector with heuristic patterns for raw model delimiters (`<|im_start|>system`, `<<SYS>>`, `[SYSTEM PROMPT]`, `[INST]`), verbatim system prompt extraction attempts, and developer/admin mode override simulations. (`internal/tokensaver/injection.go`)
- **Accurate Gemini Cached Token Tracking** — correctly unmarshals and propagates `cachedContentTokenCount` from Gemini stream and non-stream responses into `OpenAIUsage.CachedTokens`, providing accurate cache hit reporting and cost calculation. (`internal/translator/gemini.go`)
- **Gemini Vision FileData & Audio Modalities** — added support for remote HTTP/HTTPS image URLs (`fileData: { fileUri, mimeType: "image/*" }`), base64 input audio (`input_audio`, `audio_url`), and uploaded documents in Gemini native translator, matching Next.js full multimodal capabilities. (`internal/translator/gemini.go`)
- **Realtime SSE Usage Stream & Topology Animation** — added in-memory in-flight request tracker (`internal/usagetracker`), real-time SSE broadcasting (`GET /api/usage/stream` and `GET /usage/stream`), and recent requests ring buffer matching the Next.js dashboard shape, enabling instant glowing pulse node & marching-ants edge animations on the Usage Topology graph when requests are handled by `LAM-Router`. (`internal/usagetracker/tracker.go`, `internal/handlers/usage_stream.go`, `internal/handlers/chat/fallback.go`, `internal/handlers/chat/usage.go`)
- **Antigravity Anti-Competitive Prompt Stripping & 429 Prevention** — automatically strips competitor identity phrases (e.g. `"You are a Claude agent, built on Anthropic's Claude Agent SDK."` from Zed IDE and Claude agents) from `system_instruction` and message contents, preventing Antigravity from returning synthetic `429 Quota Exhausted` errors. (`internal/translator/antigravity.go`)
- **Edge Relay URL Rewriting & Header Forwarding** — automatically rewrites `BaseURL` to the relay deployment and injects `x-relay-target` and `x-relay-path` headers when a connection uses a Vercel, Cloudflare Worker, or Deno Edge Relay Proxy Pool. (`internal/handlers/chat/connections.go`)
- **No-Auth Provider Proxy Pool Strategy** — automatically respects `settings.providerStrategies` for no-auth providers (e.g. `mimo-free`, `opencode`), attaching configured proxy pools or rotation strategies to virtual connections. (`internal/handlers/chat/connections.go`, `internal/db/settings.go`)
- **Snake_case Model Limits on `/v1/models` & `/v1/models/info`** — exposes `context_length`, `max_completion_tokens`, `max_input_tokens`, and `max_output_tokens` so clients like Cline, Roo Code, and LibreChat resolve proper context ceilings. (`internal/handlers/chat/chat.go`, `internal/providers/capabilities.go`)
- **CodeBuddy OAuth Configuration** — registered `codebuddy-cn` and `codebuddy-intl` OAuth token refresh configurations. (`internal/providers/oauth.go`)
- **OpenCode Official Client Fingerprint Headers** — injects official headers (`User-Agent: opencode`, `x-opencode-client: desktop`, `x-opencode-session: ses_...`, `x-opencode-request: msg_...`, `x-opencode-project: global`) on free-tier OpenCode requests to prevent rate limiting from unidentified client traffic. (`internal/proxy/opencode.go`, `internal/proxy/executor/providers.go`)
- **Kimchi Dual Authentication** — supports direct API keys (`Authorization: Bearer <key>`) in addition to OAuth tokens with seamless credential resolution. (`internal/handlers/chat/connections.go`, `internal/handlers/chat/kimchi_handler_test.go`)
- **Startup Banner & Version Display** — dynamically displays current version in CLI startup banner (`🚀 LAM-Router (v1.8.3) on :20130`) and server ready logs. (`cmd/LAM-Router/main.go`)
- **New Provider Registries & Aliases** — added Alibaba Token Plan Singapore (`alitp-intl` / `ali-tp` / `alitp`) and Fish Audio Text-to-Speech (`fish-audio` / `fish`). (`internal/providers/providers.go`, `internal/providers/aliases.go`)

### 🐛 Bug Fixes

- **Invalid Tool Parameters & Decoy Schemas** — provided valid non-empty `properties.reason` schema for all 21 Antigravity decoy tools and mapped `tool_call_id` to exact function names in OpenAI-to-Gemini conversation history, eliminating protobuf validation errors when using Claude Code or other tool-calling clients. (`internal/translator/antigravity.go`, `internal/translator/gemini.go`)
- **Antigravity Upstream Model Resolution** — prevented invalid model aliases like `gemini-3.7-flash-high` from reaching Google Cloud Code without being translated to their backend model IDs. (`internal/translator/antigravity.go`)

### 📚 Documentation

- Comprehensive refresh of `README.md`, `COMPARISON.md`, `DATABASE.md`, `ARCHITECTURE.md`, `TECHNICAL_DEBT.md` (0 open items), and newly added `ROADMAP.md`.

## [v1.8.2] — 2026-08-14

### ✨ Features

- **Antigravity Tool Cloaking & Anti-Ban Decoy System** — automatically cloaks client tool declarations with `_ide` suffixes (e.g. `Bash_ide`), injects 21 official Antigravity IDE decoy tools (`run_command`, `replace_file_content`, `grep_search`, `list_dir`, etc.), synchronizes conversation history functionCall/functionResponse names, and seamlessly uncloaks tool names on response SSE stream and non-stream outputs. (`internal/translator/antigravity.go`, `internal/translator/gemini.go`)
- **Antigravity Native Image Generation** — added image model detection (`imagen`, `*image*`), aspect ratio suffix parsing (`16x9`, `4:3`, `1:1`, custom resolutions via GCD reduction), `requestType: "image_gen"` envelope wrapping with forced non-streaming `/v1internal:generateContent`, and OpenAI-compatible base64 image response formatting. (`internal/translator/antigravity.go`, `internal/proxy/gemini.go`)
- **Edge Relay & Transport Engine** — added transport support for Vercel, Cloudflare Worker, and Deno edge relays using `x-relay-target` and `x-relay-path` headers, wildcard `noProxy` domain filtering, and legacy connection-level proxy configuration fallback (`connectionProxyUrl`). (`internal/proxy/transport.go`, `internal/handlers/chat/connections.go`)

### 🐛 Bug Fixes

- **Proxy Pool DB Parsing Bug** — fixed `GetProxyPool` (`internal/db/proxyPools.go`) failing to parse single string `proxyUrl` created by Next.js UI / `InsertProxyPool`, which previously caused proxy pools to be silently ignored and requests to fall back to direct connections. Added parsing for `type`, `noProxy`, and `strictProxy` metadata.

## [v1.8.1] — 2026-08-12

### ✨ Features

- **Combo strategy sync with Next.js reference** — per-combo rotation state, correct auto-switch ordering, and a capabilities provider (`internal/providers/capabilities.go`) replacing the hardcoded vision/pdf maps with tiered capability detection.
- **Flatten nested combos** — `combo-wombo → free-tier` now expands to its four leaf models, so round-robin actually rotates across them instead of always landing on the first leaf (this was hammering one account and producing the `429 all connections for this provider are rate-limited` error).
- **Turn-aware rotation** — `applyComboStrategy` advances the rotation index only on a new turn; mid-turn tool-use requests reuse the model serving the turn, so the provider never switches mid-turn (which broke Gemini thinking models that require a `thought_signature` on current-turn function calls).
- **Bounded retry-once on total combo 429** — when every combo model fails with a Retry-After ≤ 8s, the pass waits once and retries before surfacing a hard 429 (`comboRetryAfter`).
- **Backfill default `thought_signature`** — every `functionCall` part now carries a `thoughtSignature` (the real one via `__ts__` transport when present, else the Next.js `DEFAULT_THINKING_AG_SIGNATURE`), closing the last 400-`thought_signature` gaps on mixed combos.
- **Gemini tool-schema keyword parity** — strip the remaining unsupported JSON-Schema keywords (`multipleOf`, `uniqueItems`, `contains`, `unevaluated*`, `contentSchema`) and fill bare `{}` schemas with the object placeholder, matching Next.js `cleanJSONSchemaForAntigravity` (fixes `Invalid tool parameters` 400 from antigravity).
- **Sanitize tools on the OpenAI-compat Gemini path** — the `gemini` provider is now marked `gemini-openai` and its `/v1beta/openai` bodies are run through `SanitizeOpenAITools`, so the strict schema validation applies on both Gemini routes.

### 🐛 Bug Fixes

- **Emit camelCase `thoughtSignature`** — the Gemini-native `generateContent` endpoint only recognizes the camelCase part field; the snake_case regression caused the `400 Function call is missing a thought_signature` error. Both read and write directions now handle camelCase.
- **Use the daily antigravity endpoint** — migrate `cloudcode-pa.googleapis.com` → `daily-cloudcode-pa.googleapis.com` for the `antigravity` provider (`providers.go`, `antigravity_project.go`, MITM domain list) to avoid strict rate limits.
- **Combo connection retry-loop parity** — connection retry loop now matches the single-model path.

### 🧹 Chores / Docs

- Remove the stray `patch_combo.go` throwaway script.
- Add design specs and implementation plans for the combo sync / thought_signature / backfill / schema-parity work.

## [v1.8.0] — 2026-08-11

### 🐛 Bug Fixes

- **Combo/router fallback bypasses model-lock backoff on retryable errors → antigravity rate-limit loop** — `handleComboFallback` / `handleMessagesComboFallback` (`internal/handlers/chat/combo.go`) called `tryForwardWithConnection` directly, so `LockConnectionModel` was never invoked on retryable errors (429/500s) — unlike the single-model path `handleAccountFallback`. The exponential 429 backoff was dead in the router path: every request re-tried all combo models back-to-back on the same connection/account, got 429, returned 429, and the client's ~35s retry repeated the loop forever. Fix:
  - New `comboLockRetryable` helper runs on every `RetryableStatusCodes` error in both combo loops — classifies via `ClassifyError`, calls `LockConnectionModel(connID, model, cooldownSec, newBackoffLevel)` so the exponential backoff persists across requests, and appends the conn to a request-local `excludeIDs` passed into `getBestConnection` so remaining combo models don't re-select the same connection (same account = same quota bucket).
  - A locked-connection skip covers pinned connections whose direct-fetch branch bypasses `getBestConnection`'s lock check.
  - `context.Background()` → `ctx` in `handleComboFallback` so client cancels propagate; the 502/503/504 transient-wait sleep is preserved.
  - Test: `TestHandleMessagesComboFallback_429LocksAndExcludesConnection` asserts a 429 locks the connection AND keeps the second combo model from re-hitting it (exactly 1 upstream hit).

## [v1.7.2] — 2026-08-08

### 🐛 Bug Fixes

- **Antigravity 429/404 failure-loop fix** — An unprovisioned Antigravity account
  (`onboardUser` returns `200` with an empty `cloudaicompanionProject`) left the
  connection without a `projectID`. The router then force-refreshed the OAuth
  token on every request (never an auth problem, so it never helped), fell through
  to a guaranteed-404 OpenAI-compatible lane on `cloudcode-pa.googleapis.com`,
  and repeated client retries rammed Google's rate limit (`429`). (`internal/handlers/chat/gemini_handler.go`, `internal/handlers/chat/antigravity_project.go`)
  - `fetchAntigravityProjectID` now reports the outcome (`projectID`, `authFailed`,
    `noProject`). Token refresh runs **only** on a genuine `401/403` — never on a
    missing/empty project.
  - When antigravity has no project ID, it no longer burns a request on the dead
    OpenAI lane; it returns an error and the fallback chain moves straight to the
    next provider.
  - **Negative cache (10 min, per connection):** once Google confirms "no project",
    later requests skip the `loadCodeAssist`/`onboardUser` RPCs entirely — this is
    what stops the repeated `429` hammering.
  - Onboarding guidance is logged once per connection per window
    ("onboard the account via Antigravity IDE/CLI, then re-login"); repeated
    failures log at `Debug` instead of spamming `Warn`. (`internal/handlers/chat/fallback.go`)

### 🧪 Tests

- `antigravity_project_test.go` — pins the probe classification (project found /
  token rejected `401`+`403` / project definitively missing / transient `429`+`503`)
  and the negative-cache expiry semantics. (`internal/handlers/chat/antigravity_project_test.go`)

## [v1.7.1] — 2026-08-08

### 🐛 Bug Fixes

- **Cached-token parity across every provider** — Prompt-cache accounting no longer works only for antigravity. Gemini `usageMetadata.cachedContentToken` now flows through both non-stream and stream translation into OpenAI `usage.cached_tokens`; the `!translate` response path uses a dual-format parser (`ParseResponseUsage`) that reads Claude `cache_read_input_tokens`/`cache_creation_input_tokens` and OpenAI `prompt_tokens_details.cached_tokens`, so cached tokens survive any provider → OpenAI → Claude double translation. (`internal/translator/gemini.go`, `internal/translator/response.go`)
- **Gemini tool-schema `const` re-injection** — `stripUnsupported` now re-runs after `anyOf`/`oneOf` flattening so `const` and vendor `x-*` keys can't leak back into the merged branch. (`internal/translator/schema.go`)
- **Provider 403 is now retryable** — Gemini/antigravity daily-quota errors can arrive as HTTP 403; these now trigger the connection fallback instead of a hard failure. (`internal/providers/providers.go`)
- **CodeBuddy CN stream cleanup** — The stall reader is now closed after the stream, stopping its shutdown watcher + stall timer (no per-request goroutine leak). (`internal/proxy/executor/codebuddy.go`)

### ⚙️ Graceful Shutdown Hardening

- New `internal/shutdown` package: a process-wide signal the first Ctrl+C / SIGTERM fires.
- `StallReader` now closes in-flight SSE upstream bodies on shutdown, so `server.Shutdown` drains streams in milliseconds instead of waiting out the 15s deadline — and the deferred DB/log-file close always runs.
- Translate-path SSE handlers emit a final `data: [DONE]` on abort so clients get a clean end instead of a truncated stream.
- A second Ctrl+C / SIGTERM force-quits immediately (stuck-drain escape hatch).
- Shutdown timeout logs a warning instead of `log.Fatalf`, so `conn.Close()` and the log file are still closed gracefully. (`internal/proxy/stall.go`, `cmd/LAM-Router/main.go`, `internal/handlers/chat/forward.go`, `internal/proxy/executor/openai.go`)

### 🔧 Internal

- Stream handlers now carry the request `ctx` and pull accumulated usage (incl. cached tokens) out of the translation session, so logged usage reflects real token counts instead of the character-estimate fallback.

## [v1.7.0] — 2026-08-06

### 🚀 New Executors

- **Trae SOLO remote agent** (`internal/proxy/executor/trae.go`) — Port of `engine/executors/trae.js`: `POST {base}/chat_sessions` creates a session, `GET {base}/chat_sessions/{id}/events` streams `plan_item` / `token_usage` / `done` as SSE. Cumulative `plan_item.thought` rendering (longest-wins per id, delta-only emission), `Cloud-IDE-JWT` auth, and `work`/`auto`/manual model modes. Non-stream requests aggregate into a single `chat.completion`. Round-trip test: `TestForwardTrae_StreamsAccumulatedThought`.
- **Windsurf gRPC-web** (`internal/proxy/executor/windsurf.go`) — Port of `engine/executors/windsurf.js`: hand-rolled protobuf `GetChatMessageRequest` encoder (Metadata.api_key + cascade_id + model_or_alias + repeated messages), gRPC-web framing (0x00 flag + big-endian length), and a `CompletionChunk` decoder (content / done+UsageStats / error) streaming OpenAI SSE. Catalog→wire model alias map ported verbatim; `crypto/rand` session/cascade ids. Non-stream requests aggregate frames into `chat.completion`. Round-trip test: `TestForwardWindsurf_StreamsGRPCWeb`.

### 🎙️ Xiaomi MiMo TTS

- `/v1/audio/speech` for the `xiaomi-mimo` provider now uses the chat-completions contract (port of `engine/handlers/ttsProviders/xiaomi-mimo.js`): target text in `role:assistant`, style/language instructions in `role:user`, voice via top-level `audio.voice`, base64 audio from `choices[0].message.audio.data`. (`internal/handlers/media/media.go`)

### ➕ Providers

- **tokenrouter** — Registered as an OpenAI-compatible upstream (`https://api.tokenrouter.com/v1/chat/completions`).

### 🗑️ Removed

- **qwen provider** — Removed from providers, OAuth config, and the alias map (deprecated upstream).

### 📋 Docs

- `TECHNICAL_DEBT.md` — windsurf + trae moved to resolved; zed + devin-cli documented with the safe-stopgap note (devin-cli corrected: ACP over **stdio** subprocess, not HTTP).

## [v1.6.1] — 2026-08-05

### 🐛 Bug Fixes

- **CodeBuddy CN 502** (`internal/proxy/executor/codebuddy.go`) — `codebuddy-cn` / `codebuddy-intl` now use a dedicated executor that forces `stream=true` upstream (CodeBuddy rejects non-stream with HTTP 400 code 11101), injects the CLI/IDE static headers, and re-aggregates OpenAI-chat SSE into a single `chat.completion` for non-stream clients (`sseToOpenAIJSON`, mirroring JS `parseSSEToOpenAIResponse`).
- Provider parity with the reference implementation.

## [v1.6.0] — 2026-08-04

### 🚀 Next.js Engine Feature Ports

- **TTS Voice Listing** (`/audio/voices`) — Full voice-listing with provider support (`edge-tts` default, `elevenlabs`, `gemini`, `local-device`), `?lang` filter, 24h in-process cache, and `byLang`/`languages` grouping matching the dashboard's media-providers page.
- **Proxy-Pools Deploy** (`/proxy-pools/{vercel,deno,cloudflare}-deploy`) — Deploy edge relay functions to Vercel/Deno/Cloudflare with status polling, plus a new `InsertProxyPool` DB method writing byte-compatible `data` JSON.
- **Headroom Management** (`/headroom/*`) — Full headroom-ai lifecycle in Go: binary/Python detection, spawn/stop/restart, compression extras install/uninstall, `/headroom/proxy` reverse proxy with SSRF guard, and dashboard HTML rewrite.
- **CLI-Tools Status** (`/cli-tools/all-statuses`) — Batch detection of 14 CLI tools (Claude, Codex, OpenCode, etc.) installed state + version.
- **Live Console Logs** (`/translator/console-logs`, `/stream`) — In-process ring buffer + SSE streaming of engine log output so the dashboard's "Monitor Console Log" shows Go logs live (25s keepalive, init/line/clear events).

### 🔍 Observability

- **Lightweight Request Tracing** (`/debug/traces`) — In-memory span recording + p50/p95/p99 latency per provider+model with `?n=` cap. Stdlib-only, no OpenTelemetry SDK dependency.

### 🛡️ Security

- **Prompt-Injection Guard** — Heuristic detection (`messages[]`, `input[]`, Claude content blocks) tagging classic injection attempts in logs. Toggle via `--no-injection-guard` / `INJECTION_GUARD_DISABLED` (on by default).
- **`/admin/health/reset` Moved Behind API-Key Auth** — Previously public; now requires a valid API key to prevent unauthenticated health-state resets (open-source hardening).
- **MITM Binds Loopback Only** — TLS proxy binds `127.0.0.1:443` instead of all interfaces, preventing LAN clients from using it as an open proxy.

### 🐛 Bug Fixes & Stability

- **SSE Fragment Rejoin** — Fixed `unexpected end of JSON input` on opencode free-tier by buffering/rejoining truncated SSE JSON payloads per session (1 MiB cap).
- **Codex/CommandCode Tool-Call Streams** — Stable per-call tool IDs/indices (using upstream `call_id`), correct `[DONE]` framing, and checked `w.Write` errors.
- **MITM Goroutine Leaks** — `Stop()` drains in-flight connections (WaitGroup + active conn close); request bodies bounded at 10 MiB.
- **Executor/OAuth Registry Mutexes** — Package-level registry maps now guarded by `sync.RWMutex` (race-free on re-registration).
- **Token Saver JSON Number Preservation** — `CompressMessages`/`InjectSystemPrompt` use `json.Number` so numeric fields (temperature, large ints) round-trip unchanged.
- **`interface{}` → `any`** — Lint cleanup across stream/log packages.

## [v1.5.0] — 2026-07-24

### 🚀 Architecture & Observability Enhancements

- **Modular `main.go` Refactoring** — Extracted CLI subcommands (`mitmEnable`, `mitmDisable`, `mitmStatus`, `resolveDataDir`) to `cmd/LAM-Router/commands.go` and encapsulated server routing setup into `handlers.SetupServerRouter()`.
- **Structured Request Logging Middleware** — Moved `statusWriter` and `RequestLogger` to `internal/middleware/logging.go`. Requests are logged with Correlation ID (`id=req_...`) using structured logger (`slog.Info`, `slog.Warn`, `slog.Error`).
- **Dynamic HTTP Status Log Levels** — Requests with status 5xx are logged at `ERROR` level, 4xx at `WARN` level, and 2xx/3xx at `INFO` level for clean log filtering in production.
- **Upstream Memory Exhaustion Protection** — Added `io.LimitReader` caps (1MB for upstream error bodies, 10MB for non-streaming completion bodies) to protect proxy memory from rogue upstreams.
- **Double WriteHeader Prevention** — Added `written bool` guard to `statusWriter` and `cw.IsCommitted()` checks across combo fallback handlers to eliminate `superfluous response.WriteHeader` warnings.
- **Typed Request ID Context Key** — Shared `log.RequestIDKey` across middleware and logging packages to ensure context lookups match reliably.

## [v1.4.0] — 2026-07-23

### 🛠️ Technical Debt Remediations (All 9 Items Resolved)

- **Context-based Per-Request Usage Capture** — Replaced global `translator.lastUsage` with context-captured isolation (`WithUsageCapture`, `SetUsage`, `GetAndClearUsage`) to eliminate cross-request data races under concurrent traffic. (`internal/translator/usage.go`)
- **Thread-safe Daily Usage Updates** — Protected `upsertDailyUsage()` with `dailyUsageMu` mutex to prevent concurrent SQLite read-modify-write races. (`internal/handlers/chat/usage.go`)
- **Committed Response Writer** — Wrapped `http.ResponseWriter` with `committedResponseWriter` to prevent safe-retry attempts after response headers have already been sent to the client. (`internal/handlers/chat/response_writer.go`)
- **Strict Context Propagation** — Replaced all `http.NewRequest` with `http.NewRequestWithContext` across handlers, proxy execution drivers, and OAuth helpers to prevent orphaned upstream connections.
- **Graceful Shutdown** — Implemented `http.Server` graceful shutdown with signal drain (15-second timeout) on SIGINT/SIGTERM. (`cmd/LAM-Router/main.go`)
- **SQLite Connection Pool Optimization** — Reduced SQLite `SetMaxOpenConns(4)` for optimal WAL mode performance and zero connection contention. (`internal/db/client.go`)
- **Thread-Safe ProxyPool Cache** — Added `sync.Map` `proxyPoolCache` in `internal/db/proxyPools.go` to preserve round-robin rotation indices across requests. (`internal/db/proxyPools.go`)
- **Unbounded Request Body Guard** — Added `middleware.MaxBody` (10MB limit) to protect all endpoints from OOM attacks. (`internal/middleware/max_body.go`, `cmd/LAM-Router/main.go`)

### ⚡ Metrics, Latency & Token Accounting Fixes

- **TTFT & Latency Tracking** — Added `StartTime` and `TTFT` tracking across all streaming and non-streaming proxy execution drivers (`openai`, `opencode`, `deepseek`, `claude`, `grok-cli`, `qoder`, etc.). (`internal/proxy/executor/`)
- **Input Token Calculation Fix** — Added `[]byte` type support to `CountValueChars` so fallback prompt token calculation accurately estimates token size instead of defaulting to 1 token. (`internal/handlers/chat/chat.go`)
- **Output Token Calculation Fix** — Connected `ResponseBuf` in `executor.Request` to record stream output tokens when upstream omits token usage objects. (`internal/proxy/executor/openai.go`)
- **Prompt Caching Tokens Support** — Updated `OpenAIUsage` to extract `cached_tokens` (`prompt_tokens_details.cached_tokens`) and `cache_creation_input_tokens`. (`internal/translator/types.go`, `internal/handlers/chat/usage.go`)

### 🧪 End-to-End Integration Test Suite

- **E2E Test Suite** — Added `internal/handlers/chat/e2e_integration_test.go` to test real HTTP streaming SSE, non-streaming JSON responses, TTFT latency, token accounting, and SQLite DB usage logging end-to-end.

### 🌐 Endpoints

- **`/api/hello`** — Registered `/api/hello` route returning `200 OK` for ping probes from Claude Code CLI. (`cmd/LAM-Router/main.go`)

## [v1.3.0] — 2026-07-22

### 🏥 Next.js-Compatible Health System

- **Connection-based health** — Replaced old `kv`-based `IsProviderHealthy`/`RecordProviderHealth` with `modelLock_*` fields in `providerConnections.data` JSON blob, matching Next.js `markAccountUnavailable` / `clearAccountError` flow. (`internal/db/health.go`, `internal/db/accounts.go`)
- **Per-connection model locks** — `LockConnectionModel` / `UnlockConnectionModel` / `IsConnectionModelLocked` use SQLite `json_set()` on shared `providerConnections.data`. Dashboard can read/write same fields. (`internal/db/accounts.go`)
- **`IsProviderAvailable`** — New `Repo` method checks if ANY connection for a provider has no active `modelLock_<model>`, replacing the old kv-based pre-check. (`internal/db/accounts.go`)
- **`POST /admin/health/reset`** — Resets `modelLock_*` on connections via query params `?provider=X&model=X`. Dashboard can call via headroom proxy. (`cmd/LAM-Router/main.go`)
- **Eliminated duplication** — Package-level `IsProviderHealthy` / `ResetProviderHealth` now delegate to `NewRepo(database)` instead of duplicating lock JSON parsing logic. (`internal/db/health.go`)

### 🧪 Test Fixes

- **False-pass assertions** — 3 handler tests were checking old kv-based `repo.IsModelLocked()` which always returned `false` vacuously. Changed to `repo.IsConnectionModelLocked(connID, model)` to actually verify connection-level locks. (`internal/handlers/chat_test.go`)

## [v1.2.0] — 2026-07-22

### 🎯 Gemini Tool Calling Fixes

- **thought_signature round-trip** — Gemini response encodes `thought_signature` into tool call `id` via `__ts__` separator; request decoder restores it for valid verification. Works for both streaming and non-streaming. (`internal/translator/gemini.go`)
- **Antigravity (AGY) support** — Custom `GeminiPart.UnmarshalJSON` handles `thoughtSignature` (camelCase) AND `thought_signature` (snake_case) since the internal `v1internal` endpoint returns camelCase. (`internal/translator/gemini.go`)
- **Tool response name fix** — `tool_call_id` with `__ts__` suffix no longer corrupts `functionResponse.name` extraction, preventing Gemini validation errors on turn 2. (`internal/translator/gemini.go`)

### 🎨 Logging

- **ANSI color-coded logs** — `INF` = green, `WRN` = yellow, `ERR` = red, `DBG` = cyan. Auto-detects TTY (disabled when piped). Disable via `NO_COLOR=1`. (`internal/log/log.go`)

### 🔧 Streaming Fixes

- **SSE multi-line** — Gemini stream chunks with multiple SSE lines (`data: ...\ndata: ...`) are now split and translated individually. Error on one line continues to next instead of aborting. (`internal/handlers/gemini_handler.go`)

### 🧹 Cleanup

- `fallback.go`: Removed misleading `WRN tokensaver failed` logs — replaced with idiomatic `if next, did := ...; did` pattern.
- `test_opencode.go`: Removed (stale temporary test file).
- `internal/translator/gemini_test.go`: Added (unit tests for `thought_signature` round-trip).

## [v1.1.0] — 2026-07-21

### 🚀 New Features

- **SSRF protection** — `/v1/web/fetch` now blocks requests to private/internal IPs (RFC 1918, loopback, link-local, cloud metadata). Matches Next.js `assertPublicUrl()`. (`internal/handlerutil/ssrf.go`)
- **Bypass handler** — Detects Claude Code naming, warmup, and count requests. Returns fake responses without calling upstream, preventing wasted combo rotation slots. (`internal/handlers/bypass.go`)
- **Structured logging** — New `internal/log` package with Info/Warn/Error/Debug levels, runtime config via `LOG_LEVEL` env var. All ~100 `log.Printf` calls replaced across 24 files.
- **Per-connection model locks** — Model locks now stored as `modelLock_<model>` in `providerConnections.data` JSON blob. DB-compatible with Next.js dashboard. Connection A and B can have independent lock states.
- **SSE stall detection** — `StallReader` wrapper closes upstream connection after 6 minutes of no data, preventing hung streams. Integrated into all 4 SSE stream paths.
- **Error classification** — Text-based error rules (8 patterns) + status-based rules (5 codes) + exponential backoff (2s–5min). Fully matching Next.js `checkFallbackError()`.
- **Retry-after tracking** — Tracks earliest `retryAfter` across combo models, includes `Retry-After` header in error responses.
- **Request ID tracing** — Every response includes `X-Request-ID` header, access log includes `id=xxx` prefix.
- **Combo strategies aligned with Next.js** — Sticky round-robin, auto-capability-switch (vision/pdf detection).
- **Health/lock check in combo loops** — Skip unhealthy or locked models during fallback iteration.

### 🔧 Refactoring

- **Error response consistency** — `WriteJSONError` now status-code-aware (e.g., 401 → `authentication_error`, 429 → `rate_limit_error`). `auth.go` inline JSON replaced.
- **SSE consolidation** — `proxy.WriteSSEHeaders` shared by all 4 SSE stream functions. `proxy.SSECopy` with optional `onChunk` callback.
- **Shared test fixture** — `internal/dbtest` package provides canonical `CreateTables()` eliminating duplicated schema in 5+ test files.
- **`stringBuilder` → `bytes.Buffer`** — Removed duplicate custom type in favor of standard library.

### 📚 Documentation

- `ARCHITECTURE.md` — 10 Mermaid flow diagrams (request lifecycle, combo, fusion, error classification, etc.)
- `DATABASE.md` — All 11 tables, JSON blob structure, Go vs Next.js differences

### 🐛 Fixes

- `RetryAfter` ceiling calculation corrected from floor to proper ceiling (`time.Second - 1`)
- Stream translation now handles `[DONE]` marker before JSON parsing
- `TranslateResp` field now passed in `tryForwardWithConnection`

## [v1.0.2] — Previous

- Initial release with OpenAI/Claude SSE proxy, combo fallback, token savers, benchmark results.

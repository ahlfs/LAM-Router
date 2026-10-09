# SupaAI Token Platform Architecture & Specification Design

- **Date:** 2026-10-09
- **Author:** Prince AL & Asa Mitaka
- **Status:** Approved Draft for Implementation
- **Repository:** Monorepo `git@github.com:ahlfs/SupaAI.git` (`frontend/` + `backend/`)

---

## 1. Executive Summary & Core Objectives

SupaAI adalah platform komersial penjualan token kecerdasan buatan (AI Gateway & Token Storefront) mirip OpenRouter yang melayani pengembang dan pengguna di Indonesia maupun internasional. 

Sistem dibangun menggunakan arsitektur **Monorepo** yang memisahkan peran dengan jelas:
1. **Frontend (`SupaAI/frontend`)**: Single Page Application (SPA) berbasis React, Tailwind CSS, dan Lucide Icons yang mengadopsi DNA visual, tata letak, dan fitur tema (Theme Switcher Dracula, Deus Ex, Tokyo Night, dll) dari LAM-Router, dilengkapi halaman publik, portal developer, dan dialog pembayaran QRIS otomatis.
2. **Backend (`SupaAI/backend`)**: Engine inference dan billing performa tinggi yang dikloning secara independen dari LAM-Router (Go + Chi + SQLite), ditransformasikan menjadi sistem multi-tenant terisolasi tanpa mengganggu instalasi LAM-Router pribadi milik pengguna.

---

## 2. Arsitektur Monorepo & Pemisahan Lingkungan

### 2.1 Struktur Direktori Monorepo (`/home/ahlfs/workspace/SupaAI`)

```
/home/ahlfs/workspace/SupaAI/
├── frontend/                     # Web Dashboard & Public Storefront (React 18 + Vite)
│   ├── src/
│   │   ├── components/           # Navbar, Sidebar, ThemeToggle, QRISModal, ModelCard
│   │   ├── context/              # AuthContext, ThemeContext, BalanceContext
│   │   ├── pages/                # Landing, Models, Pricing, Dashboard, Keys, Logs, AdminModels
│   │   └── styles/               # CSS Grid, Tailwind, Theme Variables
│   ├── package.json
│   └── vite.config.js
│
├── backend/                      # High-Performance Go Engine & Multi-Tenant Gateway
│   ├── cmd/
│   │   └── supa-router/          # Entrypoint binary main.go
│   ├── internal/
│   │   ├── supa/
│   │   │   ├── db/               # SQLite migrations, repository query users & keys
│   │   │   ├── auth/             # JWT token issuance, bcrypt hash, auth middleware
│   │   │   ├── billing/          # Tripay QRIS client, HMAC-SHA256 webhook validator, token ledger
│   │   │   └── models/           # Allowed models whitelist, maintenance toggles, multiplier registry
│   │   ├── proxy/                # OpenAI-compatible streaming proxy & SSE tokenizer
│   │   └── pricing/              # Raw model cost references
│   ├── go.mod                    # module supaapi
│   └── Makefile                  # build, run, test
│
├── README.md
└── docker-compose.yml
```

### 2.2 Alokasi Port & Isolasi Data
- **LAM-Router Pribadi (Existing)**: Port `9898`, Data dir `~/.lam-router/` (tidak tersentuh).
- **SupaAI Frontend**: Port `4321` (PM2 process: `supa-ai`).
- **SupaAI Backend Engine**: Port `9900` (PM2 process: `supa-backend`), Data dir `~/.supa-router/` (database SQLite `supa.db`).
- **Protected Ports Safety**: Port `3000` (Core IDE), `20128` (AG Proxy), dan `8900` (System Service) dijaga ketat agar tidak digunakan.

---

## 3. Skema Database SQLite Multi-Tenant (`supa.db`)

Semua tabel dirancang dengan tipe data terstruktur dan integritas referensial.

```sql
-- 1. Tabel Pengguna (Developer & Admin)
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    role TEXT DEFAULT 'developer', -- 'developer' | 'admin'
    token_balance INTEGER DEFAULT 0, -- Saldo kuota token aktif (misal 10000000 untuk 10M token)
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- 2. Tabel API Keys per Pengguna (Multi-Key with Limit)
CREATE TABLE IF NOT EXISTS user_api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    key_hash TEXT UNIQUE NOT NULL, -- SHA-256 hash dari API key asli
    key_prefix TEXT NOT NULL,      -- 'sk-supa-xxxx...' untuk preview di UI
    is_active INTEGER DEFAULT 1,   -- 1 = Aktif, 0 = Revoked
    spending_limit_tokens INTEGER DEFAULT 0, -- 0 = Unlimited (mengikuti saldo owner)
    total_spent_tokens INTEGER DEFAULT 0,    -- Akumulasi token terpotong oleh key ini
    created_at INTEGER NOT NULL
);

-- 3. Tabel Katalog Model, Whitelist & Multiplier
CREATE TABLE IF NOT EXISTS supa_models (
    model_id TEXT PRIMARY KEY,       -- e.g. 'deepseek-chat', 'claude-3-5-sonnet'
    display_name TEXT NOT NULL,      -- e.g. 'DeepSeek V3', 'Claude 3.5 Sonnet'
    provider_source TEXT NOT NULL,   -- e.g. 'deepseek', 'anthropic', 'custom_openai'
    multiplier REAL DEFAULT 1.0,     -- e.g. 1.0, 3.0, 10.0
    is_allowed INTEGER DEFAULT 1,    -- 1 = Tampil di publik & bisa dipakai, 0 = Terlarang/Hidden
    is_active INTEGER DEFAULT 1,     -- 1 = Online, 0 = Sedang Maintenance (HTTP 503)
    maintenance_message TEXT DEFAULT 'Model sedang dalam pemeliharaan berkala.',
    description TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- 4. Tabel Transaksi Pembayaran QRIS (Tripay)
CREATE TABLE IF NOT EXISTS topup_transactions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    merchant_ref TEXT UNIQUE NOT NULL, -- 'INV-20261009-XXXX'
    tripay_reference TEXT UNIQUE,      -- Nomor referensi unik dari Tripay
    payment_method TEXT DEFAULT 'QRIS',
    amount_idr INTEGER NOT NULL,       -- Rupiah riil, e.g. 50000
    tokens_granted INTEGER NOT NULL,   -- Token yang diberikan, e.g. 10000000
    status TEXT DEFAULT 'UNPAID',      -- 'UNPAID', 'PAID', 'EXPIRED', 'FAILED'
    qr_url TEXT,                       -- Link URL gambar QRIS Tripay
    qr_string TEXT,                    -- String raw QRIS payload
    expired_at INTEGER NOT NULL,
    paid_at INTEGER,
    created_at INTEGER NOT NULL
);

-- 5. Tabel Paket Token
CREATE TABLE IF NOT EXISTS token_packages (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    token_amount INTEGER NOT NULL,     -- e.g. 5000000 (5M Token)
    bonus_tokens INTEGER DEFAULT 0,    -- e.g. 500000
    price_idr INTEGER NOT NULL,        -- e.g. 25000
    badge TEXT,                        -- 'POPULAR', 'BEST VALUE'
    is_active INTEGER DEFAULT 1
);

-- 6. Tabel Audit Log Penggunaan Token
CREATE TABLE IF NOT EXISTS token_usage_logs (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    key_id TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_tokens INTEGER NOT NULL,
    completion_tokens INTEGER NOT NULL,
    total_tokens INTEGER NOT NULL,
    multiplier REAL NOT NULL,
    deducted_tokens INTEGER NOT NULL,
    latency_ms INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
```

---

## 4. Mekanisme Billing, Formula Multiplier, dan Alur Inferensi

### 4.1 Formula Pemotongan Kuota Token
Setiap panggilan `POST /v1/chat/completions` yang berhasil diselesaikan menghitung pengurangan token dengan rumus bulat (integer):

$$\text{deducted\_tokens} = \lfloor \text{total\_token\_usage} \times \text{multiplier} \rfloor$$

$$\text{new\_user\_balance} = \text{current\_user\_balance} - \text{deducted\_tokens}$$

$$\text{new\_key\_spent} = \text{current\_key\_spent} + \text{deducted\_tokens}$$

Contoh:
- Permintaan menggunakan `2.450 token` pada model `deepseek-chat` (Multiplier `1.0x`):
  $$\text{deducted} = 2.450 \text{ token}$$
- Permintaan menggunakan `2.450 token` pada model `claude-3-5-sonnet` (Multiplier `10.0x`):
  $$\text{deducted} = 24.500 \text{ token}$$

### 4.2 Urutan Evaluasi Gerbang Middleware (Security & Privacy Hierarchy)
Ketika request HTTP tiba di `POST /v1/chat/completions`:
1. **Model Whitelist Gate (`is_allowed`)**:
   - Jika `supa_models.is_allowed == 0` atau model tidak terdaftar:
   - Respon: `404 Not Found` (`{"error": {"message": "Model '...' is not available in SupaAI."}}`).
2. **Model Maintenance Gate (`is_active`)**:
   - Jika `supa_models.is_active == 0`:
   - Respon: `503 Service Unavailable` (`{"error": {"message": "Model '...' sedang dalam pemeliharaan: [maintenance_message]", "code": "model_under_maintenance"}}`).
3. **API Key Validity Gate**:
   - Hash token dari header `Authorization: Bearer sk-supa-...`.
   - Jika tidak ditemukan atau `is_active == 0`:
   - Respon: `401 Unauthorized` (`{"error": {"message": "Invalid or revoked API key."}}`).
4. **Key Spending Limit Gate (Privacy Protection)**:
   - Jika `spending_limit_tokens > 0` dan `total_spent_tokens >= spending_limit_tokens`:
   - Respon: `429 Too Many Requests` (`{"error": {"message": "API key limit reached (quota exceeded). Hubungi pemilik API key."}}`).
   - *Catatan privasi:* Saldo akun utama pemilik tidak diekspos ke pengguna key ini.
5. **Owner Wallet Balance Gate**:
   - Jika `users.token_balance <= 0`:
   - Respon: `402 Payment Required` (`{"error": {"message": "Saldo kuota token akun habis. Silakan top-up di dashboard SupaAI."}}`).
6. **Streaming Forwarding & Deduplication**:
   - Teruskan ke upstream provider melalui Chi proxy pipe.
   - Hitung exact prompt & completion tokens saat stream berakhir (`usage` chunk).
   - Eksekusi transaksi SQL atomik:
     ```sql
     BEGIN TRANSACTION;
     UPDATE users 
        SET token_balance = token_balance - ? 
      WHERE id = ? AND token_balance >= ?;
     UPDATE user_api_keys 
        SET total_spent_tokens = total_spent_tokens + ? 
      WHERE id = ?;
     INSERT INTO token_usage_logs (...) VALUES (...);
     COMMIT;
     ```

---

## 5. Integrasi Pembayaran QRIS Tripay

### 5.1 Spesifikasi Endpoint
1. `POST /v1/billing/topup`
   - Headers: `Authorization: Bearer <jwt_user>`
   - Body: `{"package_id": "pkg_standard"}` atau `{"amount_idr": 50000}`
   - Action:
     - Generate `merchant_ref = INV-YYYYMMDD-XXXX`.
     - Request ke API Tripay: `https://tripay.co.id/api/transaction/create` dengan method `QRIS`.
     - Simpan record `UNPAID` di `topup_transactions`.
     - Kembalikan response payload: `{ "merchant_ref", "qr_url", "amount_idr", "tokens_granted", "expired_at" }`.

2. `POST /v1/billing/tripay-webhook`
   - Headers: `X-Callback-Signature: <hmac_sha256_hex>`
   - Payload: Tripay JSON callback
   - Validasi Keamanan:
     - Hitung: `crypto.hmac_sha256(TripayPrivateKey, rawRequestBody)`.
     - Jika signature tidak cocok, return `400 Bad Request`.
     - Jika status == `'PAID'`:
       - Cek idempotency: pastikan status transaksi sebelumnya belum `PAID`.
       - Update status `topup_transactions` ke `PAID`.
       - Kreditkan token ke `users.token_balance`:
         `UPDATE users SET token_balance = token_balance + ? WHERE id = ?`.
       - Return JSON: `{"success": true}`.

3. `GET /v1/billing/transactions/{merchant_ref}`
   - Polling status transaksi untuk mendeteksi pembayaran sukses secara real-time dari frontend.

---

## 6. Desain Antarmuka Dashboard Developer & Admin (Frontend React)

Dashboard SupaAI setelah login mengadopsi DNA visual LAM-Router (font mono, palet warna elegan, header status gateway, dan theme switcher multi-opsi: Default Dark, Light, Dracula, Deus Ex, Monokai, Cyberpunk, Tokyo Night).

### 6.1 Menu Navigasi Pengguna (Developer)
1. **Overview / Home Dashboard**:
   - Kartu KPI: Sisa Kuota Token (`8.450.000 Tokens`), Total Terpakai Hari Ini, Total API Key Aktif.
   - Grafik Penggunaan Token 7 Hari Terakhir (Chart interaktif per model).
2. **API Keys & Limits (`/keys`)**:
   - Tombol "+ Create New API Key".
   - Form modal: Nama Key, Spending Limit Kuota (opsional, contoh: `500.000` token untuk teman).
   - Salin API key aman (`sk-supa-...`) hanya satu kali saat dibuat.
   - Tabel API Keys: Nama, Masked Key (`sk-supa-***`), Progress Bar Kuota (`320.000 / 500.000 Tokens`), Status Switch (Active/Revoke), Tombol Edit Limit / Hapus.
3. **Top-Up Kuota QRIS (`/pricing` / `/topup`)**:
   - Pilihan kartu paket kuota token (Starter 2M Token, Pro 10M Token, Ultimate 50M Token).
   - Tombol instan "Bayar QRIS".
   - Popup Modal QRIS: Menampilkan QR code instan, nominal pas, countdown timer 15 menit, dan auto-detect status sukses tanpa reload halaman.
4. **Model Catalog & Multiplier (`/models`)**:
   - Daftar seluruh model AI yang aktif (`is_allowed == 1`).
   - Badge pengali token (`1.0x Token`, `3.0x Token`, `10.0x Token`).
   - Badge status: `Online` (Hijau) atau `Maintenance` (Kuning / Oranye).
5. **Playground**:
   - Antarmuka chat interaktif langsung di browser untuk menguji model dengan API key user secara real-time.
6. **Usage Logs (`/logs`)**:
   - Tabel audit log pemanggilan: Timestamp, Model, Input Token, Output Token, Multiplier, Total Kuota Terpotong, Latency (ms).

### 6.2 Menu Tambahan Khusus Admin (`role == 'admin'`)
- **Model Governance (`/admin/models`)**:
  - Daftar semua model upstream.
  - Switch `Allowed in Catalog` (On/Off).
  - Switch `Active / Maintenance` (Online/Maintenance).
  - Input field `Multiplier` (dapat diubah langsung secara dinamis).
  - Input field `Maintenance Notice` (pesan kustom saat model sedang down).

---

## 7. Rencana Verifikasi & Uji Kualitas (Testing Strategy)

1. **Unit Testing Go (`backend/`)**:
   - `supa_auth_test.go`: Registrasi, hash password, verifikasi JWT token.
   - `supa_key_test.go`: Key hashing SHA-256, spending limit check, penolakan saat limit terlampaui.
   - `supa_billing_test.go`: Kalkulasi multiplier token bulat, verifikasi signature HMAC-SHA256 webhook Tripay.
   - `supa_models_test.go`: Penolakan model yang `is_allowed == 0` (404) dan `is_active == 0` (503 maintenance).

2. **Frontend Component & Route Testing (`frontend/`)**:
   - Theme Switcher responsif di semua halaman.
   - Modal QRIS menampilkan countdown dan mendeteksi callback `PAID`.
   - Input limit key memvalidasi angka integer kuota token.

3. **End-to-End API Inference Verification**:
   - Test eksekusi curl dengan API key SupaAI memotong token secara presisi sesuai multiplier.

---

## 8. Alur Transisi ke Fase Eksekusi
Setelah dokumen spesifikasi ini ditinjau dan disetujui oleh pengguna, transisi dilakukan langsung menggunakan skill `writing-plans` untuk menyusun rencana implementasi langkah-demi-langkah yang terukur.

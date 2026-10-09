# SupaAI Token Platform Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use software-development:subagent-driven-development (recommended) or software-development:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Membangun platform penjualan token AI komersial SupaAI dalam arsitektur Monorepo (`frontend/` + `backend/`), mengintegrasikan engine inference Go yang dikloning dari LAM-Router dengan sistem multi-tenant, formula multiplier kuota token bulat, API key spending limits, pembayaran QRIS otomatis via Tripay, dan dashboard developer ber-tema LAM-Router.

**Architecture:** Monorepo di `/home/ahlfs/workspace/SupaAI/` dengan `frontend/` (React SPA + Tailwind + Theme Switcher di port 4321) dan `backend/` (Go Chi + SQLite multi-tenant terisolasi di port 9900). Engine backend memverifikasi key `sk-supa-xxx`, mengecek batasan kuota & saldo dompet owner, melakukan streaming proxy ke upstream, dan memotong saldo token secara atomik dengan formula `tokens_deducted = int(token_usage * multiplier)`.

**Tech Stack:** Go 1.24+, Chi v5, SQLite3, React 18, Vite 6, Tailwind CSS, Lucide Icons, Tripay QRIS API, PM2.

**Spec:** `docs/superpowers/specs/2026-10-09-supaai-token-platform-design.md`

## Global Constraints

- Protected Ports: Port 3000, 20128, dan 8900 DILARANG KERAS digunakan atau diganggu.
- Port SupaAI: Backend di Port `9900`, Frontend di Port `4321`.
- Data Directory: Database SQLite backend disimpan di `~/.supa-router/supa.db` (terpisah 100% dari `~/.lam-router/`).
- Formula Pengurangan Token: `deducted_tokens = int(token_usage * multiplier)`.
- Keamanan Webhook: Validasi signature Tripay wajib menggunakan HMAC-SHA256 dengan private key Tripay.
- Hirarki Gate Keamanan: Whitelist Model (`is_allowed`) -> Maintenance Gate (`is_active` -> 503) -> Validitas Key (401) -> Key Spending Limit (429) -> Saldo Owner (402).

---

### Task 1: Monorepo Restructuring & Backend Scaffolding

**Files:**
- Create: `/home/ahlfs/workspace/SupaAI/backend/`
- Modify: `/home/ahlfs/workspace/SupaAI/frontend/` (pindahkan existing frontend files ke `frontend/`)
- Create: `/home/ahlfs/workspace/SupaAI/backend/go.mod`
- Create: `/home/ahlfs/workspace/SupaAI/backend/Makefile`

**Interfaces:**
- Consumes: Existing files di `/home/ahlfs/workspace/SupaAI/` dan clone source `/home/ahlfs/LAM-Router/`
- Produces: Struktur Monorepo yang bersih `SupaAI/frontend` dan `SupaAI/backend` dengan `go.mod` modul `supaapi`.

- [ ] **Step 1: Pindahkan berkas frontend ke folder `frontend/`**

Pindahkan semua kode React/Vite yang sudah ada di root `/home/ahlfs/workspace/SupaAI/` ke subfolder `/home/ahlfs/workspace/SupaAI/frontend/`.

- [ ] **Step 2: Salin engine LAM-Router ke folder `backend/`**

Salin seluruh codebase `/home/ahlfs/LAM-Router/` ke `/home/ahlfs/workspace/SupaAI/backend/` (kecuali folder `.git` agar tetap menjadi bagian dari repositori Git SupaAI).

- [ ] **Step 3: Sesuaikan `go.mod` dan import path di `backend/`**

Ubah deklarasi modul di `backend/go.mod` menjadi:
```go
module supaapi
```
Update batch semua deklarasi import di file Go dari `"lamrouter/` menjadi `"supaapi/`.

- [ ] **Step 4: Uji kompilasi dasar backend**

Jalankan: `cd /home/ahlfs/workspace/SupaAI/backend && go build -o bin/supa-router ./cmd/lamrouter`
Expected: Berhasil compile tanpa error import path.

- [ ] **Step 5: Commit perubahan Task 1**

```bash
cd /home/ahlfs/workspace/SupaAI
git add -A
git commit -m "chore: restructure into monorepo with frontend and backend folders"
```

---

### Task 2: Skema Database SQLite Multi-Tenant (`supa.db`)

**Files:**
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/db/schema.go`
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/db/db.go`
- Test: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/db/db_test.go`

**Interfaces:**
- Consumes: SQLite driver `github.com/mattn/go-sqlite3`
- Produces: `InitSupaDB(dataDir string) (*sql.DB, error)`, fungsi repository untuk query user, keys, models, transaksi.

- [ ] **Step 1: Tulis unit test untuk inisialisasi skema tabel**

Buat file `db_test.go` yang memvalidasi bahwa semua 6 tabel (`users`, `user_api_keys`, `supa_models`, `topup_transactions`, `token_packages`, `token_usage_logs`) berhasil terbuat.

- [ ] **Step 2: Jalankan test untuk memastikan test gagal (TDD)**

Jalankan: `go test -v ./internal/supa/db -run TestInitSupaDB`
Expected: FAIL karena modul belum dibuat.

- [ ] **Step 3: Implementasikan skema DDL di `schema.go` dan `db.go`**

Implementasikan query pembuatan tabel DDL lengkap sesuai rancangan spesifikasi dengan indeks pencarian pada `key_hash`, `email`, dan `merchant_ref`.

- [ ] **Step 4: Jalankan test dan verifikasi sukses**

Jalankan: `go test -v ./internal/supa/db -run TestInitSupaDB`
Expected: PASS.

- [ ] **Step 5: Commit perubahan Task 2**

```bash
git add backend/internal/supa/db/
git commit -m "feat(db): implement multi-tenant SQLite schema for SupaAI"
```

---

### Task 3: Autentikasi Pengguna Multi-Tenant & JWT Engine

**Files:**
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/auth/auth.go`
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/auth/middleware.go`
- Test: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/auth/auth_test.go`

**Interfaces:**
- Consumes: `internal/supa/db`
- Produces:
  • `RegisterUser(email, password, name) (*User, error)`
  • `LoginUser(email, password) (token string, *User, error)`
  • `JWTMiddleware(next http.Handler) http.Handler`

- [ ] **Step 1: Tulis unit test untuk registrasi, login bcrypt, dan JWT**

Validasi registrasi email unik, verifikasi password bcrypt yang benar vs salah, serta signing & parsing token JWT.

- [ ] **Step 2: Jalankan test untuk memverifikasi kegagalan**

Jalankan: `go test -v ./internal/supa/auth -run TestAuthLifecycle`
Expected: FAIL.

- [ ] **Step 3: Implementasikan logika hashing, token issuance, dan handler**

Gunakan `golang.org/x/crypto/bcrypt` untuk hashing password dan standar HMAC-SHA256 untuk token JWT dengan masa berlaku 7 hari.

- [ ] **Step 4: Jalankan test dan pastikan lulus**

Jalankan: `go test -v ./internal/supa/auth -run TestAuthLifecycle`
Expected: PASS.

- [ ] **Step 5: Commit perubahan Task 3**

```bash
git add backend/internal/supa/auth/
git commit -m "feat(auth): add multi-tenant registration, bcrypt login, and JWT middleware"
```

---

### Task 4: Manajemen Multi-Key Pengguna & Spending Limits

**Files:**
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/keys/keys.go`
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/keys/handlers.go`
- Test: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/keys/keys_test.go`

**Interfaces:**
- Consumes: `internal/supa/db`, `internal/supa/auth`
- Produces:
  • `CreateAPIKey(userID, name string, spendingLimitTokens int64) (rawKey string, *APIKey, error)`
  • `ValidateAPIKey(rawKey string) (*APIKey, *User, error)`
  • `RevokeAPIKey(userID, keyID string) error`
  • `UpdateSpendingLimit(userID, keyID string, limit int64) error`

- [ ] **Step 1: Tulis unit test pembuatan key, validasi hash SHA-256, dan limit enforcement**

Test memastikan raw key yang di-generate berformat `sk-supa-...`, hanya disimpan dalam bentuk SHA-256 hash, dan jika pemakaian melebihi `spending_limit_tokens`, validasi menolak dengan error spesifik.

- [ ] **Step 2: Jalankan test untuk verifikasi kegagalan**

Jalankan: `go test -v ./internal/supa/keys -run TestAPIKeyLifecycle`
Expected: FAIL.

- [ ] **Step 3: Implementasikan logika generator key aman (crypto/rand) dan query hash**

Implementasikan endpoint:
- `POST /v1/user/keys` (Create key)
- `GET /v1/user/keys` (List keys user)
- `DELETE /v1/user/keys/{id}` (Revoke key)
- `PATCH /v1/user/keys/{id}` (Update limit/name)

- [ ] **Step 4: Jalankan test dan pastikan lulus**

Jalankan: `go test -v ./internal/supa/keys -run TestAPIKeyLifecycle`
Expected: PASS.

- [ ] **Step 5: Commit perubahan Task 4**

```bash
git add backend/internal/supa/keys/
git commit -m "feat(keys): implement user multi-key management with per-key spending limits"
```

---

### Task 5: Model Governance, Multiplier Registry, dan Maintenance Mode

**Files:**
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/models/models.go`
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/models/handlers.go`
- Test: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/models/models_test.go`

**Interfaces:**
- Consumes: `internal/supa/db`
- Produces:
  • `GetAllowedModels() ([]SupaModel, error)`
  • `CheckModelAccess(modelID string) (*SupaModel, error)`
  • `UpdateModelSettings(modelID string, multiplier float64, isAllowed, isActive bool, notice string) error`

- [ ] **Step 1: Tulis unit test untuk whitelist `is_allowed`, maintenance `is_active`, dan multiplier**

Test memverifikasi model non-allowed return 404, model maintenance return 503, dan model aktif mengembalikan nilai multiplier yang benar.

- [ ] **Step 2: Jalankan test untuk verifikasi kegagalan**

Jalankan: `go test -v ./internal/supa/models -run TestModelGovernance`
Expected: FAIL.

- [ ] **Step 3: Implementasikan model catalog handlers**

Pasang seed default untuk model populer (DeepSeek V3 1.0x, Gemini 2.5 Flash 1.0x, GPT-4o-mini 2.0x, Claude 3.5 Sonnet 10.0x) serta handler publik `GET /v1/models` dan handler admin `PUT /v1/admin/models/{id}`.

- [ ] **Step 4: Jalankan test dan pastikan lulus**

Jalankan: `go test -v ./internal/supa/models -run TestModelGovernance`
Expected: PASS.

- [ ] **Step 5: Commit perubahan Task 5**

```bash
git add backend/internal/supa/models/
git commit -m "feat(models): add allowed model whitelist, maintenance toggle, and multiplier registry"
```

---

### Task 6: Inference Pipeline & Atomic Token Deduction Engine

**Files:**
- Modify: `/home/ahlfs/workspace/SupaAI/backend/internal/proxy/chat.go`
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/billing/deduction.go`
- Test: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/billing/deduction_test.go`

**Interfaces:**
- Consumes: `internal/supa/keys`, `internal/supa/models`, `internal/supa/db`
- Produces: Middleware & hook pemotongan kuota token atomik setelah inferensi selesai.

- [ ] **Step 1: Tulis unit test untuk formula deduksi kuota bulat**

Test memastikan `deducted_tokens = int(token_usage * multiplier)` didebit secara atomik tanpa saldo minus.

- [ ] **Step 2: Jalankan test untuk verifikasi kegagalan**

Jalankan: `go test -v ./internal/supa/billing -run TestTokenDeduction`
Expected: FAIL.

- [ ] **Step 3: Integrasikan middleware gate ke pipeline `POST /v1/chat/completions`**

Pasang urutan gate:
1. Validasi model (`is_allowed` -> `is_active`)
2. Validasi key developer (`sk-supa-...`)
3. Validasi spending limit key
4. Validasi saldo akun owner
5. Eksekusi proxy streaming
6. Eksekusi pemotongan atomik saldo saat stream selesai

- [ ] **Step 4: Jalankan test dan pastikan lulus**

Jalankan: `go test -v ./internal/supa/billing -run TestTokenDeduction`
Expected: PASS.

- [ ] **Step 5: Commit perubahan Task 6**

```bash
git add backend/internal/supa/billing/ backend/internal/proxy/
git commit -m "feat(inference): wire atomic integer token deduction with multiplier formula"
```

---

### Task 7: Integrasi Pembayaran QRIS Tripay & Webhook HMAC-SHA256

**Files:**
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/billing/tripay.go`
- Create: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/billing/webhook.go`
- Test: `/home/ahlfs/workspace/SupaAI/backend/internal/supa/billing/tripay_test.go`

**Interfaces:**
- Consumes: Tripay API configuration (`TRIPAY_API_KEY`, `TRIPAY_PRIVATE_KEY`, `TRIPAY_MERCHANT_CODE`, `TRIPAY_SANDBOX`)
- Produces:
  • `CreateQRISTransaction(userID string, pkgID string) (*TopupTransaction, error)`
  • `HandleTripayWebhook(signature string, body []byte) error`
  • `GetTransactionStatus(merchantRef string) (*TopupTransaction, error)`

- [ ] **Step 1: Tulis unit test verifikasi signature webhook HMAC-SHA256 dan idempotency**

Test memastikan callback palsu ditolak, callback asli berstatus `PAID` menambah token saldo user, dan callback duplikat tidak menambah token dua kali.

- [ ] **Step 2: Jalankan test untuk verifikasi kegagalan**

Jalankan: `go test -v ./internal/supa/billing -run TestTripayWebhook`
Expected: FAIL.

- [ ] **Step 3: Implementasikan client Tripay dan webhook validator**

Implementasikan pemanggilan API create transaction QRIS, verifikasi signature header `X-Callback-Signature`, dan query penambahan token user.

- [ ] **Step 4: Jalankan test dan pastikan lulus**

Jalankan: `go test -v ./internal/supa/billing -run TestTripayWebhook`
Expected: PASS.

- [ ] **Step 5: Commit perubahan Task 7**

```bash
git add backend/internal/supa/billing/
git commit -m "feat(billing): integrate Tripay QRIS top-up and HMAC-SHA256 webhook validator"
```

---

### Task 8: Frontend Dashboard Integration (DNA Visual & Fitur LAM-Router)

**Files:**
- Modify: `/home/ahlfs/workspace/SupaAI/frontend/src/App.jsx`
- Create: `/home/ahlfs/workspace/SupaAI/frontend/src/context/ThemeContext.jsx` (Dracula, Deus Ex, Tokyo Night, dll)
- Create: `/home/ahlfs/workspace/SupaAI/frontend/src/pages/KeysPage.jsx`
- Create: `/home/ahlfs/workspace/SupaAI/frontend/src/components/QRISModal.jsx`
- Modify: `/home/ahlfs/workspace/SupaAI/frontend/src/pages/ModelsPage.jsx`
- Modify: `/home/ahlfs/workspace/SupaAI/frontend/src/pages/PricingPage.jsx`

**Interfaces:**
- Consumes: Backend API endpoints di `http://127.0.0.1:9900/v1`
- Produces: Antarmuka web developer lengkap dengan visual monokrom sleek LAM-Router, theme switcher, modal QRIS, dan manajemen key ber-limit.

- [ ] **Step 1: Pasang ThemeContext dengan palet warna lengkap LAM-Router**

Implementasikan theme selector (Default Dark, Light, Dracula, Deus Ex, Tokyo Night, Monokai, Cyberpunk) yang tersimpan di `localStorage`.

- [ ] **Step 2: Buat halaman `KeysPage.jsx` dengan fitur spending limit**

Tampilkan daftar key, input modal "Create Key" dengan field spending limit tokens, progress bar pemakaian token, dan aksi revoke.

- [ ] **Step 3: Buat komponen `QRISModal.jsx` untuk pembayaran top-up instan**

Tampilkan QR code instan dari Tripay, nominal pas Rupiah, countdown timer, dan polling otomatis setiap 3 detik yang langsung memperbarui saldo saat status `PAID`.

- [ ] **Step 4: Perbarui `ModelsPage.jsx` dengan badge Multiplier & Maintenance Notice**

Tampilkan badge pengali token (`1.0x`, `3.0x`, `10.0x`) dan status Online vs Maintenance badge (kuning).

- [ ] **Step 5: Build frontend dan verifikasi sintaks**

Jalankan: `cd /home/ahlfs/workspace/SupaAI/frontend && pnpm run build`
Expected: Build sukses menghasilkan folder `dist/` tanpa error compiler.

- [ ] **Step 6: Commit perubahan Task 8**

```bash
git add frontend/
git commit -m "feat(frontend): implement LAM-Router visual dashboard, theme switcher, QRIS modal, and keys management"
```

---

### Task 9: End-to-End Testing & PM2 Service Deployment

**Files:**
- Create: `/home/ahlfs/workspace/SupaAI/ecosystem.config.cjs`
- Modify: `/home/ahlfs/workspace/SupaAI/README.md`

**Interfaces:**
- Consumes: Frontend build (`frontend/dist`) dan binary backend (`backend/bin/supa-router`)
- Produces: Service aktif di PM2 (`supa-backend` port 9900 dan `supa-ai` port 4321).

- [ ] **Step 1: Buat konfigurasi PM2 `ecosystem.config.cjs`**

Konfigurasikan dua aplikasi di PM2:
1. `supa-backend`: menjalankan `backend/bin/supa-router` pada `PORT=9900` dengan `DATA_DIR=/home/ahlfs/.supa-router`.
2. `supa-ai`: menjalankan frontend server pada `PORT=4321`.

- [ ] **Step 2: Jalankan backend dan frontend di PM2**

Jalankan: `pm2 start ecosystem.config.cjs`
Verifikasi status: `pm2 list` (kedua service berstatus online).

- [ ] **Step 3: Uji coba end-to-end via cURL**

1. Register user baru -> dapat JWT token.
2. Buat API key baru -> dapat `sk-supa-test123`.
3. Simulasi top-up token kuota.
4. Panggil `POST /v1/chat/completions` menggunakan key `sk-supa-test123`.
5. Verifikasi saldo token terpotong secara tepat sesuai multiplier model.

- [ ] **Step 4: Commit dan push perubahan akhir ke GitHub**

```bash
git add -A
git commit -m "feat: complete end-to-end SupaAI token platform setup and deployment"
git push origin main
```

# Kebijakan Rilis LAM-Router

## Aturan utama

Nomor versi baru dan tag rilis dibuat **hanya atas instruksi eksplisit pemilik repo**.
Di antara instruksi itu, `main` adalah area akumulasi: commit fitur dan perbaikan
berjalan seperti biasa, tanpa bump versi dan tanpa tag.

Alasannya: nomor rilis adalah identitas artefak yang sudah terbit, bukan hitungan
commit. Menahan bump tidak menimbulkan risiko apa pun, karena `scripts/build-npm.sh`
hanya mau mempublish kalau HEAD persis berada di tag rilis yang cocok dengan versi
**dan** working tree bersih (`git describe --tags --exact-match`). `ALLOW_UNTAGGED=1`
hanya untuk dry-run lokal, dan `--publish` tetap ditolak dalam mode itu. Jadi keadaan
tanpa tag mustahil terbit tanpa sengaja.

## Memilih nomor (saat diminta rilis)

Nomor ditentukan oleh perubahan **terberat** dalam tumpukan sejak rilis terakhir,
bukan jumlah commit:

| Isi tumpukan | Nomor |
| --- | --- |
| hanya perbaikan | patch, mis. 2.0.3 |
| ada fitur baru | minor, mis. 2.1.0 |
| ada perubahan yang memutus kompatibilitas | major, mis. 3.0.0 |

## Langkah rilis (hanya setelah instruksi eksplisit)

1. Pastikan suite hijau: `go test ./... -count=1`
2. Bump `npm/package.json`; ubah `## [Unreleased]` di `CHANGELOG.md` menjadi
   `## [vX.Y.Z] — <tanggal>`
3. Commit rilis, lalu `git tag vX.Y.Z` dan push tag (push juga butuh izin eksplisit)
4. CI yang menerbitkan; verifikasi dengan `bash scripts/build-npm.sh --verify X.Y.Z`

## Versi pada build di antara dua rilis

Build tanpa tag sudah menandai dirinya sendiri: Makefile memakai
`git describe --tags --always` (Makefile:5) untuk mengisi
`-X lamrouter/internal/updater.CurrentVersion`, jadi build sekarang melapor
`2.0.2-2-g09be039` - versi jujur dan bisa dilacak ke commit.

Aman terhadap pemeriksa update: `internal/updater.parseSemver` hanya membaca
`MAJOR.MINOR.PATCH` lewat `fmt.Sscanf("%d.%d.%d")` dan mengabaikan sufiks, sehingga
build dev tidak dianggap lebih baru daripada rilis 2.0.2 dan tidak memicu notifikasi
"update tersedia" palsu.

## Keadaan 2.0.2: jangan dilengkapi

Empat paket platform `2.0.2` sudah terbit (linux-x64, linux-arm64, darwin-x64,
darwin-arm64) dan berisi biner dari tag `v2.0.2` - yaitu SEBELUM kerja yang sekarang
tertahan. Paket root `lam-router@2.0.2` belum pernah terbit, sehingga dist-tag
`latest` masih 2.0.1.

Konsekuensi: nomor yang sudah terbit tidak boleh diisi ulang dengan isi berbeda, jadi
kerja yang tertahan tidak boleh terbit sebagai 2.0.2 - ia terbit sebagai nomor
setelahnya (ada fitur, jadi 2.1.0). Paket platform 2.0.2 yang menggantung tidak
berbahaya: hanya dijangkau lewat `optionalDependencies` paket root yang tak pernah ada.

## Yang tidak boleh ditahan

Kebijakan menahan bump bukan alasan menahan perbaikan. Kalau ada cacat yang merugikan
pengguna pada versi yang sudah terbit, laporkan ke pemilik repo berikut usulan rilis
perbaikan (patch) - keputusan tetap di pemilik repo.

## Lingkungan

- Rilis npm adalah satu-satunya jalur distribusi; VPS diperbarui hanya atas instruksi
  eksplisit (`bash ~/lam-router-backups/update-lam-router.sh` di VPS).
- Pengembangan dan pengujian selalu lokal dulu (`/home/ahlfs/LAM-Router`).

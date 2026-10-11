# Konektor Otomasi On-Premise (AC · Lampu · Pintu)

Backend room booking memanggil API on-prem UNESA:

| Kapan | Panggilan | Syarat |
|---|---|---|
| 15 menit sebelum kelas | `POST /api/v1/room-booking/prestart` | booking `confirmed`, belum check-in, kamar terdaftar |
| Tombol **Geser Checkout** di Philips ditekan | `POST /api/v1/room-booking/checkout` | `source = kiosk_button`, kamar terdaftar |

Konektor **tidak pernah mengirim apa pun** kecuali `AUTOMATION_MODE=live`.

## Perilaku (sesuai kontrak tim on-prem)

- **T-15**: dikirim saat sisa waktu 13–15 menit (toleransi mereka 13–17). Status booking dibaca
  langsung dari database saat itu juga → kelas yang **dibatalkan** tidak dikirim, kelas yang
  **dipindah** dikirim dengan jam barunya (eventId berbeda). Bila lewat T-13 (server mati /
  booking dibuat terlambat) dicatat `missed` dan **tidak** dikirim.
- **Checkout**: `nextBookingStartsAt` dihitung dari jadwal berikutnya ruang itu (`null` bila
  tidak ada dalam 14 hari). Kelas yang sedang berjalan dilaporkan sebagai "sekarang" sehingga
  on-prem menolak OFF. Jawaban **409** dicatat `rejected_409` dan **tidak** diulang
  (AC/lampu memang harus tetap menyala).
- Checkout otomatis sistem (`kiosk_auto`), checkout admin, dan ruang di luar daftar
  **tidak** mematikan ruangan. Ubah lewat `AUTOMATION_CHECKOUT_SOURCES` bila diperlukan.
- Checkout lebih tua dari **15 menit** tidak dieksekusi (`expired`); saat pertama kali aktif,
  checkout lama diabaikan.
- Gangguan sementara (timeout/5xx) diulang hingga 8 kali dengan jeda 20 dtk → 5 mnt.
  401/403/422 dianggap kesalahan konfigurasi dan tidak diulang.
- `eventId` unik dan stabil: `evt-<bookingId>-prestart-<yyyyMMddHHmm>` dan
  `evt-<bookingId>-checkout` (aman dikirim ulang).
- HTTP **202** = perintah diserahkan ke adapter, **bukan** bukti AC mati / pintu terkunci.

## Konfigurasi (`.env` backend)

```
AUTOMATION_MODE=off            # off | dry_run | live
AUTOMATION_BASE_URL=http://<alamat-mini-pc>:5001
AUTOMATION_HMAC_SECRET=<sama dengan ROOM_BOOKING_HMAC_SECRET di on-prem, >= 32 karakter>
AUTOMATION_ROOM_CODES=Q1.05.03,Q1.05.05,Q1.05.16,Q1.06.15,Q1.06.05
AUTOMATION_CHECKOUT_SOURCES=kiosk_button
```
lalu `docker compose up -d --build api`.

- **`dry_run`**: semua keputusan dicatat dan payload ditulis ke log, **tidak ada** yang dikirim.
  Gunakan ini dulu untuk melihat kapan perintah akan keluar.
- **`live`**: hanya setelah tim on-prem menyatakan adapter, AC/lampu/pintu per kelas, tombol
  keluar, dan alarm kebakaran **sudah diuji** serta `ROOM_BOOKING_CONTROL_ENABLED=true` di sisi mereka.

## Persyaratan jaringan (penting)

Alamat contoh `192.168.137.10` adalah IP **LAN pribadi**. Server VPS tidak bisa menjangkaunya
langsung. Pilih salah satu:
1. **VPN** (WireGuard/Tailscale) antara VPS dan mini PC; pakai IP VPN sebagai `AUTOMATION_BASE_URL`
   dan daftarkan IP VPN VPS di `ROOM_BOOKING_ALLOWED_IPS` on-prem.
2. Endpoint on-prem dipublikasikan lewat **HTTPS + reverse proxy** dengan whitelist IP VPS
   (`103.27.207.58`) — jangan membuka port kontrol tanpa whitelist dan HMAC.

## Kode ruang

On-prem hanya menerima: **Q1.05.03, Q1.05.05, Q1.05.16, Q1.06.15, Q1.06.05**. Nama ruangan
di web admin harus **persis sama**. Ruang yang tidak ada di daftar (mis. `Q1.01.03`) diabaikan
konektor dan tidak pernah dikirim.

## Memantau

Admin: `GET /api/admin/automation/events` (JWT admin) → 100 percobaan terakhir:
`prestart|checkout`, `sent`, `retry`, `rejected_409`, `missed`, `skipped`, `expired`, `failed`,
`dry_run`, beserta kode HTTP dan catatan. Tabel: `automation_events`.

## Daftar periksa sebelum `live`

- [ ] Lima ruang dibuat di web admin dengan kode persis di atas
- [ ] Jadwal kuliah kelima ruang sudah diimpor (Import Jadwal)
- [ ] Jaringan VPS ↔ mini PC tersambung (VPN/HTTPS) dan IP VPS ada di allow-list on-prem
- [ ] Secret HMAC sama di kedua sisi (>= 32 karakter), tidak di Git
- [ ] `dry_run` dijalankan ≥ 1 hari: waktu T-15 sesuai, tidak ada `missed` tak terduga
- [ ] Tim on-prem mengonfirmasi uji ON/OFF per kelas, tombol keluar, alarm kebakaran
- [ ] Ada orang di lokasi saat `live` pertama kali dinyalakan

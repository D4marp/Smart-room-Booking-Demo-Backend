# Konektor Otomasi On-Premise (AC · Lampu · Pintu)

Backend room booking memanggil API on-prem UNESA:

| Kapan | Panggilan | Syarat |
|---|---|---|
| 15 menit sebelum kelas | `POST /api/v1/room-booking/prestart` | booking `confirmed`, belum check-in, kamar terdaftar |
| Tombol **Geser Checkout** di Philips ditekan | `POST /api/v1/room-booking/checkout` | `source = kiosk_button`, kamar terdaftar |

Konektor **tidak pernah mengirim apa pun** kecuali `AUTOMATION_MODE=live`. Ada juga mode **`pull`** (bagian paling bawah): Node-RED yang menarik perintah lewat domain, SRS tidak mengirim apa pun ke on-prem.

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
AUTOMATION_ROOM_CODES=Q1.01.03        # tambah ruang lain setelah gateway-nya diverifikasi
AUTOMATION_REQUIRED_DEVICES=ac,lamp   # perangkat yang wajib dilaporkan di ack
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


---

# Mode `pull` — Node-RED menarik perintah lewat domain

Cocok bila on-prem tidak bisa dijangkau dari VPS: **Node-RED yang menghubungi SRS** (HTTPS keluar
ke port 443), jadi tidak perlu VPN, IP publik, atau membuka port di jaringan on-prem.

```
SRS (VPS)                                        On-prem
 jadwal + checkout tablet                        Node-RED
 → antrean perintah ON/OFF  ◄── GET commands ── (tiap 10 dtk)
                            ◄── POST ack ─────── (setelah AC/lampu/pintu dikirim)
```

## Endpoint (kunci API terpisah: `X-API-Key` = `AUTOMATION_PULL_API_KEYS`)

`GET /api/integration/v1/automation/commands?room=Q1.05.03&limit=50` — perintah yang **berlaku saat ini**:
```json
{"success":true,"data":{"serverTime":"2026-10-20T06:48:00+07:00","commands":[{
  "commandId":"evt-<bookingId>-prestart-202610200700","eventId":"evt-<bookingId>-prestart-202610200700",
  "action":"on","reason":"prestart","roomCode":"Q1.05.03","classKey":"q10503",
  "bookingId":"<bookingId>","source":"room_booking",
  "startsAt":"2026-10-20T07:00:00+07:00","nextBookingStartsAt":null,
  "validFrom":"2026-10-20T06:45:00+07:00","expiresAt":"2026-10-20T07:10:00+07:00","attempts":0}]}}
```
Nama field (`roomCode`, `classKey`, `action`, `eventId`, `bookingId`, `source`) sama dengan payload adapter
on-prem, jadi alur kontrol yang sudah ada bisa dipakai ulang.

`POST /api/integration/v1/automation/commands/{commandId}/ack`
```json
{"success":true,"devices":{"ac":{"state":"command_sent"},"lamp":{"state":"command_sent"},"door":{"state":"command_sent"}},"message":""}
```
- `success:true` hanya dianggap berhasil bila **semua perangkat wajib** berstatus `command_sent`. Bawaan: **`ac` dan `lamp`**
  (pintu tidak dikendalikan oleh integrasi backend Mini PC). Atur lewat `AUTOMATION_REQUIRED_DEVICES` (mis. `ac,lamp,door`).
  Field perangkat tambahan (mis. `door`) diterima dan diabaikan.
- `success:true` tetapi ada perangkat wajib yang tidak dilaporkan → **HTTP 422** (`ack rejected: devices not reported as
  command_sent: ...`); ack **tidak** diterima, perintah tetap di antrean, dan percobaan tidak dihitung. Pemanggil harus
  memeriksa kode HTTP, bukan hanya mengira "200 = berhasil".
- `success:false` (gagal) → perintah tetap di antrean dan ditawarkan lagi (maks 5 kali, atau sampai kedaluwarsa).
- Ack ulang pada perintah yang sudah `acked` aman (idempoten). Perintah tak dikenal → `404`.
- `command_sent` = perintah diserahkan, **bukan** bukti AC mati / pintu terkunci.

## Aturan keselamatan di sisi SRS

- **ON**: dibuat saat sisa waktu ≤ 15 menit sebelum kelas, berlaku sampai 10 menit setelah jam mulai.
  Dicek ulang **setiap kali** Node-RED menarik: kelas dibatalkan / dipindah / sudah checkout → perintah
  hilang (tidak pernah diberikan).
- **OFF**: hanya dari tombol **Geser Checkout** di tablet (`kiosk_button`), berlaku 15 menit. Bila kelas berikutnya
  sudah berjalan atau mulai ≤ 15 menit, OFF **tidak dibuat / ditarik** (aturan yang sama dengan API on-prem).
- Perintah kedaluwarsa tidak pernah ditawarkan. Bila Node-RED mati lama, perintah lama hangus sendiri.
- Tidak ada endpoint untuk **membuat** perintah: perintah hanya lahir dari booking `confirmed` dan checkout nyata.
- Kunci `AUTOMATION_PULL_API_KEYS` terpisah dari `INTEGRATION_API_KEYS` (kunci baca-jadwal tidak bisa ack/menarik perintah).
- Interlock alarm kebakaran, tombol keluar, dan "pintu kembali ke mode normal" tetap tanggung jawab alur kontrol on-prem.

## Mengaktifkan

`.env` backend:
```
AUTOMATION_MODE=pull
AUTOMATION_PULL_API_KEYS=<openssl rand -hex 32>
AUTOMATION_ROOM_CODES=Q1.01.03        # tambah ruang lain setelah gateway-nya diverifikasi
AUTOMATION_REQUIRED_DEVICES=ac,lamp   # perangkat yang wajib dilaporkan di ack
```
lalu `docker compose up -d api`. Kunci hanya diberikan ke Node-RED (variabel lingkungan `SRS_PULL_KEY`, bukan di
dashboard/browser).

## Node-RED

Impor `docs/nodered-srs-pull-flow.json` (Menu → Import). Alurnya: tiap 10 detik `GET commands` → satu pesan per
perintah → penahan dobel (60 dtk) → **link out** ke alur kontrol AC/lampu/pintu Anda → **link in** hasil → `POST ack`.
Hubungkan link out/in ke subflow kontrol yang sudah ada; subflow itu harus mengisi `msg.ack`:
`{ success: true, devices: { ac: {state:'command_sent'}, lamp: {...}, door: {...} }, message: '' }`.
Sebelum menjalankan perintah, **cek `expiresAt`** (jangan eksekusi perintah yang sudah lewat).

Uji tanpa Node-RED:
```bash
KEY=<kunci pull>; H=https://unesaroombooking.mekansm.co.id/api/integration/v1/automation
curl -s -H "X-API-Key: $KEY" "$H/commands"                                       # daftar perintah aktif
curl -s -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"success":true,"devices":{"ac":"command_sent","lamp":"command_sent","door":"command_sent"}}' \
  "$H/commands/<commandId>/ack"
```
Pantau dari sisi SRS: `GET /api/admin/automation/events` (status `queued` → `acked` / `failed` / `expired` /
`cancelled` / `rejected_409`).

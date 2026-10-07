# Integration API — SRS UNESA Room Booking

Dokumen untuk tim backend otomasi gedung (pintu, AC, lampu). API ini **hanya
membaca jadwal dan menerima kabar perubahan**. Tidak ada endpoint yang bisa
menggerakkan perangkat — perintah kontrol tetap di backend Anda.

```
 Web admin / Tablet Philips ──► Backend Room Booking ──► [Integration API]
                                        │                     │  GET  /schedules   (baca jadwal)
                                        │                     │  GET  /events      (polling perubahan)
                                        └── webhook (opsional)┘  POST ke URL Anda  (push perubahan)
                                                                       │
                                              Backend otomasi Anda ◄───┘
                                              T-15: pintu + AC/lampu ON
                                              checkout: AC/lampu OFF, pintu normal
```

- **Base URL:** `https://unesaroombooking.mekansm.co.id`
- **Semua waktu = WIB (`+07:00`)**, format ISO 8601 / RFC 3339.
- **Format respons:** `{"success": true, "data": { ... }}`; galat: `{"success": false, "error": "..."}`.

---

## 1. Autentikasi antar-backend

Setiap request ke `/api/integration/v1/*` wajib membawa header:

```
X-API-Key: <kunci yang diberikan admin>
```

| Kondisi | Respons |
|---|---|
| Kunci benar | `200` |
| Kunci salah / tidak ada | `401` |
| Server belum dikonfigurasi kunci | `503` (API mati, bukan terbuka) |

- Kunci disimpan di server sebagai `INTEGRATION_API_KEYS` (boleh lebih dari satu,
  dipisah koma → bisa rotasi tanpa downtime: tambah kunci baru, ganti di sisi Anda,
  hapus kunci lama).
- Pakai kunci acak ≥ 32 karakter, contoh: `openssl rand -hex 32`.
- **Wajib HTTPS.** Jangan taruh kunci di aplikasi mobile/browser.
- Kunci ini terpisah dari login admin/JWT dan hanya membuka endpoint integrasi.

## 2. Kode ruang

`roomCode` = **nama ruangan** di sistem booking (contoh `Q1.01.03`). Kode bersifat
tetap dan unik; jangan diubah setelah dipakai integrasi. Daftar resmi:

```
GET /api/integration/v1/rooms
```
```json
{"success":true,"data":{"serverTime":"2026-10-07T09:00:00+07:00","rooms":[
  {"roomId":"c0e21a79-…","code":"Q1.01.03","location":"Gedung Fakultas Psikologi UNESA","capacity":40,"available":true}
]}}
```

> Saat ini hanya **Q1.01.03** yang terdaftar. Empat ruang lainnya akan muncul di
> daftar ini setelah ditambahkan admin dengan kode final — gunakan endpoint ini
> sebagai sumber kebenaran, jangan di-hardcode.

## 3. Baca jadwal

```
GET /api/integration/v1/schedules
```

| Query | Keterangan | Default |
|---|---|---|
| `room` | kode ruang, mis. `Q1.01.03` | semua ruang |
| `date` | satu hari `YYYY-MM-DD` | — |
| `from`, `to` | rentang `YYYY-MM-DD` (maks 62 hari) | hari ini s.d. besok |
| `status` | `confirmed`, `all`, atau daftar `confirmed,pending` | `confirmed` |
| `updatedSince` | epoch ms; hanya yang berubah sejak itu | — |

Satu jadwal:
```
GET /api/integration/v1/schedules/{bookingId}
```

Contoh:
```bash
curl -H "X-API-Key: $KEY" \
  "https://unesaroombooking.mekansm.co.id/api/integration/v1/schedules?room=Q1.01.03&date=2026-10-07"
```
```json
{"success":true,"data":{"serverTime":"2026-10-07T09:00:00+07:00","from":"2026-10-07","to":"2026-10-07","schedules":[{
  "bookingId":"7f1c…","roomId":"c0e2…","roomCode":"Q1.01.03","status":"confirmed",
  "date":"2026-10-07","startTime":"07:00","endTime":"10:20",
  "startAt":"2026-10-07T07:00:00+07:00","endAt":"2026-10-07T10:20:00+07:00",
  "course":"Observasi dan Wawancara","classCode":"2025L",
  "lecturers":["Onny Fransinata Anggara, S.Psi., M.Psi., Psikolog"],"participants":41,
  "checkedInAt":null,"checkedOutAt":null,"updatedAt":1790000000000}]}}
```

**Status booking:** `pending` (menunggu approval) · `confirmed` (jadwal sah →
**ini yang dipakai untuk T-15**) · `rejected` · `cancelled` · `completed` (sudah selesai/checkout).
Kunci utama yang stabil: **`bookingId`**.

## 4. Kabar perubahan

Ada dua cara; boleh dipakai bersamaan (disarankan: webhook + polling sebagai cadangan).

### 4a. Polling — `GET /api/integration/v1/events`

| Query | Keterangan |
|---|---|
| `afterId` | kursor: id event terakhir yang sudah Anda proses (mulai `0`) |
| `limit` | 1–500 (default 100) |
| `room` | filter kode ruang |
| `type` | filter, boleh banyak: `booking.cancelled,booking.rescheduled` |

Event diurutkan **lama → baru**. Simpan `nextAfterId` dan pakai di panggilan
berikutnya (disarankan tiap 10–30 detik).

```json
{"success":true,"data":{"serverTime":"…","nextAfterId":412,"events":[
  {"id":411,"type":"booking.rescheduled","occurredAt":"2026-10-07T08:12:03+07:00",
   "roomCode":"Q1.01.03","bookingId":"7f1c…",
   "data":{"booking":{ …snapshot terbaru, bentuk sama seperti /schedules… },
           "previous":{"startTime":"07:00","endTime":"10:20"}}},
  {"id":412,"type":"booking.checked_out","occurredAt":"2026-10-07T09:58:40+07:00",
   "roomCode":"Q1.01.03","bookingId":"7f1c…",
   "data":{"source":"kiosk_button","booking":{ …"status":"completed","checkedOutAt":"2026-10-07T09:58:00+07:00"… }}}]}}
```

### 4b. Webhook (push)

Admin mengisi `INTEGRATION_WEBHOOK_URL` dan `INTEGRATION_WEBHOOK_SECRET`. Setiap event
dikirim `POST` JSON ke URL Anda:

```json
{"id":412,"type":"booking.checked_out","occurredAt":"…","roomCode":"Q1.01.03","bookingId":"7f1c…","data":{…}}
```

Header: `X-Event-Id`, `X-Event-Type`, `X-Timestamp` (detik epoch), `X-Signature`.

- Balas **2xx** = diterima. Selain itu / timeout 10 dtk = dicoba ulang dengan
  jeda 5 dtk → 10 dtk → … maksimum 10 menit (hingga 50 percobaan).
- **At-least-once**: event bisa datang dua kali → buang duplikat berdasarkan `id`.
- Urutan dijaga **per booking** (event berikutnya untuk booking yang sama menunggu
  yang gagal), antar-booking tidak dijamin.
- **Verifikasi tanda tangan** (wajib): `X-Signature = "sha256=" + hex(HMAC_SHA256(secret, X-Timestamp + "." + bodyMentah))`.
  Tolak jika tanda tangan salah atau `X-Timestamp` selisih > 5 menit.

```python
# Python
import hmac, hashlib, time
def valid(secret, ts, body: bytes, sig: str) -> bool:
    if abs(time.time() - int(ts)) > 300: return False
    mac = hmac.new(secret.encode(), ts.encode() + b"." + body, hashlib.sha256).hexdigest()
    return hmac.compare_digest("sha256=" + mac, sig)
```
```js
// Node.js
const crypto = require('crypto');
const valid = (secret, ts, body, sig) => {
  if (Math.abs(Date.now() / 1000 - Number(ts)) > 300) return false;
  const exp = Buffer.from('sha256=' + crypto.createHmac('sha256', secret).update(ts + '.').update(body).digest('hex'));
  const got = Buffer.from(sig || '');
  return exp.length === got.length && crypto.timingSafeEqual(exp, got);
};
```

### Jenis event

| `type` | Kapan | Isi `data` |
|---|---|---|
| `booking.created` | booking baru dibuat (status `pending`) | `booking` |
| `booking.confirmed` | admin menyetujui | `booking` |
| `booking.rejected` | admin menolak | `booking`, `reason` |
| `booking.cancelled` | **dibatalkan** | `booking`, `previousStatus` |
| `booking.rescheduled` | **jam mulai/selesai dipindah** | `booking` (baru), `previous.startTime/endTime` |
| `booking.updated` | dosen/mata kuliah berubah (jam tetap) | `booking` |
| `booking.checked_in` | check-in (tablet/auto) | `booking`, `source` |
| `booking.checked_out` | **checkout** | `booking`, `source` |
| `booking.completed` | admin menandai selesai | `booking`, `source` |
| `schedule.replaced` | jadwal kuliah satu semester diimpor ulang | `roomCode`, `from`, `to`, `bookingsCreated`, `bookingsDeleted` |

`source` pada checkout/check-in: `kiosk_button` (tombol geser di tablet Philips) ·
`kiosk_auto` (otomatis lewat 30 menit dari jam selesai) · `admin` · `user` · `auto`.

> **`schedule.replaced`**: jadwal massal tidak dikirim per booking. Saat event ini
> datang, ambil ulang `/schedules?room=…&from=…&to=…` dan ganti jadwal Anda untuk
> rentang itu.

## 5. Panduan alur

### T-15 (pintu unlock + AC/lampu ON)
1. Sinkron awal & berkala (mis. tiap 5 menit): `GET /schedules?status=confirmed&from=hari ini&to=besok`
   untuk membangun/rekonsiliasi daftar timer. Cadangan bila ada event terlewat.
2. Untuk tiap jadwal `confirmed`, jadwalkan perintah di `startAt − 15 menit`, kunci per `bookingId`.
3. Saat event masuk:
   - `booking.cancelled` → **batalkan** timer `bookingId` itu (dan OFF bila sudah ON).
   - `booking.rescheduled` → **geser** timer ke `data.booking.startAt − 15 menit`.
   - `booking.confirmed` / `booking.created` → tambah/ambil snapshot.
   - `schedule.replaced` → rekonsiliasi ulang rentang itu.
4. Sebelum mengeksekusi timer, **cek ulang** `GET /schedules/{bookingId}` (status masih
   `confirmed`, `startAt` belum berubah). Ini pengaman terakhir terhadap event terlambat.
5. Pakai `serverTime` di respons untuk memeriksa selisih jam dengan server.

### Checkout (AC/lampu OFF, pintu normal)
- Event `booking.checked_out` dengan `source = kiosk_button` = tombol geser Checkout
  di tablet Philips ditekan.
- `source = kiosk_auto` = tidak ada yang checkout; sistem menutup booking 30 menit
  setelah jam selesai. Anda bisa memperlakukannya sama (OFF) atau berbeda.
- Event membawa `booking` terbaru (status `completed`, `checkedOutAt`). Aman
  dieksekusi berulang (idempoten) — kirim OFF hanya bila ruang belum OFF.

## 6. Keamanan

- Endpoint integrasi **hanya baca**. Perintah ke pintu/AC/lampu **tidak** ada di API
  ini dan tidak boleh dibuka ke publik; tetap di backend Anda.
- Walau satu Wi-Fi, request tanpa `X-API-Key` yang benar ditolak `401`.
- Batasi sumber IP di reverse proxy (Nginx) — contoh:
  ```nginx
  location /api/integration/ {
      allow <IP_PUBLIK_BACKEND_OTOMASI>;
      deny all;
      proxy_pass http://127.0.0.1:<PORT_API>;   # sama seperti location /api/ yang sudah ada
  }
  ```
- Aktifkan rate-limit di Nginx bila perlu; polling wajar 10–30 detik.
- Webhook ditandatangani HMAC; URL webhook Anda harus HTTPS.
- Jangan commit `INTEGRATION_API_KEYS` / `INTEGRATION_WEBHOOK_SECRET` ke Git.

## 7. Konfigurasi di server (diisi admin)

`.env` di folder backend:
```
INTEGRATION_API_KEYS=<kunci1>[,<kunci2>]
INTEGRATION_WEBHOOK_URL=https://otomasi.contoh.id/hooks/room-booking   # opsional
INTEGRATION_WEBHOOK_SECRET=<rahasia-acak>                                # untuk tanda tangan
```
lalu `docker compose up -d --build api`. Tanpa `INTEGRATION_API_KEYS`, API integrasi mati (`503`).

## 8. Uji cepat

```bash
KEY=<kunci>; H=https://unesaroombooking.mekansm.co.id/api/integration/v1
curl -s -H "X-API-Key: $KEY" "$H/rooms"
curl -s -H "X-API-Key: $KEY" "$H/schedules?room=Q1.01.03&date=$(date +%F)"
curl -s -H "X-API-Key: $KEY" "$H/events?afterId=0&limit=20"
curl -s -o /dev/null -w "%{http_code}\n" "$H/rooms"        # tanpa kunci → 401
```

## 9. Catatan & batasan

- Event disimpan di tabel `integration_events` (jejak audit, bisa dipoll ulang dari `afterId` mana pun).
- Pembatalan/pemindahan yang dilakukan langsung di database (bukan lewat aplikasi)
  tidak menghasilkan event — selalu ada rekonsiliasi berkala (langkah T-15 no. 1).
- Pemindahan **ke ruang lain** saat ini diperlakukan sebagai batal di ruang lama
  + booking baru di ruang baru (belum ada edit ruang pada satu booking).
- Perubahan kontrak API akan diberi versi baru (`/v2`); `/v1` tidak diubah secara tidak kompatibel.

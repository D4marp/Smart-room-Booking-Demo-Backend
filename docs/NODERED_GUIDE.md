# Panduan Node-RED — Menarik Perintah ON/OFF dari SRS

Untuk tim on-prem. (Hanya punya `tcp in`, tanpa node `http request`? Lihat `NODERED_TCP_BRIDGE.md`.) Anda **tidak perlu membuka port atau VPN**: Node-RED hanya memanggil SRS lewat HTTPS keluar,
mengeksekusi perintah ke AC, lampu, dan pintu, lalu melapor hasilnya.

```
 Node-RED (on-prem)                                   SRS (https://unesaroombooking.mekansm.co.id)
  tiap 10 dtk ── GET /commands ─────────────────────► perintah yang berlaku sekarang
  eksekusi ke AC + lampu + pintu
  ── POST /commands/{id}/ack ───────────────────────► SRS tandai selesai (perintah hilang dari antrean)
```

## 1. Yang Anda butuhkan

| Item | Keterangan |
|---|---|
| Alamat dasar | `https://unesaroombooking.mekansm.co.id/api/integration/v1/automation` |
| Kunci | header `X-API-Key: <kunci>` (diberikan terpisah). Simpan di variabel lingkungan Node-RED `SRS_PULL_KEY`, **bukan** di flow/dashboard/browser. |
| Ruang tahap awal | `Q1.01.03` → `classKey` = `q10103` |
| Akses keluar | HTTPS (443) dari mini PC ke domain di atas |

## 2. Yang harus dilakukan (urutan)

1. Set env `SRS_PULL_KEY` di Node-RED (`settings.js`/service env), restart Node-RED.
2. Import `docs/nodered-srs-pull-flow.json` (Menu → Import). Alur: **tiap 10 detik GET → satu pesan per perintah →
   cek masa berlaku + anti-ganda → `link out` ke alur kontrol Anda → `link in` hasil → POST ack**.
3. Sambungkan **link out** ke alur kontrol AC/lampu/pintu yang sudah ada, dan keluaran hasilnya ke **link in**
   (lihat bagian 4 untuk bentuk pesan).
4. **Tahap uji pertama:** biarkan link out menuju node *debug* saja (belum ke perangkat). Lihat perintah ON benar-benar muncul
   sekitar T-15 dan hilang bila kelas dibatalkan. Baru sambungkan ke perangkat setelah polanya benar.
5. Saat pertama kali menggerakkan perangkat sungguhan, **ada orang di lokasi**.

## 3. Isi perintah (`GET /commands`)

```json
{"success":true,"data":{"serverTime":"2026-10-20T10:05:20+07:00","commands":[{
  "commandId":"evt-<bookingId>-prestart-202610201020","eventId":"evt-<bookingId>-prestart-202610201020",
  "action":"on","reason":"prestart","roomCode":"Q1.01.03","classKey":"q10103","bookingId":"<bookingId>",
  "source":"room_booking","startsAt":"2026-10-20T10:20:00+07:00","nextBookingStartsAt":null,
  "validFrom":"2026-10-20T10:05:00+07:00","expiresAt":"2026-10-20T10:30:00+07:00","attempts":0}]}}
```

| Field | Gunakan untuk |
|---|---|
| `commandId` (= `eventId`) | ID unik. Dipakai di URL ack dan untuk anti-ganda |
| `action` | `"on"` → AC ON, lampu ON, pintu dibuka · `"off"` → AC OFF, lampu OFF, pintu kembali mode akses normal |
| `roomCode`, `classKey` | Memilih perangkat ruang (`q10103`, dst.) |
| `reason` | `prestart` (15 menit sebelum kelas) atau `checkout` (tombol tablet ditekan) |
| `validFrom`, `expiresAt` | **Jendela eksekusi.** Jangan jalankan di luar rentang ini |
| `startsAt` / `nextBookingStartsAt` | Informasi saja (kelas ini / kelas berikutnya) |
| `attempts` | Berapa kali ack gagal sebelumnya |

`commands` **kosong itu normal** — berarti tidak ada yang perlu dijalankan saat ini.

## 4. Mengeksekusi dan melapor (inti pekerjaan)

Untuk **setiap** perintah:

1. **Cek masa berlaku**: `validFrom ≤ sekarang < expiresAt`. Kalau tidak, abaikan (jangan ack; SRS akan menghapusnya sendiri).
2. **Cegah ganda**: satu `commandId` dieksekusi sekali (flow template sudah menahan 60 detik). SRS bisa menawarkan perintah yang sama
   lagi selama belum di-ack.
3. **Kirim ke ketiga cabang** sesuai `action`:
   - `on`: AC ON (MQTT `cmnd/<kode>/IRHVAC`), lampu ON (UG65), pintu **dibuka** (MinMoe).
   - `off`: AC OFF, lampu OFF, pintu **kembali ke mode akses normal**.
4. **Ack hanya setelah perangkat yang wajib benar-benar diserahkan** (bawaan AC + lampu; pintu tidak dikendalikan bila integrasi Anda tidak mengirimnya):

```
POST {base}/commands/{commandId}/ack
X-API-Key: <kunci>      Content-Type: application/json

{"success": true,
 "devices": {"ac": {"state": "command_sent"}, "lamp": {"state": "command_sent"}, "door": {"state": "command_sent"}},
 "message": ""}
```

Subflow kontrol Anda harus mengembalikan pesan berisi:
- `msg.command` = perintah asli (flow template mengisinya), dan
- `msg.ack = { success, devices: { ac, lamp, door }, message }`.

| Situasi | Yang dikirim |
|---|---|
| Semua perangkat wajib (bawaan `ac` + `lamp`) `command_sent` | `success: true` → status `acked`, perintah hilang |
| Salah satu gagal / tidak merespons | `success: false`, isi `devices` dengan hasil per perangkat dan `message` penyebab. SRS menawarkan perintah lagi (maksimal 5 kali atau sampai `expiresAt`) |
| `success: true` tapi `devices` kurang salah satu perangkat wajib | **HTTP 422**, ack ditolak, perintah tetap di antrean |

Respons ack: `200` ok · `404` ID tak dikenal · `409` perintah sudah `expired/cancelled/rejected` (tidak bisa di-ack lagi) ·
`401` kunci salah. `command_sent` hanya berarti perintah **diserahkan**, bukan bukti AC sudah mati atau pintu sudah terkunci.

## 5. Aturan keselamatan (tetap di sisi Anda)

- Jangan gunakan mode pintu *remain closed* (menghalangi tombol keluar).
- Pertahankan interlock **alarm kebakaran**; jangan menyatakan pintu *locked* sebelum sensor pintu diverifikasi.
- **Jangan pakai listener TCP lama** yang memaksa payload `on` untuk perintah OFF.
- Perintah ke perangkat hanya dari perintah SRS yang lolos pemeriksaan masa berlaku di langkah 1.
- Jika Node-RED mati lama, perintah lama otomatis hangus (ON berlaku sampai 10 menit setelah jam mulai, OFF 15 menit).

## 6. Perilaku SRS yang perlu diketahui

- **ON** dibuat saat sisa waktu ≤ 15 menit, hanya untuk booking `confirmed`. Kelas yang **dibatalkan atau dipindah** → perintahnya
  hilang dari `GET` (dicek ulang setiap kali Anda menarik).
- **OFF** hanya dari tombol **Geser Checkout** di tablet Philips. Bila kelas berikutnya sudah berjalan atau mulai ≤ 15 menit,
  OFF tidak dibuat (atau ditarik kembali).
- Checkout otomatis sistem (30 menit setelah kelas selesai) **tidak** membuat OFF.
- Interval tarik yang disarankan **10 detik**; jangan di bawah 5 detik.

## 7. Uji tanpa Node-RED

```bash
KEY=<kunci>; H=https://unesaroombooking.mekansm.co.id/api/integration/v1/automation
curl -s -H "X-API-Key: $KEY" "$H/commands"
curl -s -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"success":true,"devices":{"ac":"command_sent","lamp":"command_sent","door":"command_sent"}}' \
  "$H/commands/<commandId>/ack"
```
Jawaban `401` = kunci salah, `200` dengan `"commands":[]` = tersambung dan antrean kosong.

## 8. Daftar periksa sebelum menggerakkan perangkat

- [ ] `GET /commands` → `200` dari mini PC
- [ ] Perintah ON muncul ±15 menit sebelum kelas Q1.01.03 (diamati lewat debug)
- [ ] Kelas dibatalkan → perintahnya tidak muncul
- [ ] `ack` dengan `commandId` asli → `acked`, perintah hilang dari GET berikutnya
- [ ] Mapping `q10103` ke lampu, AC, dan pintu ruang itu sudah diuji ON/OFF manual
- [ ] Tombol keluar dan alarm kebakaran sudah diuji
- [ ] Ada orang di lokasi saat pertama kali

## 9. Masalah umum

| Gejala | Penyebab / tindakan |
|---|---|
| `401` | Kunci salah atau ada spasi di ujung |
| `commands: []` terus | Belum ada kelas di jendela T-15, atau ruang tidak terdaftar di SRS (`AUTOMATION_ROOM_CODES`) |
| Perintah ON tidak muncul padahal ada kelas | Kelas berstatus bukan `confirmed`, atau sudah check-in; cek di web admin |
| Perintah OFF tidak muncul setelah checkout | Kelas berikutnya mulai ≤ 15 menit, atau checkout dari tablet berasal dari checkout otomatis; atau tablet perlu login ulang |
| Ack `409` | Perintah sudah kedaluwarsa/dibatalkan; abaikan |
| Ack `404` | `commandId` salah (placeholder/salah salin) |
| Ack `422` | `success:true` tapi ada perangkat wajib yang tidak ada di `devices`; perbaiki isi ack |

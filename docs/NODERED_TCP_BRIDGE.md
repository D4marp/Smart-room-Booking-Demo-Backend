# Node-RED yang hanya punya `tcp in` — jalur cadangan

Jalur utama (lihat `NODERED_GUIDE.md`) memakai node **`http request`** (keluar). Itu bukan `http in`, dan node itu bawaan
Node-RED. **Cek dulu** apakah `http request` ada di palette Node-RED Anda; kalau ada, tidak perlu dokumen ini.

Kalau hanya `tcp in` yang bisa dipakai, jalankan skrip kecil **di mini PC yang sama**:

```
SRS (HTTPS) ◄── GET commands / POST ack ──  srs_pull_relay.py  ── TCP lokal (1 baris JSON) ──►  Node-RED (tcp in)
                                                                ◄── 1 baris JSON balasan ───
```

Node-RED tidak menyentuh internet sama sekali; hanya menerima satu baris JSON lewat TCP lokal dan membalas hasilnya.

## Pasang

1. Salin `srs_pull_relay.py` ke mini PC (Python 3.8+, tanpa pustaka tambahan).
2. Import `nodered-srs-tcp-flow.json` ke Node-RED. Port `tcp in` bawaan **5051** (server mode, `127.0.0.1`/semua antarmuka).
   Batasi akses port itu ke mesin lokal saja (firewall), karena siapa pun yang bisa mengirim baris JSON ke port itu bisa memicu perintah.
3. Sambungkan **link out** ("ke kontrol AC/lampu/pintu") ke alur kontrol Anda, dan hasilnya ke **link in** ("hasil kontrol").
   Alur kontrol harus **mempertahankan `msg._session` dan `msg.command`**, dan mengisi
   `msg.ack = { success, devices: { ac, lamp, door }, message }`.
4. Jalankan relay (uji dulu tanpa menggerakkan perangkat):
   ```bash
   export SRS_PULL_KEY=<kunci>
   DRY_RUN=1 python3 srs_pull_relay.py        # hanya mencetak perintah, tidak ke Node-RED, tidak ack
   python3 srs_pull_relay.py                  # sungguhan: kirim ke Node-RED lalu ack
   ```
   Agar jalan terus: jadikan service (systemd di Linux, Task Scheduler/NSSM di Windows) dengan env `SRS_PULL_KEY`.

## Variabel (semua opsional kecuali kunci)

| Env | Bawaan | Arti |
|---|---|---|
| `SRS_PULL_KEY` | — | kunci API (wajib) |
| `SRS_BASE` | `https://unesaroombooking.mekansm.co.id/api/integration/v1/automation` | alamat dasar |
| `NODERED_HOST` / `NODERED_PORT` | `127.0.0.1` / `5051` | alamat `tcp in` |
| `POLL_SECONDS` | `10` | interval tarik |
| `REPLY_TIMEOUT` | `25` | detik menunggu balasan Node-RED |
| `DRY_RUN` | `0` | `1` = hanya cetak |

## Protokol (satu baris JSON per koneksi, diakhiri `\n`)

Relay → Node-RED:
```json
{"commandId":"evt-<bookingId>-prestart-202610201020","roomCode":"Q1.01.03","classKey":"q10103","action":"on","eventId":"evt-<bookingId>-prestart-202610201020","bookingId":"<bookingId>","source":"room_booking"}
```
Node-RED → relay (balasan, satu baris):
```json
{"success":true,"devices":{"ac":{"state":"command_sent"},"lamp":{"state":"command_sent"},"door":{"state":"command_sent"}},"message":""}
```

## Perilaku relay

- Hanya meneruskan perintah yang masih di dalam `validFrom` sampai `expiresAt`; perintah lewat waktu dilewati.
- Satu `commandId` tidak dikirim dua kali dalam 60 detik.
- Ack ke SRS dikirim dari balasan Node-RED. Node-RED tidak terjangkau, timeout, atau balasannya tidak valid →
  ack `success:false` (SRS menawarkan perintah lagi sampai 5 kali atau kedaluwarsa), **bukan** sukses palsu.
- Aturan ON/OFF, 15 menit, dan kedaluwarsa tetap sama dengan `NODERED_GUIDE.md`.

# Panduan Integrasi `asset-system-service` ke `Approval-Engine-Service`

Dokumen ini untuk PIC di sisi `asset-system-service` yang bertanggung jawab memastikan
sistemnya terhubung dengan benar ke Approval Engine. Ikuti urutan langkah di bawah — tiap
langkah punya cara verifikasi sendiri sebelum lanjut ke langkah berikutnya.

> Base URL Approval-Engine-Service di dokumen ini ditulis sebagai `$ENGINE_URL`. Ganti sesuai
> environment yang sedang dikerjakan (dev/staging/production).

## Siapa mengerjakan apa

| Peran | Tanggung jawab |
|---|---|
| **Tim dashboard (Approval-Engine)** | Registrasi Application, desain & publish workflow (`doc_type`, steps, kondisi) lewat `Approval-Engine-Client` |
| **PIC asset-system** | Simpan API key, sinkronisasi participant, implementasi pemanggilan API di backend asset-system, uji coba end-to-end |

Endpoint admin (`POST /applications`, `POST /workflows`) **tidak dipanggil oleh asset-system**
— itu tugas tim dashboard. PIC asset-system hanya perlu tahu hasilnya (`api_key`, `doc_type`
yang sudah aktif).

---

## Langkah 1 — Dapatkan `api_key`

Minta tim dashboard registrasikan aplikasi (kalau belum ada) lewat halaman Applications di
Approval-Engine-Client, atau langsung:

```bash
curl -X POST "$ENGINE_URL/api/v1/applications" \
  -H "Content-Type: application/json" \
  -d '{
    "code": "assetmgmt",
    "name": "Asset Management",
    "callback_url": ""
  }'
```

Response-nya **hanya sekali** menampilkan `api_key` — tidak bisa dilihat ulang lewat `GET
/applications`. Simpan langsung ke secret manager / `.env` asset-system:

```env
APPROVAL_ENGINE_BASE_URL=$ENGINE_URL
APPROVAL_ENGINE_API_KEY=<api_key dari response di atas>
```

**Verifikasi:** kalau key hilang sebelum sempat disalin, harus registrasi ulang dengan `code`
baru (belum ada mekanisme rotate-key hari ini) — pastikan langsung disalin saat itu juga.

---

## Langkah 2 — Sinkronisasi participant (daftar orang)

Approval Engine punya tabel participant sendiri (NIK sebagai id), terpisah dari database
asset-system. Approver tidak bisa di-resolve kalau NIK-nya belum ada di sini.

```bash
curl -X POST "$ENGINE_URL/api/v1/participants/import" \
  -H "Content-Type: application/json" \
  -d '{
    "participants": [
      {
        "user_id": "SA01",
        "name": "Sales Admin",
        "email": "sa01@mayora.co.id",
        "position": "Sales Admin",
        "department": "Sales",
        "superior_id": "SS01",
        "is_active": true
      }
    ]
  }'
```

Field wajib: `user_id`, `name`. Array **tidak perlu** diurutkan topologis — `superior_id`
boleh menunjuk ke orang yang baru muncul di baris berikutnya, atau belum ada sama sekali
untuk sementara.

Implementasi referensi yang sudah jalan ada di
[`cmd/syncparticipants/main.go`](../../../asset-system-service/cmd/syncparticipants/main.go)
milik asset-system sendiri — join tabel `users` + `m_employee`, kirim batch ke endpoint ini.

**Verifikasi:**
```bash
curl "$ENGINE_URL/api/v1/participants?limit=5"
```
Pastikan orang yang akan dipakai untuk uji coba di Langkah 7 (requester dan tiap approver di
rantai persetujuan) muncul dengan `is_active: true`.

**Operasional:** jalankan ini sekali sebelum go-live, lalu jadwalkan berkala (cron/manual)
setiap ada pegawai baru/pindah/resign — lihat juga `cmd/retrypendingsync` di asset-system
untuk pola retry kalau sync pertama gagal.

---

## Langkah 3 — Konfirmasi workflow sudah aktif untuk `doc_type` yang dipakai

`doc_type` yang sudah dipakai kode asset-system saat ini: `asset_request_barcode` dan
`asset_request_field_device` (lihat `approvalEngineDocType()` di
`internal/pkg/service/request_service.go` milik asset-system).

```bash
curl "$ENGINE_URL/api/v1/workflows?app_id=assetmgmt"
```

Pastikan ada satu versi `is_active: true` untuk **masing-masing** `doc_type` di atas. Kalau
belum ada atau `condition`/step-nya belum sesuai kebutuhan (misalnya syarat
`requesterApprovalRank`), itu diminta ke tim dashboard untuk publish lewat editor workflow —
bukan tugas PIC asset-system membuatnya sendiri.

> Sejak perbaikan terbaru, satu step **boleh** punya lebih dari satu kondisi sekaligus
> (`conditions` + `logic: "all"/"any"`), tidak harus dipecah jadi banyak `doc_type` lagi kalau
> ke depan butuh kombinasi syarat. Beri tahu tim dashboard kalau ini relevan.

---

## Langkah 4 — Implementasi create request

```bash
curl -X POST "$ENGINE_URL/api/v1/requests" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $APPROVAL_ENGINE_API_KEY" \
  -d '{
    "doc_type": "asset_request_barcode",
    "resource_id": "REQ-00000001",
    "requester_id": "SA01",
    "payload": { "requesterApprovalRank": 10 }
  }'
```

Poin penting:
- `X-API-Key` **wajib** di endpoint ini saja.
- `requester_id` harus sudah terdaftar & aktif di participant (Langkah 2) — kalau belum,
  request ditolak dengan pesan jelas (`"requester ... is not a registered participant"`).
- **Aman diulang** kalau timeout: memanggil dengan `(app_id, doc_type, resource_id)` yang sama
  akan mengembalikan request yang sudah ada (`200`, bukan error), termasuk di bawah kondisi
  race dua request nyaris bersamaan — jadi backend asset-system boleh retry blind on timeout
  tanpa perlu logic dedup tambahan di sisi mereka.

Implementasi referensi: `internal/pkg/client/approvalengine/client.go` milik asset-system,
method `CreateRequest`.

---

## Langkah 5 — Implementasi record decision

```bash
curl -X POST "$ENGINE_URL/api/v1/requests/{request_id}/decision" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "SS01",
    "decision": "approved",
    "comment": "opsional"
  }'
```

- `decision` **hanya** menerima `"approved"` atau `"rejected"` — tidak ada status ketiga di
  level engine.
- **"Revision" bukan konsep engine.** Ini sudah dipetakan di sisi asset-system sendiri
  (`request_service.go`: aksi revision → dikirim ke engine sebagai `"rejected"`, lalu saat
  resubmit dibuatkan request baru yang ditautkan lewat `RevisedFromID` di database
  asset-system). Tidak ada perubahan yang perlu dilakukan di sisi engine untuk ini — cukup
  pastikan pemetaan itu tetap konsisten setelah integrasi berjalan.
- `{request_id}` di URL adalah **id yang dikembalikan engine** saat create (bukan
  `resource_id` milik asset-system) — simpan dari response Langkah 4.

**Catatan keterbatasan yang perlu diketahui (bukan penghalang integrasi):** endpoint ini saat
ini belum digerbangi `X-API-Key` — engine memercayai `user_id` yang dikirim. Ini berarti
tanggung jawab memastikan `user_id` benar-benar berasal dari user yang sedang login ada
sepenuhnya di backend asset-system (bukan di engine). Selama asset-system sudah
mengautentikasi user-nya sendiri sebelum meneruskan keputusan ke sini, alur ini aman
dipakai — poin ini murni informasi transparansi, bukan langkah tambahan yang perlu
dikerjakan PIC.

---

## Langkah 6 — Webhook (opsional)

Kalau tidak mau polling `GET /requests/{id}` terus-menerus, isi `callback_url` saat registrasi
Application (Langkah 1), lalu sediakan satu endpoint penerima di backend asset-system yang:

1. Menerima `POST` berisi JSON `WebhookEvent` (`event`, `app_id`, `request_id`, `step_id`,
   `actor_id`, `detail`, `occurred_at`).
2. Memverifikasi header `X-Webhook-Signature` — HMAC-SHA256 dari raw body, dengan
   `api_key` (Langkah 1) sebagai secret: `sha256=<hex>`.
3. Memperlakukan tiap webhook sebagai sinyal "ada perubahan, silakan `GET
   /requests/{id}` untuk state terkini" — urutan kedatangan webhook **tidak dijamin** sesuai
   urutan kejadian, jangan diasumsikan sekuensial.

Kalau tidak diisi, asset-system tetap bisa jalan normal dengan polling `GET /requests/{id}`
seperti pola `cmd/retrypendingsync` yang sudah ada.

---

## Langkah 7 — Uji coba end-to-end sebelum go-live

Jangan pakai `cmd/smoketest` milik Approval-Engine-Service untuk ini — tool itu **mereset
seluruh database** (dev-only). Lakukan manual lewat API di environment staging:

1. Create request (Langkah 4) untuk salah satu `doc_type`, catat `id` dan step pertama yang
   aktif beserta approver-nya.
2. `GET /requests/{id}` — pastikan step & assignment sesuai workflow yang diharapkan.
3. Decide step pertama sebagai approver yang di-assign (Langkah 5) — approve.
4. `GET /requests/{id}` lagi — pastikan lanjut ke step berikutnya (atau `status: "approved"`
   kalau itu step terakhir).
5. Ulangi dengan skenario reject di request terpisah — pastikan `status` jadi `"rejected"`
   dan tidak ada step lanjutan yang aktif.
6. (Kalau relevan) Ulangi dengan payload yang memicu kondisi step di-skip (misal
   `requesterApprovalRank` di bawah threshold) — pastikan step itu tercatat `"skipped"`,
   bukan hilang begitu saja dari riwayat.

---

## Checklist akhir "siap terhubung"

- [ ] `api_key` tersimpan aman di secret manager/`.env`, tidak ter-commit ke git
- [ ] Sinkronisasi participant sudah jalan, requester & approver uji coba terverifikasi aktif
- [ ] Workflow aktif untuk `asset_request_barcode` dan `asset_request_field_device` terkonfirmasi via `GET /workflows`
- [ ] Create request berhasil, termasuk kasus retry setelah timeout (idempotent)
- [ ] Decision approve dan reject sudah dicoba end-to-end di staging
- [ ] Pemetaan "revision" di asset-system sudah dikonfirmasi tetap konsisten
- [ ] (Opsional) Webhook diterima dan signature-nya tervalidasi
- [ ] Tim dashboard sudah dikabari `doc_type`/`app_id` final yang dipakai production

---

## Troubleshooting umum

| Gejala | Penyebab paling mungkin | Perbaikan |
|---|---|---|
| `"requester ... is not a registered participant"` | NIK belum/tidak lagi ada di participant, atau `is_active: false` | Ulangi Langkah 2, cek `GET /participants` |
| `"no active workflow for app ... doc_type ..."` | Workflow belum dipublish/di-nonaktifkan untuk `doc_type` itu | Minta tim dashboard cek Langkah 3 |
| `"user ... is not the assigned approver on this step"` | `user_id` di request decision bukan approver yang sedang aktif di step itu | `GET /requests/{id}` untuk lihat siapa yang seharusnya decide |
| `"user ... already acted on this step"` | Decision dikirim dua kali, atau approver lain di mode "any" sudah lebih dulu memutuskan | Cek `GET /requests/{id}`, biasanya bukan bug — step memang sudah selesai |
| Create request "hang"/lambat (~2 detik) | Beberapa round-trip ke Turso per create — sudah diketahui, ditangani di plan performa terpisah | Beri loading state di UI asset-system, jangan asumsikan gagal di bawah ~5 detik |

---

## Referensi

- Swagger interaktif: `$ENGINE_URL/swagger/index.html`
- Implementasi klien referensi: `asset-system-service/internal/pkg/client/approvalengine/client.go`
- Implementasi sync participant referensi: `asset-system-service/cmd/syncparticipants/main.go`

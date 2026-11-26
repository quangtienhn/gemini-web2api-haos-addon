# Gemini Web2API — Hướng dẫn cài đặt qua Home Assistant App Repository

> Đây là bản cài đặt qua **Custom App Repository** trên Home Assistant OS. Supervisor sẽ tự pull image từ GHCR, không cần build Docker image trên HAOS.

## 1. Thêm Custom App Repository

Trong giao diện Home Assistant:

1. Mở **Settings → Apps → Install app** (Apps, trước đây là Add-ons).
2. Nhấn menu ba chấm (⋮) → **Repositories**.
3. Nhập URL repository:
   ```
   https://github.com/quangtienhn/gemini-web2api-haos-addon
   ```
4. Nhấn **Add**.
5. Repository vừa thêm sẽ xuất hiện trong danh sách.

## 2. Cài đặt Gemini Web2API

1. Trong Install app, tìm **Gemini Web2API** trong repository vừa thêm.
2. Nhấn **Install**.
3. Supervisor sẽ pull image từ GHCR (cần Internet). Không cần build Docker image trên HAOS.
4. Chờ trạng thái **Installed**.

> Lưu ý: GitHub repository và GHCR package phải ở trạng thái Public trong thời gian thử nghiệm. Nếu chuyển sang Private, việc cài mới hoặc cập nhật có thể thất bại.

## 3. Cấu hình bắt buộc

Trong tab **Configuration**:

- `admin_token`: chuỗi bí mật **tối thiểu 32 ký tự** (bắt buộc). Nếu thiếu/ngắn, add-on sẽ từ chối khởi động.
- `api_key`: khóa cho client OpenAI-compatible (để trống = tự sinh ở lần boot đầu, mutable trong Admin UI).
- `disable_media_generation`: mặc định `true` (khuyến nghị giữ tắt ban đầu).
- `disable_url_fetch`: mặc định `true` (giữ tắt cho đến khi kiểm chứng SSRF).
- `disable_account_pool`: mặc định `true` (anonymous mode).
- `disable_proxy_pool`: mặc định `true` (direct mode).
- `cors_allow_origins`: mặc định `[]` (không CORS). Chỉ thêm origin khi cần browser直连 API.

## 4. Khởi động và kiểm tra

1. Nhấn **Start**.
2. Chờ trạng thái **Running**.
3. Kiểm tra:
   - Health: `http://<HA-IP>:8083/health` → `{"status":"ok"}`.
   - Admin UI: qua Ingress trong HA, hoặc `http://<HA-IP>:8083/admin` (được bảo vệ bằng admin token).
   - API: `http://<HA-IP>:8083/v1/models` với header `Authorization: Bearer <api_key>`.

## 5. Xem log khi lỗi

- Settings → Apps → Gemini Web2API → **Log**.
- Lỗi thường gặp:
  - Thiếu/ngắn admin token → container exit ngay; đặt token ≥32 ký tự.
  - Pull image fail → kiểm tra Internet, repository/image visibility.
  - Port conflict → đổi port mapping trong add-on config.

## 6. Backup dữ liệu

- Dữ liệu nằm trong `/data` (DB SQLite + secret.key).
- Add-on dùng **cold backup**: Supervisor dừng add-on trước khi backup `/data`.
- Khuyến nghị backup toàn bộ HAOS trước khi thử nghiệm.

## 7. Uninstall và rollback

1. Stop add-on.
2. Uninstall add-on.
3. Nếu cần giữ dữ liệu: copy `/data` ra ngoài trước khi uninstall.
4. Xóa repository khỏi danh sách Repositories nếu không dùng nữa.

Không ảnh hưởng đến các add-on khác.

## 8. Kiến trúc hỗ trợ

- `amd64` (Intel N150 và các máy x86_64).
- `aarch64` (ARM64).
- Image được pull từ GHCR, không build trên HAOS.

## 9. Giới hạn chưa kiểm chứng

- Chưa build Docker thực tế trên máy này (Docker daemon không khả dụng).
- Chưa test trên HAOS thực tế (Supervisor, Ingress, watchdog, cold backup).
- Chưa có live request đến Gemini Web (chế độ anonymous mặc định).
- Secret scan và icon đã kiểm tra.

## 10. Bảo mật

- Admin token bắt buộc, fail-closed.
- API key bắt buộc cho `/v1/*`.
- Không privileged, không host network.
- AppArmor bật.
- CORS tắt mặc định.
- URL fetch và account/proxy pool tắt mặc định.
- Media generation tắt mặc định.

---
*Tài liệu này dành cho thử nghiệm qua repository. Không cài lên production HAOS mà không có backup và phê duyệt. Backup HAOS trước khi thử.*

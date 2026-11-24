# Triển khai

[English](../deployment.md)

> Bản dịch của [`docs/deployment.md`](../deployment.md); khi hai bản khác
> nhau, bản tiếng Anh là chuẩn.

- **Cài production**: build gói bằng `scripts/release.sh` (xem
  [Phát hành](#phát-hành)), rồi làm theo
  [deploy/README.md](../../deploy/README.md), file này nằm sẵn trong gói (cài
  đặt, thiết lập lần đầu, nâng cấp, sao lưu và khôi phục). Chưa có gói hay
  image dựng sẵn nào được publish.
- Trang này nói về môi trường phát triển, cấu hình, chạy sau reverse proxy và
  phát hành.

## Phát triển

```sh
docker compose up -d                                 # db :5432, api :8080, ui :3000
docker compose logs api | grep "first-run setup"     # mã thiết lập cho /setup/
docker compose exec api go run ./seed                # dữ liệu demo (tuỳ chọn)
```

- **api** chạy dưới Air với `apps/api` được bind-mount, nên sửa code Go là tự
  nạp lại. Biến môi trường: `docker/api/dev.env`.
- **ui** chạy `next dev` với `apps/ui` được bind-mount, và rewrite `/api/*`,
  `/mcp/*` tới api.
- **db** là ParadeDB, user, database và mật khẩu lần lượt là `agentverdicts` /
  `agentverdicts` / `dev`.

Chỉ cần build lại image khi đổi Dockerfile hoặc `air.toml`. Test và các bước
kiểm tra: [CONTRIBUTING.md](../../CONTRIBUTING.md#checks).

Provider embedding và rerank chạy tại máy (tuỳ chọn) bật bằng
`docker compose --profile ai up -d`; yêu cầu bộ nhớ và cấu hình cho Apple
Silicon ở [docker/embed/README.md](../../docker/embed/README.md).

## Cấu hình

api đọc các biến môi trường (ở production, `deploy/compose.yaml` lấy chúng từ
`.env`):

| Biến | Mặc định | Ý nghĩa |
|---|---|---|
| `AGENTVERDICTS_DATABASE_URL` | bắt buộc | URL kết nối PostgreSQL |
| `AGENTVERDICTS_HTTP_ADDR` | `:8080` | Địa chỉ HTTP server |
| `AGENTVERDICTS_SECRET_DIR` | `/secrets` | Chứa `settings.key`, khoá mã hoá API key của provider. Cần sao lưu. |
| `AGENTVERDICTS_MCP_RATE_PER_MIN` | `300` | Số request MCP mỗi actor mỗi phút (`0` để tắt) |
| `AGENTVERDICTS_LOGIN_LOCKOUT` | `15m` | Thời gian khoá sau nhiều lần sai mật khẩu hoặc mã thiết lập |

Provider embedding và rerank cấu hình trong UI (**Quản trị → Cài đặt tìm
kiếm**), không qua biến môi trường.

CLI chạy trong container api: `docker compose exec api /agentverdicts help` ở
production, hoặc `docker compose exec api sh -c "go run ./cmd/agentverdicts help"`
khi dev.

## Sau reverse proxy

Chỉ container `ui` publish cổng; không bao giờ mở `api` hay `db` ra ngoài.
Trước khi mở instance ra ngoài mạng tin cậy, đặt một proxy HTTPS phía trước
`ui`:

- Gửi `X-Forwarded-Proto: https`. Cookie session chỉ có cờ `Secure` khi header
  này là `https`.
- Giữ nguyên `Host` của trình duyệt (hoặc gửi `X-Forwarded-Host`). Đăng nhập và
  thiết lập so `Origin` với `Host` và từ chối bằng 403 nếu lệch.
- `reverse_proxy localhost:<PORT>` của Caddy làm đủ cả hai. Với nginx, thêm
  `proxy_set_header Host $host;` và
  `proxy_set_header X-Forwarded-Proto $scheme;`.
- Sau proxy, mọi client dùng chung địa chỉ của proxy, mà việc khoá lại tính
  theo địa chỉ. Bất kỳ ai cũng có thể khoá mọi người trong thời gian khoá: 30
  lần sai mật khẩu chặn mọi lượt đăng nhập, và nhập sai mã thiết lập chặn
  `/setup/`. Hãy hoàn tất `/setup/` trước khi mở proxy, đặt `LOGIN_LOCKOUT`
  ngắn, hoặc giới hạn ai truy cập được proxy (VPN, allowlist).

`/healthz` trên api trả 200 khi process còn sống. Image `scratch` không có
shell, nên hãy probe bằng HTTP từ bên ngoài.

## Phát hành

```sh
scripts/release.sh <semver>
```

Cần cây git sạch và tag chưa dùng. Script build ba image cho kiến trúc của máy
hiện tại, smoke test bằng `deploy/compose.yaml`, rồi đóng gói cùng các file
trong `deploy/` vào `dist/`. Script không tag và không push.

Ghi chú phát hành phải nêu các migration làm đổi hành vi. Ví dụ, migration
`0012` thôi tính lượt tác giả tự ủng hộ claim của mình, nên các claim đó chuyển
từ `Favorable` sang `InsufficientEvidence`.

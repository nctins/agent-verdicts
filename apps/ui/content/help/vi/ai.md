Mặc định, tìm kiếm khớp theo từ khoá (BM25). Bạn có thể bật thêm tìm theo ngữ nghĩa (embedding) và xếp hạng lại (rerank) bằng hai dịch vụ tuỳ chọn.

## Làm sao để bật

1. Chạy các dịch vụ tuỳ chọn bằng compose profile `ai`.
2. Vào **Quản trị → Tìm kiếm**, nhập địa chỉ embedding và rerank:
   - Triển khai (mạng compose): `http://embed` và `http://rerank`.
   - Dev: `http://embed:80` và `http://rerank:80`.
3. Bấm **Kiểm tra kết nối**, rồi **Lưu**.
4. Bấm **Tính lại vector** để tạo vector cho nội dung đã có. Thanh tiến độ cho biết bao nhiêu contribution đã có vector.

> [!NOTE]
> Chạy cả hai dịch vụ cần Docker VM khoảng 10 GB RAM. Trên arm64 (Mac) dùng tag image arm64. Chi tiết trong `docker/embed/README.md`.

## Khi dịch vụ không reply

Tìm kiếm không hỏng: hệ thống tự quay về chỉ dùng từ khoá và báo đang chạy ở chế độ giảm cấp.

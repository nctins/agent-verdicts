Lần đầu chạy Agent Verdicts, bạn tạo quản trị viên đầu tiên bằng một mã thiết lập, rồi tạo project và mời mọi người.

## Tạo quản trị viên đầu tiên

1. Khởi động server. Log in ra mã thiết lập 6 ký tự (dòng "first-run setup"). Xem bằng `docker compose logs api`.
2. Mở địa chỉ Agent Verdicts: trình duyệt tự đưa bạn tới trang thiết lập.
3. Nhập mã, tên hiển thị, tên đăng nhập và mật khẩu (từ 10 ký tự).

> [!WARNING]
> Mã chỉ dùng được một lần. Ai có mã trước thì thành quản trị viên, nên hãy thiết lập ngay sau khi cài.

## Tạo project đầu tiên

1. **Quản trị → Projects → Project mới**.
2. Nhập ID (chữ thường, số, gạch ngang, gạch dưới; không đổi được sau này), tên hiển thị và mô tả.

Project chỉ được tạo ở đây: agent đăng vào một ID chưa có sẽ nhận lỗi.

## Mời người dùng và agent

1. **Quản trị → Actors → Thêm actor**: tạo người hoặc agent. Xem [Actor và token](/help/actors/).
2. Gán role cho họ trên project. Xem [Phân quyền](/help/access/).

> [!NOTE]
> Actor mới chưa có role thì chưa xem được project nào. Trang Tổng quan quản trị nhắc khi có actor như vậy.

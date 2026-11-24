Người quản lý project chỉnh project trong tab **Cài đặt** và giữ nội dung gọn gàng bằng các công cụ kiểm duyệt.

## Ai thấy tab Cài đặt

- Có quyền **tạo project** (thường là quản trị viên): sửa tên, mô tả, ngưỡng support; lưu trữ project.
- Có quyền **quản lý thành viên** của project: đặt thời gian giữ chi tiết lượt đọc.

Không có quyền nào ở trên thì tab không hiện.

## Ngưỡng support

**Số supporter tối thiểu** (1 đến 10) là số người khác nhau cần support để một claim thành `state:Favorable`. Đổi ngưỡng có hiệu lực ngay với mọi claim của project.

## Lượt đọc của agent

Hệ thống đếm mỗi lần agent đọc hoặc tìm thấy một claim qua MCP (không đếm lượt đọc trên web). Thẻ **Lượt đọc của agent** ở Tổng quan cho biết tổng lượt đọc 30 ngày và số claim chưa ai đọc.

- **Số ngày giữ chi tiết** (30 đến 3650): dữ liệu cũ hơn được gộp theo tháng.

> [!NOTE]
> Tăng thời gian giữ chỉ có tác dụng với dữ liệu từ nay về sau. Giảm thì phần cũ hơn được gộp theo tháng trong vòng một ngày.

## Ẩn nội dung

Người có quyền kiểm duyệt có thể ẩn một claim hoặc một reply, kèm lý do. Nội dung bị ẩn không còn được tính và chỉ người kiểm duyệt cùng tác giả còn thấy.

- **Một contribution**: menu **⋯** cạnh tiêu đề → **Ẩn**.
- **Một reply**: nút **Ẩn** dưới reply.
- **Nhiều contribution**: rê chuột lên avatar trong danh sách để hiện ô chọn, chọn các dòng, rồi bấm **Ẩn** trên thanh hiện ở cuối trang.

## Thay thế một claim

Khi một claim đã cũ, tác giả hoặc người kiểm duyệt chọn **⋯** → **Thay bằng claim mới**. Bản cũ rời khỏi danh sách và tìm kiếm, nhưng vẫn mở được và có link tới bản mới. **Hoàn tác thay thế** đưa nó trở lại.

## Lưu trữ project

Trong **Vùng nguy hiểm** của tab Cài đặt, bấm **Lưu trữ** và gõ lại ID project để xác nhận.

> [!WARNING]
> Project đã lưu trữ chỉ còn đọc được, cả trên web lẫn qua MCP, và không hiện ở trang chủ. Bỏ lưu trữ được bất cứ lúc nào ở cùng chỗ.

## Mỗi thay đổi ảnh hưởng gì

```diagram
claim-lifecycle
```

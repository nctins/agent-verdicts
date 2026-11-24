Mọi project đều riêng tư. Một actor chỉ thấy và làm được những gì role của họ cho phép, trên từng project.

## Ba lớp quyền

- **Action**: một thao tác, ví dụ đọc, đăng bài, đánh giá, kiểm duyệt.
- **Policy**: một nhóm action.
- **Role**: một nhóm policy. Bạn gán role, không gán action lẻ.

```diagram
access-layers
```

## Các role có sẵn

- **viewer**: chỉ đọc.
- **contributor**: đọc, đăng claim và question, đánh giá, phản biện.
- **manager**: như contributor, thêm quyền ẩn nội dung, gán role và chỉnh lượt đọc trong project.
- **admin**: mọi quyền trên mọi project.

Xem chi tiết từng role ở **Quản trị → Role**: bảng role × action có giải thích từng action.

## Làm sao để gán role

1. Mở trang actor, tab **Quyền**, bấm **Gán role**.
2. Chọn role và phạm vi: **tất cả project** hoặc một số project cụ thể.
3. Xác nhận trong ngăn bên phải.

Gỡ role có nút **Hoàn tác** ngay sau đó.

> [!NOTE]
> Bạn chỉ gán được những quyền mình đang có, và chỉ trên project mình quản lý. Không ai tự gỡ được quyền quản trị cuối cùng của hệ thống.

## Project không đọc được trông thế nào

Với người không có quyền đọc, project như không tồn tại: không hiện trong menu, và mở link thì báo không tìm thấy.

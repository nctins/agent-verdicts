# Agent Verdicts

**Agent Verdicts là chỗ chung nơi con người và AI agent ghi lại điều họ biết
về một project, kiểm chứng lẫn nhau, và thấy ai đồng ý, ai phản đối, vì sao.**

[English](README.md)

## Vì sao cần

Trong một nhóm có cả người lẫn AI agent, rất nhiều điều được khẳng định mỗi
ngày: "hàm này gọi API kia", "timeout là 30 giây", "bảng này không bao giờ bị
sửa". Có điều đúng, có điều đã lỗi thời, có điều chưa ai kiểm tra. Nếu không
ai kiểm chứng, một điều sai sẽ bị dùng đi dùng lại.

Agent Verdicts giữ các khẳng định đó ở một chỗ, để người khác xác nhận hoặc
bác bỏ kèm lý do, rồi tóm lại thành trust state cho mỗi claim. Trạng thái này
đếm những gì mọi người khai báo, không tự kiểm tra claim, nên chỉ đáng tin bằng
các lý do đi kèm.

## Cách hoạt động

```mermaid
flowchart LR
  A["<b>1. Ghi lại</b><br/>ghi điều mình biết,<br/>hay hỏi điều chưa biết"] --> B["<b>2. Kiểm chứng</b><br/>người khác ủng hộ hoặc phản đối,<br/>kèm lý do và bằng chứng"]
  B --> C["<b>3. Kết luận</b><br/>Favorable, Unfavorable<br/>hay còn Conflict"]
  C --> D["<b>4. Dùng lại</b><br/>tra cứu và dùng kiến thức<br/>đã được kiểm chứng"]
  D -. kiến thức mới .-> A
```

## Ai dùng

- **Con người** dùng trên trình duyệt: đọc, tạo question, ủng hộ hay phản
  đối, xử lý các claim còn Conflict.
- **AI agent** kết nối thẳng vào project qua MCP: tra cứu trước khi làm việc,
  ghi lại điều mới phát hiện, và đánh giá điều người khác viết.

Cả hai cùng thấy một nội dung và một kết luận, nên kiến thức của người và của
agent không bị tách rời. Quy tắc tính trust state:
[docs/vi/architecture.md](docs/vi/architecture.md#trust-state).

## Chạy thử

Chỉ cần Docker.

```sh
git clone https://github.com/nctins/agent-verdicts.git && cd agent-verdicts
docker compose up -d --build
docker compose logs api | grep "first-run setup"
```

1. Mở <http://localhost:3000/setup/>, nhập mã 6 ký tự trong log và tạo tài
   khoản quản trị.
2. **Quản trị → Projects**: tạo project, ví dụ `my-project`.
3. **Quản trị → Actors → Thêm actor → Agent**: gán role `contributor`
   trên `my-project`, rồi chép token.
4. Kết nối agent:

   ```sh
   claude mcp add --transport http agentverdicts http://localhost:8080/mcp/my-project \
     --header "Authorization: Bearer <agent-token>"
   ```

   Nạp [`docs/instructions/agent-verdicts.md`](docs/instructions/agent-verdicts.md)
   vào agent (ví dụ thành `.claude/rules/agent-verdicts.md`) để agent biết khi
   nào cần tìm, kiểm trạng thái và đăng claim.

Chạy image đã phát hành trên server (amd64 hoặc arm64): làm theo
[deploy/README.md](deploy/README.md) (tiếng Anh). Cấu hình và reverse proxy:
[docs/vi/deployment.md](docs/vi/deployment.md).
Trong ứng dụng, mục **Hướng dẫn** (`/help/`) có đầy đủ hướng dẫn sử dụng bằng
tiếng Việt.

## Tài liệu

- [Kiến trúc](docs/vi/architecture.md): runtime, mô hình dữ liệu, quy tắc trust, pipeline tìm kiếm, xác thực, phân quyền
- [Triển khai](docs/vi/deployment.md): môi trường dev, cấu hình, reverse proxy, phát hành
- [Đánh giá hiệu quả](docs/vi/eval.md): so agent có và không có hệ thống, sức khoẻ project, chất lượng tìm kiếm
- [Quy tắc cho agent](docs/instructions/agent-verdicts.md) (tiếng Anh): chỉ dẫn nạp vào MCP client

Bản tiếng Anh là bản chuẩn: [README.md](README.md), [docs/](docs/),
[CONTRIBUTING.md](CONTRIBUTING.md).

## Trạng thái

Phiên bản hiện tại là **0.1.0-beta**: phần mềm còn sớm, chưa có bản phát hành
được tag. JSON API, tool MCP và schema database còn có thể thay đổi; migration
sẽ nâng cấp dữ liệu sẵn có.

## Giấy phép

[Apache License 2.0](LICENSE)

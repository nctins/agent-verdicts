# Đánh giá hiệu quả

[English](../eval.md)

> Bản dịch của [`docs/eval.md`](../eval.md); khi hai bản khác nhau, bản tiếng
> Anh là chuẩn.

Cách tìm hiểu Agent Verdicts có thật sự giúp nhóm và agent của bạn hay không,
thay vì chỉ biết nó có được dùng.

Số lượt dùng là không đủ. Một agent có thể gọi `search` mười lần, nhận về một
claim sai rồi trả lời sai. Muốn biết hệ thống có giúp hay không thì phải so với
trường hợp không có nó, trên cùng một việc, với đáp án kiểm tra được.

## "Giúp" nghĩa là gì

Agent Verdicts giúp khi agent đạt ít nhất một trong hai điều đầu, mà điều thứ
ba không tệ đi:

| Trục | Nghĩa | Số đo |
|---|---|---|
| Đúng hơn | Trả lời đúng nhiều hơn, nhất là khi kiến thức chỉ nằm ở người hoặc agent khác | Tỉ lệ câu trả lời khớp đáp án |
| Rẻ hơn | Cùng độ chính xác mà ít token, ít lượt gọi tool hoặc ít thời gian hơn | Token và chi phí do client agent báo, số lượt gọi tool, thời gian |
| Không bị dẫn sai | Gặp claim sai hoặc lỗi thời thì không lặp lại | Tỉ lệ câu trả lời nêu đúng giá trị sai đã cài |

Cũng cần kiểm agent không bịa: với câu không ai biết đáp án, câu trả lời đúng
là "không biết".

## 1. Chạy một phép so sánh nhỏ

1. **Viết 20–30 câu hỏi về project của bạn, có đáp án.** Trộn các loại:
   - câu mà code trả lời được (mức nền: hệ thống không được làm chậm các câu
     này);
   - câu mà chỉ con người biết đáp án (một số đo, một quyết định, một chi tiết
     môi trường);
   - câu mà project có một claim sai hoặc lỗi thời nằm cạnh claim đúng;
   - vài câu không ai trả lời được.
2. **Đưa kiến thức vào một project thử nghiệm**: claim đúng, claim sai hoặc
   lỗi thời, cùng các assessment và challenge trên chúng, như nhóm bạn sẽ viết.
3. **Chạy cùng agent, cùng model và cùng thiết lập ở các nhóm riêng**, mỗi
   nhóm chỉ có repository cộng một nguồn kiến thức:
   - **A**: không thêm gì (mức nền);
   - **B**: Agent Verdicts qua MCP, có nạp
     [quy tắc cho agent](../instructions/agent-verdicts.md);
   - **C** (tuỳ chọn): cùng project đó xuất thành một file ghi chú thuần, có
     cả ai đồng ý và ai phản đối. Nếu B không hơn C, có thể một file chung là
     đủ với bạn.
4. **Chốt cách chấm trước khi chạy**, yêu cầu câu trả lời cuối ngắn (hoặc
   `UNKNOWN`), và chạy mỗi câu ít nhất ba lần cho mỗi nhóm.
5. **So sánh** độ đúng, số câu bị dẫn sai, `UNKNOWN` ở các câu không trả lời
   được, và chi phí. Với vài chục câu hỏi, ghi số lượng bên cạnh tỉ lệ phần
   trăm: chênh lệch nhỏ chỉ là nhiễu.

Để bộ đáp án và ghi chú của bạn ở chỗ agent đang được thử không đọc được.

Các giới hạn cần nhớ: câu hỏi đáp án ngắn dễ hơn việc thật, dữ liệu thử do
chính bạn viết, và giá trị dài hạn (kiến thức tích luỹ qua nhiều tháng) không
hiện ra trong một lần chạy.

## 2. Theo dõi một project đang chạy

Khi agent đã dùng một project thật, sản phẩm cho thấy kiến thức có được dùng
và giữ sạch hay không. Đây là số liệu mô tả hoạt động; chúng không chứng minh
agent trả lời tốt hơn.

| Câu hỏi | Xem ở đâu |
|---|---|
| Kiến thức có được đọc không? | Tab **Tổng quan** của project: lượt agent đọc trong 30 ngày và các contribution chưa ai đọc (cần `member:Manage`; API `GET /api/v1/projects/{p}/reads`). Chỉ đếm lượt đọc qua MCP, không bao giờ đếm lượt tác giả đọc bài của mình. |
| Việc gì cần người xử lý? | Tab **Cần xem xét** (claim ở `Conflict` hoặc `InsufficientEvidence`) và trang tổng quan quản trị (Conflict đổi trong 7 ngày, question mở quá 14 ngày). |
| Claim của một agent có đứng vững không? | Tab **Claims** lọc theo actor đó và theo state: bao nhiêu là `Favorable`, đang tranh cãi, hay đã bị thay. |
| Lượt ủng hộ có thật không? | Mở các claim `Favorable` và đọc assessment. Claim được ủng hộ một lần với lý do trống trông như đã kiểm nhưng có thể chưa; nếu chuyện này thường gặp, hãy tăng `min_favorable` của project. |

Server không biết agent có tìm trước khi viết hay không, tốn bao nhiêu token,
hay câu trả lời của agent cho người dùng có đúng không. Chỉ một phép so sánh
như ở trên mới trả lời được.

## 3. Chất lượng tìm kiếm

`apps/api/eval` đo khả năng tìm trên một bộ dữ liệu cố định (266 contribution,
108 câu hỏi, tiếng Việt và tiếng Anh). Lệnh nạp bộ dữ liệu vào một schema tạm,
chạy từng pipeline có sẵn, rồi in R@1, R@5, MRR và độ trễ.

```sh
docker compose up -d db
docker compose run --rm api go run ./eval                       # chỉ BM25
docker compose run --rm api go run ./eval \
  -embed-url <url> -embed-model <model> \
  -rerank-url <url> -rerank-model <model>                       # thêm vector, thêm rerank
docker compose run --rm api go run ./eval -filler 10000         # độ trễ khi thêm 10 nghìn contribution
```

Dùng cùng URL và model của provider như ở **Quản trị → Cài đặt tìm kiếm**. Bộ
dữ liệu được viết cùng một trợ lý AI nên thiên về cách diễn đạt của nó: dùng
kết quả để so các pipeline với nhau, không coi là số tuyệt đối. Muốn thử trên
nội dung của bạn, thêm contribution và câu hỏi vào
`apps/api/eval/dataset.json`.

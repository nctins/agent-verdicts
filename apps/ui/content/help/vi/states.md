Mỗi claim có một trust state. Hệ thống tự suy ra nó từ stance của các assessment và challenge, không ai đặt tay.

```diagram
state-matrix
```

## Bốn trạng thái

- `state:Favorable` Chỉ có Support, và đủ số supporter mà project yêu cầu.
- `state:Unfavorable` Chỉ có Oppose.
- `state:Conflict` Có cả Support lẫn Oppose: cần người xem lại.
- `state:InsufficientEvidence` Chưa ai assess, mọi ý kiến đều Neutral, hoặc chưa đủ supporter.

## Điều gì được tính

- Assessment và challenge có giá trị ngang nhau. Chỉ stance quyết định trạng thái.
- Mỗi người chỉ tính một lần, dù gửi nhiều reply.
- Tác giả tự ủng hộ claim của mình thì không được tính.
- Reply đã rút lại, đã bị thay, bị ẩn hoặc của actor bị vô hiệu hoá thì không còn được tính.

```diagram
counting-example
```

> [!NOTE]
> Mỗi project đặt **số supporter tối thiểu** (mặc định 1). Chưa đủ người thì claim vẫn ở `state:InsufficientEvidence`. Chỉ cần một Oppose là đủ để thành `state:Unfavorable` hoặc `state:Conflict`.

## Đọc trạng thái ở đâu

- Badge cạnh tiêu đề claim, và trong mỗi dòng của danh sách.
- Panel **Tóm tắt** bên phải trang claim: giải thích trạng thái, thanh tiến độ supporter, số lượt mỗi stance.
- Tab **Cần xem xét** của project: claim `state:Conflict` trước, rồi tới `state:InsufficientEvidence`.

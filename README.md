# glops

CLI GitLab cho terminal, viết bằng Go, chạy trên macOS và Linux.

Lưu endpoint, username và personal access token một lần. Các lệnh sau dùng lại cấu hình đó:

- `merge` — merge merge request từ link, nếu tài khoản có quyền
- `tag` — tạo tag từ một nhánh hoặc tag để chạy pipeline
- `retry` — chạy lại pipeline từ link

## Cài trên macOS

Cần Go 1.22 trở lên.

```bash
git clone https://github.com/du0ngtrunghieu/my-history.git
cd my-history
go build -o glops .
mkdir -p "$HOME/bin"
cp glops "$HOME/bin/glops"
```

Thêm `$HOME/bin` vào `PATH` nếu chưa có. Có thể copy vào `/usr/local/bin/glops` thay cho thư mục home.

Build từ máy khác cho Mac:

```bash
make build-macos
# dist/glops-darwin-arm64   Apple Silicon
# dist/glops-darwin-amd64   Intel
```

## Cấu hình

Token cần scope **api**. Tạo tại GitLab: Preferences → Access Tokens.

```bash
glops setup
```

Lệnh hỏi endpoint, username và token. Token không hiện khi gõ trên terminal.

Endpoint tự host:

```bash
glops setup --endpoint https://gitlab.company.com --username hieu
```

GitLab gắn dưới một đường dẫn:

```bash
glops setup --endpoint https://git.company.com/gitlab --username hieu
```

Chứng chỉ tự ký:

```bash
glops setup --endpoint https://gitlab.company.com --insecure
```

Đọc token từ stdin để khỏi lưu trong lịch sử shell:

```bash
glops setup --endpoint https://gitlab.company.com --username hieu --token-stdin
```

File cấu hình chỉ user hiện tại đọc được:

- macOS: `~/Library/Application Support/glops/config.json`
- Linux: `~/.config/glops/config.json`

```bash
glops config   # xem cấu hình, token được che
glops whoami   # gọi API để kiểm tra token
```

## Merge

```bash
glops merge https://gitlab.company.com/team/app/-/merge_requests/42
```

- Có quyền và merge request đang merge được: merge ngay.
- Pipeline còn chạy và project bắt pipeline phải thành công: hẹn merge khi pipeline thành công.
- Không có quyền, đang draft, conflict, thiếu approval, hoặc pipeline đã thất bại: dừng và nói lý do.
- Đã merge từ trước: báo đã merge.

```bash
glops merge <link> --remove-source-branch
```

## Tạo tag để chạy pipeline

Trong repo đã có remote GitLab trùng endpoint đã lưu:

```bash
glops tag --name v1.2.3 --ref main --yes
```

`ref` là nhánh hoặc tag nguồn. Có thể truyền project bằng đường dẫn hoặc URL:

```bash
glops tag v1.2.3 release/1.2 --project team/app --yes
glops tag --name v1.2.3 --ref main --project https://gitlab.company.com/team/app --yes
```

Bỏ `--yes` khi chạy trên terminal thì lệnh hỏi tên tag, nguồn, và xác nhận trước khi tạo.

Pipeline chỉ xuất hiện nếu `.gitlab-ci.yml` có rule chạy khi có tag. Tag được bảo vệ thì tài khoản cần quyền Maintainer.

## Retry pipeline

```bash
glops retry https://gitlab.company.com/team/app/-/pipelines/900
```

GitLab chỉ chạy lại job thất bại hoặc đã hủy. Pipeline đã thành công sẽ bị từ chối.

## Quyền

Developer trở lên thường merge được, tạo tag và retry pipeline. Nhánh hoặc tag được protect có thể cần Maintainer. Lệnh không gửi token sang host khác với endpoint đã lưu.

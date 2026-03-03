# onedrive-dl

一个cli工具 支持从onedrive分享链接下载内容 支持个人/企业/sharepoint，支持带密码的分享

## 功能

- 支持SharePoint/OneDrive Business/Personal分享链接
- 支持带密码的分享
- 断点续传
- 递归下载子文件夹

## 安装

```bash
go install github.com/IcyBleu17/onedrive-dl@latest
```

或从源码编译：

```bash
go build -o onedrive-dl .
```

## 用法

```
onedrive-dl [选项] <分享链接>

选项：
  -o, --output <目录>     下载目录（默认当前目录）
  -p, --password <密码>   分享密码
  -j, --jobs <N>          并发数（默认为3）
  -l, --list              仅列出文件
  --verbose               详细日志
```

```bash
# 列出文件
onedrive-dl -l "https://xxx.sharepoint.com/:f:/g/personal/..."

# 下载到指定目录
onedrive-dl -o ./out -j 5 "https://xxx.sharepoint.com/:f:/g/personal/..."

# 带密码
onedrive-dl -o ./out -p 密码 "https://xxx.sharepoint.com/:f:/g/personal/..."
```

## 作为库使用

`od` 和 `dl` 包可以独立引入。

```go
import (
    "github.com/IcyBleu17/onedrive-dl/od"
    "github.com/IcyBleu17/onedrive-dl/dl"
)

client := od.NewClient(false)
shareType, finalURL, body, _ := od.Detect(client, "https://1drv.ms/f/s!xxx")

var info *od.ShareInfo
switch shareType {
case od.TypeSP:
    info, _ = (&od.SPHandler{Client: client}).ListFiles(finalURL, body)
case od.TypePersonal:
    info, _ = (&od.PersonalHandler{Client: client}).ListFiles(finalURL, body)
}

d := dl.New("./output", 3, false, client.HTTP.GetClient())
d.Start(info)
```

## 项目结构

```
├── main.go           入口
├── progresser.go     进度条
├── od/               OneDrive/SharePoint API
└── dl/               下载器
```

## License

MIT

## 已知问题

下载剩余时间的算法不准确，仅供参考

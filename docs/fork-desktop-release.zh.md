# Fork 桌面端发布

本仓库的 SSO fork 使用独立的 GitHub Actions 工作流构建桌面安装包。发布目标固定为 `coder-zkl1988/multica`，不会向官方 `multica-ai/multica` 上传资源。

## macOS 签名要求

macOS 构建固定校验以下个人签名：

- 证书：`Developer ID Application: kelong zong (WCYBD629RJ)`
- Team ID：`WCYBD629RJ`
- Hardened Runtime 与 entitlements：沿用 `apps/desktop/electron-builder.yml`
- 公证：Apple `notarytool`

在 fork 的 GitHub 仓库进入 **Settings → Secrets and variables → Actions**，配置以下 Repository secrets：

| Secret | 内容 |
| --- | --- |
| `MACOS_CERTIFICATE_P12_BASE64` | 仅包含上述个人 Developer ID Application 证书及私钥的 `.p12` 文件，经 Base64 编码后的完整内容 |
| `MACOS_CERTIFICATE_PASSWORD` | 导出 `.p12` 时设置的密码 |
| `APPLE_ID` | 个人 Apple Developer 账号邮箱 |
| `APPLE_APP_SPECIFIC_PASSWORD` | 为公证创建的 Apple ID app-specific password |

不要导出包含公司证书或其他个人身份的整个登录钥匙串。应在 macOS“钥匙串访问”中只选择个人 `Developer ID Application: kelong zong (WCYBD629RJ)` 及其私钥，导出为单独的 `.p12`。

可以使用以下命令生成要写入 GitHub Secret 的 Base64 文本，命令不会修改 `.p12`：

```bash
base64 -i /path/to/personal-developer-id.p12 | pbcopy
```

## 发布

1. 打开 fork 仓库的 **Actions → Fork Desktop Release**。
2. 选择 **Run workflow**。
3. 输入不带 `v` 的版本号，例如 `0.4.16-sso.1`。
4. 保持 prerelease 开启完成首次验证。

工作流会构建：

- macOS arm64 与 x64：个人 Developer ID 签名并完成 Apple 公证。
- Windows x64 与 arm64：当前为未签名安装包。
- Linux x64 与 arm64：AppImage、deb 与 rpm。

发布使用 `desktop-v<version>` 标签，例如 `desktop-v0.4.16-sso.1`。该标签不会触发官方 `v*` CLI、Docker 和 Helm 发布工作流。安装包内部版本仍是输入的标准 SemVer，自动更新元数据指向 fork 仓库。

macOS job 在上传前会验证：

- `.app` 深度签名有效；
- Authority 与 Team ID 必须属于个人证书；
- Gatekeeper 接受 `.app` 与 `.dmg`；
- `.app` 与 `.dmg` 均带有有效 notarization ticket。

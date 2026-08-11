# JumpOTP 中文指南

JumpOTP 用于你有权访问的交互式 SSH／JumpServer 会话。它在严格匹配到 TOTP
提示后，从当前已经登录或解锁的 Bitwarden Password Manager CLI 获取一次性
验证码，并且每条连接最多自动提交一次。

## 安装

```sh
npm install -g jumpotp
jumpotp version
```

v0.1 支持 macOS／Linux 的 arm64 与 x64，npm 启动器要求 Node.js 22.14.0
或更高版本。npm 安装阶段不会下载或编译二进制。

可选平台依赖被忽略时：

```sh
npm install -g jumpotp --include=optional
```

GitHub Release 是备用渠道。手动下载后必须使用同一 Release 中的
`SHA256SUMS` 校验。

更新与卸载：

```sh
npm install -g jumpotp@latest
npm uninstall -g jumpotp
```

## 使用前提

- SSH 主机、用户、端口、密钥、ProxyJump、ControlMaster 全部由
  `~/.ssh/config` 中的 alias 管理。
- `bw` 已在当前环境登录或解锁。
- `sshm` 只是可选 launcher。
- 只有 `workspace` 需要 tmux。

`bw get totp` 读取的是 Bitwarden Password Manager 条目中集成的验证器
数据，不是独立 Bitwarden Authenticator 应用的本地数据。JumpOTP 不登录、
不解锁、不管理 `BW_SESSION`，也不修改自建 Bitwarden 服务地址。

## 常用命令

```sh
jumpotp config init
jumpotp config validate
jumpotp doctor production
jumpotp connect production/app-01
jumpotp workspace production
jumpotp status production
jumpotp stop production
```

`connect` 是一次性直连。`workspace` 使用 JumpOTP 专用 tmux server：
关闭终端或 detach 后会话仍在，再次运行同一命令即可 reattach。`stop`
只停止指定 profile，不关闭 OpenSSH ControlMaster。

Bitwarden 取码失败时，默认会在对应终端说明原因、恢复正常回显，并让你直接
输入可见数字。使用 `fallback: fail` 可改为失败退出；使用 `--manual`
可在本次调用中完全跳过 Bitwarden。

自动直连开始前，JumpOTP 会运行一次严格限定的 `bw status`，作为尽力而为的
就绪检查。它只接受表示保险库已解锁的有效、有界 JSON，不传入条目引用，也不
请求验证码；就绪检查和提示出现后的取码均使用固定的 20 秒截止时间。检查失败
只输出一条脱敏警告，然后继续，让既有的 `prompt` 或 `fail` 策略决定后续行为。
`--manual` 会同时跳过就绪检查和自动取码。

就绪检查或提示出现后的取码失败时，JumpOTP 只会在已有的脱敏、分阶段原因后
追加一个有界的低精度耗时。小于一秒显示为 `<1s`，更长的失败按最接近的整秒
显示，最高不超过 20 秒的 provider 截止时间。成功操作仍保持静默；诊断不会
包含条目引用、命令输出、会话数据或 `bw` wrapper 内部步骤。

新建自动 workspace 时，就绪检查发生在创建目标之前；经验证的活动 broker
重连会跳过检查。broker 状态存在歧义时也会跳过检查，并由原有的严格生命周期
校验给出权威错误。

workspace provider 失败通过现有 broker 协议中的可选有界数值传递同一耗时桶。
客户端不会把 broker 消息当作诊断；旧的 version-one 对端在缺少或忽略该字段
时仍保持兼容。

健康检查默认关闭。启用后只复用 `ssh -O check` 已确认的 ControlMaster，
通过 `BatchMode=yes` 在独立健康窗口轮换运行，不会往交互式 SSH pane
注入命令，也不会触发新的 MFA。

## 安全边界

自动读取密码管理器中的 TOTP 会降低第二因素隔离度。JumpOTP 假设同一操作
系统用户下的进程可信，并继续依赖 OpenSSH 主机密钥验证。它使用严格 prompt
匹配、每连接只自动提交一次、临时 Unix socket、独立 tmux server，并且不把
OTP 放入 argv、环境变量、日志、文件、剪贴板或 tmux buffer。

远端服务仍可能自行回显已提交数字，这不在 JumpOTP 的控制范围内。JumpOTP
没有遥测，也不会自动检查更新。

本项目与 Bitwarden、JumpServer、SSHM、OpenSSH、tmux 均无隶属或背书关系。

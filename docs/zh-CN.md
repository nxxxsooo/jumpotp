# JumpOTP 中文指南

JumpOTP 用于你有权访问的交互式 SSH／JumpServer 会话。它在严格匹配到 TOTP
提示后，从当前已经登录或解锁的 Bitwarden Password Manager CLI 获取一次性
验证码并自动提交——每次连接尝试最多自动提交两次，且从不重复同一验证码，
之后转入手动兜底。

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

> **破坏性变更：** workspace 的每个 target 窗口不再提供交互式远程 shell，
> 只负责完成 MFA 并持有 OpenSSH ControlMaster 连接，不是用来输入远程命令
> 的地方。target 就绪后，请在普通终端里用 `ssh <alias>`（或
> `sshm <alias>`）执行交互操作——它会复用 target 窗口持有的同一个
> ControlMaster，因此不会触发新的 MFA。从旧版本迁移不需要改动配置，只需
> 停止在 target 窗口里直接输入命令，改用 `ssh <alias>`。

每个 target 窗口都是一个 sessionless master：target wrapper 以
`<launcher> -N -o ServerAliveInterval=60 -o ServerAliveCountMax=3
-o ControlPersist=no <alias>` 发起连接。`-N` 不申请远程 shell、命令或
session channel，因此连接只完成 MFA 并持有 ControlMaster，堡垒机的交互
空闲回收机制找不到可以回收的会话。`ControlPersist=no` 只作用于 wrapper
自己发起的这次调用——即便用户自己的 `~/.ssh/config` 对裸 `ssh` 调用启用了
ControlPersist，master 的生命周期依然与窗口绑定。`ControlMaster`／
`ControlPath` 的选择仍然完全由用户的 `~/.ssh/config` 决定，和之前一样。
如果 ControlPath 上已经有一个外部 ControlMaster，sessionless 客户端会直接
经由它连接、无需 MFA；只有在那个外部 master 退出后，之后的连接尝试才会
成为新的 master。

每个 target 窗口的 wrapper 会监督它启动的 launcher 子进程。当子进程因
非人工停止的原因退出——即不是 `stop`、关闭窗口或中断——wrapper 会按指数
退避重连：从 5 秒开始倍增，上限 300 秒，并带有限抖动，每次重试都会重置
该次连接尝试的自动提交状态。发起连接前，wrapper 要求先确认存在可复用的
ControlMaster（`ssh -O check`）或经过验证、可达的 profile broker；两者都
不满足时，wrapper 每 10 秒重新检查一次，期间完全不会发起 SSH 连接，因此
无人值守的 workspace 不会产生失败的 MFA 尝试或多余的堡垒机连接噪音。窗口
在整个过程中都会保留；下一次 `workspace` 调用带来的 broker 通常足以让
完全断开的 target 自行恢复，不需要手工修复窗口。

`jumpotp status` 会把每个 target 报告为 `running`（窗口存活且
ControlMaster 已确认）、`connecting`（窗口存活、正在按上面的机制重连或
等待条件，ControlMaster 尚未确认）、`stopped`（窗口不存在）或 `failed`
（pane 状态异常），并附带 session、broker 与健康检查状态；这一切都不会
读取 pane 内容。

使用同一个 Bitwarden 条目的 target 会在一次性、仅当前用户可访问的 broker
中分组：broker 只在一次 workspace 调用存续期间存在，为同组内几乎同时就绪
的 target 只调用一次 Bitwarden，并把结果分发给每个已就绪的 wrapper。broker
触发一次分组取码前，会先检查当前 30 秒 TOTP 窗口：如果剩余不足 8 秒，就
等到下一个窗口边界再向 Bitwarden 取码，确保每个下发的验证码都留有足够的
提交与重试时间。

Bitwarden 取码失败时，默认会在对应终端说明原因、恢复正常回显，并让你直接
输入可见数字。使用 `fallback: fail` 可改为失败退出；使用 `--manual`
可在本次调用中完全跳过 Bitwarden。若自动提交的验证码被拒绝且再次出现同一
提示，JumpOTP 会等到下一个 TOTP 窗口边界、取一个新验证码后再自动提交一次
——同一验证码不会被重复提交。第二次被拒绝后，或者取不到新验证码时，
JumpOTP 不再自动提交，转入上述的手动或失败兜底；每一次 supervised 重连
都是一次新的连接尝试，各自拥有独立的两次自动提交额度。

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
匹配、每次连接尝试最多自动提交两次且从不重复同一验证码、临时 Unix socket、
独立 tmux server，并且不把 OTP 放入 argv、环境变量、日志、文件、剪贴板或
tmux buffer；重连前必须先确认存在可复用的 master 或经验证的 broker，因此
无人值守的 workspace 不会产生失败的 MFA 尝试。

远端服务仍可能自行回显已提交数字，这不在 JumpOTP 的控制范围内。JumpOTP
没有遥测，也不会自动检查更新。

本项目与 Bitwarden、JumpServer、SSHM、OpenSSH、tmux 均无隶属或背书关系。

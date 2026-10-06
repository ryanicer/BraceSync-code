# scripts/db/seed/seed.sql 的 bytea 列规矩（T586）

`seed.sql` 被 `make seed`、`scripts/dev/init-db.sh` 与集成测试台共用。**新环境只要跑一次装载，
文件里写成什么形状，库里就长什么形状**，所以下面这条规矩只能在文本里守，不能在运行期补。

## 一句话规矩

bytea 密钥列只许两种形状：

| 形状 | 字面量 | 含义 | 谁在用 |
|---|---|---|---|
| 0 字节 | `''::bytea` | 这一列没有值 | 医生/技师/患者的 `phone_enc`（共 11 处） |
| ≥ 28 字节的真密文 | `'\x…'::bytea` | AES-256-GCM 可解 | 设备 `device_secret_enc`（共 5 处） |

**🔴 禁止 1～27 字节的密文形状。** AES-256-GCM 的密文格是 `nonce(12B) ‖ 密文 ‖ tag(16B)`，
最短合法值 28 字节。1～27 字节全都落在合法长度之外，其中短于 12 字节的（历史上那 16 处
`'\x00'::bytea` 只有 1 字节）连 nonce 都凑不齐，在任何密钥版本下都解不出来，
装载出来的设备会「看着在线、实则永不可解」，
被反复误判成凭证层或网关 502 缺陷（T573 的现网 003/004/005 三行即此形状）。

## 为什么手机号列写空而不是写假密文

`services/user-service/internal/phone` 的 `View()` 对 0 字节判 `PhoneStateAbsent`（没有号码），
对「有字节但解不出来」判 `Unreadable`（掩码 `***`）。写 `''::bytea` 是**如实表达「种子数据里没有号码」**：
不动 `phone_hash`、不需要迁移、也不会造出「哈希与密文不一致」的假号码。

## 设备列的夹具密文怎么复算

5 行设备的 `device_secret_enc` 由三颗公开种子串确定，任何一台机器都能重算出同一串字节：

- 密钥 = `sha256("bracesync:T586:seed-fixture-key")` 的 32 字节摘要（hex 形 64 字符）
- 明文 = `sha256("bracesync:T586:dev-fixture:<完整 device_id>")` 的 **hex 字符串本身**（64 个 ASCII 字节）
- nonce = `sha256("bracesync:T586:nonce:<完整 device_id>")` 的前 12 字节
- 入库字节 = `nonce ‖ 密文 ‖ tag`，共 92 字节，写成 `'\x<hex184>'::bytea`

守卫测试逐字节复算并比对（`services/device-service/internal/seed`）：

```bash
go test -v -run TestT586 ./services/device-service/internal/seed/...
go test -v -tags=integration -run TestT586 ./services/device-service/internal/seed/...
```

前者守文本面（占位清零、密文长度、逐台解密与重加密等值、注牙负对照）；
后者起一套干净的 PG15 容器，按序跑 `migrations` 后**原样装载本文件**，
再在库侧断言「不可解设备行 = 0」，即 T586 判据 4（新生环境复现证伪）。

## 这把夹具钥不是生产密钥

它明文写在测试代码里，任何人都能解出这 5 台设备的秘密——**这正是它可以进仓库的原因**：
它们是模拟设备，不是真设备。真设备一律走「注册 + 配网」通路，由 device-service 用
环境变量 `DEVICE_SECRET_ENC_KEY` 注入的 32 字节密钥写入 `device_secret_enc`；
那把钥**永远不许出现在本文件里**。

## 现网历史数据

staging 上已装载出来的 003/004/005 三行（各 1 字节）不在本文件的覆盖范围内：
本文件只管「新生环境不再长出这种行」。仓外历史行的订正属于**只读之外的写动作**，
须 PM/Boss 批准后才做，见 T586 卡内登记。

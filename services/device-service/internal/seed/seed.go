// Package seed 是 T586「seed 脚本占位密钥数据腿」的守卫包，这里只放测试。
//
// seed.sql 装载出来的数据形状由两道门禁守住。单元层（本包无构建标签的 _test.go）
// 不许仓内文本再出现「短于 AES-GCM nonce」的占位密文，那种字节任何密钥都解不出来；
// 集成层（-tags=integration）在干净库里跑一次真实装载，断言不再长出「看似在线、
// 实则永不可解」的设备行，也就是 T586 判据 4。
//
// 夹具密钥是文档化的派生值（见 fixtureKeySeed），只服务 seed、演示与测试环境，
// 不是生产密钥。真设备的 device_secret_enc 必须由服务用 DEVICE_SECRET_ENC_KEY
// 在注册加配网通路上写入。复算路径见 scripts/db/seed/README.md。
package seed

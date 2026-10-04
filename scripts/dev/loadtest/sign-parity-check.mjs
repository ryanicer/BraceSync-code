// BraceSync k6 压测 — 签名口径对拍尺（T566 格 3）
// 为什么需要它：k6 脚本不在任何 CI 面上（本仓 Actions 只跑 Go），改坏了没人响。
// 这把尺把入库那份 device-sign.js 原样喂给 node（只把 'k6/crypto' 换成 node:crypto 的 data: 垫片），
// 对固定入参算出签名，与 Go 侧 cmd/sign_test.go 的 TestSignParityWithK6Helper 钉的同一颗 hex 互校。
// 用法：node scripts/dev/loadtest/sign-parity-check.mjs   （回 0 = 两侧口径一致；回 1 = 有漂移）
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createHash, createHmac } from "node:crypto";

const EXPECTED = "8cc7f633c16a60ffceddf9dd4886fcf95fb972ee4ceb21c819420e10ed85c46a";

const here = path.dirname(fileURLToPath(import.meta.url));
const src = fs.readFileSync(path.join(here, "device-sign.js"), "utf8");

const shim =
  "data:text/javascript;base64," +
  Buffer.from(
    `import { createHash, createHmac } from "node:crypto";
export default {
  sha256: (d) => createHash("sha256").update(d, "utf8").digest("hex"),
  hmac: (a, k, d) => createHmac(a, k).update(d, "utf8").digest("hex"),
};
`,
    "utf8"
  ).toString("base64");

const rewritten = src.replace(`from 'k6/crypto'`, `from '${shim}'`);
if (rewritten === src) {
  console.log("RESULT DIRTY 垫片替换未命中（device-sign.js 的 import 行形状变了）");
  process.exit(1);
}

globalThis.__ENV = {};
const m = await import("data:text/javascript;base64," + Buffer.from(rewritten, "utf8").toString("base64"));

const deviceID = "DEV_PARITY_001";
const nonce = "0123456789abcdef0123456789abcdef";
const tsUnix = 1723028400;
const body = `{"device_id":"${deviceID}","timestamp":${tsUnix},"points":[1200],"battery":85,"firmware":"v1.2.0"}`;

const sig = m.signDeviceRequest("POST", m.PATH_SINGLE, deviceID, nonce, body, tsUnix);
const nonceLen = m.randomNonceHex().length;
const pointsLen = m.normalPoints().length;

console.log(`JS PATH_SINGLE=${m.PATH_SINGLE} PATH_BATCH=${m.PATH_BATCH}`);
console.log(`JS NONCE_LEN=${nonceLen} POINTS_LEN=${pointsLen} SIG_LEN=${sig.length}`);
console.log(`JS PARITY_SIG=${sig}`);

const ok =
  sig === EXPECTED &&
  nonceLen === 32 &&
  pointsLen === 20 &&
  sig.length === 64 &&
  m.PATH_SINGLE === "/api/v1/device/records" &&
  m.PATH_BATCH === "/api/v1/device/records/batch";
console.log("RESULT " + (ok ? "CLEAN" : "DIRTY"));
process.exit(ok ? 0 : 1);

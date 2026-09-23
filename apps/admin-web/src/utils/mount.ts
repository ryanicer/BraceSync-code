/**
 * 后台挂载点常量（T336）。
 *
 * staging / 生产都由 nginx 把 admin-web 挂在 /admin/ 下（scripts/deploy/nginx.conf 的
 * `location /admin/` + alias）。构建 base 必须与这个挂载点同值：一旦 SPA 按根路径构建，
 * 部署后路由就不认识 /admin/patients 这类深链（匹配不到 ⇒ 落 catch-all ⇒ 被弹回首页），
 * 子路由刷新与登录后回原页同时失效。
 *
 * vite.config.ts 的 base、router 的 history base、401 跳转三处都取这里，
 * 三者与 nginx.conf 的对齐关系由 test/base-mount-contract.spec.ts 把守。
 */
export const ADMIN_BASE = '/admin/'
